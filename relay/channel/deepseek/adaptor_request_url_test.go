package deepseek

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLKeepsDeepSeekBetaRoutingWithoutMapping(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeCompletions,
		RequestURLPath: "/v1/completions",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.deepseek.com",
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/beta/completions", requestURL)
}

func TestGetRequestURLUsesMappedPath(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:               constant.ChannelTypeOpenAI,
			ChannelBaseUrl:            "https://api.deepseek.com",
			ChannelRequestPathMapping: `{"channel":{"/v1/responses":"/v1/chat/completions"}}`,
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/v1/chat/completions", requestURL)
}
