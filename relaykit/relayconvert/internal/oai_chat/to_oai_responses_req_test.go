package oaichat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsRequestToResponsesRequestInstructionsAndTools(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(1),
		Messages: []dto.Message{
			{Role: "system", Content: "system rules"},
			{Role: "developer", Content: "developer rules"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
			}},
			assistantMessageWithTool("partial text", "call_1", "lookup", `{"q":"x"}`),
			{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
		},
	}

	got, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	assert.Equal(t, `"system rules\n\ndeveloper rules"`, string(got.Instructions))
	assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.content.1.type").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
	assert.Equal(t, "call_1", gjson.GetBytes(got.Input, "2.call_id").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "3.type").String())
}

func TestChatCompletionsRequestToResponsesRequestPreservesQwenThinkingBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget json.RawMessage
		want   int64
	}{
		{name: "positive budget", budget: json.RawMessage(`128`), want: 128},
		{name: "zero budget", budget: json.RawMessage(`0`), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model:          "qwen-plus",
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: tt.budget,
				Messages: []dto.Message{
					{Role: "user", Content: "hello"},
				},
			}

			got, err := ChatCompletionsRequestToResponsesRequest(req)
			require.NoError(t, err)
			assert.Equal(t, tt.budget, got.ThinkingBudget)

			encoded, err := kitutil.Marshal(got)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.True(t, value.Exists())
			assert.Equal(t, tt.want, value.Int())
		})
	}
}

func TestChatCompletionsRequestToResponsesRequestRejectsMultipleChoices(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n>1")
}

// A thinking-mode Responses upstream (for example DeepSeek) rejects a request
// that carries tools but does not pass the assistant chain of thought back, so
// the chat `reasoning_content` has to be re-emitted as a `reasoning` input item
// whose content part type is `reasoning_text`.
func TestChatCompletionsRequestToResponsesRequestPassesBackAssistantReasoning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		apply         func(msg *dto.Message)
		wantReasoning string
	}{
		{
			name:          "reasoning_content on a tool call turn",
			content:       "",
			apply:         func(msg *dto.Message) { msg.ReasoningContent = lo.ToPtr("I should call the lookup tool.") },
			wantReasoning: "I should call the lookup tool.",
		},
		{
			name:          "reasoning alias on a text turn",
			content:       "the answer is 42",
			apply:         func(msg *dto.Message) { msg.Reasoning = lo.ToPtr("Let me think.") },
			wantReasoning: "Let me think.",
		},
		{
			name:    "blank reasoning_content is not sent",
			content: "the answer is 42",
			apply:   func(msg *dto.Message) { msg.ReasoningContent = lo.ToPtr("   ") },
		},
		{
			name:    "assistant message without reasoning",
			content: "the answer is 42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assistant := assistantMessageWithTool(tt.content, "call_1", "lookup", `{"q":"x"}`)
			if tt.apply != nil {
				tt.apply(&assistant)
			}

			req := &dto.GeneralOpenAIRequest{
				Model: "deepseek-v4-pro",
				Messages: []dto.Message{
					{Role: "user", Content: "look it up"},
					assistant,
					{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
				},
			}

			got, err := ChatCompletionsRequestToResponsesRequest(req)
			require.NoError(t, err)

			if tt.wantReasoning == "" {
				assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "1.role").String())
				assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
				assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "3.type").String())
				return
			}

			assert.Equal(t, "reasoning", gjson.GetBytes(got.Input, "1.type").String())
			assert.Equal(t, "reasoning_text", gjson.GetBytes(got.Input, "1.content.0.type").String())
			assert.Equal(t, tt.wantReasoning, gjson.GetBytes(got.Input, "1.content.0.text").String())
			// The reasoning item is merged into the adjacent assistant message
			// upstream, so it must stay directly in front of that turn.
			assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "2.role").String())
			assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "3.type").String())
			assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "4.type").String())
		})
	}
}

func assistantMessageWithTool(content string, id string, name string, args string) dto.Message {
	msg := dto.Message{Role: "assistant", Content: content}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   id,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: args,
			},
		},
	})
	return msg
}
