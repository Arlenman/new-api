package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRelayServerErrorIntegrationRetriesAnotherChannelWithRetryDisabled(t *testing.T) {
	var firstCalls atomic.Int32
	firstServer := newRelayServerErrorIntegrationUpstream(
		t,
		&firstCalls,
		http.StatusInternalServerError,
		`{"error":{"message":"first-upstream-error","type":"upstream_error","code":"first_500"}}`,
	)

	var secondCalls atomic.Int32
	secondServer := newRelayServerErrorIntegrationUpstream(
		t,
		&secondCalls,
		http.StatusOK,
		`{"id":"chatcmpl-second-channel","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	)

	modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
	recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "chatcmpl-second-channel")
	assert.Equal(t, int32(1), firstCalls.Load())
	assert.Equal(t, int32(1), secondCalls.Load())
	assert.Equal(t, 0, common.RetryTimes)
	assert.Empty(t, operation_setting.AutomaticRetryStatusCodeRanges)
}

func TestRelayServerErrorIntegrationRetriesDoRequestFailedOnAnotherChannel(t *testing.T) {
	var firstCalls atomic.Int32
	firstServer := newRelayServerErrorIntegrationDisconnectUpstream(t, &firstCalls)

	var secondCalls atomic.Int32
	secondServer := newRelayServerErrorIntegrationUpstream(
		t,
		&secondCalls,
		http.StatusOK,
		`{"id":"chatcmpl-after-transport-failure","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	)

	modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
	recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "chatcmpl-after-transport-failure")
	assert.Equal(t, int32(1), firstCalls.Load())
	assert.Equal(t, int32(1), secondCalls.Load())
}

func TestRelayServerErrorIntegrationRetriesEmptyStreamOnAnotherChannel(t *testing.T) {
	testCases := []struct {
		name                  string
		firstResponseBody     string
		firstResponseSentinel string
	}{
		{name: "empty body"},
		{
			name:                  "heartbeat only",
			firstResponseBody:     ": first-channel-heartbeat\n\n",
			firstResponseSentinel: "first-channel-heartbeat",
		},
		{
			name:                  "heartbeat then done",
			firstResponseBody:     ": first-channel-heartbeat-before-done\n\ndata: [DONE]\n\n",
			firstResponseSentinel: "first-channel-heartbeat-before-done",
		},
		{name: "done only", firstResponseBody: "data: [DONE]\n\n"},
		{name: "null only", firstResponseBody: "data: null\n\n"},
		{name: "empty object only", firstResponseBody: "data: {}\n\n"},
		{name: "empty array only", firstResponseBody: "data: []\n\n"},
		{
			name:                  "role only",
			firstResponseBody:     "data: {\"id\":\"first-channel-role-only\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n\n",
			firstResponseSentinel: "first-channel-role-only",
		},
		{
			name:                  "usage only",
			firstResponseBody:     "data: {\"id\":\"first-channel-usage-only\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\ndata: [DONE]\n\n",
			firstResponseSentinel: "first-channel-usage-only",
		},
		{
			name:                  "finish only",
			firstResponseBody:     "data: {\"id\":\"first-channel-finish-only\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
			firstResponseSentinel: "first-channel-finish-only",
		},
		{
			name:                  "responses lifecycle only",
			firstResponseBody:     "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"first-channel-response-metadata\",\"status\":\"in_progress\"}}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"first-channel-response-metadata\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
			firstResponseSentinel: "first-channel-response-metadata",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var firstCalls atomic.Int32
			firstServer := newRelayServerErrorIntegrationSSEUpstream(t, &firstCalls, testCase.firstResponseBody)

			var secondCalls atomic.Int32
			secondServer := newRelayServerErrorIntegrationSSEUpstream(
				t,
				&secondCalls,
				"data: {\"id\":\"chatcmpl-after-empty-stream\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"relay-server-error-integration\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok-after-empty-stream\"},\"finish_reason\":null}]}\n\n"+
					"data: {\"id\":\"chatcmpl-after-empty-stream\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"relay-server-error-integration\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"+
					"data: [DONE]\n\n",
			)

			modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
			recorder := runRelayServerErrorIntegrationStreamRequest(t, modelName, channels[0])

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "chatcmpl-after-empty-stream")
			assert.Contains(t, recorder.Body.String(), "ok-after-empty-stream")
			assert.NotContains(t, recorder.Body.String(), "empty_response")
			if testCase.firstResponseSentinel != "" {
				assert.NotContains(t, recorder.Body.String(), testCase.firstResponseSentinel)
			}
			assert.Equal(t, int32(1), firstCalls.Load())
			assert.Equal(t, int32(1), secondCalls.Load())
			assert.Equal(t, 0, common.RetryTimes)
			assert.Empty(t, operation_setting.AutomaticRetryStatusCodeRanges)
		})
	}
}

func TestRelayServerErrorIntegrationRetriesEmptyJSONOnAnotherChannel(t *testing.T) {
	testCases := []struct {
		name              string
		firstResponseBody string
	}{
		{name: "null", firstResponseBody: "null"},
		{name: "empty object", firstResponseBody: "{}"},
		{name: "empty array", firstResponseBody: "[]"},
		{name: "empty choices", firstResponseBody: `{"id":"first-channel-empty-choices","choices":[]}`},
		{name: "role only", firstResponseBody: `{"id":"first-channel-role-only","choices":[{"message":{"role":"assistant"}}]}`},
		{name: "usage only", firstResponseBody: `{"id":"first-channel-usage-only","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`},
		{name: "finish only", firstResponseBody: `{"id":"first-channel-finish-only","choices":[{"finish_reason":"stop"}]}`},
		{name: "generic success wrapper", firstResponseBody: `{"success":true}`},
		{name: "generic status wrapper", firstResponseBody: `{"code":0,"message":"upstream returned no answer"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var firstCalls atomic.Int32
			firstServer := newRelayServerErrorIntegrationUpstream(t, &firstCalls, http.StatusOK, testCase.firstResponseBody)

			var secondCalls atomic.Int32
			secondServer := newRelayServerErrorIntegrationUpstream(
				t,
				&secondCalls,
				http.StatusOK,
				`{"id":"chatcmpl-after-empty-json","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"ok-after-empty-json"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			)

			modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
			recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "chatcmpl-after-empty-json")
			assert.Contains(t, recorder.Body.String(), "ok-after-empty-json")
			assert.NotContains(t, recorder.Body.String(), "first-channel-")
			assert.NotContains(t, recorder.Body.String(), "empty_response")
			assert.Equal(t, int32(1), firstCalls.Load())
			assert.Equal(t, int32(1), secondCalls.Load())
		})
	}
}

func TestRelayServerErrorIntegrationRetriesUntrustedHTTP200TextOnAnotherChannel(t *testing.T) {
	testCases := []struct {
		name              string
		contentType       string
		firstResponseBody string
	}{
		{name: "plain error text", contentType: "text/plain", firstResponseBody: "upstream returned no answer"},
		{name: "html proxy error", contentType: "text/html", firstResponseBody: "<html><body>temporary upstream error</body></html>"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var firstCalls atomic.Int32
			firstServer := newRelayServerErrorIntegrationUpstreamWithContentType(
				t,
				&firstCalls,
				http.StatusOK,
				testCase.contentType,
				testCase.firstResponseBody,
			)

			var secondCalls atomic.Int32
			secondServer := newRelayServerErrorIntegrationUpstream(
				t,
				&secondCalls,
				http.StatusOK,
				`{"id":"chatcmpl-after-untrusted-text","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"ok-after-untrusted-text"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			)

			modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
			recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "chatcmpl-after-untrusted-text")
			assert.Contains(t, recorder.Body.String(), "ok-after-untrusted-text")
			assert.NotContains(t, recorder.Body.String(), testCase.firstResponseBody)
			assert.Equal(t, int32(1), firstCalls.Load())
			assert.Equal(t, int32(1), secondCalls.Load())
		})
	}
}

func TestRelayServerErrorIntegrationDoesNotSwitchExplicitChannel(t *testing.T) {
	var firstCalls atomic.Int32
	firstServer := newRelayServerErrorIntegrationUpstream(
		t,
		&firstCalls,
		http.StatusInternalServerError,
		`{"error":{"message":"explicit-channel-error","type":"upstream_error","code":"explicit_500"}}`,
	)

	var secondCalls atomic.Int32
	secondServer := newRelayServerErrorIntegrationUpstream(
		t,
		&secondCalls,
		http.StatusOK,
		`{"id":"explicit-channel-sentinel","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"must-not-run"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	)

	modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{firstServer, secondServer})
	recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0], func(ctx *gin.Context) {
		ctx.Set("specific_channel_id", channels[0].Id)
	})

	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "explicit-channel-error")
	assert.Equal(t, int32(1), firstCalls.Load())
	assert.Equal(t, int32(0), secondCalls.Load())
}

func TestRelayServerErrorIntegrationStopsAfterFiveUpstreamAttempts(t *testing.T) {
	errorMessages := []string{
		"upstream-one",
		"upstream-two",
		"upstream-three",
		"upstream-four",
		"upstream-five",
	}
	calls := make([]atomic.Int32, 6)
	upstreams := make([]*relayServerErrorIntegrationUpstream, 0, 6)
	for index, message := range errorMessages {
		server := newRelayServerErrorIntegrationUpstream(
			t,
			&calls[index],
			http.StatusInternalServerError,
			fmt.Sprintf(`{"error":{"message":"%s","type":"upstream_error","code":"upstream_%d"}}`, message, index+1),
		)
		upstreams = append(upstreams, server)
	}
	sentinelServer := newRelayServerErrorIntegrationUpstream(
		t,
		&calls[5],
		http.StatusOK,
		`{"id":"sixth-channel-sentinel","object":"chat.completion","created":1,"model":"relay-server-error-integration","choices":[{"index":0,"message":{"role":"assistant","content":"must-not-run"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	)
	upstreams = append(upstreams, sentinelServer)
	require.Len(t, upstreams, 6)

	modelName, channels := setupRelayServerErrorIntegration(t, upstreams)
	recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "upstream-five")
	assert.Contains(t, recorder.Body.String(), "upstream_5")
	assert.NotContains(t, recorder.Body.String(), "sixth-channel-sentinel")

	var totalCalls int32
	for index := 0; index < 5; index++ {
		count := calls[index].Load()
		assert.Equalf(t, int32(1), count, "channel %d should be called exactly once", index+1)
		totalCalls += count
	}
	assert.Equal(t, int32(0), calls[5].Load(), "the sixth sentinel channel must not be called")
	assert.Equal(t, int32(5), totalCalls, "the upstream call budget includes the initial request")
}

func TestRelayServerErrorIntegrationPreservesSingleChannelUpstreamError(t *testing.T) {
	var calls atomic.Int32
	server := newRelayServerErrorIntegrationUpstream(
		t,
		&calls,
		http.StatusInternalServerError,
		`{"error":{"message":"single-upstream-error","type":"upstream_error","code":"single_500"}}`,
	)

	modelName, channels := setupRelayServerErrorIntegration(t, []*relayServerErrorIntegrationUpstream{server})
	recorder := runRelayServerErrorIntegrationRequest(t, modelName, channels[0])

	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	assert.Equal(t, int32(1), calls.Load())
	assert.Contains(t, recorder.Body.String(), "single-upstream-error")
	assert.Contains(t, recorder.Body.String(), "single_500")
	assert.NotContains(t, recorder.Body.String(), "get_channel_failed")
	assert.NotContains(t, recorder.Body.String(), "可用渠道不存在")
	assert.NotContains(t, recorder.Body.String(), "获取分组")
}

type relayServerErrorIntegrationUpstream struct {
	URL       string
	host      string
	roundTrip func(*http.Request) (*http.Response, error)
}

var relayServerErrorIntegrationUpstreamSequence atomic.Uint64

func newRelayServerErrorIntegrationUpstream(
	t *testing.T,
	calls *atomic.Int32,
	statusCode int,
	responseBody string,
) *relayServerErrorIntegrationUpstream {
	t.Helper()
	return newRelayServerErrorIntegrationUpstreamWithContentType(t, calls, statusCode, "application/json", responseBody)
}

func newRelayServerErrorIntegrationUpstreamWithContentType(
	t *testing.T,
	calls *atomic.Int32,
	statusCode int,
	contentType string,
	responseBody string,
) *relayServerErrorIntegrationUpstream {
	t.Helper()

	host := fmt.Sprintf("relay-server-error-upstream-%d.test", relayServerErrorIntegrationUpstreamSequence.Add(1))
	return &relayServerErrorIntegrationUpstream{
		URL:  "http://" + host,
		host: host,
		roundTrip: func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			header := make(http.Header)
			header.Set("Content-Type", contentType)
			return &http.Response{
				StatusCode: statusCode,
				Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Request:    request,
			}, nil
		},
	}
}

func newRelayServerErrorIntegrationSSEUpstream(t *testing.T, calls *atomic.Int32, responseBody string) *relayServerErrorIntegrationUpstream {
	t.Helper()
	return newRelayServerErrorIntegrationUpstreamWithContentType(t, calls, http.StatusOK, "text/event-stream", responseBody)
}

func newRelayServerErrorIntegrationDisconnectUpstream(t *testing.T, calls *atomic.Int32) *relayServerErrorIntegrationUpstream {
	t.Helper()

	host := fmt.Sprintf("relay-server-error-upstream-%d.test", relayServerErrorIntegrationUpstreamSequence.Add(1))
	return &relayServerErrorIntegrationUpstream{
		URL:  "http://" + host,
		host: host,
		roundTrip: func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("upstream connection closed before response")
		},
	}
}

type relayServerErrorIntegrationTransport map[string]*relayServerErrorIntegrationUpstream

func (transport relayServerErrorIntegrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	upstream := transport[request.URL.Host]
	if upstream == nil {
		return nil, fmt.Errorf("unexpected integration upstream host %q", request.URL.Host)
	}
	return upstream.roundTrip(request)
}

func setupRelayServerErrorIntegration(t *testing.T, upstreams []*relayServerErrorIntegrationUpstream) (string, []*model.Channel) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	originalRetryTimes := common.RetryTimes
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalAutomaticDisableChannelEnabled := common.AutomaticDisableChannelEnabled
	originalCountToken := constant.CountToken
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalStreamingTimeout := constant.StreamingTimeout
	originalCheckSensitiveEnabled := setting.CheckSensitiveEnabled
	originalRetryStatusCodes := append([]operation_setting.StatusCodeRange(nil), operation_setting.AutomaticRetryStatusCodeRanges...)
	originalDisableStatusCodes := append([]operation_setting.StatusCodeRange(nil), operation_setting.AutomaticDisableStatusCodeRanges...)
	originalModelPrices := ratio_setting.ModelPrice2JSONString()

	httpClient := service.GetHttpClient()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	originalTransport := httpClient.Transport
	transport := make(relayServerErrorIntegrationTransport, len(upstreams))
	for _, upstream := range upstreams {
		transport[upstream.host] = upstream
	}
	httpClient.Transport = transport

	databaseName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", databaseName)), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	common.RetryTimes = 0
	common.LogConsumeEnabled = false
	common.AutomaticDisableChannelEnabled = false
	constant.CountToken = false
	constant.ErrorLogEnabled = false
	constant.StreamingTimeout = 5
	setting.CheckSensitiveEnabled = false
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		httpClient.Transport = originalTransport

		common.MemoryCacheEnabled = true
		model.DB = db
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Ability{}).Error)
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Channel{}).Error)
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.UserSubscription{}).Error)
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.User{}).Error)
		model.InitChannelCache()

		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RedisEnabled = originalRedisEnabled
		common.RetryTimes = originalRetryTimes
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.AutomaticDisableChannelEnabled = originalAutomaticDisableChannelEnabled
		constant.CountToken = originalCountToken
		constant.ErrorLogEnabled = originalErrorLogEnabled
		constant.StreamingTimeout = originalStreamingTimeout
		setting.CheckSensitiveEnabled = originalCheckSensitiveEnabled
		operation_setting.AutomaticRetryStatusCodeRanges = originalRetryStatusCodes
		operation_setting.AutomaticDisableStatusCodeRanges = originalDisableStatusCodes
		if originalDB != nil && originalMemoryCacheEnabled {
			model.InitChannelCache()
		}
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UserSubscription{}, &model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "relay-server-error-user",
		Password: "relay-server-error-password",
		Quota:    1_000_000,
		Group:    "default",
	}).Error)

	modelName := "relay-server-error-integration-" + databaseName
	prices := map[string]float64{}
	require.NoError(t, common.Unmarshal([]byte(originalModelPrices), &prices))
	prices[modelName] = 0
	priceJSON, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(priceJSON)))

	channels := make([]*model.Channel, 0, len(upstreams))
	for index, upstream := range upstreams {
		priority := int64(len(upstreams) - index)
		baseURLValue := upstream.URL
		weight := uint(100)
		autoBan := 0
		baseURL := baseURLValue
		channel := &model.Channel{
			Id:       98000 + index,
			Type:     constant.ChannelTypeOpenAI,
			Key:      fmt.Sprintf("relay-server-error-key-%d", index+1),
			Status:   common.ChannelStatusEnabled,
			Name:     fmt.Sprintf("relay-server-error-channel-%d", index+1),
			Weight:   &weight,
			BaseURL:  &baseURL,
			Models:   modelName,
			Group:    "default",
			Priority: &priority,
			AutoBan:  &autoBan,
		}
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  &priority,
			Weight:    weight,
		}).Error)
		channels = append(channels, channel)
	}
	model.InitChannelCache()

	return modelName, channels
}

func runRelayServerErrorIntegrationRequest(
	t *testing.T,
	modelName string,
	initialChannel *model.Channel,
	configure ...func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	requestBody := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hello"}]}`, modelName)
	return runRelayServerErrorIntegrationRequestBody(t, modelName, initialChannel, requestBody, configure...)
}

func runRelayServerErrorIntegrationStreamRequest(
	t *testing.T,
	modelName string,
	initialChannel *model.Channel,
	configure ...func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	requestBody := fmt.Sprintf(`{"model":"%s","stream":true,"messages":[{"role":"user","content":"hello"}]}`, modelName)
	return runRelayServerErrorIntegrationRequestBody(t, modelName, initialChannel, requestBody, configure...)
}

func runRelayServerErrorIntegrationRequestBody(
	t *testing.T,
	modelName string,
	initialChannel *model.Channel,
	requestBody string,
	configure ...func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	for _, configureContext := range configure {
		configureContext(ctx)
	}

	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserQuota, 1_000_000)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenUnlimited, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 1)
	common.SetContextKey(ctx, constant.ContextKeyTokenKey, "relay-server-error-token")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, initialChannel, modelName))

	Relay(ctx, types.RelayFormatOpenAI)
	return recorder
}
