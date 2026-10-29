package app

import (
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"

	cw "infinite-canvas/backend/internal/canvasworkflow"
	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/mediatools"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/modelcatalog"
)

func workflowRevision(n int) int { return cw.NormalizeRevision(n) }

func (s *Service) CanvasWorkflowOperations() []cw.Operation {
	operations := cw.Operations()
	if snapshot, err := s.canvasWorkflowProviderConfig(); err == nil {
		upscaleChannels := []cw.FieldOption{}
		if _, err := s.upscaleConfig(""); err == nil {
			upscaleChannels = append(upscaleChannels, cw.FieldOption{Label: "官方算力", Value: "beefapi"})
		}
		textModels := []cw.FieldOption{}
		for _, model := range workflowDescribeModels(snapshot) {
			if model.Enabled && model.Capability == "text" && model.Protocol != "" {
				textModels = append(textModels, cw.FieldOption{Label: model.DisplayName, Value: model.ChannelID + "::" + model.Model})
			}
		}
		for i := range operations {
			for j := range operations[i].Fields {
				field := &operations[i].Fields[j]
				if strings.HasPrefix(operations[i].ID, "generate.") && field.Key == "modelOption" {
					raw, _ := json.Marshal(snapshot)
					for _, candidate := range modelcatalog.ParseLocalChannelModels(raw) {
						if candidate.Enabled && generation.UpscaleMediaType(candidate.Model) == "" && candidate.Capability == operations[i].OutputType && candidate.Protocol != "" {
							field.Options = append(field.Options, cw.FieldOption{Label: candidate.DisplayName + " · " + candidate.ChannelID, Value: candidate.ChannelID + "::" + candidate.Model, Capabilities: candidate.CapabilityConfig})
						}
					}
				}
				if strings.HasSuffix(operations[i].ID, ".ai-upscale") && field.Key == "channelId" {
					field.Options = upscaleChannels
				}
				if operations[i].ID == "image.describe" && field.Key == "modelOption" {
					field.Options = textModels
				}
			}
		}
	}
	path, err := mediatools.ResolveFFmpeg()
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(path)
		if err == nil && (!info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
			err = os.ErrPermission
		}
	}
	if err != nil {
		for i := range operations {
			if cw.IsMediaOperation(operations[i].ID) || operations[i].ID == "video.trim" || operations[i].ID == "video.merge" {
				operations[i].UnavailableReason = "本机媒体编码器不可用，请更新安装版后重试"
			}
		}
	}
	return operations
}
func (s *Service) workflowRevisionRow(user, id string, revision int) (model.CanvasWorkflow, error) {
	return s.workflowTemplates().RevisionRow(user, id, revision)
}
func workflowEditorDefinition(d cw.Definition, recipes workflowRecipes) cw.Definition {
	d = cw.Normalize(d)
	for i, n := range d.Nodes {
		if !strings.HasPrefix(n.OperationID, "generate.") {
			continue
		}
		recipe := recipes[n.ID]
		n.Config = map[string]any{}
		for _, key := range []string{"prompt", "size", "quality", "vquality", "videoSeconds", "seed"} {
			value, ok := recipe.Request.Input[key]
			if key != "prompt" {
				config, _ := recipe.Request.Input["config"].(map[string]any)
				value, ok = config[key]
				if (key == "seed" || key == "videoSeconds") && ok {
					if text, yes := value.(string); yes {
						number, err := strconv.ParseFloat(text, 64)
						if err != nil {
							ok = false
						} else {
							value = number
						}
					}
				}
			}
			if key == "prompt" && !ok {
				value = recipe.Request.Prompt
				ok = true
			}
			if ok {
				switch value.(type) {
				case string, float64, int, bool:
					n.Config[key] = value
				}
			}
		}
		config, _ := recipe.Request.Input["config"].(map[string]any)
		channel := recipe.ChannelID
		if channel == "@beefapi" {
			channel = "beefapi"
		}
		if channel != "" && stringValue(config["model"]) != "" {
			n.Config["modelOption"] = channel + "::" + stringValue(config["model"])
		}
		d.Nodes[i] = n
	}
	for i, edge := range d.Edges {
		bindings := []workflowBinding{}
		for _, binding := range recipes[edge.To].Bindings {
			if binding.NodeID == edge.From && !workflowPromptBinding(binding.Field) {
				bindings = append(bindings, binding)
			}
		}
		if len(bindings) == 1 {
			edge.ToPort, edge.Order = bindings[0].Field, bindings[0].Index
			d.Edges[i] = edge
		}
	}
	// Reference identity belongs to the private recipe. Reusable inputs and
	// processing outputs have no original media attached to their public slots.
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if d.PresentationVersion > 0 && (n.Role == "operation" || n.Role == "input" && n.MediaType != "text" && !n.UseDefault) {
			n.ResourceID = ""
		}
	}
	return d
}

// CanvasWorkflowTemplates is the template domain service used by the HTTP layer.
func (s *Service) CanvasWorkflowTemplates() *cw.Templates { return s.workflowTemplates() }

func (s *Service) workflowTemplates() *cw.Templates {
	return cw.NewTemplates(s.repo, workflowRecipeHost{s}, &s.canvasWorkflowMu)
}

// workflowRecipeHost compiles private recipes with the host's model selection,
// credentials and encryption. It moves into canvasworkflow with the compiler.
type workflowRecipeHost struct{ s *Service }

func (h workflowRecipeHost) Compile(user string, d cw.Definition, existing *model.CanvasWorkflow, seal bool) (cw.Compiled, error) {
	var recipes workflowRecipes
	var refs map[string]bool
	var e error
	if existing == nil {
		recipes, refs, e = h.s.compileCanvasWorkflow(user, d)
	} else {
		recipes, refs, e = h.s.compileCanvasWorkflow(user, d, *existing)
	}
	if e != nil {
		return cw.Compiled{}, e
	}
	// Presenting rewrites nodes in place; keep the caller's definition intact.
	presented := d
	presented.Nodes = append([]cw.Node(nil), d.Nodes...)
	presented.Edges = append([]cw.Edge(nil), d.Edges...)
	out := cw.Compiled{Presented: workflowEditorDefinition(presented, recipes), References: refs}
	if seal {
		raw, e := json.Marshal(recipes)
		if e != nil {
			return cw.Compiled{}, e
		}
		if out.Cipher, e = h.s.encryptSettingSecret(string(raw)); e != nil {
			return cw.Compiled{}, e
		}
	}
	return out, nil
}

func (h workflowRecipeHost) Present(d cw.Definition, recipeCipher string) (cw.Definition, error) {
	plain, e := h.s.decryptSettingSecret(recipeCipher)
	if e != nil {
		return d, e
	}
	var recipes workflowRecipes
	if e = json.Unmarshal([]byte(plain), &recipes); e != nil {
		return d, e
	}
	return workflowEditorDefinition(d, recipes), nil
}

func (h workflowRecipeHost) Operations() []cw.Operation { return h.s.CanvasWorkflowOperations() }
