package handler

import (
	"encoding/json"
	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/assistantturns"
	"infinite-canvas/backend/internal/model"
	"net/http"
	"testing"
)

func TestAssistantContinuationHTTPReusesTrustedTurnAndExcludesComposerInputs(t *testing.T) {
	var forwarded []map[string]any
	env := newAssistantTestEnv(t, func(env *assistantTestEnv) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat" {
				w.Write([]byte(`{}`))
				return
			}
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			forwarded = append(forwarded, input)
			if len(forwarded) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"reason":"temporary_database_error"}`))
				return
			}
			// Observer ends without turn_end: a retry must reuse the claimed Go turn.
			w.Header().Set("Content-Type", "application/x-ndjson")
			json.NewEncoder(w).Encode(map[string]any{"type": "session", "sessionId": input["sessionId"], "turnId": input["turnId"]})
		})
	})
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.service.UserCanvasProject(owner.ID, env.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	node := doc["nodes"].([]any)[0].(map[string]any)
	node["type"] = "image"
	node["metadata"] = map[string]any{"taskId": "failed-task", "taskStatus": "failed", "status": "error"}
	raw, _ = json.Marshal(doc)
	if _, err = env.service.UpsertUserCanvasProject(owner.ID, raw); err != nil {
		t.Fatal(err)
	}
	source, session := "aabbccddeeff", "durable:11111111-2222-3333-4444-555555555555"
	if _, err = env.service.BeginAssistantTurn(owner.ID, env.canvasID, source, app.AssistantTurnInput{PermissionMode: assistantturns.PermissionCanvas}); err != nil {
		t.Fatal(err)
	}
	if err = env.service.BindAssistantDurableSession(owner.ID, source, session, false); err != nil {
		t.Fatal(err)
	}
	if err = env.service.FinalizeAssistantTurn(source); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(assistantturns.ContinuationInput{CanvasID: env.canvasID, SessionID: session, SourceTurnID: source, ProposalID: "gp-fixture", Kind: "image", NodeIDs: []string{"n1"}})
	arm := env.call(t, http.MethodPost, "/assistant/continuations", string(body))
	if arm.Code != 200 {
		t.Fatal(arm.Code, arm.Body.String())
	}
	data := decodeEnvelope(t, arm)
	id := data["id"].(string)
	if ids, ok := data["taskIds"].([]any); !ok || len(ids) != 0 {
		t.Fatal("waiting task ids must be an empty array", data)
	}
	manifest := env.call(t, http.MethodPost, "/assistant/continuations/"+id+"/manifest", `{"sourceNodeId":"n1","submissions":[{"operationId":"proposal:gp-fixture:n1","outputNodeIds":["n1"]}]}`)
	if manifest.Code != 200 {
		t.Fatal(manifest.Code, manifest.Body.String())
	}
	operation := "proposal:gp-fixture:n1"
	if err = env.service.Database().Create(&model.Task{ID: "failed-task", UserID: owner.ID, ProjectID: env.canvasID, ClientOperationID: &operation, Type: "canvas_image", Status: model.TaskStatusFailed, InputJSON: `{"metadata":{"sourceNodeId":"n1","nodeId":"n1"}}`}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		request, _ := json.Marshal(map[string]any{"canvasId": env.canvasID, "sessionId": session, "continuationId": id, "message": "do not send draft", "permissionMode": "full-access", "requestId": "client-request", "selectedNodeIds": []string{"foreign"}, "skills": []any{map[string]any{"skillId": "bad"}}, "references": []any{map[string]any{"kind": "asset", "id": "foreign"}}})
		response := env.call(t, http.MethodPost, "/assistant/chat", string(request))
		want := 200
		if i == 0 {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if len(forwarded) != 2 {
		t.Fatal("host calls", len(forwarded))
	}
	for _, input := range forwarded {
		if input["turnId"] != data["turnId"] || input["requestId"] != data["requestId"] || input["message"] != assistantturns.ContinuationMessage || input["permissionMode"] != "canvas" {
			t.Fatalf("untrusted continuation %+v", input)
		}
		ids := input["selectedNodeIds"].([]any)
		if len(ids) != 1 || ids[0] != "n1" {
			t.Fatal("wrong actual results", input)
		}
		for _, key := range []string{"skills", "attachments", "references"} {
			if values, ok := input[key].([]any); ok && len(values) > 0 {
				t.Fatal("composer leaked", key, input)
			}
		}
	}
	var count int64
	env.service.Database().Model(&model.AssistantTurn{}).Count(&count)
	if count != 2 {
		t.Fatal("duplicate business turn", count)
	}
	invalid := env.call(t, http.MethodPost, "/assistant/cancel", `{"canvasId":"`+env.canvasID+`","sessionId":"`+session+`","requestId":"bad request"}`)
	if invalid.Code != 400 {
		t.Fatal(invalid.Code)
	}
	var row model.AssistantContinuation
	env.service.Database().First(&row, "id = ?", id)
	if row.State != "claimed" {
		t.Fatal("invalid stop mutated authority", row.State)
	}
	stopped := env.call(t, http.MethodPost, "/assistant/continuations/cancel", `{"canvasId":"`+env.canvasID+`","sessionId":"`+session+`"}`)
	if stopped.Code != 200 {
		t.Fatal(stopped.Code)
	}
	headers := map[string]string{"X-Beeftv-Agent-Token": assistantTestHostToken}
	completed := env.callWithHeaders(t, http.MethodPost, "/assistant/runtime/turns/"+row.TurnID+"/complete", "{}", headers)
	state := decodeEnvelope(t, completed)
	if completed.Code != 200 || state["open"] != false || state["continuationRevoked"] != true {
		t.Fatalf("cancelled host ack %d %s", completed.Code, completed.Body.String())
	}
	rejected := env.call(t, http.MethodPost, "/assistant/continuations/"+id+"/claim", `{}`)
	if rejected.Code == 200 {
		t.Fatal("cancelled claim revived")
	}
}
