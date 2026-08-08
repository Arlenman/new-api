package ollama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOllamaStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "ollama-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

func TestOllamaStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done marker only", body: "[DONE]\n"},
		{name: "done frame only", body: `{"model":"llama-test","done":true,"prompt_eval_count":1,"eval_count":1}`},
		{name: "empty non-done frame", body: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":""}}`},
		{name: "empty thinking frame", body: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"","thinking":null}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newOllamaStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := ollamaStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestOllamaStreamHandlerStartsOnlyAfterBusinessData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`{"model":"llama-test","created_at":"2026-08-07T00:00:00Z","done":false,"message":{"role":"assistant","content":"hello"}}`,
		`{"model":"llama-test","created_at":"2026-08-07T00:00:01Z","done":true,"done_reason":"stop","prompt_eval_count":1,"eval_count":2}`,
	}, "\n")
	c, recorder, resp, info := newOllamaStreamTestContext(body)

	usage, apiErr := ollamaStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	output := recorder.Body.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "data: [DONE]")
	assert.Contains(t, output, `"finish_reason":"stop"`)
}

func TestOllamaStreamHandlerReturnsScannerErrorAfterBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"hello"}}` + "\n"
	c, recorder, resp, info := newOllamaStreamTestContext(body)
	resp.Body = io.NopCloser(io.MultiReader(resp.Body, iotest.ErrReader(errors.New("truncated upstream"))))

	usage, apiErr := ollamaStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestOllamaStreamHandlerPreservesClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newOllamaStreamTestContext("")
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	resp.Body = io.NopCloser(iotest.ErrReader(context.Canceled))

	usage, apiErr := ollamaStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Empty(t, recorder.Body.String())
}

func TestOllamaStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "space content", body: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":" "}}`, want: `"content":" "`},
		{name: "newline reasoning", body: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"","thinking":"\n"}}`, want: `"reasoning_content":"\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newOllamaStreamTestContext(tt.body)

			usage, apiErr := ollamaStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestOllamaStreamHandlerKeepsWhitespaceBeforeBusinessEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		business string
		want     string
	}{
		{name: "text", business: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"hello"}}`, want: "hello"},
		{name: "reasoning", business: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"","thinking":"think"}}`, want: `"reasoning_content":"think"`},
		{name: "tool call", business: `{"model":"llama-test","done":false,"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"x"}}}]}}`, want: `"name":"lookup"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Join([]string{
				`{"model":"llama-test","done":false,"message":{"role":"assistant","content":" "}}`,
				tt.business,
				`{"model":"llama-test","done":true,"done_reason":"stop","prompt_eval_count":1,"eval_count":2}`,
			}, "\n")
			c, recorder, resp, info := newOllamaStreamTestContext(body)

			usage, apiErr := ollamaStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 1, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), `"content":" "`)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}
