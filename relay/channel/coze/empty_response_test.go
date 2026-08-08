package coze

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

func newCozeStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "coze-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

func TestCozeChatStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "empty delta", body: "event: conversation.message.delta\ndata: {\"content\":\"\"}\n\n"},
		{name: "terminal event only", body: "event: done\ndata: [DONE]\n\n"},
		{
			name: "completed event only",
			body: "event: conversation.chat.completed\n" +
				`data: {"id":"chat-1","status":"completed","usage":{"input_count":1,"output_count":0,"token_count":1}}` + "\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newCozeStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := cozeChatStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestCozeChatStreamHandlerForwardsBusinessEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		"event: conversation.message.delta",
		`data: {"content":"hello"}`,
		"",
		"event: conversation.chat.completed",
		`data: {"id":"chat-1","status":"completed","usage":{"input_count":1,"output_count":2,"token_count":3}}`,
		"",
	}, "\n")
	c, recorder, resp, info := newCozeStreamTestContext(body)

	usage, apiErr := cozeChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TotalTokens)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestCozeChatStreamHandlerReturnsScannerErrorAfterBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := "event: conversation.message.delta\ndata: {\"content\":\"hello\"}\n\n"
	c, recorder, resp, info := newCozeStreamTestContext(body)
	resp.Body = io.NopCloser(io.MultiReader(resp.Body, iotest.ErrReader(errors.New("truncated upstream"))))

	usage, apiErr := cozeChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestCozeChatStreamHandlerPreservesClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newCozeStreamTestContext("")
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	resp.Body = io.NopCloser(iotest.ErrReader(context.Canceled))

	usage, apiErr := cozeChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Empty(t, recorder.Body.String())
}

func TestCozeChatStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "space", data: `{"content":" "}`, want: `"content":" "`},
		{name: "newline", data: `{"content":"\n"}`, want: `"content":"\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := "event: conversation.message.delta\ndata: " + tt.data + "\n\n"
			c, recorder, resp, info := newCozeStreamTestContext(body)

			usage, apiErr := cozeChatStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestCozeChatStreamHandlerKeepsWhitespaceBeforeReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		"event: conversation.message.delta",
		`data: {"content":" "}`,
		"",
		"event: conversation.message.delta",
		`data: {"content":"","reasoning_content":"think"}`,
		"",
		"event: conversation.chat.completed",
		`data: {"id":"chat-1","status":"completed","usage":{"input_count":1,"output_count":2,"token_count":3}}`,
		"",
	}, "\n")
	c, recorder, resp, info := newCozeStreamTestContext(body)

	usage, apiErr := cozeChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), `"content":" "`)
	assert.Contains(t, recorder.Body.String(), `"reasoning_content":"think"`)
}
