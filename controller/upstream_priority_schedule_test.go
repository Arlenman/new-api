package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	upstreamPriorityEnabledOptionKey  = "upstream_priority_setting.enabled"
	upstreamPriorityIntervalOptionKey = "upstream_priority_setting.interval_seconds"
	upstreamPriorityLatencyOptionKey  = "upstream_priority_setting.max_test_latency_seconds"
)

type upstreamPriorityScheduleResponse struct {
	Success bool                                      `json:"success"`
	Data    operation_setting.UpstreamPrioritySetting `json:"data"`
}

type upstreamPriorityScheduleRunResponse struct {
	Success bool                       `json:"success"`
	Message string                     `json:"message"`
	Data    upstreamPriorityTaskRecord `json:"data"`
}

type upstreamPriorityScheduleTaskListResponse struct {
	Success bool                     `json:"success"`
	Message string                   `json:"message"`
	Data    upstreamPriorityTaskPage `json:"data"`
}

func setupUpstreamPriorityScheduleTest(t *testing.T, migrateSystemTasks bool) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalSetting := *operation_setting.GetUpstreamPrioritySetting()

	optionKeys := []string{
		upstreamPriorityEnabledOptionKey,
		upstreamPriorityIntervalOptionKey,
		upstreamPriorityLatencyOptionKey,
	}
	originalOptions := make(map[string]string, len(optionKeys))
	originalOptionPresence := make(map[string]bool, len(optionKeys))
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	for _, key := range optionKeys {
		originalOptions[key], originalOptionPresence[key] = common.OptionMap[key]
		delete(common.OptionMap, key)
	}
	common.OptionMapRWMutex.Unlock()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-priority-schedule.db")), &gorm.Config{})
	require.NoError(t, err)
	models := []any{&model.Option{}}
	if migrateSystemTasks {
		models = append(models, &model.SystemTask{}, &model.SystemTaskLock{}, &model.UpstreamChannel{})
	}
	require.NoError(t, db.AutoMigrate(models...))
	model.DB = db
	model.LOG_DB = db
	*operation_setting.GetUpstreamPrioritySetting() = operation_setting.UpstreamPrioritySetting{
		Enabled:               false,
		IntervalSeconds:       300,
		MaxTestLatencySeconds: 5,
	}

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		*operation_setting.GetUpstreamPrioritySetting() = originalSetting
		common.OptionMapRWMutex.Lock()
		for _, key := range optionKeys {
			if originalOptionPresence[key] {
				common.OptionMap[key] = originalOptions[key]
			} else {
				delete(common.OptionMap, key)
			}
		}
		common.OptionMapRWMutex.Unlock()
	})
	return db
}

func performUpstreamPriorityScheduleRequest(t *testing.T, method string, requestBody *updateUpstreamPriorityScheduleRequest) upstreamPriorityScheduleResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	var request *http.Request
	if requestBody == nil {
		request = httptest.NewRequest(method, "/api/upstream-channels/priority-schedule", nil)
	} else {
		body, err := common.Marshal(requestBody)
		require.NoError(t, err)
		request = httptest.NewRequest(method, "/api/upstream-channels/priority-schedule", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	context.Request = request
	if method == http.MethodGet {
		GetUpstreamPrioritySchedule(context)
	} else {
		UpdateUpstreamPrioritySchedule(context)
	}

	require.Equal(t, http.StatusOK, recorder.Code)
	var response upstreamPriorityScheduleResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func performUpstreamPriorityScheduleRunRequest(t *testing.T) (int, upstreamPriorityScheduleRunResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/upstream-channels/priority-schedule/run", nil)
	RunUpstreamPrioritySchedule(context)

	var response upstreamPriorityScheduleRunResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder.Code, response
}

func performUpstreamPriorityScheduleTaskListRequest(t *testing.T) upstreamPriorityScheduleTaskListResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/upstream-channels/priority-schedule/tasks?page=1&page_size=50", nil)
	ListUpstreamPriorityScheduleTasks(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response upstreamPriorityScheduleTaskListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestGetUpstreamPriorityScheduleReturnsCurrentSetting(t *testing.T) {
	setupUpstreamPriorityScheduleTest(t, true)
	*operation_setting.GetUpstreamPrioritySetting() = operation_setting.UpstreamPrioritySetting{
		Enabled:               true,
		IntervalSeconds:       720,
		MaxTestLatencySeconds: 9,
	}

	response := performUpstreamPriorityScheduleRequest(t, http.MethodGet, nil)

	require.True(t, response.Success)
	assert.Equal(t, operation_setting.UpstreamPrioritySetting{
		Enabled:               true,
		IntervalSeconds:       720,
		MaxTestLatencySeconds: 9,
	}, response.Data)
}

func TestUpdateUpstreamPrioritySchedulePersistsAndEnqueuesOnlyOnFirstEnable(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)

	response := performUpstreamPriorityScheduleRequest(t, http.MethodPut, &updateUpstreamPriorityScheduleRequest{
		Enabled:               true,
		IntervalSeconds:       600,
		MaxTestLatencySeconds: 7,
	})

	require.True(t, response.Success)
	assert.Equal(t, operation_setting.UpstreamPrioritySetting{
		Enabled:               true,
		IntervalSeconds:       600,
		MaxTestLatencySeconds: 7,
	}, *operation_setting.GetUpstreamPrioritySetting())
	var tasks []model.SystemTask
	require.NoError(t, db.Where("type = ?", model.SystemTaskTypeUpstreamPrioritySync).Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, model.SystemTaskStatusPending, tasks[0].Status)

	response = performUpstreamPriorityScheduleRequest(t, http.MethodPut, &updateUpstreamPriorityScheduleRequest{
		Enabled:               true,
		IntervalSeconds:       900,
		MaxTestLatencySeconds: 8,
	})

	require.True(t, response.Success)
	assert.Equal(t, 900, operation_setting.GetUpstreamPrioritySetting().IntervalSeconds)
	assert.Equal(t, 8, operation_setting.GetUpstreamPrioritySetting().MaxTestLatencySeconds)
	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).Where("type = ?", model.SystemTaskTypeUpstreamPrioritySync).Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)

	expectedOptions := map[string]string{
		upstreamPriorityEnabledOptionKey:  "true",
		upstreamPriorityIntervalOptionKey: "900",
		upstreamPriorityLatencyOptionKey:  "8",
	}
	for key, expected := range expectedOptions {
		var option model.Option
		require.NoError(t, db.First(&option, "key = ?", key).Error)
		assert.Equal(t, expected, option.Value)
	}
}

func TestUpdateUpstreamPriorityScheduleRejectsOutOfRangeValues(t *testing.T) {
	setupUpstreamPriorityScheduleTest(t, true)
	tests := []struct {
		name    string
		request updateUpstreamPriorityScheduleRequest
	}{
		{
			name: "interval below minimum",
			request: updateUpstreamPriorityScheduleRequest{
				Enabled:               true,
				IntervalSeconds:       operation_setting.UpstreamPriorityMinIntervalSeconds - 1,
				MaxTestLatencySeconds: 5,
			},
		},
		{
			name: "interval above maximum",
			request: updateUpstreamPriorityScheduleRequest{
				Enabled:               true,
				IntervalSeconds:       operation_setting.UpstreamPriorityMaxIntervalSeconds + 1,
				MaxTestLatencySeconds: 5,
			},
		},
		{
			name: "latency below minimum",
			request: updateUpstreamPriorityScheduleRequest{
				Enabled:               true,
				IntervalSeconds:       300,
				MaxTestLatencySeconds: operation_setting.UpstreamPriorityMinLatencySeconds - 1,
			},
		},
		{
			name: "latency above maximum",
			request: updateUpstreamPriorityScheduleRequest{
				Enabled:               true,
				IntervalSeconds:       300,
				MaxTestLatencySeconds: operation_setting.UpstreamPriorityMaxLatencySeconds + 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performUpstreamPriorityScheduleRequest(t, http.MethodPut, &test.request)
			assert.False(t, response.Success)
		})
	}

	assert.Equal(t, operation_setting.UpstreamPrioritySetting{
		Enabled:               false,
		IntervalSeconds:       300,
		MaxTestLatencySeconds: 5,
	}, *operation_setting.GetUpstreamPrioritySetting())
}

func TestUpdateUpstreamPriorityScheduleSucceedsWhenImmediateEnqueueFails(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, false)

	response := performUpstreamPriorityScheduleRequest(t, http.MethodPut, &updateUpstreamPriorityScheduleRequest{
		Enabled:               true,
		IntervalSeconds:       450,
		MaxTestLatencySeconds: 6,
	})

	require.True(t, response.Success)
	assert.Equal(t, operation_setting.UpstreamPrioritySetting{
		Enabled:               true,
		IntervalSeconds:       450,
		MaxTestLatencySeconds: 6,
	}, *operation_setting.GetUpstreamPrioritySetting())
	var optionCount int64
	require.NoError(t, db.Model(&model.Option{}).Count(&optionCount).Error)
	assert.Equal(t, int64(3), optionCount)
}

func TestRunUpstreamPriorityScheduleExecutesImmediatelyWhileScheduleDisabled(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)

	status, response := performUpstreamPriorityScheduleRunRequest(t)

	require.Equal(t, http.StatusOK, status)
	require.True(t, response.Success)
	assert.Equal(t, model.SystemTaskStatusSucceeded, response.Data.Status)
	assert.Equal(t, model.SystemTaskTypeUpstreamPriorityTest, response.Data.Type)
	assert.Equal(t, "manual", response.Data.Trigger)
	require.NotEmpty(t, response.Data.TaskID)
	assert.Positive(t, response.Data.StartedAt)
	assert.GreaterOrEqual(t, response.Data.CompletedAt, response.Data.StartedAt)

	var task model.SystemTask
	require.NoError(t, db.Where("task_id = ?", response.Data.TaskID).First(&task).Error)
	assert.Equal(t, model.SystemTaskTypeUpstreamPriorityTest, task.Type)
	assert.Equal(t, model.SystemTaskStatusSucceeded, task.Status)
	assert.Nil(t, task.ActiveKey)
	assert.Positive(t, task.StartedAt)
	assert.GreaterOrEqual(t, task.CompletedAt, task.StartedAt)
	payload := upstreamPrioritySyncTaskPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	assert.True(t, payload.Manual)

	var pendingCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).
		Where("type = ? AND status = ?", model.SystemTaskTypeUpstreamPriorityTest, model.SystemTaskStatusPending).
		Count(&pendingCount).Error)
	assert.Zero(t, pendingCount)
	var lockCount int64
	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Zero(t, lockCount)
}

func TestRunUpstreamPriorityScheduleRunsWhileScheduledTaskIsActive(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	existing, err := model.CreateSystemTask(model.SystemTaskTypeUpstreamPrioritySync, nil, nil)
	require.NoError(t, err)
	claimedScheduled, claimed, err := model.ClaimSystemTask(
		existing.ID,
		model.SystemTaskTypeUpstreamPrioritySync,
		"scheduled-runner",
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, claimedScheduled)

	status, response := performUpstreamPriorityScheduleRunRequest(t)

	require.Equal(t, http.StatusOK, status)
	require.True(t, response.Success)
	assert.Equal(t, model.SystemTaskStatusSucceeded, response.Data.Status)
	assert.Equal(t, model.SystemTaskTypeUpstreamPriorityTest, response.Data.Type)
	assert.NotEqual(t, existing.TaskID, response.Data.TaskID)

	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).
		Where("type IN ?", []string{model.SystemTaskTypeUpstreamPrioritySync, model.SystemTaskTypeUpstreamPriorityTest}).
		Count(&taskCount).Error)
	assert.Equal(t, int64(2), taskCount)

	reloadedScheduled, err := model.GetSystemTaskByTaskID(existing.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedScheduled)
	assert.Equal(t, model.SystemTaskStatusRunning, reloadedScheduled.Status)
	require.NoError(t, model.FinishSystemTask(existing.TaskID, "scheduled-runner", model.SystemTaskStatusSucceeded, nil, ""))
}

func TestRunUpstreamPriorityScheduleRejectsExistingManualTask(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	existing, err := model.CreateSystemTask(model.SystemTaskTypeUpstreamPriorityTest, upstreamPrioritySyncTaskPayload{Manual: true}, nil)
	require.NoError(t, err)

	status, response := performUpstreamPriorityScheduleRunRequest(t)

	require.Equal(t, http.StatusConflict, status)
	assert.False(t, response.Success)
	assert.Equal(t, existing.TaskID, response.Data.TaskID)
	assert.Contains(t, response.Message, "手动上游优先级测试正在执行")
	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).Where("type = ?", model.SystemTaskTypeUpstreamPriorityTest).Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)
}

func TestListUpstreamPriorityScheduleTasksReturnsExecutionRecords(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	now := common.GetTimestamp()
	resultJSON, err := common.Marshal(upstreamPrioritySyncSummary{
		Refreshed:       4,
		Ranked:          3,
		Tested:          2,
		Passed:          1,
		PriorityUpdated: 2,
		Skipped:         1,
		Errors:          []string{"one channel failed"},
	})
	require.NoError(t, err)
	legacyManualPayload, err := common.Marshal(upstreamPrioritySyncTaskPayload{Manual: true})
	require.NoError(t, err)

	tasks := []*model.SystemTask{
		{
			TaskID:      "systask_scheduled",
			Type:        model.SystemTaskTypeUpstreamPrioritySync,
			Status:      model.SystemTaskStatusSucceeded,
			Result:      string(resultJSON),
			CreatedAt:   now - 20,
			UpdatedAt:   now - 12,
			StartedAt:   now - 18,
			CompletedAt: now - 12,
		},
		{
			TaskID:    "systask_legacy_manual",
			Type:      model.SystemTaskTypeUpstreamPrioritySync,
			Status:    model.SystemTaskStatusFailed,
			Payload:   string(legacyManualPayload),
			Error:     "legacy failure",
			CreatedAt: now - 10,
			UpdatedAt: now - 4,
		},
		{
			TaskID:      "systask_manual",
			Type:        model.SystemTaskTypeUpstreamPriorityTest,
			Status:      model.SystemTaskStatusSucceeded,
			Result:      string(resultJSON),
			CreatedAt:   now - 3,
			UpdatedAt:   now - 1,
			StartedAt:   now - 3,
			CompletedAt: now - 1,
		},
		{
			TaskID:    "systask_unrelated",
			Type:      model.SystemTaskTypeLogCleanup,
			Status:    model.SystemTaskStatusSucceeded,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	for _, task := range tasks {
		require.NoError(t, db.Create(task).Error)
	}

	response := performUpstreamPriorityScheduleTaskListRequest(t)

	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 3)
	assert.Equal(t, 1, response.Data.Page)
	assert.Equal(t, 50, response.Data.PageSize)
	assert.Equal(t, int64(3), response.Data.Total)
	assert.Equal(t, "systask_manual", response.Data.Items[0].TaskID)
	assert.Equal(t, "manual", response.Data.Items[0].Trigger)
	require.NotNil(t, response.Data.Items[0].DurationMS)
	assert.Equal(t, int64(2000), *response.Data.Items[0].DurationMS)
	assert.Equal(t, "systask_legacy_manual", response.Data.Items[1].TaskID)
	assert.Equal(t, "manual", response.Data.Items[1].Trigger)
	assert.Equal(t, now-10, response.Data.Items[1].StartedAt)
	assert.Equal(t, now-4, response.Data.Items[1].CompletedAt)
	require.NotNil(t, response.Data.Items[1].DurationMS)
	assert.Equal(t, int64(6000), *response.Data.Items[1].DurationMS)
	assert.Equal(t, "legacy failure", response.Data.Items[1].Error)
	assert.Equal(t, "systask_scheduled", response.Data.Items[2].TaskID)
	assert.Equal(t, "scheduled", response.Data.Items[2].Trigger)
	require.NotNil(t, response.Data.Items[2].DurationMS)
	assert.Equal(t, int64(6000), *response.Data.Items[2].DurationMS)
	assert.NotNil(t, response.Data.Items[2].Result)
}

func TestBuildUpstreamPriorityTaskRecordLeavesPendingDurationEmpty(t *testing.T) {
	record := buildUpstreamPriorityTaskRecord(&model.SystemTask{
		TaskID:    "systask_pending",
		Type:      model.SystemTaskTypeUpstreamPrioritySync,
		Status:    model.SystemTaskStatusPending,
		CreatedAt: common.GetTimestamp(),
	})

	assert.Zero(t, record.StartedAt)
	assert.Zero(t, record.CompletedAt)
	assert.Nil(t, record.DurationMS)
}

func TestListUpstreamPriorityScheduleTasksAddsHostToLegacyErrors(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	channel := &model.UpstreamChannel{Name: "Legacy channel", BaseURL: "https://api.example.com/v1", Provider: "new-api"}
	require.NoError(t, db.Create(channel).Error)
	resultJSON, err := common.Marshal(map[string]any{
		"errors": []string{"upstream channel " + strconv.Itoa(channel.Id) + " refresh failed: unauthorized"},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.SystemTask{
		TaskID: "systask_legacy_host",
		Type:   model.SystemTaskTypeUpstreamPrioritySync,
		Status: model.SystemTaskStatusFailed,
		Result: string(resultJSON),
	}).Error)

	response := performUpstreamPriorityScheduleTaskListRequest(t)
	require.Len(t, response.Data.Items, 1)
	result, ok := response.Data.Items[0].Result.(map[string]any)
	require.True(t, ok)
	issues, ok := result["issues"].([]any)
	require.True(t, ok)
	require.Len(t, issues, 1)
	issue, ok := issues[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "api.example.com", issue["host"])
	assert.Equal(t, "Legacy channel", issue["channel_name"])
}

func TestClearUpstreamPriorityScheduleTasksDeletesCompletedRecords(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	require.NoError(t, db.Create(&model.SystemTask{TaskID: "systask_clear", Type: model.SystemTaskTypeUpstreamPrioritySync, Status: model.SystemTaskStatusSucceeded}).Error)
	require.NoError(t, db.Create(&model.SystemTask{TaskID: "systask_keep", Type: model.SystemTaskTypeLogCleanup, Status: model.SystemTaskStatusSucceeded}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/upstream-channels/priority-schedule/tasks", nil)
	ClearUpstreamPriorityScheduleTasks(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var targetCount int64
	require.NoError(t, db.Unscoped().Model(&model.SystemTask{}).Where("task_id = ?", "systask_clear").Count(&targetCount).Error)
	assert.Zero(t, targetCount)
	var unrelatedCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).Where("task_id = ?", "systask_keep").Count(&unrelatedCount).Error)
	assert.Equal(t, int64(1), unrelatedCount)
}

func TestClearUpstreamPriorityScheduleTasksRejectsActiveTask(t *testing.T) {
	db := setupUpstreamPriorityScheduleTest(t, true)
	activeType := model.SystemTaskTypeUpstreamPrioritySync
	require.NoError(t, db.Create(&model.SystemTask{TaskID: "systask_active_clear", Type: activeType, Status: model.SystemTaskStatusRunning, ActiveKey: &activeType}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/upstream-channels/priority-schedule/tasks", nil)
	ClearUpstreamPriorityScheduleTasks(context)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var count int64
	require.NoError(t, db.Model(&model.SystemTask{}).Where("task_id = ?", "systask_active_clear").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
