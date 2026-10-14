package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/outbound"
	"infinite-canvas/backend/internal/protocol"
)

func recoveryTestAdapter(t *testing.T) protocol.Adapter {
	t.Helper()
	body, err := os.ReadFile("../../../plugin-packages/newapi-video-generations-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := protocol.LoadManifest(body)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestVideoRecoveryLostReceiptNeverResubmits(t *testing.T) {
	allowLoopbackProviderTest(t)
	creates, polls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creates++
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		polls++
	}))
	defer server.Close()
	result, err := runProtocolAdapterTaskWithPolicy(context.Background(), canvasGenerationInput{Mode: "video", Prompt: "test", Config: providerConfig{BaseURL: server.URL, Model: "seedance-2.0-mini", InterfaceType: "newapi-channel-2"}}, recoveryTestAdapter(t), fastVideoPollPolicy())
	var unknown providerSubmissionUnknownError
	if creates != 1 || polls != 0 || result != nil || !errors.As(err, &unknown) {
		t.Fatalf("creates=%d polls=%d result=%v err=%v", creates, polls, result, err)
	}
	if !persistedFailureBlocksRetry(persistableTaskFailureMessage(err), "submission_unknown") {
		t.Fatal("lost receipt permits a paid retry")
	}
}

func TestVideoRecoverySubmissionClassification(t *testing.T) {
	for _, err := range []error{providerHTTPError{StatusCode: 429}, providerHTTPError{StatusCode: 404}, providerCircuitOpenError{}, context.Canceled} {
		var unknown providerSubmissionUnknownError
		if errors.As(uncertainVideoSubmission(context.Background(), err), &unknown) {
			t.Fatalf("known rejection wrapped: %v", err)
		}
	}
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, context.DeadlineExceeded, providerHTTPError{StatusCode: 524}} {
		var unknown providerSubmissionUnknownError
		if !errors.As(uncertainVideoSubmission(context.Background(), err), &unknown) {
			t.Fatalf("lost receipt not protected: %v", err)
		}
	}
}

func TestVideoRecoverySocketResetNeverResubmits(t *testing.T) {
	allowLoopbackProviderTest(t)
	creates, polls, downloads := 0, 0, 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creates++
			_, _ = io.WriteString(w, `{"code":"success","data":{"task_id":"original-paid-task","status":"IN_PROGRESS"}}`)
			return
		}
		if r.URL.Path == "/result.mp4" {
			downloads++
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("test-video"))
			return
		}
		polls++
		if polls == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			// Abort TCP with RST: on Windows the client receives WSAECONNRESET.
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		if r.URL.Path != "/v1/video/generations/original-paid-task" {
			t.Errorf("unexpected task query: %s", r.URL.Path)
		}
		_, _ = fmt.Fprintf(w, `{"code":"success","data":{"task_id":"original-paid-task","status":"SUCCESS","result_url":%q}}`, server.URL+"/result.mp4")
	}))
	defer server.Close()
	input := canvasGenerationInput{Mode: "video", Prompt: "test", Config: providerConfig{BaseURL: server.URL, Model: "seedance-2.0-mini", InterfaceType: "newapi-channel-2"}}
	result, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, recoveryTestAdapter(t), fastVideoPollPolicy())
	if err != nil || result == nil || creates != 1 || polls != 2 || downloads != 1 {
		t.Fatalf("creates=%d polls=%d downloads=%d result=%v err=%v", creates, polls, downloads, result, err)
	}
}

func TestVideoRecoveryDownloadDisconnectKeepsOriginalTask(t *testing.T) {
	for _, alwaysBroken := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysBroken), func(t *testing.T) {
			allowLoopbackProviderTest(t)
			creates, polls, downloads := 0, 0, 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					creates++
					_, _ = io.WriteString(w, `{"code":"success","data":{"task_id":"original-paid-task","status":"IN_PROGRESS"}}`)
					return
				}
				if r.URL.Path == "/result.mp4" {
					downloads++
					if alwaysBroken || downloads == 1 {
						conn, _, _ := w.(http.Hijacker).Hijack()
						_ = conn.Close()
						return
					}
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = w.Write([]byte("test-video"))
					return
				}
				polls++
				_, _ = fmt.Fprintf(w, `{"code":"success","data":{"task_id":"original-paid-task","status":"SUCCESS","result_url":%q}}`, server.URL+"/result.mp4")
			}))
			defer server.Close()
			input := canvasGenerationInput{Mode: "video", Prompt: "test", Config: providerConfig{BaseURL: server.URL, Model: "seedance-2.0-mini", InterfaceType: "newapi-channel-2"}}
			result, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, recoveryTestAdapter(t), fastVideoPollPolicy())
			if creates != 1 || polls != 1 || downloads < 2 {
				t.Fatalf("creates=%d polls=%d downloads=%d", creates, polls, downloads)
			}
			if !alwaysBroken {
				if err != nil || result == nil {
					t.Fatalf("failed to recover: %v", err)
				}
				return
			}
			started := time.Now()
			task := model.Task{ID: "local", Type: "canvas_video", ProviderRequestID: "original-paid-task", StartedAt: &started}
			body, _ := json.Marshal(input)
			svc := &Service{}
			if err == nil || !svc.shouldDeferVideoProviderTask(task, string(body), err) {
				t.Fatalf("download result abandoned: %v", err)
			}
			started = started.Add(-25 * time.Hour)
			if svc.shouldDeferVideoProviderTask(task, string(body), err) {
				t.Fatal("automatic recovery is unbounded")
			}
			if svc.shouldDeferVideoProviderTask(task, string(body), context.Canceled) {
				t.Fatal("cancelled task resurrected")
			}
		})
	}
}

func TestVideoRecoveryBeefAPIQueryUsesOnlyGet(t *testing.T) {
	allowLoopbackProviderTest(t)
	queries, downloads := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unsafe method %s", r.Method)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing query authentication")
		}
		if r.URL.Path == "/v1/videos/original/content" {
			downloads++
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("test-video"))
			return
		}
		queries++
		_, _ = io.WriteString(w, `{"data":{"id":"original","status":"completed"}}`)
	}))
	defer server.Close()
	result, _, err := queryBeefAPIVideoResult(context.Background(), canvasGenerationInput{Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key"}}, "original")
	if err != nil || result == nil || queries != 1 || downloads != 1 {
		t.Fatalf("result=%v err=%v queries=%d downloads=%d", result, err, queries, downloads)
	}
}

func TestVideoRecoveryTaskLogsExplainFailureWithoutRawPayload(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	log := model.TaskLog{ID: "log-recovery", UserID: "owner", TaskID: "task-recovery", Level: "error", Message: "任务处理失败", Payload: "视频结果下载失败：EOF https://private.example/result?token=secret-value"}
	if err := db.Create(&log).Error; err != nil {
		t.Fatal(err)
	}
	logs, err := svc.TaskLogs("owner", "task-recovery")
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%v err=%v", logs, err)
	}
	if !strings.Contains(logs[0].Summary, "任务处理失败") || logs[0].Message != "" || logs[0].Payload != "" || strings.Contains(logs[0].Summary, "secret-value") {
		t.Fatalf("unsafe or missing summary: %#v", logs[0])
	}
	other, err := svc.TaskLogs("other-user", "task-recovery")
	if err != nil || len(other) != 0 {
		t.Fatal("logs crossed ownership")
	}
}

func TestFailedBeefAPIVideoRecoveryUsesFreshBackupAndKeepsOriginalTask(t *testing.T) {
	allowLoopbackProviderTest(t)
	payload, err := os.ReadFile("testdata/reference-fragmented-30fps.mp4")
	if err != nil {
		t.Fatal(err)
	}
	creates, polls, primary, backups, mediaDownloads := 0, 0, 0, 0, 0
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Gateway-Secret") != "" || r.Header.Get("X-Beeftv-Video-Delivery") != "" {
			t.Error("gateway credentials reached external media")
		}
		if r.URL.Path == "/tos.mp4" {
			primary++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		mediaDownloads++
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer media.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			creates++
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Gateway-Secret") != "private-value" {
			t.Error("gateway request lost authentication")
		}
		if r.URL.Path == "/v1/videos/original" {
			polls++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "metadata": map[string]any{"url": media.URL + "/tos.mp4?signature=expired"}})
			return
		}
		if r.URL.Path != "/v1/videos/original/content" || r.URL.Query().Get("delivery") != "backup" {
			t.Errorf("unexpected recovery route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		backups++
		if backups == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"video_delivery_pending"}}`))
			return
		}
		http.Redirect(w, r, media.URL+"/cos.mp4?signature=fresh", http.StatusFound)
	}))
	defer gateway.Close()
	svc, db := openDBRuntimeMatrix(t, t.TempDir())
	defer closeDBRuntimeMatrix(t, svc, db)
	input := canvasGenerationInput{Mode: "video", Prompt: "test", Config: providerConfig{
		BaseURL: gateway.URL, APIKey: "test-key", Model: "seedance-2.0-mini", InterfaceType: "newapi",
		Headers: []outbound.OutboundHeader{{Name: "X-Gateway-Secret", Value: "private-value"}},
	}}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task := model.Task{ID: "original-local-task", UserID: "local", Type: "canvas_video", Status: model.TaskStatusFailed, ProviderRequestID: "original", InputJSON: string(inputJSON), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	ctx := generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: gateway.URL})
	recovered, err := svc.QueryFailedVideoTask(ctx, task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Recovered || recovered.Task.ID != task.ID || recovered.Task.ProviderRequestID != "original" || recovered.Task.Status != model.TaskStatusSucceeded || creates != 0 || polls != 1 || primary != 1 || backups != 2 || mediaDownloads != 1 {
		t.Fatalf("recovered=%+v creates=%d polls=%d primary=%d backups=%d media=%d", recovered, creates, polls, primary, backups, mediaDownloads)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.ResultJSON, "signature=") {
		t.Fatal("recovery persisted a signed media URL")
	}
	var resources []model.Resource
	if err := db.Find(&resources).Error; err != nil || len(resources) != 1 {
		t.Fatalf("resources=%d err=%v", len(resources), err)
	}
	resource := resources[0]
	if resource.Kind != "video" || resource.MimeType != "video/mp4" || !strings.HasSuffix(resource.ObjectKey, ".mp4") || resource.Width != 640 || resource.Height != 640 {
		t.Fatalf("recovered MP4 was not saved as a video: %+v", resource)
	}
	if recovered.Task.ResultState != "READY" || len(recovered.Task.Outputs) != 1 || recovered.Task.Outputs[0].ResourceID != resource.ID || recovered.Task.Outputs[0].MaterializedAssetID == "" {
		t.Fatalf("recovery did not expose the ready materialized resource: state=%s outputs=%+v", recovered.Task.ResultState, recovered.Task.Outputs)
	}
	data, err := os.ReadFile(filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resource.ObjectKey)))
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("stored media differs: bytes=%d err=%v", len(data), err)
	}
}
