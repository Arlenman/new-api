package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSystemTaskPayload struct {
	TargetTimestamp int64 `json:"target_timestamp"`
	BatchSize       int   `json:"batch_size"`
}

type testSystemTaskState struct {
	Total     int64 `json:"total"`
	Processed int64 `json:"processed"`
	Progress  int   `json:"progress"`
	Remaining int64 `json:"remaining"`
}

func createLegacyPendingSystemTask(t *testing.T, taskType string) *SystemTask {
	t.Helper()
	taskID, err := GenerateSystemTaskID()
	require.NoError(t, err)
	task := &SystemTask{
		TaskID: taskID,
		Type:   taskType,
		Status: SystemTaskStatusPending,
	}
	require.NoError(t, DB.Create(task).Error)
	return task
}

func TestSystemTaskCreateAndActiveLifecycle(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	state := testSystemTaskState{}
	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, payload, state)
	require.NoError(t, err)
	require.NotNil(t, task.ActiveKey)
	assert.Equal(t, SystemTaskTypeLogCleanup, *task.ActiveKey)

	var decodedPayload testSystemTaskPayload
	require.NoError(t, task.DecodePayload(&decodedPayload))
	assert.Equal(t, payload, decodedPayload)

	activeTask, err := GetActiveSystemTask(SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.NotNil(t, activeTask)
	assert.Equal(t, task.TaskID, activeTask.TaskID)

	runnerID := "runner-a"
	claimedTask, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Positive(t, claimedTask.StartedAt)

	err = FinishSystemTask(claimedTask.TaskID, runnerID, SystemTaskStatusSucceeded, map[string]int64{"deleted_count": 0}, "")
	require.NoError(t, err)

	finishedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finishedTask)
	assert.Nil(t, finishedTask.ActiveKey)
	assert.Positive(t, finishedTask.CompletedAt)
	assert.GreaterOrEqual(t, finishedTask.CompletedAt, finishedTask.StartedAt)

	activeTask, err = GetActiveSystemTask(SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.Nil(t, activeTask)

	_, err = CreateSystemTask(SystemTaskTypeLogCleanup, payload, state)
	require.NoError(t, err)
}

func TestCreateClaimedSystemTaskCreatesRunningTaskAndLock(t *testing.T) {
	truncateTables(t)

	runnerID := "runner-immediate"
	task, err := CreateClaimedSystemTask(
		SystemTaskTypeUpstreamPriorityTest,
		testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100},
		testSystemTaskState{},
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, SystemTaskStatusRunning, task.Status)
	assert.Equal(t, runnerID, task.LockedBy)
	assert.Positive(t, task.StartedAt)
	require.NotNil(t, task.ActiveKey)
	assert.Equal(t, SystemTaskTypeUpstreamPriorityTest, *task.ActiveKey)

	var lock SystemTaskLock
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(&lock).Error)
	assert.Equal(t, SystemTaskTypeUpstreamPriorityTest, lock.Type)
	assert.Equal(t, runnerID, lock.LockedBy)

	var pendingCount int64
	require.NoError(t, DB.Model(&SystemTask{}).
		Where("task_id = ? AND status = ?", task.TaskID, SystemTaskStatusPending).
		Count(&pendingCount).Error)
	assert.Zero(t, pendingCount)

	require.NoError(t, FinishSystemTask(task.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""))
}

func TestListSystemTasksByTypesFiltersAndOrders(t *testing.T) {
	truncateTables(t)

	tasks := []*SystemTask{
		{TaskID: "systask_priority_scheduled", Type: SystemTaskTypeUpstreamPrioritySync, Status: SystemTaskStatusSucceeded},
		{TaskID: "systask_unrelated", Type: SystemTaskTypeLogCleanup, Status: SystemTaskStatusSucceeded},
		{TaskID: "systask_priority_manual", Type: SystemTaskTypeUpstreamPriorityTest, Status: SystemTaskStatusFailed},
	}
	for _, task := range tasks {
		require.NoError(t, DB.Create(task).Error)
	}

	listed, err := ListSystemTasksByTypes([]string{
		SystemTaskTypeUpstreamPrioritySync,
		SystemTaskTypeUpstreamPriorityTest,
	}, 50)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, "systask_priority_manual", listed[0].TaskID)
	assert.Equal(t, "systask_priority_scheduled", listed[1].TaskID)
}

func TestSystemTaskActiveKeyPreventsDuplicateActiveRun(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.NoError(t, err)
	_, err = CreateSystemTask(SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.Error(t, err)

	activeTask, err := GetActiveSystemTask(SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.NotNil(t, activeTask)
	assert.Equal(t, task.TaskID, activeTask.TaskID)
}

func TestSystemTaskLockPreventsConcurrentClaim(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.NoError(t, err)
	secondTask := createLegacyPendingSystemTask(t, SystemTaskTypeLogCleanup)

	claimedTask, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	_, claimed, err = ClaimSystemTask(secondTask.ID, SystemTaskTypeLogCleanup, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.False(t, claimed)

	assert.Equal(t, "runner-a", claimedTask.LockedBy)

	reloadedSecond, err := GetSystemTaskByTaskID(secondTask.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedSecond)
	assert.Equal(t, SystemTaskStatusPending, reloadedSecond.Status)
}

func TestExpiredSystemTaskLockFailsOldRunAndClaimsLegacyPendingRun(t *testing.T) {
	truncateTables(t)

	first, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(first.ID, SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, DB.Model(&SystemTaskLock{}).
		Where("task_id = ?", first.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	second := createLegacyPendingSystemTask(t, SystemTaskTypeLogCleanup)
	claimedTask, claimed, err := ClaimSystemTask(second.ID, SystemTaskTypeLogCleanup, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, second.TaskID, claimedTask.TaskID)
	assert.Equal(t, "runner-b", claimedTask.LockedBy)

	reloadedFirst, err := GetSystemTaskByTaskID(first.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedFirst)
	assert.Equal(t, SystemTaskStatusFailed, reloadedFirst.Status)
	assert.Equal(t, "task lease expired", reloadedFirst.Error)
	assert.Nil(t, reloadedFirst.ActiveKey)
}

func TestExpireStaleSystemTaskLockFailsOldRunAndAllowsNewRun(t *testing.T) {
	truncateTables(t)

	first, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(first.ID, SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, DB.Model(&SystemTaskLock{}).
		Where("task_id = ?", first.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	require.NoError(t, ExpireStaleSystemTaskLocks(common.GetTimestamp()))

	reloadedFirst, err := GetSystemTaskByTaskID(first.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedFirst)
	assert.Equal(t, SystemTaskStatusFailed, reloadedFirst.Status)
	assert.Equal(t, "task lease expired", reloadedFirst.Error)
	assert.Nil(t, reloadedFirst.ActiveKey)

	var lockCount int64
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("task_id = ?", first.TaskID).Count(&lockCount).Error)
	assert.Equal(t, int64(0), lockCount)

	second, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, first.TaskID, second.TaskID)
}

func TestFindEarliestPendingSystemTasks(t *testing.T) {
	truncateTables(t)

	empty, err := FindEarliestPendingSystemTasks(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	firstA, err := CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)
	ignoredB, err := CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(ignoredB.ID, "type_b", "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(ignoredB.TaskID, "runner-b", SystemTaskStatusFailed, nil, "failed"))
	firstB, err := CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	ignoredC, err := CreateSystemTask("type_c", nil, nil)
	require.NoError(t, err)
	_, claimed, err = ClaimSystemTask(ignoredC.ID, "type_c", "runner-c", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(ignoredC.TaskID, "runner-c", SystemTaskStatusFailed, nil, "failed"))

	tasks, err := FindEarliestPendingSystemTasks([]string{"type_a", "type_b", "type_c", "missing"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, firstA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, firstB.TaskID, tasks["type_b"].TaskID)
	assert.Nil(t, tasks["type_c"])
	assert.Nil(t, tasks["missing"])
}

func TestGetLatestSystemTask(t *testing.T) {
	truncateTables(t)

	latest, err := GetLatestSystemTask(SystemTaskTypeChannelTest)
	require.NoError(t, err)
	require.Nil(t, latest)

	first, err := CreateSystemTask(SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := ClaimSystemTask(first.ID, SystemTaskTypeChannelTest, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(first.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""))

	second, err := CreateSystemTask(SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	latest, err = GetLatestSystemTask(SystemTaskTypeChannelTest)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.TaskID, latest.TaskID)
}

func TestGetLatestSystemTasks(t *testing.T) {
	truncateTables(t)

	empty, err := GetLatestSystemTasks(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	firstA, err := CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)
	firstB, err := CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(firstA.ID, "type_a", "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(firstA.TaskID, "runner-a", SystemTaskStatusSucceeded, nil, ""))
	secondA, err := CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)

	tasks, err := GetLatestSystemTasks([]string{"type_a", "type_b", "missing"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.NotEqual(t, firstA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, secondA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, firstB.TaskID, tasks["type_b"].TaskID)
	assert.Nil(t, tasks["missing"])
}

func TestRenewSystemTaskLock(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	newLockUntil := common.GetTimestamp() + 600
	require.NoError(t, RenewSystemTaskLock(task.TaskID, runnerID, newLockUntil))

	var lock SystemTaskLock
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(&lock).Error)
	assert.Equal(t, newLockUntil, lock.LockedUntil)

	// A different runner cannot renew a lease it does not hold.
	assert.ErrorIs(t, RenewSystemTaskLock(task.TaskID, "runner-b", common.GetTimestamp()+600), ErrSystemTaskLockLost)

	// After the task finishes it is no longer running, so renew fails.
	require.NoError(t, FinishSystemTask(task.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""))
	assert.ErrorIs(t, RenewSystemTaskLock(task.TaskID, runnerID, common.GetTimestamp()+600), ErrSystemTaskLockLost)
}

func TestFinishSystemTaskRetainsExecutor(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "node-1-abc123"
	_, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, FinishSystemTask(task.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""))

	reloaded, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, SystemTaskStatusSucceeded, reloaded.Status)
	assert.Equal(t, runnerID, reloaded.LockedBy, "executor-of-record must be retained for history")

	var lockCount int64
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Equal(t, int64(0), lockCount)
}

func TestSystemTaskUpdatesRequireCurrentLock(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, DB.Model(&SystemTaskLock{}).
		Where("task_id = ?", task.TaskID).
		Updates(map[string]any{"locked_by": "runner-b"}).Error)

	assert.ErrorIs(t, UpdateSystemTaskState(task.TaskID, runnerID, testSystemTaskState{Progress: 10}), ErrSystemTaskLockLost)
	assert.ErrorIs(t, FinishSystemTask(task.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""), ErrSystemTaskLockLost)
}

func TestSystemTaskUpdatesRequireUnexpiredLock(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := ClaimSystemTask(task.ID, SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, DB.Model(&SystemTaskLock{}).
		Where("task_id = ?", task.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	assert.ErrorIs(t, UpdateSystemTaskState(task.TaskID, runnerID, testSystemTaskState{Progress: 10}), ErrSystemTaskLockLost)
	assert.ErrorIs(t, FinishSystemTask(task.TaskID, runnerID, SystemTaskStatusSucceeded, nil, ""), ErrSystemTaskLockLost)

	reloaded, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, SystemTaskStatusRunning, reloaded.Status)
	assert.Empty(t, reloaded.State)
}

func TestListSystemTasksByTypesPaginated(t *testing.T) {
	truncateTables(t)
	for index := 1; index <= 5; index++ {
		require.NoError(t, DB.Create(&SystemTask{
			TaskID: "systask_page_" + string(rune('0'+index)),
			Type:   SystemTaskTypeUpstreamPrioritySync,
			Status: SystemTaskStatusSucceeded,
		}).Error)
	}
	require.NoError(t, DB.Create(&SystemTask{TaskID: "systask_other", Type: SystemTaskTypeLogCleanup, Status: SystemTaskStatusSucceeded}).Error)

	tasks, total, err := ListSystemTasksByTypesPaginated([]string{SystemTaskTypeUpstreamPrioritySync}, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, tasks, 2)
	assert.Equal(t, "systask_page_3", tasks[0].TaskID)
	assert.Equal(t, "systask_page_2", tasks[1].TaskID)
}

func TestDeleteCompletedSystemTasksByTypesPhysicallyDeletesOnlyTargetTasks(t *testing.T) {
	truncateTables(t)
	target := &SystemTask{TaskID: "systask_delete_target", Type: SystemTaskTypeUpstreamPrioritySync, Status: SystemTaskStatusSucceeded}
	unrelated := &SystemTask{TaskID: "systask_keep_other", Type: SystemTaskTypeLogCleanup, Status: SystemTaskStatusSucceeded}
	require.NoError(t, DB.Create(target).Error)
	require.NoError(t, DB.Create(unrelated).Error)
	require.NoError(t, DB.Create(&SystemTaskLock{Type: SystemTaskTypeUpstreamPrioritySync, TaskID: target.TaskID, LockedBy: "old"}).Error)

	deleted, err := DeleteCompletedSystemTasksByTypes([]string{SystemTaskTypeUpstreamPrioritySync, SystemTaskTypeUpstreamPriorityTest})
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	var targetCount int64
	require.NoError(t, DB.Unscoped().Model(&SystemTask{}).Where("task_id = ?", target.TaskID).Count(&targetCount).Error)
	assert.Zero(t, targetCount)
	var lockCount int64
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("task_id = ?", target.TaskID).Count(&lockCount).Error)
	assert.Zero(t, lockCount)
	var unrelatedCount int64
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id = ?", unrelated.TaskID).Count(&unrelatedCount).Error)
	assert.Equal(t, int64(1), unrelatedCount)
}

func TestDeleteCompletedSystemTasksByTypesRejectsActiveTasks(t *testing.T) {
	truncateTables(t)
	activeType := SystemTaskTypeUpstreamPrioritySync
	active := &SystemTask{TaskID: "systask_active", Type: activeType, Status: SystemTaskStatusRunning, ActiveKey: &activeType}
	completed := &SystemTask{TaskID: "systask_completed", Type: SystemTaskTypeUpstreamPriorityTest, Status: SystemTaskStatusFailed}
	require.NoError(t, DB.Create(active).Error)
	require.NoError(t, DB.Create(completed).Error)

	deleted, err := DeleteCompletedSystemTasksByTypes([]string{SystemTaskTypeUpstreamPrioritySync, SystemTaskTypeUpstreamPriorityTest})
	require.ErrorIs(t, err, ErrActiveSystemTasks)
	assert.Zero(t, deleted)
	var count int64
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id IN ?", []string{active.TaskID, completed.TaskID}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
