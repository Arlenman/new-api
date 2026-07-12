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
