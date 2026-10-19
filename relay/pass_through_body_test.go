package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func newChatRelayInfo(setting dto.ChannelSettings) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelSetting: setting,
		},
	}
}

func TestShouldPassThroughChannelBody(t *testing.T) {
	t.Run("global pass-through keeps native chat body untouched", func(t *testing.T) {
		assert.True(t, shouldPassThroughChannelBody(newChatRelayInfo(dto.ChannelSettings{}), true))
	})

	t.Run("reasoning_content compat disables pass-through", func(t *testing.T) {
		info := newChatRelayInfo(dto.ChannelSettings{ReasoningContentCompat: true})
		assert.False(t, shouldPassThroughChannelBody(info, true))
	})

	t.Run("channel level pass-through is also disabled by the compat switch", func(t *testing.T) {
		info := newChatRelayInfo(dto.ChannelSettings{
			PassThroughBodyEnabled: true,
			ReasoningContentCompat: true,
		})
		assert.False(t, shouldPassThroughChannelBody(info, false))
	})

	t.Run("pass-through off keeps the conversion path", func(t *testing.T) {
		assert.False(t, shouldPassThroughChannelBody(newChatRelayInfo(dto.ChannelSettings{}), false))
	})
}
