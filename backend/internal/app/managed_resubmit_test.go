package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/model"
	localtask "infinite-canvas/backend/internal/task"
)

// 记录假 BeefAPI 收到的提交，用来核对补交是否和原请求一字不差。
type managedSubmitLog struct {
	mu      sync.Mutex
	headers []http.Header
	bodies  [][]byte
}

func (l *managedSubmitLog) add(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.headers = append(l.headers, r.Header.Clone())
	l.bodies = append(l.bodies, body)
}

func (l *managedSubmitLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.bodies)
}

func managedNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":{"code":"media_task_not_found","category":"unknown","message":"This query did not establish the original attempt."}}`))
}

// 托管图片：原请求发出后回复丢失，BeefAPI 查不到原编号。
func managedMissingImageFixture(t *testing.T, serverURL string) (*Service, *gorm.DB, model.Task, []byte) {
	t.Helper()
	svc, db, task, row, _ := imageSubmissionFixture(t, false)
	task.Status, task.Stage, task.LeaseOwner, task.LeaseExpiresAt = model.TaskStatusFailed, "submission_unknown", "", nil
	task.InputJSON = `{"mode":"image","config":{"channelId":"beefapi"}}`
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	attempt, _ := svc.repo.LatestRouteAttempt(task.ID)
	attempt.ChannelID = beefapi.ChannelID
	if err := svc.repo.SaveRouteAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	wire, _ := svc.imageSubmissionWire(row)
	wire.URL, wire.ChannelID, wire.ReplayProtocol, wire.MediaType = serverURL+"/v1/media/tasks", beefapi.ChannelID, beefapi.MediaProtocolVersion, "image"
	wire.Header.Set("Idempotency-Key", "original-ref-0001")
	raw, _ := json.Marshal(wire)
	row.RequestCipher, _ = svc.encryptSettingSecret(string(raw))
	row.QueryCipher, row.SendCount = "", 1
	if err := db.Save(row).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, task, wire.Body
}

func TestManagedImageMissingSubmissionResubmitsOnlyOnManualRecovery(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var submits managedSubmitLog
	var known sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/v1/media/tasks/")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/media/tasks":
			submits.add(r)
			known.Store(r.Header.Get("Idempotency-Key"), true)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"task_ref": r.Header.Get("Idempotency-Key"), "media_type": "image", "status": "queued", "outputs": []any{}})
		case r.Method == http.MethodGet:
			if _, ok := known.Load(ref); !ok {
				managedNotFound(w)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"task_ref": ref, "media_type": "image", "status": "in_progress", "outputs": []any{}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	svc, _, task, originalBody := managedMissingImageFixture(t, server.URL)
	base := generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: server.URL})

	// 后台恢复只查询：不补交，也不下结论。
	background, cancel := context.WithTimeout(base, time.Second)
	_ = svc.recoverFailedImage(background, task)
	cancel()
	latest, _ := svc.repo.Task(task.ID)
	if submits.count() != 0 || latest.RecoveryOutcome != nil {
		t.Fatalf("background recovery resubmitted or concluded: submits=%d task=%+v", submits.count(), latest)
	}

	// 用户点「继续取回」：用原 Key、原编号、原内容补交一次，并很快再查。
	if _, err := svc.RecoverTask(base, task.UserID, task.ID); err != nil {
		t.Fatal(err)
	}
	latest, _ = svc.repo.Task(task.ID)
	if submits.count() != 1 {
		t.Fatalf("manual recovery submits=%d, want 1", submits.count())
	}
	header, body := submits.headers[0], submits.bodies[0]
	if header.Get("Idempotency-Key") != "original-ref-0001" || header.Get("Authorization") != "Bearer private-test-key" || !bytes.Equal(body, originalBody) {
		t.Fatalf("resubmission differs from the frozen original: key=%q auth=%q body=%s", header.Get("Idempotency-Key"), header.Get("Authorization"), body)
	}
	if latest.RecoveryOutcome != nil || latest.NextPollAt == nil || time.Until(*latest.NextPollAt) > 20*time.Second {
		t.Fatalf("resubmitted task not polled soon: %+v", latest)
	}

	// BeefAPI 已有这个编号：再点也只查询，不会再补交。
	if _, err := svc.RecoverTask(base, task.UserID, task.ID); err != nil {
		t.Fatal(err)
	}
	if submits.count() != 1 {
		t.Fatalf("known submission was resubmitted again: %d", submits.count())
	}
}

func TestManagedImageResubmissionResponseDecidesOutcome(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"key-taken-by-other-request", http.StatusConflict, `{"error":{"code":"idempotency_conflict","message":"different request"}}`, localtask.RecoveryUnrecoverable},
		{"rejected", http.StatusBadRequest, `{"error":{"code":"invalid_request","category":"input","message":"bad"}}`, localtask.RecoveryNoResult},
		{"failed-projection", http.StatusOK, `{"task_ref":"original-ref-0001","media_type":"image","status":"failed","outputs":[],"error":{"code":"media_generation_failed","category":"upstream","message":"failed"}}`, localtask.RecoveryNoResult},
		{"unavailable", http.StatusServiceUnavailable, `{"error":{"code":"media_task_unavailable","message":"retry"}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var submits managedSubmitLog
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					managedNotFound(w)
					return
				}
				submits.add(r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			svc, _, task, _ := managedMissingImageFixture(t, server.URL)
			ctx := generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: server.URL})
			if _, err := svc.RecoverTask(ctx, task.UserID, task.ID); err != nil {
				t.Fatal(err)
			}
			latest, _ := svc.repo.Task(task.ID)
			got := ""
			if latest.RecoveryOutcome != nil {
				got = *latest.RecoveryOutcome
			}
			if submits.count() != 1 || got != tc.want {
				t.Fatalf("submits=%d outcome=%q, want 1 and %q", submits.count(), got, tc.want)
			}
		})
	}
}

func TestManagedImageMissingSubmissionWaitsWhenBeefAPIRecordedSettlement(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var submits managedSubmitLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			submits.add(r)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		managedNotFound(w)
	}))
	defer server.Close()
	svc, db, task, _ := managedMissingImageFixture(t, server.URL)
	settled := "settled"
	task.BeefBillingState = &settled
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	ctx := generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: server.URL})
	_, _ = svc.RecoverTask(ctx, task.UserID, task.ID)
	latest, _ := svc.repo.Task(task.ID)
	if submits.count() != 0 || latest.RecoveryOutcome != nil {
		t.Fatalf("submission known to BeefAPI was resubmitted or concluded: submits=%d task=%+v", submits.count(), latest)
	}
}

func TestManagedVideoLostSubmissionResubmitsOnlyOnManualRecovery(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var submits managedSubmitLog
	var known sync.Map
	var lost sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/models":
			w.Header().Set(beefapi.MediaProtocolHeader, beefapi.MediaProtocolVersion)
			_ = json.NewEncoder(w).Encode(map[string]any{"media_protocol": beefapi.MediaDiscovery{Version: beefapi.MediaProtocolVersion, SupportedMediaTypes: []string{"video"}, TaskSubmitURL: "/v1/media/tasks", TaskQueryURLTemplate: "/v1/media/tasks/{task_ref}", UploadsURL: "/v1/media/uploads"}, "data": []any{map[string]any{"id": "opaque-video-2030"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/media/tasks":
			submits.add(r)
			first := false
			lost.Do(func() { first = true })
			if first {
				// 原请求的回复丢失，BeefAPI 也没有留下这次提交的记录。
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			known.Store(r.Header.Get("Idempotency-Key"), true)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"task_ref": r.Header.Get("Idempotency-Key"), "media_type": "video", "status": "queued", "outputs": []any{}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/media/tasks/"):
			ref := strings.TrimPrefix(r.URL.Path, "/v1/media/tasks/")
			if _, ok := known.Load(ref); !ok {
				managedNotFound(w)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"task_ref": ref, "media_type": "video", "status": "in_progress", "outputs": []any{}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path, dir := filepath.Join(t.TempDir(), "workspace.db"), t.TempDir()
	svc, db := newManagedImageDirectService(t, path, dir)
	defer closeDB(t, db)
	input := generation.Input{Mode: "video", Prompt: "fixture", Config: generation.Config{CredentialRef: beefapi.CredentialRef, MediaProtocolVersion: beefapi.MediaProtocolVersion, APIKey: "fixture", BaseURL: server.URL, Model: "opaque-video-2030", VideoSeconds: "4", VideoGenerateAudio: "false"}}
	raw, _ := json.Marshal(input)
	expires := time.Now().Add(time.Hour)
	task := model.Task{ID: "video-lost", UserID: "user-1", Type: "canvas_video", Status: model.TaskStatusRunning, InputJSON: string(raw), LeaseOwner: "worker", LeaseExpiresAt: &expires}
	attempt := &model.RouteAttempt{ID: "video-lost-attempt", TaskID: task.ID, ChannelID: beefapi.ChannelID, Status: "dispatching", DispatchState: "submission_unknown", StartedAt: time.Now()}
	for _, row := range []any{&task, attempt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	firstCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx := generation.WithEndpoints(firstCtx, generation.Endpoints{BeefAPIVideoBaseURL: server.URL})
	ctx = svc.bindGenerationRuntime(ctx, generation.CallMeta{UserID: task.UserID, TaskID: task.ID, TaskType: task.Type})
	ctx = context.WithValue(withProviderSubmissionKey(ctx, attempt), imageTaskContext{}, task)
	if _, err := generation.Execute(ctx, input); err == nil || submits.count() != 1 {
		t.Fatalf("original submit: submits=%d err=%v", submits.count(), err)
	}
	task.Status, task.LeaseOwner, task.LeaseExpiresAt, task.Stage = model.TaskStatusFailed, "", nil, "submission_unknown"
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: server.URL})

	if err := svc.recoverFailedVideos(base, 8); err != nil {
		t.Logf("background pass: %v", err)
	}
	if submits.count() != 1 {
		t.Fatalf("background recovery resubmitted: %d", submits.count())
	}

	if _, err := svc.RecoverTask(base, task.UserID, task.ID); err != nil {
		t.Fatal(err)
	}
	latest, _ := svc.repo.Task(task.ID)
	if submits.count() != 2 {
		t.Fatalf("manual recovery submits=%d, want original + one resubmission", submits.count())
	}
	if submits.headers[1].Get("Idempotency-Key") != submits.headers[0].Get("Idempotency-Key") || submits.headers[1].Get("Authorization") != submits.headers[0].Get("Authorization") || !bytes.Equal(submits.bodies[1], submits.bodies[0]) {
		t.Fatal("resubmission differs from the original request")
	}
	if latest.RecoveryOutcome != nil {
		t.Fatalf("resubmitted video was concluded: %+v", latest)
	}
}

// 生成过程中（含重启后接着跟进）查不到原编号时，原请求可能还在路上：
// 只能继续等，不能判成「明确被拒、可直接重新生成」，否则原请求之后到达照样扣费。
func TestManagedImageGenerationResumeKeepsWaitingWhenSubmissionMissing(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var submits managedSubmitLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			submits.add(r)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		managedNotFound(w)
	}))
	defer server.Close()
	svc, _, task, _ := managedMissingImageFixture(t, server.URL)
	attempt, _ := svc.repo.LatestRouteAttempt(task.ID)
	ctx, cancel := context.WithTimeout(generation.WithEndpoints(context.Background(), generation.Endpoints{BeefAPIVideoBaseURL: server.URL}), 300*time.Millisecond)
	defer cancel()
	ctx = context.WithValue(ctx, imageAttemptContext{}, attempt.ID)
	_, handled, err := svc.recoverImageSubmission(ctx, task)
	if !handled || err == nil || submits.count() != 0 {
		t.Fatalf("handled=%v err=%v submits=%d", handled, err, submits.count())
	}
	if imageTaskEnded(err) {
		t.Fatalf("missing submission during generation was treated as a definite rejection: %v", err)
	}
}
