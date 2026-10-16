package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

// The chat -> responses -> chat round trip must keep the assistant chain of
// thought: thinking-mode upstreams reject requests that carry tools when the
// chain of thought is not passed back, and chained deployments convert a
// request in both directions.
func TestChatResponsesRoundTripKeepsAssistantReasoning(t *testing.T) {
	reasoning := "I should run the tests first."
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.ReasoningContent = &reasoning
	assistant.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "shell",
				Arguments: `{"command":["go","test","./..."]}`,
			},
		},
	})

	responsesRequest, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "deepseek-v4.1-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "check the build"},
			assistant,
			{Role: "tool", ToolCallId: "call_1", Content: "ok"},
		},
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "shell"}},
		},
	})
	require.NoError(t, err)

	chatRequest, err := ResponsesRequestToChatCompletionsRequest(responsesRequest)
	require.NoError(t, err)

	require.Len(t, chatRequest.Messages, 3)
	require.Equal(t, "user", chatRequest.Messages[0].Role)
	require.Equal(t, "assistant", chatRequest.Messages[1].Role)
	require.NotNil(t, chatRequest.Messages[1].ReasoningContent)
	require.Equal(t, reasoning, *chatRequest.Messages[1].ReasoningContent)
	require.Len(t, chatRequest.Messages[1].ParseToolCalls(), 1)
	require.Equal(t, "tool", chatRequest.Messages[2].Role)
}
