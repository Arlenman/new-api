package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveLogsAreAdminOnly(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		TokenId:   11,
		Type:      LogTypeSensitive,
		CreatedAt: 100,
		Content:   "upstream content safety policy blocked request",
		RequestId: "req-sensitive",
		ModelName: "test-model",
		ChannelId: 2,
		TokenName: "client",
		Username:  "alice",
		Other:     `{"admin_info":{"sensitive_request":{"prompt":"secret prompt"}}}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		TokenId:   11,
		Type:      LogTypeError,
		CreatedAt: 101,
		Content:   "ordinary error",
		RequestId: "req-error",
	}).Error)

	adminLogs, adminTotal, err := GetAllLogs(LogTypeSensitive, 0, 0, "", "", "", 0, 10, 0, "", "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, adminTotal)
	require.Len(t, adminLogs, 1)
	assert.Contains(t, adminLogs[0].Other, "secret prompt")

	selfLogs, selfTotal, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, selfTotal)
	require.Len(t, selfLogs, 1)
	assert.Equal(t, LogTypeError, selfLogs[0].Type)

	explicitSelfLogs, explicitSelfTotal, err := GetUserLogs(1, LogTypeSensitive, 0, 0, "", "", 0, 10, "", "", "", "")
	require.NoError(t, err)
	assert.Zero(t, explicitSelfTotal)
	assert.Empty(t, explicitSelfLogs)

	tokenLogs, err := GetLogByTokenId(11)
	require.NoError(t, err)
	require.Len(t, tokenLogs, 1)
	assert.Equal(t, LogTypeError, tokenLogs[0].Type)
}

func TestRecordSensitiveRequestLogStoresAdminInfo(t *testing.T) {
	truncateTables(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("username", "alice")
	c.Set(common.RequestIdKey, "req-sensitive")
	c.Set(common.UpstreamRequestIdKey, "upstream-sensitive")

	err := RecordSensitiveRequestLog(c, 1, RecordSensitiveRequestLogParams{
		TokenId:   11,
		TokenName: "client",
		ModelName: "test-model",
		ChannelId: 2,
		Group:     "default",
		IsStream:  true,
		Content:   "Upstream content safety policy blocked request",
		SensitiveRequest: map[string]interface{}{
			"source": "upstream",
			"prompt": "[user]\nsecret prompt",
		},
	})
	require.NoError(t, err)

	var log Log
	require.NoError(t, LOG_DB.Where("type = ?", LogTypeSensitive).First(&log).Error)
	assert.Equal(t, "alice", log.Username)
	assert.Equal(t, "req-sensitive", log.RequestId)
	assert.Equal(t, "upstream-sensitive", log.UpstreamRequestId)
	assert.True(t, log.IsStream)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	sensitiveRequest, ok := adminInfo["sensitive_request"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "[user]\nsecret prompt", sensitiveRequest["prompt"])
}

func TestLogSensitiveRequestOptionUpdatesImmediatelyAndPersists(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
	original := setting.LogSensitiveRequestEnabled
	t.Cleanup(func() {
		setting.LogSensitiveRequestEnabled = original
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, "LogSensitiveRequestEnabled")
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, UpdateOption("LogSensitiveRequestEnabled", "true"))
	assert.True(t, setting.LogSensitiveRequestEnabled)
	assert.Equal(t, "true", common.OptionMap["LogSensitiveRequestEnabled"])

	var option Option
	require.NoError(t, DB.Where("key = ?", "LogSensitiveRequestEnabled").First(&option).Error)
	assert.Equal(t, "true", option.Value)

	require.NoError(t, UpdateOption("LogSensitiveRequestEnabled", "false"))
	assert.False(t, setting.LogSensitiveRequestEnabled)
}
