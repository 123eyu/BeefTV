package app

import (
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	localasset "infinite-canvas/backend/internal/asset"
	cw "infinite-canvas/backend/internal/canvasworkflow"
	"infinite-canvas/backend/internal/model"
)

func workflowBad(message string) error { return cw.BadRequest(message) }

func workflowNotFound() error { return cw.NotFound() }

func workflowTemplate(row model.CanvasWorkflow) (cw.Template, error) { return cw.TemplateFromRow(row) }

func workflowRun(row model.CanvasWorkflowRun) (cw.Run, error) {
	var v cw.Run
	err := json.Unmarshal([]byte(row.StateJSON), &v)
	if v.Steps == nil {
		v.Steps = []cw.Step{}
	}
	if v.Outputs == nil {
		v.Outputs = []cw.Output{}
	}
	v.ID = row.ID
	v.TemplateID = row.TemplateID
	v.TemplateRevision = workflowRevision(row.TemplateRevision)
	v.TemplateName = row.TemplateName
	v.Status = row.Status
	v.CreatedAt = row.CreatedAt
	v.UpdatedAt = row.UpdatedAt
	return v, err
}

func (s *Service) workflowResource(user, id, kind string) (*model.Resource, error) {
	r, e := s.repo.ResourceForUser(user, id)
	if e != nil || r == nil || r.Status != model.ResourceStatusReady {
		return nil, workflowBad("工作流素材不存在或尚未保存，请先保存素材")
	}
	if kind != "" && localasset.NormalizeKind(r.Kind, r.MimeType) != kind {
		return nil, workflowBad("工作流素材类型与节点不一致")
	}
	return r, nil
}

func (s *Service) CanvasWorkflowRun(user, id string) (cw.Run, error) {
	var row model.CanvasWorkflowRun
	if e := s.repo.DB().Where("id = ? AND user_id = ?", id, user).First(&row).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return cw.Run{}, workflowNotFound()
		}
		return cw.Run{}, e
	}
	return workflowRun(row)
}

func (s *Service) CanvasWorkflowRuns(user, template string) ([]cw.Run, error) {
	if _, e := s.workflowTemplates().Get(user, template); e != nil {
		return nil, e
	}
	var rows []model.CanvasWorkflowRun
	if e := s.repo.DB().Where("user_id = ? AND template_id = ?", user, template).Order("created_at DESC").Limit(100).Find(&rows).Error; e != nil {
		return nil, e
	}
	out := []cw.Run{}
	for _, r := range rows {
		v, e := workflowRun(r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
