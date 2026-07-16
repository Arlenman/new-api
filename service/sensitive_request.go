package service

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

const (
	SensitiveRequestPromptMaxBytes          = 64 * 1024
	SensitiveRequestUpstreamMessageMaxBytes = 8 * 1024
)

type SensitiveRequestSnapshot struct {
	Prompt      string
	PromptBytes int
	Truncated   bool
}

type SensitiveUpstreamSignal struct {
	Reason          string
	ErrorCode       string
	StatusCode      int
	UpstreamMessage string
}

type sensitivePromptSection struct {
	role string
	text string
}

func BuildSensitiveRequestSnapshot(request dto.Request) SensitiveRequestSnapshot {
	sections := make([]sensitivePromptSection, 0)
	appendSection := func(role string, values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			sections = append(sections, sensitivePromptSection{role: normalizeSensitiveRole(role), text: value})
		}
	}

	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		for i := range r.Messages {
			message := &r.Messages[i]
			for _, part := range message.ParseContent() {
				if part.Type == dto.ContentTypeText && part.Text != "" {
					appendSection(message.Role, part.Text)
				}
			}
		}
		appendTextValue(appendSection, "prompt", r.Prompt)
		appendTextValue(appendSection, "prefix", r.Prefix)
		appendTextValue(appendSection, "suffix", r.Suffix)
		appendTextValue(appendSection, "input", r.Input)
		appendSection("instructions", r.Instruction)
	case *dto.OpenAIResponsesRequest:
		appendRawSensitiveText(&sections, "instructions", r.Instructions)
		appendRawSensitiveText(&sections, "input", r.Input)
		appendRawSensitiveText(&sections, "prompt", r.Prompt)
	case *dto.OpenAIResponsesCompactionRequest:
		appendRawSensitiveText(&sections, "instructions", r.Instructions)
		appendRawSensitiveText(&sections, "input", r.Input)
	case *dto.ClaudeRequest:
		if r.IsStringSystem() {
			appendSection("system", r.GetStringSystem())
		} else {
			for _, part := range r.ParseSystem() {
				if part.Type == dto.ContentTypeText {
					appendSection("system", part.GetText())
				}
			}
		}
		for i := range r.Messages {
			message := &r.Messages[i]
			if message.IsStringContent() {
				appendSection(message.Role, message.GetStringContent())
				continue
			}
			parts, err := message.ParseContent()
			if err != nil {
				continue
			}
			for _, part := range parts {
				if part.Type == dto.ContentTypeText {
					appendSection(message.Role, part.GetText())
				}
			}
		}
		appendSection("prompt", r.Prompt)
	case *dto.GeminiChatRequest:
		appendGeminiSensitiveText(appendSection, r)
	case *dto.GeminiEmbeddingRequest:
		appendGeminiEmbeddingSensitiveText(appendSection, r)
	case *dto.GeminiBatchEmbeddingRequest:
		for _, embeddingRequest := range r.Requests {
			appendGeminiEmbeddingSensitiveText(appendSection, embeddingRequest)
		}
	case *dto.RerankRequest:
		appendSection("query", r.Query)
		for _, document := range r.Documents {
			switch value := document.(type) {
			case string:
				appendSection("document", value)
			case map[string]any:
				if text, ok := value["text"].(string); ok {
					appendSection("document", text)
				}
			case dto.RerankDocument:
				appendTextValue(appendSection, "document", value.Text)
			case *dto.RerankDocument:
				if value != nil {
					appendTextValue(appendSection, "document", value.Text)
				}
			}
		}
	case *dto.EmbeddingRequest:
		for _, input := range r.ParseInput() {
			appendSection("input", input)
		}
	case *dto.ImageRequest:
		appendSection("prompt", r.Prompt)
	case *dto.AudioRequest:
		appendSection("input", r.Input)
		appendSection("instructions", r.Instructions)
	}

	var builder strings.Builder
	for _, section := range sections {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteByte('[')
		builder.WriteString(section.role)
		builder.WriteString("]\n")
		builder.WriteString(section.text)
	}

	prompt := builder.String()
	promptBytes := len(prompt)
	prompt, truncated := truncateSensitiveText(prompt, SensitiveRequestPromptMaxBytes)
	return SensitiveRequestSnapshot{
		Prompt:      prompt,
		PromptBytes: promptBytes,
		Truncated:   truncated,
	}
}

func appendTextValue(appendSection func(string, ...string), role string, value any) {
	switch typed := value.(type) {
	case string:
		appendSection(role, typed)
	case []string:
		appendSection(role, typed...)
	case []any:
		for _, item := range typed {
			appendTextValue(appendSection, role, item)
		}
	}
}

func appendGeminiEmbeddingSensitiveText(appendSection func(string, ...string), request *dto.GeminiEmbeddingRequest) {
	if request == nil {
		return
	}
	for _, part := range request.Content.Parts {
		appendSection("input", part.Text)
	}
}

func appendGeminiSensitiveText(appendSection func(string, ...string), request *dto.GeminiChatRequest) {
	if request == nil {
		return
	}
	if request.SystemInstructions != nil {
		for _, part := range request.SystemInstructions.Parts {
			appendSection("system", part.Text)
		}
	}
	for _, content := range request.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		for _, part := range content.Parts {
			appendSection(role, part.Text)
		}
	}
	for i := range request.Requests {
		appendGeminiSensitiveText(appendSection, &request.Requests[i])
	}
}

func appendRawSensitiveText(sections *[]sensitivePromptSection, defaultRole string, raw []byte) {
	if len(raw) == 0 {
		return
	}
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return
	}
	appendSensitiveJSONValue(sections, normalizeSensitiveRole(defaultRole), value)
}

func appendSensitiveJSONValue(sections *[]sensitivePromptSection, role string, value any) {
	switch typed := value.(type) {
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			*sections = append(*sections, sensitivePromptSection{role: normalizeSensitiveRole(role), text: text})
		}
	case []any:
		for _, item := range typed {
			appendSensitiveJSONValue(sections, role, item)
		}
	case map[string]any:
		itemRole := role
		if rawRole, ok := typed["role"].(string); ok && rawRole != "" {
			itemRole = normalizeSensitiveRole(rawRole)
		}
		itemType, _ := typed["type"].(string)
		if isSensitiveTextPartType(itemType) {
			if text, ok := typed["text"].(string); ok {
				appendSensitiveJSONValue(sections, itemRole, text)
			}
			return
		}
		if content, ok := typed["content"]; ok && (itemType == "" || strings.EqualFold(itemType, "message")) {
			appendSensitiveJSONValue(sections, itemRole, content)
			return
		}
		for _, key := range []string{"instructions", "prompt", "input"} {
			if nested, ok := typed[key]; ok {
				appendSensitiveJSONValue(sections, key, nested)
			}
		}
		if itemType == "" {
			if text, ok := typed["text"].(string); ok {
				appendSensitiveJSONValue(sections, itemRole, text)
			}
		}
	}
}

func isSensitiveTextPartType(partType string) bool {
	switch strings.ToLower(partType) {
	case "text", "input_text", "output_text":
		return true
	default:
		return false
	}
}

func normalizeSensitiveRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return "input"
	}
	return role
}

func truncateSensitiveText(value string, maxBytes int) (string, bool) {
	if maxBytes < 0 || len(value) <= maxBytes {
		return value, false
	}
	if maxBytes == 0 {
		return "", true
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}

func ClassifySensitiveUpstreamBlock(marker string, apiErr *types.NewAPIError) (SensitiveUpstreamSignal, bool) {
	signal := SensitiveUpstreamSignal{}
	markerLower := strings.ToLower(strings.TrimSpace(marker))

	switch {
	case strings.Contains(markerLower, "content_filter"):
		signal.Reason = "content_filter"
		signal.ErrorCode = "content_filter"
	case strings.Contains(markerLower, "claude_stop_reason=refusal"):
		signal.Reason = "refusal"
		signal.ErrorCode = "refusal"
	case strings.Contains(markerLower, "gemini_block_reason=") || strings.Contains(markerLower, "gemini_finish_reason="):
		value := markerLower[strings.LastIndex(markerLower, "=")+1:]
		if !isGeminiSafetyReason(value) {
			return SensitiveUpstreamSignal{}, false
		}
		signal.Reason = value
		signal.ErrorCode = value
	case strings.Contains(markerLower, "responses_incomplete_reason="):
		value := markerLower[strings.LastIndex(markerLower, "=")+1:]
		if value != "content_filter" {
			return SensitiveUpstreamSignal{}, false
		}
		signal.Reason = "content_filter"
		signal.ErrorCode = value
	}

	if apiErr != nil {
		signal.StatusCode = apiErr.StatusCode
		if apiErr.OriginalStatusCode != 0 {
			signal.StatusCode = apiErr.OriginalStatusCode
		}
		signal.UpstreamMessage, _ = truncateSensitiveText(apiErr.Error(), SensitiveRequestUpstreamMessageMaxBytes)
		errorCode := strings.ToLower(strings.TrimSpace(string(apiErr.GetErrorCode())))
		if signal.ErrorCode == "" {
			signal.ErrorCode = errorCode
		}
		if signal.Reason == "" && isStructuredSensitiveErrorCode(errorCode) {
			signal.Reason = "content_filter"
		}
		if signal.Reason == "" && containsSensitiveFallbackPhrase(strings.ToLower(apiErr.Error())) {
			signal.Reason = "content_filter"
		}
	}

	if signal.Reason == "" {
		return SensitiveUpstreamSignal{}, false
	}
	if signal.StatusCode == 0 && markerLower != "" && apiErr == nil {
		signal.StatusCode = http.StatusOK
	}
	return signal, true
}

func isGeminiSafetyReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "safety", "blocklist", "prohibited_content", "spii", "recitation":
		return true
	default:
		return false
	}
}

func isStructuredSensitiveErrorCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "content_filter", "prompt_blocked", "content_moderation_failed", "content_policy_violation", "moderation_blocked", string(types.ErrorCodeViolationFeeGrokCSAM):
		return true
	default:
		return false
	}
}

func containsSensitiveFallbackPhrase(message string) bool {
	for _, phrase := range []string{
		"sensitive word",
		"content moderation",
		"policy violation",
		"敏感词",
		"内容审核",
		"违规内容",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func SensitiveRequestFailureMetadata(source string, signal SensitiveUpstreamSignal, matchedWords []string, snapshot SensitiveRequestSnapshot) map[string]interface{} {
	if matchedWords == nil {
		matchedWords = []string{}
	}
	return map[string]interface{}{
		"source":           source,
		"reason":           signal.Reason,
		"error_code":       signal.ErrorCode,
		"status_code":      signal.StatusCode,
		"upstream_message": signal.UpstreamMessage,
		"matched_words":    matchedWords,
		"prompt":           snapshot.Prompt,
		"prompt_bytes":     snapshot.PromptBytes,
		"truncated":        snapshot.Truncated,
	}
}

func SensitiveRequestLogFailureMessage(source string, channelID int, err error) string {
	return fmt.Sprintf("failed to record sensitive request log: source=%s, channel_id=%d, error=%v", source, channelID, err)
}
