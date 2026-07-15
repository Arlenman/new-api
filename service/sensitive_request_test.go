package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSensitiveRequestSnapshotPreservesRolesAndExcludesMedia(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "system", Content: "follow policy"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "inspect this prompt"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,SECRET_IMAGE"}},
				map[string]any{"type": "file", "file": map[string]any{"file_data": "SECRET_FILE"}},
			}},
			{Role: "assistant", Content: "previous answer"},
		},
		Instruction: "be concise",
	}

	snapshot := BuildSensitiveRequestSnapshot(request)

	assert.Contains(t, snapshot.Prompt, "[system]\nfollow policy")
	assert.Contains(t, snapshot.Prompt, "[user]\ninspect this prompt")
	assert.Contains(t, snapshot.Prompt, "[assistant]\nprevious answer")
	assert.Contains(t, snapshot.Prompt, "[instructions]\nbe concise")
	assert.NotContains(t, snapshot.Prompt, "SECRET_IMAGE")
	assert.NotContains(t, snapshot.Prompt, "SECRET_FILE")
	assert.False(t, snapshot.Truncated)
	assert.Equal(t, len(snapshot.Prompt), snapshot.PromptBytes)
}

func TestBuildSensitiveRequestSnapshotExtractsResponsesClaudeAndGeminiText(t *testing.T) {
	responses := &dto.OpenAIResponsesRequest{
		Instructions: json.RawMessage(`"system instruction"`),
		Input: json.RawMessage(`[
			{"role":"user","content":[
				{"type":"input_text","text":"response input"},
				{"type":"input_image","image_url":"https://example.com/private.png"},
				{"type":"input_file","file_url":"https://example.com/private.pdf"}
			]}
		]`),
	}
	responseSnapshot := BuildSensitiveRequestSnapshot(responses)
	assert.Contains(t, responseSnapshot.Prompt, "[instructions]\nsystem instruction")
	assert.Contains(t, responseSnapshot.Prompt, "[user]\nresponse input")
	assert.NotContains(t, responseSnapshot.Prompt, "example.com")

	claude := &dto.ClaudeRequest{
		System: "claude system",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "claude text"},
				map[string]any{"type": "image", "source": map[string]any{"data": "SECRET_CLAUDE_IMAGE"}},
			},
		}},
	}
	claudeSnapshot := BuildSensitiveRequestSnapshot(claude)
	assert.Contains(t, claudeSnapshot.Prompt, "[system]\nclaude system")
	assert.Contains(t, claudeSnapshot.Prompt, "[user]\nclaude text")
	assert.NotContains(t, claudeSnapshot.Prompt, "SECRET_CLAUDE_IMAGE")

	gemini := &dto.GeminiChatRequest{
		SystemInstructions: &dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "gemini system"}}},
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{
				{Text: "gemini text"},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "SECRET_GEMINI_IMAGE"}},
			},
		}},
	}
	geminiSnapshot := BuildSensitiveRequestSnapshot(gemini)
	assert.Contains(t, geminiSnapshot.Prompt, "[system]\ngemini system")
	assert.Contains(t, geminiSnapshot.Prompt, "[user]\ngemini text")
	assert.NotContains(t, geminiSnapshot.Prompt, "SECRET_GEMINI_IMAGE")
}

func TestBuildSensitiveRequestSnapshotExtractsGeminiEmbeddingText(t *testing.T) {
	request := &dto.GeminiBatchEmbeddingRequest{
		Requests: []*dto.GeminiEmbeddingRequest{
			{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "first embedding input"}}}},
			{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Text: "second embedding input"},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "SECRET_EMBEDDING_IMAGE"}},
			}}},
		},
	}

	snapshot := BuildSensitiveRequestSnapshot(request)

	assert.Contains(t, snapshot.Prompt, "[input]\nfirst embedding input")
	assert.Contains(t, snapshot.Prompt, "[input]\nsecond embedding input")
	assert.NotContains(t, snapshot.Prompt, "SECRET_EMBEDDING_IMAGE")
}

func TestBuildSensitiveRequestSnapshotTruncatesUTF8Safely(t *testing.T) {
	request := &dto.ImageRequest{Prompt: strings.Repeat("中文", SensitiveRequestPromptMaxBytes)}

	snapshot := BuildSensitiveRequestSnapshot(request)

	require.True(t, snapshot.Truncated)
	assert.Greater(t, snapshot.PromptBytes, SensitiveRequestPromptMaxBytes)
	assert.LessOrEqual(t, len(snapshot.Prompt), SensitiveRequestPromptMaxBytes)
	assert.True(t, utf8.ValidString(snapshot.Prompt))
	assert.Contains(t, snapshot.Prompt, "[prompt]\n")
}

func TestClassifySensitiveUpstreamBlockUsesStructuredSignals(t *testing.T) {
	tests := []struct {
		name   string
		marker string
		err    *types.NewAPIError
		reason string
	}{
		{
			name:   "openai finish reason",
			marker: "openai_finish_reason=content_filter",
			reason: "content_filter",
		},
		{
			name:   "gemini safety",
			marker: "gemini_finish_reason=SAFETY",
			reason: "safety",
		},
		{
			name:   "claude refusal",
			marker: "claude_stop_reason=refusal",
			reason: "refusal",
		},
		{
			name: "structured error code",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "request rejected",
				Type:    "invalid_request_error",
				Code:    "prompt_blocked",
			}, http.StatusBadRequest),
			reason: "content_filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, ok := ClassifySensitiveUpstreamBlock(tt.marker, tt.err)
			require.True(t, ok)
			assert.Equal(t, tt.reason, signal.Reason)
			if tt.marker != "" && tt.err == nil {
				assert.Equal(t, http.StatusOK, signal.StatusCode)
			}
		})
	}
}

func TestClassifySensitiveUpstreamBlockUsesControlledPhrasesOnly(t *testing.T) {
	positive := types.WithOpenAIError(types.OpenAIError{
		Message: "request failed content moderation",
		Code:    "bad_request",
	}, http.StatusBadRequest)
	signal, ok := ClassifySensitiveUpstreamBlock("", positive)
	require.True(t, ok)
	assert.Equal(t, "content_filter", signal.Reason)
	assert.Equal(t, "bad_request", signal.ErrorCode)

	negativeMessages := []string{
		"authentication failed",
		"insufficient quota",
		"network timeout",
		"request blocked",
		"safety configuration is invalid",
	}
	for _, message := range negativeMessages {
		t.Run(message, func(t *testing.T) {
			err := types.WithOpenAIError(types.OpenAIError{Message: message, Code: "bad_request"}, http.StatusBadRequest)
			_, ok := ClassifySensitiveUpstreamBlock("", err)
			assert.False(t, ok)
		})
	}
}

func TestSensitiveRequestFailureMetadataNormalizesMatchedWords(t *testing.T) {
	metadata := SensitiveRequestFailureMetadata(
		"upstream",
		SensitiveUpstreamSignal{Reason: "content_filter"},
		nil,
		SensitiveRequestSnapshot{Prompt: "[user]\nprompt", PromptBytes: 13},
	)

	matchedWords, ok := metadata["matched_words"].([]string)
	require.True(t, ok)
	assert.Empty(t, matchedWords)
}
