package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
