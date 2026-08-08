package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryUsesAutomaticDisableStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})

	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusInternalServerError, End: http.StatusInternalServerError},
	}

	err := types.NewErrorWithStatusCode(errors.New("upstream error: do request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	require.True(t, shouldRetry(ctx, err, 1))
	require.False(t, shouldRetry(ctx, err, 0))
}

func TestShouldRetryByAutomaticDisableStatusCode(t *testing.T) {
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})

	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusInternalServerError, End: http.StatusInternalServerError},
	}

	err := types.NewErrorWithStatusCode(errors.New("upstream error: do request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.True(t, shouldRetryByAutomaticDisableStatusCode(err))

	skipRetryErr := types.NewErrorWithStatusCode(errors.New("invalid request"), types.ErrorCodeInvalidRequest, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	require.False(t, shouldRetryByAutomaticDisableStatusCode(skipRetryErr))

	alwaysSkipErr := types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout)
	require.False(t, shouldRetryByAutomaticDisableStatusCode(alwaysSkipErr))
}

func TestIsPlaygroundRelayRequestRecognizesCompatibilityPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		path string
		want bool
	}{
		{path: "/pg/images/generations", want: true},
		{path: "/pg/v1/images/generations", want: true},
		{path: "/pg/images/edits", want: true},
		{path: "/pg/v1/images/edits", want: true},
		{path: "/pg/responses", want: true},
		{path: "/pg/v1/responses", want: true},
		{path: "/v1/images/generations", want: false},
		{path: "/v1/responses", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			require.Equal(t, test.want, isPlaygroundRelayRequest(ctx))
		})
	}
}

func TestShouldRetryAllowsPlaygroundImageGatewayTimeouts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.True(t, shouldRetry(ctx, err, 1))

	err = types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout)
	require.True(t, shouldRetry(ctx, err, 1))
}

func TestShouldIncreaseRetryBudgetForPlaygroundImageGatewayTimeouts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
}

func TestShouldRetryAllowsPlaygroundResponsesTransportFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/responses", nil)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
	require.True(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryAllowsPlaygroundResponsesGatewayTimeouts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/responses", nil)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)

	for _, statusCode := range []int{http.StatusGatewayTimeout, 524} {
		err := types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, statusCode)
		require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
		require.True(t, shouldRetry(ctx, err, 1))
	}
}

func TestPlaygroundResponsesFailureDoesNotRetryAfterStreamOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/responses", nil)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)
	ctx.Writer.WriteHeaderNow()

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestPlaygroundImageTransportFailureGetsLimitedRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
	require.True(t, shouldRetry(ctx, err, 1))
}

func TestPlaygroundImageFailuresRetryDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)
		ctx.Set("channel_affinity_skip_retry_on_failure", true)
		require.True(t, service.ShouldSkipRetryAfterChannelAffinityFailure(ctx))
		return ctx
	}

	transportContext := newContext()
	transportErr := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.True(t, shouldRetry(transportContext, transportErr, 1))

	timeoutContext := newContext()
	common.SetContextKey(timeoutContext, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)
	timeoutErr := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.True(t, shouldRetry(timeoutContext, timeoutErr, 1))
}

func TestPlaygroundImageTransportFailureSkipsRetryWithSingleCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 1)

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestHandlePlaygroundImageChannelFailureExcludesFailedChannelForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	handlePlaygroundRelayChannelFailure(ctx, 101, err, true)

	excludedChannelIDs, ok := common.GetContextKeyType[map[int]struct{}](ctx, constant.ContextKeyPlaygroundRelayExcludedChannelIds)
	require.True(t, ok)
	require.Contains(t, excludedChannelIDs, 101)
}

func TestHandlePlaygroundImageChannelFailureDoesNotExcludeWithoutRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	handlePlaygroundRelayChannelFailure(ctx, 101, err, false)

	_, ok := common.GetContextKeyType[map[int]struct{}](ctx, constant.ContextKeyPlaygroundRelayExcludedChannelIds)
	require.False(t, ok)
}

func TestPlaygroundImageTransportFailureRetryDoesNotAffectRegularRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	require.False(t, shouldIncreaseRelayRetryBudget(ctx, err))
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestPlaygroundImageTransportFailureDoesNotRetryAfterWritingOrWithSpecificChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusInternalServerError, End: http.StatusInternalServerError},
	}
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	err := types.NewErrorWithStatusCode(errors.New("socks connect failed: EOF"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/edits", nil)
		return ctx
	}

	writtenContext := newContext()
	writtenContext.Writer.WriteHeaderNow()
	require.False(t, shouldRetry(writtenContext, err, 1))

	specificChannelContext := newContext()
	specificChannelContext.Set("specific_channel_id", 123)
	require.False(t, shouldRetry(specificChannelContext, err, 1))

	tokenSpecificChannelContext := newContext()
	common.SetContextKey(tokenSpecificChannelContext, constant.ContextKeyTokenSpecificChannelId, 123)
	require.False(t, shouldRetry(tokenSpecificChannelContext, err, 1))
}

func TestShouldStopPlaygroundImageGatewayTimeoutRetryAfterSameChannelRetryLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Set("use_channel", []string{"106"})

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.False(t, shouldStopPlaygroundRelayGatewayTimeoutRetry(ctx, 106, err, 1))
	require.False(t, shouldStopPlaygroundRelayGatewayTimeoutRetry(ctx, 107, err, 1))
	require.False(t, shouldStopPlaygroundRelayGatewayTimeoutRetry(ctx, 106, err, 0))

	ctx.Set("use_channel", []string{"106", "106"})
	require.True(t, shouldStopPlaygroundRelayGatewayTimeoutRetry(ctx, 106, err, 2))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	require.False(t, shouldStopPlaygroundRelayGatewayTimeoutRetry(ctx, 106, err, 1))
}

func TestShouldRetrySkipsPlaygroundImageGatewayTimeoutWhenOnlySameChannelAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Set("use_channel", []string{"106"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 1)

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.False(t, shouldRetry(ctx, err, 1))

	ctx.Set("use_channel", []string{"106", "107"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)
	require.True(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryKeepsGatewayTimeoutsNonRetryableOutsidePlaygroundImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestShouldAutoDisableChannelSkipsPlaygroundImageGatewayTimeouts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)

	origDisableEnabled := common.AutomaticDisableChannelEnabled
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origDisableEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})

	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusGatewayTimeout, End: http.StatusGatewayTimeout},
		{Start: 524, End: 524},
	}

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.False(t, shouldAutoDisableChannel(ctx, err))

	err = types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout)
	require.False(t, shouldAutoDisableChannel(ctx, err))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	err = types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.True(t, shouldAutoDisableChannel(ctx, err))
}

func TestShouldRetryModelCapacityError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return ctx
	}
	capacityErr := types.NewErrorWithStatusCode(
		errors.New("upstream error: Selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseBody,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)

	require.True(t, shouldRetry(newContext(), capacityErr, 1))
	require.False(t, shouldRetry(newContext(), capacityErr, 0))

	writtenContext := newContext()
	writtenContext.Writer.WriteHeaderNow()
	require.False(t, shouldRetry(writtenContext, capacityErr, 1))

	specificChannelContext := newContext()
	common.SetContextKey(specificChannelContext, constant.ContextKeyTokenSpecificChannelId, 123)
	require.False(t, shouldRetry(specificChannelContext, capacityErr, 1))

	affinityContext := newContext()
	affinityContext.Set("channel_affinity_skip_retry_on_failure", true)
	require.True(t, shouldRetry(affinityContext, capacityErr, 1))

	nonCapacityErr := types.NewErrorWithStatusCode(
		errors.New("upstream error: selected model is unavailable"),
		types.ErrorCodeBadResponseBody,
		http.StatusTooManyRequests,
	)
	require.False(t, shouldRetry(newContext(), nonCapacityErr, 1))
}

func TestShouldIncreaseRetryBudgetForModelCapacityError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err := types.NewErrorWithStatusCode(
		errors.New("Selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseBody,
		http.StatusTooManyRequests,
	)
	require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
}

func TestShouldAutoDisableChannelSkipsModelCapacityError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	origDisableEnabled := common.AutomaticDisableChannelEnabled
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origDisableEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusTooManyRequests, End: http.StatusTooManyRequests},
	}

	err := types.NewErrorWithStatusCode(
		errors.New("Selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	require.False(t, shouldAutoDisableChannel(ctx, err))
}

func TestPlaygroundImageQuotaExhaustionRetriesWithoutDisablingChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDisableEnabled := common.AutomaticDisableChannelEnabled
	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origDisableEnabled
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 2)

	err := types.NewErrorWithStatusCode(
		errors.New("no available image quota"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)

	require.True(t, shouldRetry(ctx, err, 1))
	require.True(t, shouldIncreaseRelayRetryBudget(ctx, err))
	require.False(t, shouldAutoDisableChannel(ctx, err))

	singleCandidateCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	singleCandidateCtx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)
	singleCandidateCtx.Set("use_channel", []string{"101"})
	common.SetContextKey(singleCandidateCtx, constant.ContextKeyPlaygroundRelayCandidateChannelCount, 1)
	require.False(t, shouldRetry(singleCandidateCtx, err, 1))

	handlePlaygroundRelayChannelFailure(ctx, 101, err, true)
	excludedChannelIDs, ok := common.GetContextKeyType[map[int]struct{}](ctx, constant.ContextKeyPlaygroundRelayExcludedChannelIds)
	require.True(t, ok)
	require.Contains(t, excludedChannelIDs, 101)
}

func TestImageQuotaExhaustionRemainsAutoDisableEligibleOutsidePlaygroundImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDisableEnabled := common.AutomaticDisableChannelEnabled
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origDisableEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusTooManyRequests, End: http.StatusTooManyRequests},
	}

	err := types.NewErrorWithStatusCode(
		errors.New("no available image quota"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)

	regularImageCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	regularImageCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	require.True(t, shouldAutoDisableChannel(regularImageCtx, err))

	playgroundResponsesCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	playgroundResponsesCtx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/responses", nil)
	require.True(t, shouldAutoDisableChannel(playgroundResponsesCtx, err))
}

func TestShouldRetryForRelayServerErrorUsesFiveTotalAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err := types.NewErrorWithStatusCode(
		errors.New("bad response status code 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	err.OriginalStatusCode = http.StatusInternalServerError

	require.True(t, shouldRetryForRelayServerError(ctx, err, 1))
	require.True(t, shouldRetryForRelayServerError(ctx, err, 4))
	require.False(t, shouldRetryForRelayServerError(ctx, err, 5))
}

func TestShouldRetryForRelayServerErrorRecognizesDoRequestFailedWithoutRetryConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	err := types.NewErrorWithStatusCode(
		errors.New("upstream error: do request failed"),
		types.ErrorCodeDoRequestFailed,
		http.StatusInternalServerError,
	)

	require.True(t, shouldRetryForRelayServerError(ctx, err, 1))
}

func TestShouldRetryForRelayServerErrorRecognizesEmptyUpstreamResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err := types.NewErrorWithStatusCode(
		errors.New("upstream returned no response data"),
		types.ErrorCodeEmptyResponse,
		http.StatusInternalServerError,
	)

	require.True(t, shouldRetryForRelayServerError(ctx, err, 1))
}

func TestShouldRetryForRelayServerErrorBypassesBadResponseBodySkipRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err := types.NewErrorWithStatusCode(
		errors.New("upstream rejected request"),
		types.ErrorCodeBadResponseBody,
		http.StatusInternalServerError,
	)
	err.OriginalStatusCode = http.StatusInternalServerError

	require.False(t, shouldRetry(ctx, err, 1))
	require.True(t, shouldRetryForRelayServerError(ctx, err, 1))
}

func TestShouldRetryForRelayServerErrorHonorsSkipRetryAndSpecificChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return ctx
	}

	skipRetryErr := types.NewErrorWithStatusCode(
		errors.New("local server error"),
		types.ErrorCodeDoRequestFailed,
		http.StatusInternalServerError,
		types.ErrOptionWithSkipRetry(),
	)
	require.False(t, shouldRetryForRelayServerError(newContext(), skipRetryErr, 1))

	err := types.NewErrorWithStatusCode(
		errors.New("bad response status code 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	err.OriginalStatusCode = http.StatusInternalServerError

	specificChannelContext := newContext()
	specificChannelContext.Set("specific_channel_id", 123)
	require.False(t, shouldRetryForRelayServerError(specificChannelContext, err, 1))

	tokenSpecificChannelContext := newContext()
	common.SetContextKey(tokenSpecificChannelContext, constant.ContextKeyTokenSpecificChannelId, 123)
	require.False(t, shouldRetryForRelayServerError(tokenSpecificChannelContext, err, 1))
}

func TestShouldRetryForRelayServerErrorDoesNotTreatLocalOrOtherStatusAsTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	localErr := types.NewErrorWithStatusCode(
		errors.New("local server error"),
		types.ErrorCodeBadResponseBody,
		http.StatusInternalServerError,
	)
	require.False(t, shouldRetryForRelayServerError(ctx, localErr, 1))

	gatewayErr := types.NewErrorWithStatusCode(
		errors.New("gateway timeout"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)
	gatewayErr.OriginalStatusCode = http.StatusGatewayTimeout
	require.False(t, shouldRetryForRelayServerError(ctx, gatewayErr, 1))
}

func TestShouldRetryForRelayServerErrorIgnoresHeartbeatWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	_, writeErr := ctx.Writer.Write([]byte(relayHeartbeatPayload))
	require.NoError(t, writeErr)

	err := types.NewErrorWithStatusCode(
		errors.New("bad response status code 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	err.OriginalStatusCode = http.StatusInternalServerError

	require.True(t, shouldRetryForRelayServerError(ctx, err, 1))
}

func TestShouldRetryForRelayServerErrorStopsWhenClientDisconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)

	err := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	err.OriginalStatusCode = http.StatusInternalServerError

	require.False(t, shouldRetryForRelayServerError(ctx, err, 1))
}

func TestHandleRelayServerErrorChannelFailureExcludesFailedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err := types.NewErrorWithStatusCode(
		errors.New("bad response status code 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	err.OriginalStatusCode = http.StatusInternalServerError

	handleRelayServerErrorChannelFailure(ctx, 101, err, true)

	excludedChannelIDs, ok := common.GetContextKeyType[map[int]struct{}](ctx, constant.ContextKeyRelayServerErrorExcludedChannelIds)
	require.True(t, ok)
	require.Contains(t, excludedChannelIDs, 101)
}

func TestShouldPreserveRelayServerErrorWhenFallbackChannelIsUnavailable(t *testing.T) {
	channelErr := types.NewError(
		&noAvailableRelayChannelError{message: "no available channel"},
		types.ErrorCodeGetChannelFailed,
		types.ErrOptionWithSkipRetry(),
	)
	serverErr := types.NewErrorWithStatusCode(
		errors.New("bad response status code 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	serverErr.OriginalStatusCode = http.StatusInternalServerError
	require.True(t, shouldPreserveRelayErrorOnChannelSelectionFailure(nil, serverErr, channelErr))

	otherErr := types.NewErrorWithStatusCode(
		errors.New("bad gateway"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)
	otherErr.OriginalStatusCode = http.StatusBadGateway
	require.False(t, shouldPreserveRelayErrorOnChannelSelectionFailure(nil, otherErr, channelErr))
}

func TestRelayRetryStateReturnsToOrdinaryRetryRulesAfterServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryTimes := common.RetryTimes
	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.RetryTimes = origRetryTimes
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	common.RetryTimes = 0
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	state := newRelayRetryState()
	state.recordUpstreamAttempt()

	serverError := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	serverError.OriginalStatusCode = http.StatusInternalServerError
	require.True(t, state.shouldRetry(ctx, serverError, 0))
	require.True(t, state.shouldContinue(1))

	state.beginIteration()
	state.recordUpstreamAttempt()
	gatewayError := types.NewErrorWithStatusCode(errors.New("bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	gatewayError.OriginalStatusCode = http.StatusBadGateway
	require.False(t, state.shouldRetry(ctx, gatewayError, 1))
	require.False(t, state.shouldContinue(2))
}

func TestRelayRetryStateStopsAfterFiveUpstreamAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryTimes := common.RetryTimes
	t.Cleanup(func() { common.RetryTimes = origRetryTimes })
	common.RetryTimes = 0

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	state := newRelayRetryState()
	err := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	err.OriginalStatusCode = http.StatusInternalServerError

	for attempt := 1; attempt <= relayServerErrorMaxAttempts; attempt++ {
		state.recordUpstreamAttempt()
		willRetry := state.shouldRetry(ctx, err, attempt-1)
		if attempt < relayServerErrorMaxAttempts {
			require.True(t, willRetry)
			require.True(t, state.shouldContinue(attempt))
			state.beginIteration()
			continue
		}
		require.False(t, willRetry)
		require.False(t, state.shouldContinue(attempt))
	}
}

func TestRelayRetryStateKeepsConfiguredBudgetForOrdinaryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryTimes := common.RetryTimes
	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		common.RetryTimes = origRetryTimes
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
	})
	common.RetryTimes = relayServerErrorMaxAttempts + 5
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{
		Start: http.StatusBadGateway,
		End:   http.StatusBadGateway,
	}}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	state := newRelayRetryState()
	ordinaryErr := types.NewErrorWithStatusCode(errors.New("ordinary upstream error"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	for attempt := 1; attempt <= relayServerErrorMaxAttempts; attempt++ {
		state.recordUpstreamAttempt()
		willRetry := state.shouldRetry(ctx, ordinaryErr, attempt-1)
		require.True(t, willRetry)
		require.True(t, state.shouldContinue(attempt))
		state.beginIteration()
	}

	require.Zero(t, state.upstreamAttemptLimit)
	require.True(t, state.shouldContinue(relayServerErrorMaxAttempts), "ordinary errors must keep the configured retry budget")
}

func TestRelayRetryStateHardCapsServerErrorAttemptsAboveConfiguredRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryTimes := common.RetryTimes
	t.Cleanup(func() { common.RetryTimes = origRetryTimes })
	common.RetryTimes = relayServerErrorMaxAttempts + 5

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	state := newRelayRetryState()
	err := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	err.OriginalStatusCode = http.StatusInternalServerError

	for attempt := 1; attempt <= relayServerErrorMaxAttempts; attempt++ {
		state.recordUpstreamAttempt()
		willRetry := state.shouldRetry(ctx, err, attempt-1)
		if attempt < relayServerErrorMaxAttempts {
			require.True(t, willRetry)
			state.beginIteration()
			continue
		}
		require.False(t, willRetry)
	}

	require.False(t, state.shouldContinue(relayServerErrorMaxAttempts), "server-error mode must not fall back to a larger configured retry budget")
}

func TestRelayResponseWriterDistinguishesHeartbeatFromBusinessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)

	_, err := ctx.Writer.Write([]byte(relayHeartbeatPayload))
	require.NoError(t, err)
	require.False(t, writer.businessResponseWritten.Load())
	require.True(t, ctx.Writer.Written())
	require.True(t, recorder.Flushed)
	require.Equal(t, relayHeartbeatPayload, recorder.Body.String())

	_, err = ctx.Writer.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
	require.NoError(t, err)
	require.True(t, writer.businessResponseWritten.Load())
	require.Contains(t, recorder.Body.String(), relayHeartbeatPayload)
	require.Contains(t, recorder.Body.String(), "data: ")
}

func TestRelayResponseWriterSuppressesTerminalOnlyStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.WriteHeader(http.StatusOK)

	_, err := ctx.Writer.Write([]byte("data: [DONE]\n\n"))
	require.NoError(t, err)
	ctx.Writer.Flush()
	ctx.Writer.WriteHeaderNow()

	require.False(t, writer.businessResponseWritten.Load())
	require.False(t, ctx.Writer.Written())
	require.False(t, recorder.Flushed)
	require.Empty(t, recorder.Body.String())
}

func TestRelayResponseWriterSuppressesMetadataOnlyStream(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "role only", body: "data: {\"id\":\"role-only\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"},
		{name: "usage only", body: "data: {\"id\":\"usage-only\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n"},
		{name: "finish only", body: "data: {\"id\":\"finish-only\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"},
		{name: "responses lifecycle only", body: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-metadata-only\",\"status\":\"in_progress\"}}\n\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, nil)
			ctx.Writer.Header().Set("Content-Type", "text/event-stream")
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			ctx.Writer.Flush()
			ctx.Writer.WriteHeaderNow()

			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, ctx.Writer.Written())
			require.False(t, recorder.Flushed)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterFlushesUnterminatedFinalBusinessSSEEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.WriteHeader(http.StatusOK)

	event := "data: {\"id\":\"final-event\",\"choices\":[{\"delta\":{\"content\":\"final\"}}]}\n"
	firstPart := event[:len(event)/2]
	secondPart := event[len(event)/2:]

	n, err := ctx.Writer.Write([]byte(firstPart))
	require.NoError(t, err)
	require.Equal(t, len(firstPart), n)
	n, err = ctx.Writer.Write([]byte(secondPart))
	require.NoError(t, err)
	require.Equal(t, len(secondPart), n)
	require.False(t, writer.businessResponseWritten.Load())
	require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyRelayBusinessResponseWritten))
	require.False(t, ctx.Writer.Written())
	require.Empty(t, recorder.Body.String())

	staleEmptyResponse := helper.NewEmptyResponseError("")
	require.Nil(t, finishRelayUpstreamAttempt(ctx, staleEmptyResponse))

	require.True(t, writer.businessResponseWritten.Load())
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRelayBusinessResponseWritten))
	require.True(t, ctx.Writer.Written())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, event, recorder.Body.String())
}

func TestRelayResponseWriterKeepsUnterminatedEmptySSEEventsNonBusiness(t *testing.T) {
	testCases := []struct {
		name                string
		event               string
		wantBody            string
		wantStreamCommitted bool
	}{
		{name: "heartbeat", event: ": PING", wantBody: ": PING", wantStreamCommitted: true},
		{name: "event metadata", event: "event: response.completed"},
		{name: "done", event: "data: [DONE]"},
		{name: "empty data", event: "data:"},
		{name: "role metadata", event: "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, nil)
			ctx.Writer.Header().Set("Content-Type", "text/event-stream")
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.event))
			require.NoError(t, err)
			require.Equal(t, len(testCase.event), n)
			require.False(t, writer.businessResponseWritten.Load())
			require.Empty(t, recorder.Body.String())

			staleEmptyResponse := helper.NewEmptyResponseError("")
			require.Same(t, staleEmptyResponse, finishRelayUpstreamAttempt(ctx, staleEmptyResponse))

			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyRelayBusinessResponseWritten))
			require.Equal(t, testCase.wantStreamCommitted, writer.clientStreamCommitted.Load())
			require.Equal(t, testCase.wantBody, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterDefersIncompleteImageEvents(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "partial image",
			body: "event: image_generation.partial_image\n" +
				"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n",
		},
		{
			name: "completed event without image",
			body: "event: image_generation.completed\n" +
				"data: {\"type\":\"image_generation.completed\",\"revised_prompt\":\"draw a cat\",\"usage\":{\"total_tokens\":7}}\n\n",
		},
		{
			name: "completed data without image",
			body: "event: image_edit.completed\n" +
				"data: {\"type\":\"image_edit.completed\",\"data\":[{\"revised_prompt\":\"edit a cat\"}]}\n\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, nil)
			ctx.Writer.Header().Set("Content-Type", "text/event-stream")
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			ctx.Writer.Flush()
			ctx.Writer.WriteHeaderNow()

			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, ctx.Writer.Written())
			require.False(t, recorder.Flushed)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterFlushesPartialImageAfterCompletedImage(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")

	partialEvent := "event: image_generation.partial_image\n" +
		"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n"
	completedEvent := "event: image_generation.completed\n" +
		"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final\"}\n\n"

	_, err := ctx.Writer.Write([]byte(partialEvent))
	require.NoError(t, err)
	require.False(t, writer.businessResponseWritten.Load())
	require.Empty(t, recorder.Body.String())

	_, err = ctx.Writer.Write([]byte(completedEvent))
	require.NoError(t, err)
	require.True(t, writer.businessResponseWritten.Load())
	require.True(t, ctx.Writer.Written())
	require.Equal(t, partialEvent+completedEvent, recorder.Body.String())
}

func TestRelayResponseWriterCommitsCompletedImageFromData(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")

	event := "event: image_edit.completed\n" +
		"data: {\"type\":\"image_edit.completed\",\"data\":[{\"url\":\"https://example.test/image.png\"}]}\n\n"
	_, err := ctx.Writer.Write([]byte(event))
	require.NoError(t, err)
	require.True(t, writer.businessResponseWritten.Load())
	require.True(t, ctx.Writer.Written())
	require.Equal(t, event, recorder.Body.String())
}

func TestRelayResponseWriterSuppressesEmptyJSONBodies(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "null", body: "null"},
		{name: "empty object", body: "{}"},
		{name: "empty array", body: "[]"},
		{name: "empty choices", body: `{"id":"empty-choices","choices":[]}`},
		{name: "role only", body: `{"id":"role-only","choices":[{"message":{"role":"assistant"}}]}`},
		{name: "usage only", body: `{"id":"usage-only","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`},
		{name: "finish only", body: `{"id":"finish-only","choices":[{"finish_reason":"stop"}]}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, nil)
			ctx.Writer.Header().Set("Content-Type", "application/json")
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)

			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, ctx.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterSuppressesUntrustedHTTP200TextAndStatusBodies(t *testing.T) {
	testCases := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "plain upstream message", contentType: "text/plain", body: "upstream returned no information"},
		{name: "html proxy error", contentType: "text/html", body: "<html>temporary upstream error</html>"},
		{name: "generic success wrapper", contentType: "application/json", body: `{"success":true}`},
		{name: "generic status wrapper", contentType: "application/json", body: `{"code":0,"message":"upstream returned no answer"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeChatCompletions,
			})
			ctx.Writer.Header().Set("Content-Type", testCase.contentType)
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, ctx.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterCommitsAudioTranscriptionAndTranslationText(t *testing.T) {
	testCases := []struct {
		name           string
		relayMode      int
		responseFormat string
		contentType    string
		body           string
	}{
		{
			name:           "transcription text",
			relayMode:      relayconstant.RelayModeAudioTranscription,
			responseFormat: "text",
			contentType:    "text/plain; charset=utf-8",
			body:           "This is a valid transcript.",
		},
		{
			name:           "transcription srt",
			relayMode:      relayconstant.RelayModeAudioTranscription,
			responseFormat: "srt",
			contentType:    "application/x-subrip",
			body:           "1\n00:00:00,000 --> 00:00:01,000\nHello\n",
		},
		{
			name:           "translation vtt",
			relayMode:      relayconstant.RelayModeAudioTranslation,
			responseFormat: "vtt",
			contentType:    "text/vtt",
			body:           "WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, &relaycommon.RelayInfo{
				RelayMode: testCase.relayMode,
				Request: &dto.AudioRequest{
					ResponseFormat: testCase.responseFormat,
				},
			})
			ctx.Writer.Header().Set("Content-Type", testCase.contentType)
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			require.True(t, writer.businessResponseWritten.Load())
			require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRelayBusinessResponseWritten))
			require.True(t, ctx.Writer.Written())
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, testCase.body, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterDoesNotTrustAudioHTMLOrUnexpectedPlainText(t *testing.T) {
	testCases := []struct {
		name           string
		relayMode      int
		responseFormat string
		contentType    string
		body           string
	}{
		{
			name:           "audio text rejects html content type",
			relayMode:      relayconstant.RelayModeAudioTranscription,
			responseFormat: "text",
			contentType:    "text/html",
			body:           "<html>temporary upstream error</html>",
		},
		{
			name:           "audio text rejects html body with plain content type",
			relayMode:      relayconstant.RelayModeAudioTranslation,
			responseFormat: "text",
			contentType:    "text/plain",
			body:           "<!DOCTYPE html><html>gateway error</html>",
		},
		{
			name:           "json response format does not trust plain text",
			relayMode:      relayconstant.RelayModeAudioTranscription,
			responseFormat: "json",
			contentType:    "text/plain",
			body:           "upstream returned no information",
		},
		{
			name:           "speech mode does not trust plain text",
			relayMode:      relayconstant.RelayModeAudioSpeech,
			responseFormat: "text",
			contentType:    "text/plain",
			body:           "upstream returned no information",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, &relaycommon.RelayInfo{
				RelayMode: testCase.relayMode,
				Request: &dto.AudioRequest{
					ResponseFormat: testCase.responseFormat,
				},
			})
			ctx.Writer.Header().Set("Content-Type", testCase.contentType)
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			require.False(t, writer.businessResponseWritten.Load())
			require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyRelayBusinessResponseWritten))
			require.False(t, ctx.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterCommitsBinaryAudio(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "audio/mpeg")
	ctx.Writer.WriteHeader(http.StatusOK)
	payload := []byte{0xff, 0xfb, 0x90, 0x64}

	n, err := ctx.Writer.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.True(t, writer.businessResponseWritten.Load())
	require.Equal(t, payload, recorder.Body.Bytes())
}

func TestRelayResponseWriterCommitsNonTextBusinessJSON(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "embedding", body: `{"data":[{"embedding":[0.1,0.2],"index":0}]}`},
		{name: "image", body: `{"data":[{"url":"https://example.test/image.png"}]}`},
		{name: "rerank", body: `{"results":[{"index":0,"relevance_score":0.9}]}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writer := installRelayResponseWriter(ctx, nil)
			ctx.Writer.Header().Set("Content-Type", "application/json")
			ctx.Writer.WriteHeader(http.StatusOK)

			n, err := ctx.Writer.Write([]byte(testCase.body))
			require.NoError(t, err)
			require.Equal(t, len(testCase.body), n)
			require.True(t, writer.businessResponseWritten.Load())
			require.True(t, ctx.Writer.Written())
			require.Equal(t, http.StatusOK, recorder.Code)
			require.JSONEq(t, testCase.body, recorder.Body.String())
		})
	}
}

func TestRelayResponseWriterFlushesDeferredMetadataWithBusinessData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")

	_, err := ctx.Writer.Write([]byte("event: response.output_text.delta\n\n"))
	require.NoError(t, err)
	require.Empty(t, recorder.Body.String())

	_, err = ctx.Writer.Write([]byte("data: {\"delta\":\"ok\"}\n\n"))
	require.NoError(t, err)
	require.True(t, writer.businessResponseWritten.Load())
	require.Equal(t, "event: response.output_text.delta\n\ndata: {\"delta\":\"ok\"}\n\n", recorder.Body.String())
}

func TestRelayResponseWriterUsesFirstPendingStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "application/json")

	ctx.Writer.WriteHeader(http.StatusOK)
	ctx.Writer.WriteHeader(http.StatusInternalServerError)
	_, err := ctx.Writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"content":"ok"`)
}

func TestBeginRelayUpstreamAttemptClearsPendingStatusAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	info := &relaycommon.RelayInfo{}

	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("X-Upstream-Channel", "first")
	ctx.Writer.WriteHeader(http.StatusAccepted)
	_, err := ctx.Writer.Write([]byte("data: {\"id\":\"first-channel-marker\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
	require.NoError(t, err)
	require.False(t, writer.businessResponseWritten.Load())

	beginRelayUpstreamAttempt(ctx, info)
	ctx.Writer.Header().Set("Content-Type", "application/json")
	ctx.Writer.Header().Set("X-Upstream-Channel", "second")
	ctx.Writer.WriteHeader(http.StatusOK)
	_, err = ctx.Writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "second", recorder.Header().Get("X-Upstream-Channel"))
	assert.NotContains(t, recorder.Body.String(), "first-channel-marker")
	assert.Contains(t, recorder.Body.String(), `"content":"ok"`)
}

func TestBeginRelayUpstreamAttemptDiscardsPreviousPendingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	_, err := ctx.Writer.Write([]byte(": first-channel\n\ndata: [DONE]\n\n"))
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{
		ReceivedResponseCount: 3,
		SendResponseCount:     2,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	beginRelayUpstreamAttempt(ctx, info)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	_, err = ctx.Writer.Write([]byte("data: {\"id\":\"second-channel\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
	require.NoError(t, err)

	require.Zero(t, info.ReceivedResponseCount)
	require.Zero(t, info.SendResponseCount)
	require.Nil(t, info.StreamStatus)
	require.Contains(t, recorder.Body.String(), "first-channel")
	require.Contains(t, recorder.Body.String(), "second-channel")
}

func TestWriteCommittedRelayStreamErrorAfterHeartbeat(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := installRelayResponseWriter(ctx, nil)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	_, err := ctx.Writer.Write([]byte(relayHeartbeatPayload))
	require.NoError(t, err)
	require.False(t, writer.businessResponseWritten.Load())
	require.True(t, writer.clientStreamCommitted.Load())

	relayErr := types.NewErrorWithStatusCode(errors.New("upstream returned no response"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	require.True(t, writeCommittedRelayStreamError(ctx, types.RelayFormatOpenAI, relayErr))
	require.Contains(t, recorder.Body.String(), relayHeartbeatPayload)
	require.Contains(t, recorder.Body.String(), `"code":"empty_response"`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestRelayRetryStateDoesNotEnableSpecialServerErrorRetryForRealtime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origRetryTimes := common.RetryTimes
	origRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	origDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.RetryTimes = origRetryTimes
		operation_setting.AutomaticRetryStatusCodeRanges = origRetryRanges
		operation_setting.AutomaticDisableStatusCodeRanges = origDisableRanges
	})
	common.RetryTimes = 0
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	operation_setting.AutomaticDisableStatusCodeRanges = nil

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	state := newRelayRetryState()
	state.relayServerErrorRetryEnabled = false
	state.recordUpstreamAttempt()

	serverError := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	serverError.OriginalStatusCode = http.StatusInternalServerError
	require.False(t, state.shouldRetry(ctx, serverError, 0))
	require.False(t, state.shouldContinue(1))
}

func TestShouldRetryForRelayServerErrorStopsAfterBusinessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	installRelayResponseWriter(ctx, nil)
	_, err := ctx.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	require.NoError(t, err)

	serverError := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	serverError.OriginalStatusCode = http.StatusInternalServerError
	require.False(t, shouldRetryForRelayServerError(ctx, serverError, 1))
}
