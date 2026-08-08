package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type emptyStreamHandler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)

func newOpenAIEmptyResponseTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
		IsStream:           true,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	return c, recorder, resp, info
}

func TestOpenAITextStreamHandlersRejectDoneOnlyBeforeSyntheticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	tests := []struct {
		name    string
		handler emptyStreamHandler
	}{
		{name: "chat completions", handler: OaiStreamHandler},
		{name: "responses", handler: OaiResponsesStreamHandler},
		{name: "responses converted to chat", handler: OaiResponsesToChatStreamHandler},
		{name: "chat converted to responses", handler: OaiChatToResponsesStreamHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newOpenAIEmptyResponseTestContext("data: [DONE]\n\n")

			usage, apiErr := tt.handler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Empty(t, recorder.Body.String(), "empty upstream streams must not emit usage, terminal events, or [DONE]")
		})
	}
}

func TestOpenaiHandlerRejectsEmptyResponseBodyBeforeWritingClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, body := range []string{"", " \n\t\r"} {
		c, recorder, resp, info := newOpenAIEmptyResponseTestContext(body)
		info.IsStream = false

		usage, apiErr := OpenaiHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
		assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
		assert.Empty(t, recorder.Body.String())
	}
}

func TestOaiResponsesToChatBufferedStreamHandlerRejectsEmptyUpstreamBeforeSyntheticResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "comments and done", body: ": keepalive\n\ndata: [DONE]\n\n"},
		{
			name: "responses lifecycle metadata only",
			body: "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":1710000000,\"model\":\"gpt-test\",\"status\":\"in_progress\",\"output\":[],\"parallel_tool_calls\":true,\"store\":true,\"temperature\":1,\"top_p\":1,\"max_output_tokens\":1024,\"tool_choice\":\"auto\",\"truncation\":\"disabled\"}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-test\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0,\"total_tokens\":1}}}\n\n" +
				"data: [DONE]\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newOpenAIEmptyResponseTestContext(tt.body)
			info.IsStream = false

			usage, apiErr := OaiResponsesToChatBufferedStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Empty(t, recorder.Body.String(), "empty buffered streams must not synthesize a completed response")
		})
	}
}
