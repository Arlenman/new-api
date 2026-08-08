package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRelayResponseBoundaryRejectsEmptySuccessfulAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyRelayResponseBoundaryInstalled, true)
	common.SetContextKey(ctx, constant.ContextKeyRelayBusinessResponseWritten, false)

	err := validateRelayResponseBoundary(ctx, &relaycommon.RelayInfo{})

	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
}

func TestValidateRelayResponseBoundaryAcceptsOnlyClientBusinessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	businessCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	businessCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(businessCtx, constant.ContextKeyRelayResponseBoundaryInstalled, true)
	common.SetContextKey(businessCtx, constant.ContextKeyRelayBusinessResponseWritten, true)
	require.Nil(t, validateRelayResponseBoundary(businessCtx, &relaycommon.RelayInfo{}))

	countedCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	countedCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(countedCtx, constant.ContextKeyRelayResponseBoundaryInstalled, true)
	err := validateRelayResponseBoundary(countedCtx, &relaycommon.RelayInfo{ReceivedResponseCount: 1})
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
}

func TestValidateRelayResponseBoundarySkipsDirectCallsAndDisconnectedClients(t *testing.T) {
	gin.SetMode(gin.TestMode)

	directCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	directCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, validateRelayResponseBoundary(directCtx, &relaycommon.RelayInfo{}))

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	disconnectedCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	disconnectedCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	common.SetContextKey(disconnectedCtx, constant.ContextKeyRelayResponseBoundaryInstalled, true)
	require.Nil(t, validateRelayResponseBoundary(disconnectedCtx, &relaycommon.RelayInfo{}))
}
