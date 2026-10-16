package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

// A Responses request mapped onto the chat completions endpoint must pass the
// chain of thought back on the assistant message, otherwise thinking-mode
// upstreams (DeepSeek with tools) answer with a 400.
func TestConvertOpenAIResponsesRequestForMappedChatPassesBackReasoning(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "deepseek-v4.1-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:         "deepseek-v4.1-flash",
			ChannelRequestPathMapping: `{"models":{"deepseek-v4.1-flash":{"/v1/responses":"/v1/chat/completions"}}}`,
		},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIResponsesRequestForUpstream(nil, info, &openai.Adaptor{}, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: []byte(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"check the build"}]},
			{"type":"reasoning","id":"68df115f-2c71-4835-940c-294955bf1dc2","summary":[],"content":[{"type":"reasoning_text","text":"I should run the tests first."}],"encrypted_content":"0942fe45-fffe-45dc-bebf-85a652be437d-0"},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"go\",\"test\",\"./...\"]}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
		Tools: []byte(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	})

	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, types.RelayFormatOpenAI, info.GetFinalRequestRelayFormat())
	require.Len(t, chatRequest.Messages, 3)
	require.Equal(t, "user", chatRequest.Messages[0].Role)

	require.Equal(t, "assistant", chatRequest.Messages[1].Role)
	require.NotNil(t, chatRequest.Messages[1].ReasoningContent)
	require.Equal(t, "I should run the tests first.", *chatRequest.Messages[1].ReasoningContent)
	require.Len(t, chatRequest.Messages[1].ParseToolCalls(), 1)

	require.Equal(t, "tool", chatRequest.Messages[2].Role)
	require.Equal(t, "call_1", chatRequest.Messages[2].ToolCallId)
}

// A Codex client that only ever received the chain of thought as a reasoning summary
// (which is what the Responses response conversion produces for a chat upstream) replays
// that summary and sends no `reasoning_text` content at all. The chat upstream still has
// to receive it back, so the summary is the fallback source.
func TestConvertOpenAIResponsesRequestForMappedChatFallsBackToReasoningSummary(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "deepseek-v4.1-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:         "deepseek-v4.1-flash",
			ChannelRequestPathMapping: `{"models":{"deepseek-v4.1-flash":{"/v1/responses":"/v1/chat/completions"}}}`,
		},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIResponsesRequestForUpstream(nil, info, &openai.Adaptor{}, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: []byte(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"check the build"}]},
			{"type":"reasoning","id":"68df115f-2c71-4835-940c-294955bf1dc2","summary":[{"type":"summary_text","text":"I should run the tests first."}],"content":null,"encrypted_content":null},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"go\",\"test\",\"./...\"]}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
		Tools: []byte(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	})

	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Messages, 3)
	require.Equal(t, "assistant", chatRequest.Messages[1].Role)
	require.NotNil(t, chatRequest.Messages[1].ReasoningContent)
	require.Equal(t, "I should run the tests first.", *chatRequest.Messages[1].ReasoningContent)
	require.Len(t, chatRequest.Messages[1].ParseToolCalls(), 1)
}

// Without a request path mapping the Responses request keeps its native
// `reasoning` input items, so a Responses client such as Codex reaches a
// Responses upstream with its chain of thought intact.
func TestConvertOpenAIResponsesRequestForUnmappedEndpointKeepsReasoningItems(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "deepseek-v4.1-flash",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "deepseek-v4.1-flash"},
	}
	info.InitRequestConversionChain()

	converted, err := ConvertOpenAIResponsesRequestForUpstream(nil, info, &openai.Adaptor{}, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: []byte(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"check the build"}]},
			{"type":"reasoning","id":"68df115f-2c71-4835-940c-294955bf1dc2","summary":[],"content":[{"type":"reasoning_text","text":"I should run the tests first."}],"encrypted_content":"0942fe45-fffe-45dc-bebf-85a652be437d-0"},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"go\",\"test\",\"./...\"]}"}
		]`),
	})

	require.NoError(t, err)
	responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
	require.Equal(t, "reasoning", gjson.GetBytes(responsesRequest.Input, "1.type").String())
	require.Equal(t, "reasoning_text", gjson.GetBytes(responsesRequest.Input, "1.content.0.type").String())
	require.Equal(t, "I should run the tests first.", gjson.GetBytes(responsesRequest.Input, "1.content.0.text").String())
	require.Equal(t, "function_call", gjson.GetBytes(responsesRequest.Input, "2.type").String())
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

// A chat request mapped onto the Responses endpoint must pass the assistant
// chain of thought back, otherwise thinking-mode upstreams answer with a 400.
func TestConvertOpenAIRequestForMappedResponsesPassesBackReasoning(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "deepseek-v4-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:         "deepseek-v4-pro",
			ChannelRequestPathMapping: `{"channel":{"/v1/chat/completions":"/v1/responses"}}`,
		},
	}
	info.InitRequestConversionChain()

	reasoning := "I should call the lookup tool."
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.ReasoningContent = &reasoning
	assistant.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		},
	})

	converted, err := ConvertOpenAIRequestForUpstream(nil, info, &openai.Adaptor{}, &dto.GeneralOpenAIRequest{
		Model: "deepseek-v4-pro",
		Messages: []dto.Message{
			{Role: "user", Content: "look it up"},
			assistant,
			{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
		},
	})
	require.NoError(t, err)

	responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
	require.Equal(t, "reasoning", gjson.GetBytes(responsesRequest.Input, "1.type").String())
	require.Equal(t, "reasoning_text", gjson.GetBytes(responsesRequest.Input, "1.content.0.type").String())
	require.Equal(t, reasoning, gjson.GetBytes(responsesRequest.Input, "1.content.0.text").String())
	require.Equal(t, "assistant", gjson.GetBytes(responsesRequest.Input, "2.role").String())
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
