package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Claude assistant turns carry the chain of thought in `thinking` blocks. Chat
// completions (and every format converted from it, including the Responses API)
// keep the same information in `reasoning_content`, so it must survive the
// conversion: thinking-mode upstreams reject a tool-carrying request whose
// previous turns lost their chain of thought.
func TestClaudeMessagesRequestToOpenAIChatPreservesAssistantThinking(t *testing.T) {
	thinking := "Let me check the weather first."
	req := dto.ClaudeRequest{
		Model: "claude-sonnet-4-5",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "what is the weather in Paris?"},
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{Type: "thinking", Thinking: &thinking, Signature: "sig-abc"},
					{Type: "tool_use", Id: "toolu_1", Name: "get_weather", Input: map[string]any{"city": "Paris"}},
				},
			},
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "tool_result", ToolUseId: "toolu_1", Content: "15 degrees"},
				},
			},
		},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(req, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)

	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "assistant", got.Messages[1].Role)
	assert.Equal(t, thinking, got.Messages[1].GetReasoningContent())
	toolCalls := got.Messages[1].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "toolu_1", toolCalls[0].ID)
	assert.Equal(t, "get_weather", toolCalls[0].Function.Name)
	assert.Equal(t, "tool", got.Messages[2].Role)
	assert.Equal(t, "toolu_1", got.Messages[2].ToolCallId)
}

func TestClaudeMessagesRequestToOpenAIChatSkipsBlankAssistantThinking(t *testing.T) {
	blank := "  "
	req := dto.ClaudeRequest{
		Model: "claude-sonnet-4-5",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hi"},
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{Type: "thinking", Thinking: &blank},
					{Type: "text", Text: strPtr("hello")},
				},
			},
		},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(req, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)

	assert.Equal(t, "assistant", got.Messages[1].Role)
	assert.Empty(t, got.Messages[1].GetReasoningContent())
	parts := got.Messages[1].ParseContent()
	require.Len(t, parts, 1)
	assert.Equal(t, "hello", parts[0].Text)
}

func strPtr(s string) *string {
	return &s
}
