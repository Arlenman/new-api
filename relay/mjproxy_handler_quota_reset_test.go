package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyPeriodicQuotaTest(t *testing.T, modelName string, relayMode int, requestPath string, requestBody string) (*gin.Context, *relaycommon.RelayInfo, *atomic.Int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalModelPrices := ratio_setting.ModelPrice2JSONString()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/midjourney-quota.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Midjourney{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"swap_face":0.001,"mj_imagine":0.001}`))

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
	})

	user := &model.User{Id: 7101, Username: "midjourney-user", Quota: 10_000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{
		Id:                  7201,
		UserId:              user.Id,
		Key:                 "midjourney-periodic-token",
		Name:                "midjourney-token",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         10_000,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    model.TokenQuotaResetDaily,
		QuotaResetAmount:    500,
		QuotaResetRemaining: 100,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  time.Now().Add(-time.Hour).Unix(),
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, db.Create(token).Error)

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"ok","result":"task-1","properties":{}}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, requestPath, strings.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 7301)
	ctx.Set("base_url", upstream.URL)
	ctx.Set("channel_id", 7301)
	ctx.Set("token_name", token.Name)

	info := &relaycommon.RelayInfo{
		TokenId:                token.Id,
		TokenKey:               token.Key,
		UserId:                 user.Id,
		UsingGroup:             "default",
		UserGroup:              "default",
		TokenQuotaResetEnabled: true,
		StartTime:              time.Now(),
		RelayMode:              relayMode,
		OriginModelName:        modelName,
		RequestId:              "midjourney-periodic-quota-test",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	return ctx, info, &upstreamCalls
}

func TestRelaySwapFaceChecksPeriodicQuotaBeforeUpstream(t *testing.T) {
	ctx, info, upstreamCalls := setupMidjourneyPeriodicQuotaTest(
		t,
		"swap_face",
		relayconstant.RelayModeSwapFace,
		"/mj/insight-face/swap",
		`{"sourceBase64":"source","targetBase64":"target"}`,
	)

	response := RelaySwapFace(ctx, info)

	require.NotNil(t, response)
	assert.Contains(t, response.Description, model.ErrTokenQuotaResetExhausted.Error())
	assert.Equal(t, int32(0), upstreamCalls.Load())
}

func TestRelayMidjourneySubmitChecksPeriodicQuotaBeforeUpstream(t *testing.T) {
	ctx, info, upstreamCalls := setupMidjourneyPeriodicQuotaTest(
		t,
		"mj_imagine",
		relayconstant.RelayModeMidjourneyImagine,
		"/mj/submit/imagine",
		`{"prompt":"draw a cat"}`,
	)

	response := RelayMidjourneySubmit(ctx, info)

	require.NotNil(t, response)
	assert.Contains(t, response.Description, model.ErrTokenQuotaResetExhausted.Error())
	assert.Equal(t, int32(0), upstreamCalls.Load())
}
