package helper

import (
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStreamResponse(t *testing.T) {
	doneWithoutData := relaycommon.NewStreamStatus()
	doneWithoutData.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	clientGone := relaycommon.NewStreamStatus()
	clientGone.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
	handlerStop := relaycommon.NewStreamStatus()
	handlerStop.SetEndReason(relaycommon.StreamEndReasonHandlerStop, nil)
	scannerErrorAfterData := relaycommon.NewStreamStatus()
	scannerErrorAfterData.SetEndReason(relaycommon.StreamEndReasonScannerErr, nil)

	tests := []struct {
		name      string
		info      *relaycommon.RelayInfo
		wantError bool
	}{
		{name: "nil relay info", info: nil, wantError: true},
		{name: "no data events", info: &relaycommon.RelayInfo{}, wantError: true},
		{name: "done without data", info: &relaycommon.RelayInfo{StreamStatus: doneWithoutData}, wantError: true},
		{name: "received data event", info: &relaycommon.RelayInfo{ReceivedResponseCount: 1}, wantError: false},
		{name: "client disconnected before data", info: &relaycommon.RelayInfo{StreamStatus: clientGone}, wantError: false},
		{name: "handler stopped without any upstream data", info: &relaycommon.RelayInfo{StreamStatus: handlerStop}, wantError: true},
		{
			name: "stream terminated after a business chunk",
			info: &relaycommon.RelayInfo{
				ReceivedResponseCount: 1,
				StreamStatus:          scannerErrorAfterData,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStreamResponse(tt.info)
			if !tt.wantError {
				require.Nil(t, err)
				return
			}

			require.NotNil(t, err)
			assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
		})
	}
}

func TestValidateResponseBody(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want bool
	}{
		{name: "nil body", body: nil, want: true},
		{name: "empty body", body: []byte{}, want: true},
		{name: "whitespace body", body: []byte(" \n\t\r"), want: true},
		{name: "empty choices", body: []byte(`{"choices":[]}`), want: true},
		{name: "json body with content", body: []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResponseBody(tt.body)
			if !tt.want {
				require.Nil(t, err)
				return
			}

			require.NotNil(t, err)
			assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
		})
	}
}

func TestHasBusinessResponseDataDistinguishesMetadataFromContent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "role only chunk", body: `{"choices":[{"delta":{"role":"assistant"}}]}`, want: false},
		{name: "finish only chunk", body: `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, want: false},
		{name: "usage only chunk", body: `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, want: false},
		{name: "responses lifecycle event", body: `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`, want: false},
		{
			name: "responses created event with full configuration snapshot",
			body: `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":1,"status":"in_progress","model":"gpt-4o","output":[],"parallel_tool_calls":false,"store":true,"temperature":1,"top_p":1,"max_output_tokens":1024,"tool_choice":"auto","truncation":"disabled","instructions":"system prompt","usage":null,"metadata":{}}}`,
			want: false,
		},
		{
			name: "responses completed event with empty output and usage",
			body: `{"type":"response.completed","sequence_number":4,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o","output":[],"parallel_tool_calls":false,"store":true,"temperature":1,"top_p":1,"usage":{"input_tokens":10,"output_tokens":0,"total_tokens":10},"metadata":{}}}`,
			want: false,
		},
		{
			name: "gemini prompt feedback only",
			body: `{"promptFeedback":{"safetyRatings":[{"category":"HARM_CATEGORY_HATE_SPEECH","probability":"NEGLIGIBLE"}]}}`,
			want: false,
		},
		{
			name: "gemini thought signature and finish only",
			body: `{"candidates":[{"content":{"role":"model","parts":[{"thoughtSignature":"c2ln"}]},"finishReason":"STOP","safetyRatings":[{"category":"HARM_CATEGORY_HATE_SPEECH","probability":"NEGLIGIBLE"}]}]}`,
			want: false,
		},
		{name: "empty data array", body: `{"data":[]}`, want: false},
		{name: "chat content", body: `{"choices":[{"delta":{"content":"hello"}}]}`, want: true},
		{name: "reasoning content", body: `{"choices":[{"delta":{"reasoning_content":"think"}}]}`, want: true},
		{name: "tool call name", body: `{"choices":[{"delta":{"tool_calls":[{"function":{"name":"lookup"}}]}}]}`, want: true},
		{name: "claude tool arguments delta", body: `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Beijing\"}"}}`, want: true},
		{name: "responses text delta", body: `{"type":"response.output_text.delta","delta":"hello"}`, want: true},
		{name: "responses function arguments delta", body: `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"city\":\"Bei"}`, want: true},
		{name: "responses function arguments done with arguments", body: `{"type":"response.function_call_arguments.done","arguments":"{\"city\":\"Beijing\"}"}`, want: true},
		{
			name: "responses completed event with output text",
			body: `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}}`,
			want: true,
		},
		{name: "gemini text content", body: `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`, want: true},
		{name: "generic boolean result", body: `{"success":true}`, want: false},
		{name: "generic status message", body: `{"code":0,"message":"upstream returned no answer"}`, want: false},
		{name: "image data", body: `{"data":[{"url":"https://example.test/image.png"}]}`, want: true},
		{name: "plain text", body: `hello`, want: false},
		{name: "html proxy error", body: `<html>temporary upstream error</html>`, want: false},
		{name: "malformed SSE payload", body: `not-json`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HasBusinessResponseData([]byte(tt.body)))
		})
	}
}

func TestHasBusinessResponseDataRecognizesRefusalAndRedactedThinking(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "chat message refusal",
			body: `{"choices":[{"message":{"role":"assistant","refusal":"I cannot help with that"}}]}`,
			want: true,
		},
		{
			name: "chat delta refusal",
			body: `{"choices":[{"delta":{"refusal":"I cannot help with that"}}]}`,
			want: true,
		},
		{
			name: "chat empty refusal",
			body: `{"choices":[{"message":{"role":"assistant","refusal":"  "}}]}`,
			want: false,
		},
		{
			name: "responses refusal output item",
			body: `{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"I cannot help with that"}]}`,
			want: true,
		},
		{
			name: "responses refusal done event",
			body: `{"type":"response.refusal.done","refusal":"I cannot help with that"}`,
			want: true,
		},
		{
			name: "responses empty refusal done event",
			body: `{"type":"response.refusal.done","refusal":"\t"}`,
			want: false,
		},
		{
			name: "claude redacted thinking block",
			body: `{"type":"redacted_thinking","data":"opaque-data","signature":"signature-only-metadata"}`,
			want: true,
		},
		{
			name: "claude redacted thinking start event",
			body: `{"type":"content_block_start","content_block":{"type":"redacted_thinking","data":"opaque-data"}}`,
			want: true,
		},
		{
			name: "claude redacted thinking signature only",
			body: `{"type":"redacted_thinking","signature":"signature-only-metadata"}`,
			want: false,
		},
		{
			name: "claude redacted thinking start signature only",
			body: `{"type":"content_block_start","content_block":{"type":"redacted_thinking","signature":"signature-only-metadata"}}`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HasBusinessResponseData([]byte(tt.body)))
		})
	}
}
