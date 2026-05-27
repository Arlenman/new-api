package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestShouldRetryDoesNotUseAutomaticDisableStatusCodesForAlwaysSkipStatusCodes(t *testing.T) {
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
		{Start: http.StatusInternalServerError, End: 599},
	}

	err := types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout)

	require.False(t, shouldRetry(ctx, err, 1))
}
