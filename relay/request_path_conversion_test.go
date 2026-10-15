package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestForMappedChat(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "glm-5.3",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:         "glm-5.3",
			ChannelRequestPathMapping: `{"models":{"glm-5.3":{"/v1/responses":"/v1/chat/completions"}}}`,
		},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIResponsesRequestForUpstream(nil, info, &openai.Adaptor{}, dto.OpenAIResponsesRequest{
		Model: "glm-5.3",
		Input: []byte(`[{"role":"user","content":"hello"}]`),
	})

	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Messages, 1)
	require.Equal(t, "hello", chatRequest.Messages[0].StringContent())
	require.Equal(t, types.RelayFormatOpenAI, info.GetFinalRequestRelayFormat())
}

func TestConvertOpenAIRequestForMappedResponses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "glm-5.3",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:         "glm-5.3",
			ChannelRequestPathMapping: `{"models":{"glm-5.3":{"/v1/chat/completions":"/v1/responses"}}}`,
		},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIRequestForUpstream(nil, info, &openai.Adaptor{}, &dto.GeneralOpenAIRequest{
		Model: "glm-5.3",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	})

	require.NoError(t, err)
	responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.NotEmpty(t, responsesRequest.Input)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestConvertOpenAIResponsesRequestForUnmappedEndpointKeepsResponses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "glm-5.3",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "glm-5.3"},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIResponsesRequestForUpstream(nil, info, &openai.Adaptor{}, dto.OpenAIResponsesRequest{
		Model: "glm-5.3",
		Input: []byte(`[{"role":"user","content":"hello"}]`),
	})

	require.NoError(t, err)
	_, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestShouldPassThroughRequestBodyDisablesPassthroughForMappedEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAIResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelRequestPathMapping: `{"channel":{"/v1/responses":"/v1/chat/completions"}}`,
		},
	}

	require.False(t, shouldPassThroughRequestBody(info, true))
	require.False(t, shouldPassThroughRequestBody(info, false))

	info.ChannelRequestPathMapping = ""
	require.True(t, shouldPassThroughRequestBody(info, true))
}
