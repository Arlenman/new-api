package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAzureUsesMappedPath(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:               constant.ChannelTypeAzure,
			ChannelBaseUrl:            "https://azure.example",
			ChannelRequestPathMapping: `{"channel":{"/v1/responses":"/v1/chat/completions"}}`,
			UpstreamModelName:         "gpt-4o",
			ApiVersion:                "2024-10-21",
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://azure.example/openai/deployments/gpt-4o/chat/completions?api-version=2024-10-21", requestURL)
}

func TestGetRequestURLAzureKeepsResponsesEndpointWithoutMapping(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeAzure,
			ChannelBaseUrl: "https://azure.example",
			ApiVersion:     "2024-10-21",
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://azure.example/openai/v1/responses?api-version=preview", requestURL)
}
