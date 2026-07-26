package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserToolImageTaskCreateIsIdempotentAndDetectsChangedRequest(t *testing.T) {
	truncateTables(t)

	input := UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "client-task-1",
		SessionID:       "session-1",
		MessageKey:      "message-1",
		TokenID:         7,
		RequestSnapshot: JSONValue(`{"model":"gpt-image-1","n":1}`),
	}
	created, replayed, err := CreateOrGetUserToolImageTask(input)
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.False(t, replayed)
	assert.Equal(t, UserToolImageTaskStatusPending, created.Status)
	assert.Equal(t, created.TaskID, created.BillingRequestID)

	retried, replayed, err := CreateOrGetUserToolImageTask(input)
	require.NoError(t, err)
	require.NotNil(t, retried)
	assert.True(t, replayed)
	assert.Equal(t, created.TaskID, retried.TaskID)

	input.RequestSnapshot = JSONValue(`{"model":"gpt-image-1","n":2}`)
	_, _, err = CreateOrGetUserToolImageTask(input)
	assert.ErrorIs(t, err, ErrUserToolImageTaskIdempotencyConflict)

	otherUserInput := input
	otherUserInput.UserID = 202
	otherUserInput.RequestSnapshot = JSONValue(`{"model":"gpt-image-1","n":1}`)
	other, replayed, err := CreateOrGetUserToolImageTask(otherUserInput)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.NotEqual(t, created.TaskID, other.TaskID)
}

func TestUserToolImageTaskLeasePreventsStaleWorkerAndAllowsTakeover(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "lease-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"a house"}`),
	})
	require.NoError(t, err)

	first, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-a", 2000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "worker-a", first.LeaseOwner)
	assert.Equal(t, 1, first.Attempt)

	_, claimed, err = ClaimUserToolImageTask(task.TaskID, "worker-b", 3000, 1500)
	require.NoError(t, err)
	assert.False(t, claimed)

	assert.ErrorIs(t, CompleteUserToolImageTask(task.TaskID, "worker-a", "item-a", 2100), ErrUserToolImageTaskLeaseLost)

	takenOver, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-b", 4000, 2100)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "worker-b", takenOver.LeaseOwner)
	assert.Equal(t, 2, takenOver.Attempt)

	require.NoError(t, CompleteUserToolImageTask(task.TaskID, "worker-b", "item-b", 2200))
	finished, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusCompleted, finished.Status)
	assert.Equal(t, "item-b", finished.ResultItemID)
	assert.Equal(t, int64(0), finished.LeaseUntil)

	_, claimed, err = ClaimUserToolImageTask(task.TaskID, "worker-c", 5000, 2300)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestUserToolImageTaskCompletionPersistsResultSnapshotAtomically(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "result-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"persist result"}`),
	})
	require.NoError(t, err)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)

	resultSnapshot := JSONValue(`{"created":123,"data":[{"url":"/api/playground/files/pgf_result/content"}]}`)
	require.NoError(t, CompleteUserToolImageTaskWithResult(task.TaskID, "worker-a", "pgf_result", resultSnapshot, 2000))

	finished, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusCompleted, finished.Status)
	assert.Equal(t, "pgf_result", finished.ResultItemID)
	assert.JSONEq(t, string(resultSnapshot), string(finished.ResultSnapshot))
	assert.Equal(t, int64(0), finished.LeaseUntil)

	assert.ErrorIs(
		t,
		CompleteUserToolImageTaskWithResult(
			task.TaskID,
			"worker-a",
			"pgf_overwrite",
			JSONValue(`{"data":[{"url":"https://sensitive.example/image.png"}]}`),
			2100,
		),
		ErrUserToolImageTaskLeaseLost,
	)

	reloaded, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.JSONEq(t, string(resultSnapshot), string(reloaded.ResultSnapshot))
}

func TestUserToolImageTaskPreparedCompletionCannotBeClaimedOrRegenerated(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "prepared-result-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"persist before billing"}`),
	})
	require.NoError(t, err)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)

	resultSnapshot := JSONValue(`{"created":123,"data":[{"url":"/api/playground/files/pgf_prepared/content"}]}`)
	require.NoError(t, PrepareUserToolImageTaskCompletion(
		task.TaskID,
		"worker-a",
		"pgf_prepared",
		resultSnapshot,
		2000,
	))

	prepared, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusFinalizing, prepared.Status)
	assert.Equal(t, "pgf_prepared", prepared.ResultItemID)
	assert.JSONEq(t, string(resultSnapshot), string(prepared.ResultSnapshot))

	ready, err := ListReadyUserToolImageTasks(20, 4000)
	require.NoError(t, err)
	assert.Empty(t, ready)

	_, claimed, err = ClaimUserToolImageTask(task.TaskID, "worker-b", 5000, 4000)
	require.NoError(t, err)
	assert.False(t, claimed)

	require.NoError(t, CompletePreparedUserToolImageTask(task.TaskID, "worker-a", 2500))
	completed, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusCompleted, completed.Status)
	assert.Equal(t, int64(0), completed.LeaseUntil)
}

func TestCompleteSettledPreparedUserToolImageTaskRecoversOwnerTaskIdempotently(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "recover-settled-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"recover after restart"}`),
	})
	require.NoError(t, err)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-before-restart", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)

	resultSnapshot := JSONValue(`{"created":123,"data":[{"url":"/pg/image-files/pgf_recovered/content"}]}`)
	require.NoError(t, PrepareUserToolImageTaskCompletion(
		task.TaskID,
		"worker-before-restart",
		"pgf_recovered",
		resultSnapshot,
		2000,
	))
	require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-recover-1", 140000, 2100))
	require.NoError(t, MarkUserToolImageTaskSettled(task.TaskID, 140000, 2200))

	_, recovered, err := CompleteSettledPreparedUserToolImageTask(202, task.TaskID, 2500)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.False(t, recovered)

	stillOwned, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusFinalizing, stillOwned.Status)
	assert.Equal(t, UserToolImageTaskBillingStatusSettled, stillOwned.BillingStatus)

	recoveredTask, recovered, err := CompleteSettledPreparedUserToolImageTask(101, task.TaskID, 2500)
	require.NoError(t, err)
	require.True(t, recovered)
	assert.Equal(t, UserToolImageTaskStatusCompleted, recoveredTask.Status)
	assert.Equal(t, UserToolImageTaskBillingStatusSettled, recoveredTask.BillingStatus)
	assert.Equal(t, int64(140000), recoveredTask.PreConsumedQuota)
	assert.Equal(t, int64(140000), recoveredTask.SettledQuota)
	assert.Equal(t, "pgf_recovered", recoveredTask.ResultItemID)
	assert.JSONEq(t, string(resultSnapshot), string(recoveredTask.ResultSnapshot))
	assert.Empty(t, recoveredTask.LeaseOwner)
	assert.Zero(t, recoveredTask.LeaseUntil)
	assert.Equal(t, int64(2500), recoveredTask.FinishedAt)
	assert.Equal(t, int64(2500), recoveredTask.UpdatedAt)

	repeated, recovered, err := CompleteSettledPreparedUserToolImageTask(101, task.TaskID, 3000)
	require.NoError(t, err)
	assert.False(t, recovered)
	assert.Equal(t, UserToolImageTaskStatusCompleted, repeated.Status)
	assert.Equal(t, int64(2500), repeated.FinishedAt)
	assert.Equal(t, int64(2500), repeated.UpdatedAt)
	assert.Equal(t, int64(140000), repeated.SettledQuota)
}

func TestCompleteSettledPreparedUserToolImageTaskRequiresValidSettledFinalizingResult(t *testing.T) {
	testCases := []struct {
		name          string
		status        string
		billingStatus string
		result        JSONValue
		wantRecovered bool
	}{
		{
			name:          "valid base64 image",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[{"b64_json":"aW1hZ2U="}]}`),
			wantRecovered: true,
		},
		{
			name:          "empty snapshot",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
		},
		{
			name:          "malformed snapshot",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[`),
		},
		{
			name:          "empty image data",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[]}`),
		},
		{
			name:          "usage without image data",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"usage":{"input_tokens":10}}`),
		},
		{
			name:          "revised prompt without image",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[{"revised_prompt":"safe prompt"}]}`),
		},
		{
			name:          "blank image fields",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[{"url":"  ","b64_json":"\t"}]}`),
		},
		{
			name:          "running task",
			status:        UserToolImageTaskStatusRunning,
			billingStatus: UserToolImageTaskBillingStatusSettled,
			result:        JSONValue(`{"data":[{"url":"/pg/image-files/pgf_running/content"}]}`),
		},
		{
			name:          "pre consumed billing",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusPreConsumed,
			result:        JSONValue(`{"data":[{"url":"/pg/image-files/pgf_pre_consumed/content"}]}`),
		},
		{
			name:          "refund pending billing",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusRefundPending,
			result:        JSONValue(`{"data":[{"url":"/pg/image-files/pgf_refund_pending/content"}]}`),
		},
		{
			name:          "refund failed billing",
			status:        UserToolImageTaskStatusFinalizing,
			billingStatus: UserToolImageTaskBillingStatusRefundFailed,
			result:        JSONValue(`{"data":[{"url":"/pg/image-files/pgf_refund_failed/content"}]}`),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
				UserID:          101,
				Tool:            UserToolImagePlayground,
				ClientTaskID:    "recover-eligibility-" + testCase.name,
				RequestSnapshot: JSONValue(`{"prompt":"eligibility"}`),
			})
			require.NoError(t, err)

			require.NoError(t, DB.Model(&UserToolImageTask{}).Where("task_id = ?", task.TaskID).Updates(map[string]any{
				"status":          testCase.status,
				"billing_status":  testCase.billingStatus,
				"result_snapshot": testCase.result,
				"lease_owner":     "worker-before-restart",
				"lease_until":     1,
			}).Error)

			updated, recovered, err := CompleteSettledPreparedUserToolImageTask(101, task.TaskID, 2500)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantRecovered, recovered)
			if testCase.wantRecovered {
				assert.Equal(t, UserToolImageTaskStatusCompleted, updated.Status)
				assert.Empty(t, updated.LeaseOwner)
				assert.Zero(t, updated.LeaseUntil)
				assert.Equal(t, int64(2500), updated.FinishedAt)
				return
			}
			assert.Equal(t, testCase.status, updated.Status)
			assert.Equal(t, testCase.billingStatus, updated.BillingStatus)
			assert.Equal(t, "worker-before-restart", updated.LeaseOwner)
			assert.Equal(t, int64(1), updated.LeaseUntil)
			assert.Zero(t, updated.FinishedAt)
		})
	}
}

func TestUserToolImageTaskPreparedCompletionCanFailWithoutRetryingGeneration(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "prepared-failure-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"billing fails"}`),
	})
	require.NoError(t, err)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, PrepareUserToolImageTaskCompletion(
		task.TaskID,
		"worker-a",
		"pgf_prepared",
		JSONValue(`{"data":[{"url":"/api/playground/files/pgf_prepared/content"}]}`),
		2000,
	))

	failed, err := FailUserToolImageTaskTerminal(
		task.TaskID,
		"worker-a",
		"playground_image_settle_billing",
		"failed to settle playground image billing",
		2500,
	)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusFailed, failed.Status)
	assert.Equal(t, "playground_image_settle_billing", failed.ErrorCode)

	_, claimed, err = ClaimUserToolImageTask(task.TaskID, "worker-b", 5000, 4000)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestUserToolImageTaskTerminalFailureDoesNotRetry(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "terminal-failure-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"fail once"}`),
	})
	require.NoError(t, err)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)

	failed, err := FailUserToolImageTaskTerminal(
		task.TaskID,
		"worker-a",
		"playground_image_persist_image",
		"failed to persist playground image: HTTP 404",
		2000,
	)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusFailed, failed.Status)
	assert.Equal(t, "playground_image_persist_image", failed.ErrorCode)
	assert.Equal(t, "failed to persist playground image: HTTP 404", failed.ErrorMessage)
	assert.Equal(t, int64(0), failed.LeaseUntil)

	ready, err := ListReadyUserToolImageTasks(20, 4000)
	require.NoError(t, err)
	assert.Empty(t, ready)

	_, claimed, err = ClaimUserToolImageTask(task.TaskID, "worker-b", 5000, 4000)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestGetUserToolImageTaskIsOwnerScoped(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "owner-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"owner only"}`),
	})
	require.NoError(t, err)

	_, err = GetUserToolImageTask(202, task.TaskID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	owned, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, task.TaskID, owned.TaskID)
}

func TestUserToolImageTaskFailureRetriesAndStopsAtMaximumAttempts(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "retry-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"retry"}`),
	})
	require.NoError(t, err)

	for attempt := 1; attempt <= UserToolImageTaskDefaultMaxAttempts; attempt++ {
		claimedTask, claimed, claimErr := ClaimUserToolImageTask(task.TaskID, "worker", int64(attempt*2000), int64((attempt-1)*2000+1))
		require.NoError(t, claimErr)
		require.True(t, claimed)
		assert.Equal(t, attempt, claimedTask.Attempt)

		failed, failErr := FailUserToolImageTask(task.TaskID, "worker", "upstream_error", "provider failed", int64(attempt*2000-1))
		require.NoError(t, failErr)
		if attempt < UserToolImageTaskDefaultMaxAttempts {
			assert.Equal(t, UserToolImageTaskStatusPending, failed.Status)
		} else {
			assert.Equal(t, UserToolImageTaskStatusFailed, failed.Status)
		}
	}

	ready, err := ListReadyUserToolImageTasks(20, 10000)
	require.NoError(t, err)
	assert.Empty(t, ready)

	finalTask, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusFailed, finalTask.Status)
	assert.Equal(t, "upstream_error", finalTask.ErrorCode)
}

func TestUserToolImageTaskCancelIsOwnerScopedAndTerminal(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "cancel-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"cancel"}`),
	})
	require.NoError(t, err)

	cancelled, err := CancelUserToolImageTask(202, task.TaskID, 1000)
	require.NoError(t, err)
	assert.False(t, cancelled)

	cancelled, err = CancelUserToolImageTask(101, task.TaskID, 1001)
	require.NoError(t, err)
	assert.True(t, cancelled)

	finalTask, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskStatusCancelled, finalTask.Status)

	cancelled, err = CancelUserToolImageTask(101, task.TaskID, 1002)
	require.NoError(t, err)
	assert.False(t, cancelled)

	_, claimed, err := ClaimUserToolImageTask(task.TaskID, "worker", 2000, 1002)
	require.NoError(t, err)
	assert.False(t, claimed)

	assert.False(t, errors.Is(ErrUserToolImageTaskLeaseLost, ErrUserToolImageTaskIdempotencyConflict))
}

func TestUserToolImageTaskBillingAuditSettlesIdempotently(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "billing-settle-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"settle once"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusPending, task.BillingStatus)
	assert.Zero(t, task.PreConsumedQuota)
	assert.Zero(t, task.SettledQuota)
	assert.Zero(t, task.RefundedQuota)

	require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-billing-1", 140000, 1000))
	preConsumed, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusPreConsumed, preConsumed.BillingStatus)
	assert.Equal(t, "request-billing-1", preConsumed.BillingRequestID)
	assert.Equal(t, int64(140000), preConsumed.PreConsumedQuota)
	assert.Equal(t, int64(1000), preConsumed.UpdatedAt)

	require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-billing-1", 140000, 1100))
	preConsumedAgain, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, int64(140000), preConsumedAgain.PreConsumedQuota)
	assert.Equal(t, int64(1000), preConsumedAgain.UpdatedAt)

	assert.ErrorIs(
		t,
		MarkUserToolImageTaskPreConsumed(task.TaskID, "request-billing-1", 280000, 1200),
		ErrUserToolImageTaskBillingConflict,
	)
	assert.ErrorIs(
		t,
		MarkUserToolImageTaskPreConsumed(task.TaskID, "request-billing-2", 140000, 1200),
		ErrUserToolImageTaskBillingConflict,
	)

	require.NoError(t, MarkUserToolImageTaskSettled(task.TaskID, 175000, 2000))
	settled, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusSettled, settled.BillingStatus)
	assert.Equal(t, int64(140000), settled.PreConsumedQuota)
	assert.Equal(t, int64(175000), settled.SettledQuota)
	assert.Zero(t, settled.RefundedQuota)
	assert.Equal(t, int64(2000), settled.UpdatedAt)

	require.NoError(t, MarkUserToolImageTaskSettled(task.TaskID, 175000, 2100))
	settledAgain, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, int64(175000), settledAgain.SettledQuota)
	assert.Equal(t, int64(2000), settledAgain.UpdatedAt)

	assert.ErrorIs(t, MarkUserToolImageTaskSettled(task.TaskID, 350000, 2200), ErrUserToolImageTaskBillingConflict)
	unchanged, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, int64(175000), unchanged.SettledQuota)
}

func TestUserToolImageTaskBillingAuditRefundRetryIsIdempotent(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "billing-refund-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"refund once"}`),
	})
	require.NoError(t, err)
	require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-refund-1", 140000, 1000))

	require.NoError(t, MarkUserToolImageTaskRefundPending(task.TaskID, 2000))
	require.NoError(t, MarkUserToolImageTaskRefundPending(task.TaskID, 2100))
	refundPending, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusRefundPending, refundPending.BillingStatus)
	assert.Equal(t, int64(2000), refundPending.UpdatedAt)

	require.NoError(t, MarkUserToolImageTaskRefundFailed(task.TaskID, 3000))
	require.NoError(t, MarkUserToolImageTaskRefundFailed(task.TaskID, 3100))
	refundFailed, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusRefundFailed, refundFailed.BillingStatus)
	assert.Equal(t, int64(3000), refundFailed.UpdatedAt)
	assert.Zero(t, refundFailed.RefundedQuota)

	require.NoError(t, MarkUserToolImageTaskRefundPending(task.TaskID, 4000))
	require.NoError(t, MarkUserToolImageTaskRefunded(task.TaskID, 140000, 5000))
	require.NoError(t, MarkUserToolImageTaskRefunded(task.TaskID, 140000, 5100))
	refunded, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusRefunded, refunded.BillingStatus)
	assert.Equal(t, int64(140000), refunded.PreConsumedQuota)
	assert.Zero(t, refunded.SettledQuota)
	assert.Equal(t, int64(140000), refunded.RefundedQuota)
	assert.Equal(t, int64(5000), refunded.UpdatedAt)

	assert.ErrorIs(t, MarkUserToolImageTaskRefunded(task.TaskID, 280000, 5200), ErrUserToolImageTaskBillingConflict)
	assert.ErrorIs(t, MarkUserToolImageTaskRefundPending(task.TaskID, 5300), ErrUserToolImageTaskBillingConflict)
	unchanged, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, int64(140000), unchanged.RefundedQuota)
}

func TestUserToolImageTaskBillingPreConsumeCompensationIsAtomicAndIdempotent(t *testing.T) {
	for _, terminal := range []string{
		UserToolImageTaskBillingStatusRefundFailed,
		UserToolImageTaskBillingStatusRefunded,
	} {
		for _, source := range []string{
			UserToolImageTaskBillingStatusPending,
			UserToolImageTaskBillingStatusPreConsumed,
			UserToolImageTaskBillingStatusRefundPending,
		} {
			t.Run(source+"_to_"+terminal, func(t *testing.T) {
				truncateTables(t)
				task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
					UserID:          101,
					Tool:            UserToolImagePlayground,
					ClientTaskID:    source + "-" + terminal,
					RequestSnapshot: JSONValue(`{"prompt":"compensate"}`),
				})
				require.NoError(t, err)
				if source != UserToolImageTaskBillingStatusPending {
					require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-compensate", 140000, 1000))
				}
				if source == UserToolImageTaskBillingStatusRefundPending {
					require.NoError(t, MarkUserToolImageTaskRefundPending(task.TaskID, 1100))
				}

				if terminal == UserToolImageTaskBillingStatusRefunded {
					err = MarkUserToolImageTaskPreConsumeRefunded(task.TaskID, "request-compensate", 140000, 2000)
				} else {
					err = MarkUserToolImageTaskPreConsumeRefundFailed(task.TaskID, "request-compensate", 140000, 2000)
				}
				require.NoError(t, err)

				updated, err := GetUserToolImageTask(101, task.TaskID)
				require.NoError(t, err)
				assert.Equal(t, terminal, updated.BillingStatus)
				assert.Equal(t, "request-compensate", updated.BillingRequestID)
				assert.Equal(t, int64(140000), updated.PreConsumedQuota)
				if terminal == UserToolImageTaskBillingStatusRefunded {
					assert.Equal(t, int64(140000), updated.RefundedQuota)
				} else {
					assert.Zero(t, updated.RefundedQuota)
				}

				if terminal == UserToolImageTaskBillingStatusRefunded {
					err = MarkUserToolImageTaskPreConsumeRefunded(task.TaskID, "request-compensate", 140000, 2100)
				} else {
					err = MarkUserToolImageTaskPreConsumeRefundFailed(task.TaskID, "request-compensate", 140000, 2100)
				}
				require.NoError(t, err)
				assert.ErrorIs(t, MarkUserToolImageTaskPreConsumeRefunded(task.TaskID, "other-request", 140000, 2200), ErrUserToolImageTaskBillingConflict)
				assert.ErrorIs(t, MarkUserToolImageTaskPreConsumeRefundFailed(task.TaskID, "request-compensate", 280000, 2200), ErrUserToolImageTaskBillingConflict)
			})
		}
	}
}

func TestUserToolImageTaskBillingAuditRejectsInvalidTransitionsAndAmounts(t *testing.T) {
	truncateTables(t)

	task, _, err := CreateOrGetUserToolImageTask(UserToolImageTaskInput{
		UserID:          101,
		Tool:            UserToolImagePlayground,
		ClientTaskID:    "billing-invalid-task-1",
		RequestSnapshot: JSONValue(`{"prompt":"invalid billing transition"}`),
	})
	require.NoError(t, err)

	assert.ErrorIs(t, MarkUserToolImageTaskSettled(task.TaskID, 100, 1000), ErrUserToolImageTaskBillingConflict)
	assert.ErrorIs(t, MarkUserToolImageTaskRefundPending(task.TaskID, 1000), ErrUserToolImageTaskBillingConflict)
	assert.ErrorIs(t, MarkUserToolImageTaskRefunded(task.TaskID, 100, 1000), ErrUserToolImageTaskBillingConflict)
	assert.ErrorIs(t, MarkUserToolImageTaskRefundFailed(task.TaskID, 1000), ErrUserToolImageTaskBillingConflict)
	require.Error(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-invalid", -1, 1000))
	require.Error(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "", 100, 1000))

	require.NoError(t, MarkUserToolImageTaskPreConsumed(task.TaskID, "request-invalid", 100, 2000))
	assert.ErrorIs(t, MarkUserToolImageTaskRefunded(task.TaskID, 100, 3000), ErrUserToolImageTaskBillingConflict)
	assert.ErrorIs(t, MarkUserToolImageTaskRefundFailed(task.TaskID, 3000), ErrUserToolImageTaskBillingConflict)
	require.NoError(t, MarkUserToolImageTaskSettled(task.TaskID, 80, 3000))
	require.NoError(t, MarkUserToolImageTaskRefundPending(task.TaskID, 4000))
	require.NoError(t, MarkUserToolImageTaskRefunded(task.TaskID, 80, 5000))

	finalTask, err := GetUserToolImageTask(101, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, UserToolImageTaskBillingStatusRefunded, finalTask.BillingStatus)
	assert.Equal(t, int64(100), finalTask.PreConsumedQuota)
	assert.Equal(t, int64(80), finalTask.SettledQuota)
	assert.Equal(t, int64(80), finalTask.RefundedQuota)
}
