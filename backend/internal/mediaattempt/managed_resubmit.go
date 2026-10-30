package mediaattempt

import (
	"context"
	"errors"
	"time"

	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/model"
	localtask "infinite-canvas/backend/internal/task"
)

// managedRecoveryCollectWait 是一次恢复里等待托管任务结果的上限。
const managedRecoveryCollectWait = 20 * time.Second

// ErrManagedResubmitted 表示刚用原编号补交了原请求，结果要等下一轮查询。
var ErrManagedResubmitted = errors.New("已用原编号补交原请求，正在等待结果")

// ResolveMissingManagedSubmission 处理「BeefAPI 用原 Key 查不到原编号」（beefapi.IsMediaTaskNotFound）。
// 返回非空 outcome 时，调用方把它写成恢复结论；resubmitted 表示已补交、应尽快再查。
//
//   - 本地已有 BeefAPI 结算记录：BeefAPI 记过这次提交，只是暂时不可见，继续等它的结论。
//   - 原请求已超过保存期：既不能补交，也无法确认，结论为「无法取回」，重新生成前要确认。
//   - 用户手动继续取回、原请求还在：用原 Key、原编号、原内容补交一次。BeefAPI 按编号去重，
//     收到过就返回原任务，没收到才第一次执行，所以不会产生第二次付费生成。
//   - 后台恢复：只查询，不补交。
//
// 方案见 docs/architecture/unconfirmed-submission.md。
func (s *Service) ResolveMissingManagedSubmission(ctx context.Context, task model.Task) (string, bool, error) {
	if task.BeefUsageState != nil || task.BeefBillingState != nil || task.BeefReconcileDeadline != nil {
		return "", false, nil
	}
	attempt, err := s.repo.LatestRouteAttempt(task.ID)
	if err != nil || attempt == nil || attempt.RouteRun != task.RouteRun {
		return "", false, err
	}
	row, err := s.repo.ImageSubmission(attempt.ID, task.ID, task.UserID)
	if err != nil {
		return "", false, err
	}
	if row.RequestCipher == "" || time.Since(row.CreatedAt) >= RecoveryLifetime {
		return localtask.RecoveryUnrecoverable, false, nil
	}
	if !localtask.ManualRecovery(ctx) {
		return "", false, nil
	}
	wire, err := s.ImageSubmissionWire(row)
	if err != nil {
		return "", false, err
	}
	if wire.ReplayProtocol != beefapi.MediaProtocolVersion {
		return "", false, errors.New("原请求不是托管媒体提交，不能补交")
	}
	query, managed, err := s.ManagedTaskQuery(ctx, task, row)
	if err != nil || !managed {
		return "", false, err
	}
	resubmitted, err := query.Client.Resubmit(ctx, wire.URL, wire.Header, wire.Body, query.Key, query.Kind)
	switch {
	case err == nil && resubmitted.Status == "failed":
		return localtask.RecoveryNoResult, false, nil
	case err == nil:
		return "", true, nil
	case beefapi.IsIdempotencyConflict(err):
		return localtask.RecoveryUnrecoverable, false, nil
	case beefapi.IsDefiniteMediaRejection(err) || beefapi.IsTerminalMediaTaskError(err):
		return localtask.RecoveryNoResult, false, nil
	default:
		return "", false, err
	}
}
