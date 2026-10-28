package app

import (
	"encoding/json"
	"reflect"
	"strings"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/assistantturns"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/operations"
	localtask "infinite-canvas/backend/internal/task"
)

// The existing continuation receipt advances the exact confirmed inputs. Model
// admission waits until their files are saved; this does not create a new queue
// or retry failed/unknown purchases. Called under the claim's scope transaction.
func (s *Service) prepareAssistantProduction(tx *gorm.DB, receipt *model.AssistantContinuation) (bool, error) {
	var source model.AssistantTurn
	if err := tx.First(&source, "turn_id = ? AND user_id = ?", receipt.SourceTurnID, receipt.UserID).Error; err != nil {
		return false, err
	}
	if source.GenerationAuthorizationJSON == "" {
		return true, nil
	}
	var plan operations.GenerationAuthorization
	if err := json.Unmarshal([]byte(source.GenerationAuthorizationJSON), &plan); err != nil {
		return false, err
	}
	if plan.SourceTurnID == "" {
		return true, nil
	}
	if source.Undone || source.ContinuationInvalidated || plan.SourceTurnID != source.TurnID || plan.ProposalID != receipt.ProposalID {
		return false, operations.PreconditionFailed("production_revoked", "本次制作授权已停止", nil)
	}
	domain := s.BindDomain(tx).(*operationSession)
	raw, err := domain.UserCanvasProject(receipt.UserID, receipt.CanvasID)
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	nodes := map[string]map[string]any{}
	for _, value := range doc["nodes"].([]any) {
		node := value.(map[string]any)
		nodes[node["id"].(string)] = node
	}
	ids := make([]string, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		ids = append(ids, target.NodeID)
	}
	current, err := operations.GenerationTargets(domain, doc, ids)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(current, plan.Targets) {
		return false, &assistantturns.Error{Reason: "continuation_intent_changed", Message: "制作方案已修改，自动制作已停止，请确认新的方案"}
	}
	saved, planned := map[string]bool{}, map[string]bool{}
	pending := []operations.GenerationTarget{}
	allSaved := true
	failed, running, targetChanged, changed := false, false, false, false
	for _, target := range plan.Targets {
		planned[target.NodeID] = true
		key := productionTaskKey(plan, target.NodeID)
		task, err := domain.repo.TaskByClientOperation(receipt.UserID, key)
		if err != nil {
			return false, err
		}
		node := nodes[target.NodeID]
		meta, _ := node["metadata"].(map[string]any)
		nodeTaskID, _ := meta["taskId"].(string)
		nodeTaskID = strings.TrimSpace(nodeTaskID)
		if task == nil {
			if node == nil || nodeTaskID != "" || meta["status"] == "loading" || meta["taskStatus"] == "queued" || meta["taskStatus"] == "running" || meta["generationSubmissionState"] == "submitting" {
				targetChanged = true
				failed = true
				allSaved = false
				continue
			}
			pending = append(pending, target)
			allSaved = false
			continue
		}
		if err := validateProductionTask(task, receipt.CanvasID, target); err != nil {
			return false, err
		}
		if node == nil || nodeTaskID != task.ID {
			targetChanged = true
			failed = true
			allSaved = false
			continue
		}
		if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
			failed = true
			allSaved = false
			if syncProductionNodeFailure(node, meta, task, key) {
				changed = true
			}
			continue
		}
		var settled bool
		saved[target.NodeID], settled, err = productionTargetState(tx, receipt.UserID, target, task, meta)
		if err != nil {
			return false, err
		}
		// A successful supplier task is still in flight until its canonical file
		// and target binding are saved, even when a sibling has already failed.
		running = running || !settled
		failed = failed || (settled && !saved[target.NodeID])
		allSaved = allSaved && saved[target.NodeID]
	}
	if changed {
		encoded, err := json.Marshal(doc)
		if err != nil {
			return false, err
		}
		if _, _, err := domain.CommitUserCanvasDocument(receipt.UserID, receipt.CanvasID, assistantDocRevision(doc), encoded); err != nil {
			return false, err
		}
	}
	if targetChanged {
		if running {
			return false, nil
		}
		return false, &assistantturns.Error{Reason: "continuation_target_changed", Message: "暂不能自动继续，请查看本次生成或手动继续"}
	}
	if failed {
		// Keep the single wake until every already-admitted sibling settles.
		// Do not admit pending dependants or repurchase a failed task.
		return !running, nil
	}
	if allSaved {
		return true, nil
	}
	ready := []operations.GenerationTarget{}
	for _, target := range pending {
		dependenciesSaved := true
		for _, id := range target.ReferenceNodeIDs {
			if planned[id] && !saved[id] {
				dependenciesSaved = false
			}
		}
		if dependenciesSaved {
			ready = append(ready, target)
		}
	}
	if len(ready) == 0 {
		return false, nil
	}
	_, err = domain.admitProductionTargets(receipt.UserID, receipt.CanvasID, plan, ready)
	return false, err
}

func productionTargetState(tx *gorm.DB, userID string, target operations.GenerationTarget, task *model.Task, meta map[string]any) (saved bool, settled bool, err error) {
	key, _ := meta["storageKey"].(string)
	if task.Status != model.TaskStatusSucceeded {
		return false, false, nil
	}
	var records []model.Result
	if err := tx.Where("task_id = ? AND user_id = ? AND kind = ?", task.ID, userID, localtask.ResultKindGenerationOutput).Find(&records).Error; err != nil {
		return false, false, err
	}
	outputs, err := localtask.DecodeOutputResults(records)
	if err != nil {
		return false, false, err
	}
	for _, output := range outputs {
		if output.OutputIndex != 0 || output.MediaType != target.Kind || output.TargetBinding == nil || output.TargetBinding.NodeID != target.NodeID {
			continue
		}
		if output.MaterializationErrorCode != "" {
			return false, true, nil
		}
		if output.ResourceID == "" || meta["taskId"] != task.ID || meta["status"] != "success" || meta["resourceReloadAvailable"] == true || !strings.HasPrefix(key, "resource:") || key != "resource:"+output.ResourceID {
			continue
		}
		var count int64
		err := tx.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ?", output.ResourceID, userID, model.ResourceStatusReady).Count(&count).Error
		return count == 1, count == 1, err
	}
	return false, false, nil
}

func syncProductionNodeFailure(node, meta map[string]any, task *model.Task, key string) bool {
	if node == nil || task == nil {
		return false
	}
	if meta == nil {
		meta = map[string]any{}
		node["metadata"] = meta
	}
	changed := false
	for field, value := range map[string]any{
		"taskId":                    task.ID,
		"taskStatus":                string(task.Status),
		"taskClientOperationId":     key,
		"generationSubmissionState": "accepted",
		"status":                    "error",
		"error":                     task.Error,
		"errorDetails":              task.Error,
	} {
		if meta[field] != value {
			meta[field] = value
			changed = true
		}
	}
	return changed
}
