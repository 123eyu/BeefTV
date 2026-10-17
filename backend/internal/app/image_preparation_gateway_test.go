package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/model"
)

func TestImageGatewayPreparationFailureDoesNotBecomeUnknownOrResend(t *testing.T) {
	s, db, task, row, _ := imageSubmissionFixture(t, false)
	calls := 0
	_, _, err := s.sendImageSubmissionWith(context.Background(), task, row, func(req *http.Request, admit func() error) ([]byte, string, error) {
		calls++
		if err := admit(); err != nil {
			return nil, "", err
		}
		return nil, "", generation.RequestPreparationError{Cause: providerHTTPError{StatusCode: 503, Body: `{"error":{"code":"image_reference_upload_failed","metadata":{"submission_state":"not_submitted"}}}`}}
	}, noImageWait)
	var preparation generation.RequestPreparationError
	if !errors.As(err, &preparation) || isImageRecoveryError(err) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	failure := classifyTaskFailure(err)
	if failure.Uncertain || failure.Retryable || !failure.BlocksAutomaticRetry() || failure.Category != generation.CategoryProviderUnavailable || failure.Action != "请稍后重试" {
		t.Fatalf("preparation failure misclassified: %#v", failure)
	}
	attempt, readErr := s.repo.LatestRouteAttempt(task.ID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	s.finishTaskRouteAttempt(attempt, &task, err)
	if attempt.DispatchState != "rejected_no_job" {
		t.Fatalf("dispatch=%s", attempt.DispatchState)
	}
	if retryErr := s.validateImageTaskRetry(&task); retryErr != nil {
		t.Fatalf("confirmed unsent task cannot be retried: %v", retryErr)
	}
	var count int64
	db.Model(&model.ImageSubmission{}).Where("task_id = ?", task.ID).Count(&count)
	if count != 1 {
		t.Fatalf("unexpected extra generation attempt: %d", count)
	}
}
