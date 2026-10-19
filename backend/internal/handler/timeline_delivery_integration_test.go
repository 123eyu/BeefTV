package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/editing"
)

func TestDurableTimelineDeliveryUsesRealGoWorkerAndCanonicalSubtitleLines(t *testing.T) {
	env := newAssistantTestEnv(t, nil)
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	bin, err := editing.ResolveFFmpegBinary()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "source.mp4")
	output, err := exec.Command(bin, "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=320x240:r=24:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", source).CombinedOutput()
	if err != nil {
		t.Fatalf("source media: %v %s", err, output)
	}
	file, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	stat, _ := file.Stat()
	resource, err := env.service.UploadLocalResourceFile(owner.ID, "source.mp4", stat.Size(), "video", 320, 240, 1000, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	asset, _ := json.Marshal(map[string]any{"id": "source-asset", "title": "Source", "kind": "video", "resourceId": resource.ID, "coverUrl": "", "tags": []string{},
		"data": map[string]any{"storageKey": "resource:" + resource.ID, "width": 320, "height": 240, "bytes": resource.Size, "mimeType": resource.MimeType}})
	if _, err := env.service.UpsertUserAsset(owner.ID, asset); err != nil {
		t.Fatal(err)
	}
	current, err := env.service.UserCanvasProject(owner.ID, env.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var existing struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(current, &existing); err != nil {
		t.Fatal(err)
	}
	doc, _ := json.Marshal(map[string]any{"id": env.canvasID, "title": "Delivery test", "revision": existing.Revision,
		"nodes": []any{map[string]any{"id": "source", "type": "video", "title": "Source", "metadata": map[string]any{"assetId": "source-asset", "storageKey": "resource:" + resource.ID}}}, "connections": []any{}})
	summary, err := env.service.UpsertUserCanvasProject(owner.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	const turnID = "ccddee1122334455"
	env.beginTurn(t, turnID, assistantTurnInput(nil, nil))
	env.service.StartWorker()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = env.service.StopWorker(ctx)
	})
	backend := httptest.NewServer(env.router)
	defer backend.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"root": root, "directory": directory, "backend": backend.URL + "/api", "canvasId": env.canvasID,
		"turnId": turnID, "hostToken": assistantTestHostToken, "revision": summary.Revision})
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "agent-host/test-support/timeline-delivery-go-probe.mjs"), configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("real delivery: %v\n%s", err, output)
	}
	var result struct {
		TaskID  string `json:"taskId"`
		NodeID  string `json:"nodeId"`
		Project struct {
			ResourceID string `json:"resourceId"`
		} `json:"project"`
	}
	raw, err := os.ReadFile(filepath.Join(directory, "delivery-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	tasks, err := env.service.Tasks(owner.ID, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("unexpected tasks: %v %v", tasks, err)
	}
	task, err := env.service.Task(owner.ID, result.TaskID)
	if err != nil || task.Status != "succeeded" || task.Type != "timeline_render" {
		t.Fatalf("render not complete: %v %v", task, err)
	}
	canvas, err := env.service.UserCanvasProject(owner.ID, env.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Timeline editing.Project `json:"timeline"`
	}
	if err := json.Unmarshal(canvas, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Timeline.Clips[1].Text != "First line\nSecond line" {
		t.Fatalf("subtitle lines not canonical: %q", saved.Timeline.Clips[1].Text)
	}
	plan, err := editing.Compile(saved.Timeline, nil, editing.Options{})
	if err != nil || !strings.Contains(plan.SubtitleSRT, "First line\nSecond line") {
		t.Fatalf("render text differs: %v", err)
	}
	_, body, err := env.service.OpenResource(owner.ID, result.Project.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	projectJSON := false
	for _, item := range z.File {
		stream, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		projectJSON = projectJSON || item.Name == "projects.json"
	}
	if !projectJSON || len(z.File) < 3 {
		t.Fatal("editable project or embedded media missing")
	}
}
