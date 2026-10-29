package app

import (
	"context"
	"encoding/json"
	"strings"

	"infinite-canvas/backend/internal/beefapi"
	cw "infinite-canvas/backend/internal/canvasworkflow"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/modelcatalog"
)

const workflowDescribePrompt = "请根据参考图片反推一段适合用于 AI 生图的提示词。只输出提示词正文，不要解释。覆盖主体、构图、风格、光线、色彩、材质、镜头和氛围。"

// Text task execution is separate from canvas media-generation capabilities.
// An absent vision declaration is left to the configured model, as on the main canvas.
func workflowDescribeModels(snapshot map[string]any) []modelcatalog.LocalChannelModel {
	raw, _ := json.Marshal(snapshot)
	out := []modelcatalog.LocalChannelModel{}
	for _, candidate := range modelcatalog.ParseLocalChannelModels(raw) {
		if !candidate.Enabled || candidate.Capability != "text" || candidate.Protocol == "" {
			continue
		}
		if candidate.CapabilityConfig != nil {
			b, _ := json.Marshal(candidate.CapabilityConfig)
			profile, err := modelcatalog.DecodeModelCapabilityConfig(string(b))
			if err != nil || profile == nil || profile.Text == nil || profile.Text.References.MaxImages < 1 {
				continue
			}
		}
		out = append(out, candidate)
	}
	preferred := stringValue(snapshot["textModel"])
	for i, item := range out {
		if item.ChannelID+"::"+item.Model == preferred {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out
}

func (s *Service) workflowDescribeRequest(n cw.Node, resource, operation string) (CreateTaskRequest, error) {
	option, _ := n.Config["modelOption"].(string)
	snapshot, err := s.canvasWorkflowProviderConfig()
	if err != nil {
		return CreateTaskRequest{}, err
	}
	for _, candidate := range workflowDescribeModels(snapshot) {
		if !candidate.Enabled || candidate.Capability != "text" || candidate.ChannelID+"::"+candidate.Model != option || candidate.Protocol == "" {
			continue
		}
		config := map[string]any{"model": candidate.Model, "interfaceType": candidate.Protocol, "baseUrl": candidate.BaseURL, "capabilityConfig": candidate.CapabilityConfig}
		input := map[string]any{"mode": "text", "config": config}
		channelID := candidate.ChannelID
		if beefapi.IsManagedChannel(candidate.ChannelID, "", candidate.BaseURL) {
			channelID = "@beefapi"
		}
		if err := s.refreshCanvasWorkflowCredentials(input, channelID); err != nil {
			return CreateTaskRequest{}, err
		}
		prompt, _ := n.Config["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			prompt = workflowDescribePrompt
		}
		input["prompt"] = prompt
		if resource != "" {
			input["referenceImages"] = []any{map[string]any{"storageKey": "resource:" + resource}}
		}
		input["metadata"] = map[string]any{"clientOperationId": operation, "nodeId": n.ID, "source": "canvas-workflow"}
		return CreateTaskRequest{Type: "canvas_text", Prompt: prompt, Model: candidate.Model, Input: input}, nil
	}
	return CreateTaskRequest{}, workflowBad("请选择已启用、支持识图的文本模型")
}

func (s *Service) executeWorkflowUpscale(user, operation string, n cw.Node, resource string) (*model.Task, error) {
	request, err := cw.UpscaleRequest(n)
	if err != nil {
		return nil, err
	}
	request.ResourceID, request.ClientOperationID, request.NodeID = resource, operation, n.ID
	return s.CreateMediaUpscale(context.Background(), user, request)
}
