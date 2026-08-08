package tencent

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

func newTencentStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "tencent-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "hunyuan-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

func TestTencentStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "usage only", body: "data: {\"Usage\":{\"TotalTokens\":1}}\n\n"},
		{name: "stop frame only", body: "data: {\"Choices\":[{\"FinishReason\":\"stop\",\"Delta\":{\"Role\":\"assistant\",\"Content\":\"\"}}]}\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newTencentStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := tencentStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestTencentStreamHandlerForwardsBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := "data: {\"Choices\":[{\"Delta\":{\"Role\":\"assistant\",\"Content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newTencentStreamTestContext(body)

	usage, apiErr := tencentStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestTencentStreamHandlerReturnsScannerErrorAfterBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := "data: {\"Choices\":[{\"Delta\":{\"Content\":\"hello\"}}]}\n"
	c, recorder, resp, info := newTencentStreamTestContext(body)
	resp.Body = io.NopCloser(io.MultiReader(resp.Body, iotest.ErrReader(errors.New("truncated upstream"))))

	usage, apiErr := tencentStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestTencentStreamHandlerPreservesClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newTencentStreamTestContext("")
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	resp.Body = io.NopCloser(iotest.ErrReader(context.Canceled))

	usage, apiErr := tencentStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Empty(t, recorder.Body.String())
}

func TestTencentStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "space", body: "data: {\"Choices\":[{\"Delta\":{\"Role\":\"assistant\",\"Content\":\" \"}}]}\n\n", want: `"content":" "`},
		{name: "newline", body: "data: {\"Choices\":[{\"Delta\":{\"Role\":\"assistant\",\"Content\":\"\\n\"}}]}\n\n", want: `"content":"\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newTencentStreamTestContext(tt.body)

			usage, apiErr := tencentStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestTencentStreamHandlerKeepsWhitespaceBeforeBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"Choices":[{"Delta":{"Role":"assistant","Content":" "}}]}`,
		`data: {"Choices":[{"Delta":{"Role":"assistant","Content":"hello"}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	c, recorder, resp, info := newTencentStreamTestContext(body)

	usage, apiErr := tencentStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), `"content":" "`)
	assert.Contains(t, recorder.Body.String(), "hello")
}
