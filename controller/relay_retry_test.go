package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
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
	require.False(t, shouldStopPlaygroundImageGatewayTimeoutRetry(ctx, 106, err, 1))
	require.False(t, shouldStopPlaygroundImageGatewayTimeoutRetry(ctx, 107, err, 1))
	require.False(t, shouldStopPlaygroundImageGatewayTimeoutRetry(ctx, 106, err, 0))

	ctx.Set("use_channel", []string{"106", "106"})
	require.True(t, shouldStopPlaygroundImageGatewayTimeoutRetry(ctx, 106, err, 2))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	require.False(t, shouldStopPlaygroundImageGatewayTimeoutRetry(ctx, 106, err, 1))
}

func TestShouldRetrySkipsPlaygroundImageGatewayTimeoutWhenOnlySameChannelAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Set("use_channel", []string{"106"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundImageCandidateChannelCount, 1)

	err := types.NewErrorWithStatusCode(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524)
	require.False(t, shouldRetry(ctx, err, 1))

	ctx.Set("use_channel", []string{"106", "107"})
	common.SetContextKey(ctx, constant.ContextKeyPlaygroundImageCandidateChannelCount, 2)
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
