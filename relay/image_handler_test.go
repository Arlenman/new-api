package relay

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imageHandlerBillingTestSettler struct {
	mu               sync.Mutex
	preConsumedQuota int
	chargedQuota     int
	needsRefund      bool
	asyncRefundCalls int
	syncRefundCalls  int
}

func (s *imageHandlerBillingTestSettler) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chargedQuota = actualQuota
	s.needsRefund = false
	return nil
}

func (s *imageHandlerBillingTestSettler) Refund(_ *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncRefundCalls++
	s.chargedQuota = 0
	s.needsRefund = false
}

func (s *imageHandlerBillingTestSettler) RefundSync(_ *gin.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncRefundCalls++
	s.chargedQuota = 0
	s.needsRefund = false
	return nil
}

func (s *imageHandlerBillingTestSettler) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefund
}

func (s *imageHandlerBillingTestSettler) GetPreConsumedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preConsumedQuota
}

func (s *imageHandlerBillingTestSettler) GetChargedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chargedQuota
}

func (s *imageHandlerBillingTestSettler) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if targetQuota > s.preConsumedQuota {
		s.preConsumedQuota = targetQuota
		s.chargedQuota = targetQuota
		s.needsRefund = true
	}
	return nil
}

func (s *imageHandlerBillingTestSettler) refundCallCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asyncRefundCalls, s.syncRefundCalls
}

func setupImageHandlerTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDBType := common.MainDatabaseType()
	originalLogDBType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalStreamingTimeout := constant.StreamingTimeout

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "image-handler.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UserToolImageTask{}))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	constant.StreamingTimeout = 300
	service.InitHttpClient()

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.RedisEnabled = originalRedisEnabled
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.StreamingTimeout = originalStreamingTimeout
	})
	return db
}

func newImageHandlerTestContext(
	t *testing.T,
	upstreamURL string,
	taskID string,
	requestID string,
	channelID int,
	stream bool,
) (*gin.Context, *relaycommon.RelayInfo, *imageHandlerBillingTestSettler) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, requestID)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstreamURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-image-2")
	if taskID != "" {
		common.SetContextKey(c, constant.ContextKeyPlaygroundImageTaskID, taskID)
	}

	settler := &imageHandlerBillingTestSettler{
		preConsumedQuota: 30,
		chargedQuota:     30,
		needsRefund:      true,
	}
	request := &dto.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "draw a cat",
		Stream: common.GetPointer(stream),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelId:      channelID,
			ChannelBaseUrl: upstreamURL,
			ApiType:        constant.APITypeOpenAI,
			ApiKey:         "sk-test",
		},
		StartTime:               time.Now(),
		IsStream:                stream,
		IsPlayground:            taskID != "",
		RelayMode:               relayconstant.RelayModeImagesGenerations,
		OriginModelName:         "gpt-image-2",
		RequestURLPath:          "/v1/images/generations",
		RelayFormat:             types.RelayFormatOpenAIImage,
		FinalRequestRelayFormat: types.RelayFormatOpenAIImage,
		RequestId:               requestID,
		Request:                 request,
		Billing:                 settler,
	}
	return c, info, settler
}

func seedImageHandlerBillingTask(t *testing.T, db *gorm.DB, taskID, requestID string) {
	t.Helper()
	task := &model.UserToolImageTask{
		TaskID:          taskID,
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "client_" + taskID,
		RequestSnapshot: model.JSONValue(`{}`),
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, model.MarkUserToolImageTaskPreConsumed(taskID, requestID, 30, 0))
}

func loadImageHandlerBillingTask(t *testing.T, db *gorm.DB, taskID string) model.UserToolImageTask {
	t.Helper()
	var task model.UserToolImageTask
	require.NoError(t, db.Where("task_id = ?", taskID).First(&task).Error)
	return task
}

func TestImageHelperAdaptorFailuresKeepManagedBillingForCheckedRefund(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		stream      bool
	}{
		{
			name:        "json empty data",
			contentType: "application/json",
			body:        `{"data":[],"usage":{"input_tokens":444,"output_tokens":1756,"total_tokens":2200}}`,
		},
		{
			name:        "json usage only",
			contentType: "application/json",
			body:        `{"usage":{"input_tokens":444,"output_tokens":1756,"total_tokens":2200}}`,
		},
		{
			name:        "sse partial only",
			contentType: "text/event-stream",
			body: strings.Join([]string{
				`event: image_generation.partial_image`,
				`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			stream: true,
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupImageHandlerTestDatabase(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(upstream.Close)

			taskID := "uitask_image_handler_" + strings.ReplaceAll(tt.name, " ", "_")
			requestID := "req_image_handler_" + strings.ReplaceAll(tt.name, " ", "_")
			channelID := 168 + index
			seedImageHandlerBillingTask(t, db, taskID, requestID)
			c, info, settler := newImageHandlerTestContext(t, upstream.URL, taskID, requestID, channelID, tt.stream)

			relayErr := ImageHelper(c, info)
			require.NotNil(t, relayErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, relayErr.GetErrorCode())
			assert.Equal(t, channelID, info.ChannelMeta.ChannelId)

			info.Billing.Refund(c)
			asyncCalls, syncCalls := settler.refundCallCounts()
			assert.Zero(t, asyncCalls, "generic relay refund must not race the managed checked refund")
			assert.Zero(t, syncCalls)

			refunded, refundErr := service.RefundPlaygroundImageBillingChecked(c)
			require.NoError(t, refundErr)
			assert.True(t, refunded)
			asyncCalls, syncCalls = settler.refundCallCounts()
			assert.Zero(t, asyncCalls)
			assert.Equal(t, 1, syncCalls)

			task := loadImageHandlerBillingTask(t, db, taskID)
			assert.Equal(t, model.UserToolImageTaskBillingStatusRefunded, task.BillingStatus)
			assert.Equal(t, int64(30), task.RefundedQuota)

			refunded, refundErr = service.RefundPlaygroundImageBillingChecked(c)
			require.NoError(t, refundErr)
			assert.False(t, refunded)
			_, syncCalls = settler.refundCallCounts()
			assert.Equal(t, 1, syncCalls)
		})
	}
}

func TestImageHelperClientDisconnectWithoutManagedTaskKeepsOrdinaryRefund(t *testing.T) {
	c, info, settler := newImageHandlerTestContext(t, "https://example.invalid", "", "req_ordinary_disconnect", 301, true)
	originalBilling := info.Billing

	service.PreparePlaygroundImageBilling(c, info)

	assert.Same(t, originalBilling, info.Billing)
	info.Billing.Refund(c)
	asyncCalls, syncCalls := settler.refundCallCounts()
	assert.Equal(t, 1, asyncCalls)
	assert.Zero(t, syncCalls)
}
