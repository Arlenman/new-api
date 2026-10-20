package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsResponseToResponsesPreservesTextToolCallsAndUsage(t *testing.T) {
	chat := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 456,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      assistantMessageWithTool("I will call.", "call_1", "lookup", `{"q":"x"}`),
				FinishReason: "tool_calls",
			},
		},
		Usage: dto.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
	}

	resp, usage, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, "resp_1", resp.ID)
	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, `"completed"`, string(resp.Status))
	assert.Equal(t, 3, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	require.Len(t, resp.Output, 2)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[0].Type)
	assert.Equal(t, "I will call.", resp.Output[0].Content[0].Text)
	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[1].Type)
	assert.Equal(t, "call_1", resp.Output[1].CallId)
	assert.Equal(t, "lookup", resp.Output[1].Name)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(resp.Output[1].Arguments))
}

func TestChatCompletionsResponseToResponsesMapsIncompleteFinishReasons(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantReason   string
	}{
		{name: "length", finishReason: "length", wantReason: responsesIncompleteReasonMaxTokens},
		{name: "content filter", finishReason: "content_filter", wantReason: responsesIncompleteReasonContentFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{
						Message:      dto.Message{Role: "assistant", Content: "partial"},
						FinishReason: tt.finishReason,
					},
				},
			}, "resp_1")
			require.NoError(t, err)

			assert.Equal(t, `"incomplete"`, string(resp.Status))
			require.NotNil(t, resp.IncompleteDetails)
			assert.Equal(t, tt.wantReason, resp.IncompleteDetails.Reason)
			require.Len(t, resp.Output, 1)
			assert.Equal(t, "incomplete", resp.Output[0].Status)
		})
	}
}

func TestChatCompletionsResponseToResponsesReasoningItemCarriesSummaryAndReasoningText(t *testing.T) {
	reasoning := "Deep thought."
	chat := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 456,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      dto.Message{Role: "assistant", Content: "answer", ReasoningContent: lo.ToPtr(reasoning)},
				FinishReason: "stop",
			},
		},
	}

	resp, _, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1")
	require.NoError(t, err)
	require.Len(t, resp.Output, 2)

	item := resp.Output[1]
	require.Equal(t, responsesOutputTypeReasoning, item.Type)
	require.NotNil(t, item.Summary)
	require.Len(t, *item.Summary, 1)
	assert.Equal(t, "summary_text", (*item.Summary)[0].Type)
	assert.Equal(t, reasoning, (*item.Summary)[0].Text)
	require.Len(t, item.Content, 1)
	assert.Equal(t, "reasoning_text", item.Content[0].Type)
	assert.Equal(t, reasoning, item.Content[0].Text)

	// Responses clients deserialize `summary` as a required array and only replay
	// a reasoning item when `content` carries a `reasoning_text` part, so the wire
	// shape is part of the contract.
	data, err := kitutil.Marshal(item)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"summary":[{"type":"summary_text","text":"Deep thought."}]`)
	assert.Contains(t, string(data), `"content":[{"type":"reasoning_text","text":"Deep thought.","annotations":[]}]`)
}

func TestChatCompletionsStreamToResponsesReasoningItemCarriesSummaryAndReasoningText(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.Created = 123

	events := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: lo.ToPtr("Deep thought.")}},
		},
	})
	finishReason := "stop"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	var added, done *dto.ResponsesOutput
	for _, event := range events {
		if event.Payload.Item == nil || event.Payload.Item.Type != responsesOutputTypeReasoning {
			continue
		}
		switch event.Type {
		case responsesEventOutputItemAdded:
			added = event.Payload.Item
		case responsesEventOutputItemDone:
			done = event.Payload.Item
		}
	}
	require.NotNil(t, added)
	require.NotNil(t, added.Summary)
	require.NotNil(t, added.Content)
	addedData, err := kitutil.Marshal(added)
	require.NoError(t, err)
	assert.Contains(t, string(addedData), `"summary":[]`)
	assert.Contains(t, string(addedData), `"content":[]`)

	require.NotNil(t, done)
	require.NotNil(t, done.Summary)
	require.Len(t, *done.Summary, 1)
	assert.Equal(t, "summary_text", (*done.Summary)[0].Type)
	assert.Equal(t, "Deep thought.", (*done.Summary)[0].Text)
	require.Len(t, done.Content, 1)
	assert.Equal(t, "reasoning_text", done.Content[0].Type)
	assert.Equal(t, "Deep thought.", done.Content[0].Text)

	// The completed response repeats the same item, so clients that only look at
	// the terminal event still keep the chain of thought.
	final := events[len(events)-1].Payload.Response
	require.NotNil(t, final)
	var finalReasoning *dto.ResponsesOutput
	for i := range final.Output {
		if final.Output[i].Type == responsesOutputTypeReasoning {
			finalReasoning = &final.Output[i]
		}
	}
	require.NotNil(t, finalReasoning)
	require.NotNil(t, finalReasoning.Summary)
	require.Len(t, *finalReasoning.Summary, 1)
	assert.Equal(t, "Deep thought.", (*finalReasoning.Summary)[0].Text)
	require.Len(t, finalReasoning.Content, 1)
	assert.Equal(t, "reasoning_text", finalReasoning.Content[0].Type)
}

func TestChatCompletionsStreamToResponsesEventsAggregatesUsageAndToolArgs(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.Created = 123
	toolIndex := 0

	var events []ChatToResponsesStreamEvent
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 123,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: lo.ToPtr("hello")}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "lookup"}},
			}}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, Function: dto.FunctionResponse{Arguments: `{"q":"x"}`}},
			}}},
		},
	})...)
	finishReason := "tool_calls"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 4, TotalTokens: 6},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	require.Len(t, events, 10)
	assert.Equal(t, responsesEventCreated, events[0].Type)
	assert.Equal(t, responsesEventOutputTextDelta, events[2].Type)
	assert.Equal(t, "hello", events[2].Payload.Delta)
	assert.Equal(t, responsesEventFunctionArgsDelta, events[4].Type)
	assert.Equal(t, `{"q":"x"}`, events[4].Payload.Delta)
	assert.Equal(t, responsesEventCompleted, events[9].Type)
	require.NotNil(t, events[9].Payload.Response)
	assert.Equal(t, 6, events[9].Payload.Response.Usage.TotalTokens)
	require.Len(t, events[9].Payload.Response.Output, 2)
	assert.Equal(t, "hello", events[9].Payload.Response.Output[0].Content[0].Text)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(events[9].Payload.Response.Output[1].Arguments))
}

func mustResponsesEventsFromChatChunk(t *testing.T, state *ChatToResponsesStreamState, chunk *dto.ChatCompletionsStreamResponse) []ChatToResponsesStreamEvent {
	t.Helper()
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	return events
}
