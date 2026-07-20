package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		models = append(models, &model.SystemTask{}, &model.SystemTaskLock{})
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
