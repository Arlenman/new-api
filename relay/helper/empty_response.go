package helper

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func NewEmptyResponseError(message string) *types.NewAPIError {
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
}

func ValidateStreamResponse(info *relaycommon.RelayInfo) *types.NewAPIError {
	if info != nil {
		if info.ReceivedResponseCount > 0 {
			return nil
		}
		if info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
			return nil
		}
	}
	return NewEmptyResponseError("upstream returned no valid SSE data")
}

func ValidateResponseBody(body []byte) *types.NewAPIError {
	if HasBusinessResponseData(body) {
		return nil
	}
	return NewEmptyResponseError("")
}

// HasBusinessResponseData reports whether body contains an actual model result.
// Transport metadata (roles, usage, finish reasons and lifecycle events) is not
// enough. The checks below intentionally follow the response shapes supported by
// the relay adapters instead of treating every non-empty string as output.
func HasBusinessResponseData(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
		return false
	}

	var value any
	if err := common.Unmarshal(trimmed, &value); err != nil {
		// A malformed text/HTML response is not evidence of a successful model
		// response and must remain retryable instead of being copied as HTTP 200.
		return false
	}
	return jsonValueHasBusinessData(value, "")
}

func jsonValueHasBusinessData(value any, key string) bool {
	if value == nil {
		return false
	}

	switch value := value.(type) {
	case string:
		return isBusinessTextKey(key) && strings.TrimSpace(value) != ""
	case bool, float64:
		return isBusinessResultKey(key)
	case []any:
		return businessArrayHasData(value, key)
	case map[string]any:
		return businessObjectHasData(value, key)
	default:
		return false
	}
}

func businessObjectHasData(object map[string]any, key string) bool {
	if responseObjectIndicatesFailure(object) {
		return false
	}

	if hasBusinessData, isResponsesEvent := openAIResponsesEventHasBusinessData(object); isResponsesEvent {
		return hasBusinessData
	}
	if hasBusinessData, isClaudeEvent := claudeEventHasBusinessData(object); isClaudeEvent {
		return hasBusinessData
	}
	if itemType, ok := object["type"].(string); ok {
		switch normalizeResponseMetadataKey(itemType) {
		case "function_call", "reasoning", "message", "image_generation_call", "refusal":
			if responsesOutputValueHasBusinessData(object) {
				return true
			}
		case "tool_use", "redacted_thinking":
			if claudeContentBlockHasBusinessData(object) {
				return true
			}
		}
	}

	for childKey, childValue := range object {
		normalizedKey := normalizeResponseMetadataKey(childKey)
		switch normalizedKey {
		case "choices":
			if openAIChoicesHaveBusinessData(childValue) {
				return true
			}
		case "candidates":
			if geminiCandidatesHaveBusinessData(childValue) {
				return true
			}
		case "data":
			if responseDataHasBusinessData(childValue) {
				return true
			}
		case "embedding", "embeddings":
			if embeddingValueHasBusinessData(childValue) {
				return true
			}
		case "results":
			if rerankResultsHaveBusinessData(childValue) {
				return true
			}
		case "function", "function_call":
			if functionObjectHasBusinessData(childValue) {
				return true
			}
		case "content", "delta", "file_data", "image_url", "inline_data", "input", "item", "message", "output", "output_text", "part", "parts", "reasoning", "reasoning_content", "result", "text", "tool_calls", "url", "b64_json", "audio", "answer", "arguments", "executable_code", "code_execution_result", "partial_image_b64", "content_block", "summary", "thinking", "completion":
			if jsonValueHasBusinessData(childValue, normalizedKey) {
				return true
			}
		}
	}
	return false
}

func responseObjectIndicatesFailure(object map[string]any) bool {
	if value, exists := object["error"]; exists && value != nil {
		return true
	}
	if value, exists := object["success"]; exists {
		if success, ok := value.(bool); ok && !success {
			return true
		}
	}
	if value, exists := object["code"]; exists {
		if code, ok := value.(float64); ok && code != 0 {
			return true
		}
		if code, ok := value.(string); ok && strings.TrimSpace(code) != "" && code != "0" {
			return true
		}
	}
	if value, exists := object["status"]; exists {
		if status, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "error", "failed", "failure", "cancelled", "canceled", "aborted":
				return true
			}
		}
	}
	if value, exists := object["type"]; exists {
		if eventType, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(eventType), "error") {
			return true
		}
	}
	return false
}

func openAIChoicesHaveBusinessData(value any) bool {
	choices, ok := value.([]any)
	if !ok {
		return false
	}
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok || responseObjectIndicatesFailure(choice) {
			continue
		}
		for key, child := range choice {
			switch normalizeResponseMetadataKey(key) {
			case "message", "delta":
				if chatObjectHasBusinessData(child) {
					return true
				}
			case "text":
				if jsonValueHasBusinessData(child, "text") {
					return true
				}
			}
		}
	}
	return false
}

func chatObjectHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || responseObjectIndicatesFailure(object) {
		return false
	}
	for key, child := range object {
		normalizedKey := normalizeResponseMetadataKey(key)
		switch normalizedKey {
		case "content", "refusal", "reasoning", "reasoning_content", "text", "transcript":
			if jsonValueHasBusinessData(child, normalizedKey) {
				return true
			}
		case "tool_calls", "function_call":
			if toolCallsHaveBusinessData(child) || functionObjectHasBusinessData(child) {
				return true
			}
		case "audio":
			if audioObjectHasBusinessData(child) {
				return true
			}
		}
	}
	return false
}

func toolCallsHaveBusinessData(value any) bool {
	if object, ok := value.(map[string]any); ok {
		return functionObjectHasBusinessData(object)
	}
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if function, ok := object["function"]; ok && functionObjectHasBusinessData(function) {
			return true
		}
		if functionObjectHasBusinessData(object) {
			return true
		}
	}
	return false
}

func functionObjectHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || responseObjectIndicatesFailure(object) {
		return false
	}
	for key, childValue := range object {
		switch normalizeResponseMetadataKey(key) {
		case "name":
			if name, ok := childValue.(string); ok && strings.TrimSpace(name) != "" {
				return true
			}
		case "arguments", "partial_json":
			if text, ok := childValue.(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
		case "input":
			if child, ok := childValue.(map[string]any); ok && len(child) > 0 {
				return true
			}
		}
	}
	return false
}

func audioObjectHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, child := range object {
		switch normalizeResponseMetadataKey(key) {
		case "data", "transcript", "url":
			if jsonValueHasBusinessData(child, normalizeResponseMetadataKey(key)) {
				return true
			}
		}
	}
	return false
}

func geminiCandidatesHaveBusinessData(value any) bool {
	candidates, ok := value.([]any)
	if !ok {
		return false
	}
	for _, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok || responseObjectIndicatesFailure(candidate) {
			continue
		}
		content, ok := candidate["content"].(map[string]any)
		if !ok {
			continue
		}
		if parts, ok := content["parts"]; ok && geminiPartsHaveBusinessData(parts) {
			return true
		}
	}
	return false
}

func geminiPartsHaveBusinessData(value any) bool {
	parts, ok := value.([]any)
	if !ok {
		return false
	}
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok || responseObjectIndicatesFailure(part) {
			continue
		}
		for key, child := range part {
			switch normalizeResponseMetadataKey(key) {
			case "text", "executable_code", "code_execution_result", "inline_data", "file_data":
				if geminiPartValueHasBusinessData(child, normalizeResponseMetadataKey(key)) {
					return true
				}
			case "function_call", "function_response":
				if functionObjectHasBusinessData(child) || jsonValueHasBusinessData(child, "result") {
					return true
				}
			}
		}
	}
	return false
}

func geminiPartValueHasBusinessData(value any, key string) bool {
	if key == "text" {
		return jsonValueHasBusinessData(value, key)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for childKey, child := range object {
		normalizedKey := normalizeResponseMetadataKey(childKey)
		if normalizedKey == "code" || normalizedKey == "output" || normalizedKey == "data" || normalizedKey == "file_uri" {
			if jsonValueHasBusinessData(child, normalizedKey) {
				return true
			}
		}
	}
	return false
}

func responseDataHasBusinessData(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || responseObjectIndicatesFailure(object) {
			continue
		}
		if chatObjectHasBusinessData(object) || imageObjectHasBusinessData(object) || embeddingValueHasBusinessData(object["embedding"]) {
			return true
		}
	}
	return false
}

func imageObjectHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, child := range object {
		switch normalizeResponseMetadataKey(key) {
		case "url", "b64_json", "result", "partial_image_b64":
			if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}

func embeddingValueHasBusinessData(value any) bool {
	if object, ok := value.(map[string]any); ok {
		return embeddingValueHasBusinessData(object["values"]) || embeddingValueHasBusinessData(object["embedding"])
	}
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		switch item := item.(type) {
		case float64:
			return true
		case map[string]any, []any:
			if embeddingValueHasBusinessData(item) {
				return true
			}
		}
	}
	return false
}

func rerankResultsHaveBusinessData(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || responseObjectIndicatesFailure(object) {
			continue
		}
		if index, exists := object["index"]; exists {
			if _, ok := index.(float64); ok {
				return true
			}
		}
		if score, exists := object["relevance_score"]; exists {
			if _, ok := score.(float64); ok {
				return true
			}
		}
		if score, exists := object["score"]; exists {
			if _, ok := score.(float64); ok {
				return true
			}
		}
		if document, exists := object["document"]; exists && document != nil {
			return true
		}
	}
	return false
}

func businessArrayHasData(items []any, key string) bool {
	for _, item := range items {
		if jsonValueHasBusinessData(item, normalizeResponseMetadataKey(key)) {
			return true
		}
	}
	return false
}

func isBusinessTextKey(key string) bool {
	switch normalizeResponseMetadataKey(key) {
	case "answer", "arguments", "audio", "b64_json", "completion", "content", "data", "delta", "file_data", "file_uri", "function_call", "input", "inline_data", "output_text", "partial_image_b64", "partial_json", "reasoning", "reasoning_content", "refusal", "result", "text", "transcript", "url", "thinking", "name", "code", "output":
		return true
	default:
		return false
	}
}

func isBusinessResultKey(key string) bool {
	return normalizeResponseMetadataKey(key) == "embedding"
}

func openAIResponsesEventHasBusinessData(event map[string]any) (bool, bool) {
	eventType, ok := event["type"].(string)
	if !ok || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "response.") {
		return false, false
	}

	normalizedType := normalizeResponseMetadataKey(eventType)
	switch normalizedType {
	case "response_output_text_delta":
		if text, ok := event["delta"].(string); ok {
			return strings.TrimSpace(text) != "", true
		}
	case "response_function_call_arguments_delta":
		if arguments, ok := event["delta"].(string); ok {
			return strings.TrimSpace(arguments) != "", true
		}
	case "response_function_call_arguments_done":
		if arguments, ok := event["arguments"].(string); ok {
			return strings.TrimSpace(arguments) != "", true
		}
	case "response_refusal_done":
		if refusal, ok := event["refusal"].(string); ok {
			return strings.TrimSpace(refusal) != "", true
		}
	case "response_output_item_added", "response_output_item_done":
		if item, ok := event["item"]; ok {
			return responsesOutputValueHasBusinessData(item), true
		}
	}

	for childKey, childValue := range event {
		normalizedKey := normalizeResponseMetadataKey(childKey)
		if normalizedKey == "response" || isResponseMetadataKey(normalizedKey) {
			continue
		}
		if normalizedKey == "item" || normalizedKey == "output" || normalizedKey == "summary" || normalizedKey == "delta" || normalizedKey == "output_text" || normalizedKey == "part" || normalizedKey == "result" || normalizedKey == "partial_image_b64" {
			if responsesOutputValueHasBusinessData(childValue) {
				return true, true
			}
		}
	}

	if response, ok := event["response"].(map[string]any); ok {
		if output, ok := response["output"]; ok {
			return responsesOutputValueHasBusinessData(output), true
		}
	}
	return false, true
}

func responsesOutputValueHasBusinessData(value any) bool {
	if object, ok := value.(map[string]any); ok {
		if responseObjectIndicatesFailure(object) {
			return false
		}
		itemType, _ := object["type"].(string)
		switch normalizeResponseMetadataKey(itemType) {
		case "function_call":
			if name, ok := object["name"].(string); ok && strings.TrimSpace(name) != "" {
				return true
			}
		case "refusal":
			if refusal, ok := object["refusal"].(string); ok && strings.TrimSpace(refusal) != "" {
				return true
			}
		case "reasoning":
			if summary, ok := object["summary"]; ok && responsesOutputValueHasBusinessData(summary) {
				return true
			}
		case "message":
			if content, ok := object["content"]; ok && responsesOutputValueHasBusinessData(content) {
				return true
			}
		case "image_generation_call":
			if imageObjectHasBusinessData(object) {
				return true
			}
		}
		for key, child := range object {
			normalizedKey := normalizeResponseMetadataKey(key)
			if normalizedKey == "text" || normalizedKey == "delta" || normalizedKey == "output_text" || normalizedKey == "partial_json" || normalizedKey == "result" || normalizedKey == "partial_image_b64" || normalizedKey == "content" || normalizedKey == "summary" || normalizedKey == "item" || normalizedKey == "name" {
				if normalizedKey == "name" {
					if name, ok := child.(string); ok && strings.TrimSpace(name) != "" && normalizeResponseMetadataKey(itemType) == "function_call" {
						return true
					}
					continue
				}
				if responsesOutputValueHasBusinessData(child) || jsonValueHasBusinessData(child, normalizedKey) {
					return true
				}
			}
		}
		return false
	}
	if items, ok := value.([]any); ok {
		for _, item := range items {
			if responsesOutputValueHasBusinessData(item) {
				return true
			}
		}
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return false
}

func claudeEventHasBusinessData(event map[string]any) (bool, bool) {
	eventType, ok := event["type"].(string)
	if !ok || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "content_block") {
		return false, false
	}
	if strings.EqualFold(eventType, "content_block_delta") {
		return claudeDeltaHasBusinessData(event["delta"]), true
	}
	if strings.EqualFold(eventType, "content_block_start") {
		return claudeContentBlockHasBusinessData(event["content_block"]), true
	}
	return false, true
}

func claudeDeltaHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, child := range object {
		switch normalizeResponseMetadataKey(key) {
		case "text", "thinking", "partial_json":
			if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}

func claudeContentBlockHasBusinessData(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	blockType, _ := object["type"].(string)
	switch normalizeResponseMetadataKey(blockType) {
	case "tool_use":
		if name, ok := object["name"].(string); ok && strings.TrimSpace(name) != "" {
			return true
		}
		if input, ok := object["input"].(map[string]any); ok && len(input) > 0 {
			return true
		}
	case "redacted_thinking":
		if data, ok := object["data"].(string); ok && strings.TrimSpace(data) != "" {
			return true
		}
	}
	return chatObjectHasBusinessData(object)
}

func isResponseMetadataKey(key string) bool {
	switch normalizeResponseMetadataKey(key) {
	case "id", "object", "created", "created_at", "updated_at", "model", "system_fingerprint", "fingerprint", "service_tier", "role", "type", "event", "status", "sequence_number", "index", "output_index", "content_index", "item_id", "response_id", "message_id", "conversation_id", "request_id", "timestamp", "finish_reason", "finishreason", "stop_reason", "stopreason", "stop_sequence", "usage", "usage_metadata", "prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens", "cached_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "reasoning_tokens", "logprobs", "avg_logprobs", "metadata", "error", "prompt_feedback", "safety_ratings", "safety_rating", "block_reason", "block_reason_message", "thought_signature", "thought", "citation_metadata", "grounding_metadata", "grounding_supports", "web_search_queries", "category", "probability", "severity", "mime_type", "is_finished":
		return true
	default:
		return false
	}
}

func normalizeResponseMetadataKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(key) + 4)
	var previous rune
	for index, current := range key {
		switch {
		case current == '-' || current == ' ' || current == '.':
			if builder.Len() > 0 && previous != '_' {
				builder.WriteByte('_')
			}
			previous = '_'
		case unicode.IsUpper(current):
			if index > 0 && previous != '_' && !unicode.IsUpper(previous) {
				builder.WriteByte('_')
			}
			builder.WriteRune(unicode.ToLower(current))
			previous = current
		default:
			builder.WriteRune(unicode.ToLower(current))
			previous = current
		}
	}
	return builder.String()
}
