package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// validateRelayResponseBoundary prevents an upstream attempt that produced no
// client-visible business response from being billed as a successful relay.
// The controller installs this boundary only for ordinary Relay requests, so
// direct adaptor tests and task/WebSocket relays keep their existing behavior.
func validateRelayResponseBoundary(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || !common.GetContextKeyBool(c, constant.ContextKeyRelayResponseBoundaryInstalled) {
		return nil
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return nil
	}
	if common.GetContextKeyBool(c, constant.ContextKeyRelayBusinessResponseWritten) {
		return nil
	}
	if info != nil && info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
		return nil
	}
	return helper.NewEmptyResponseError("")
}
