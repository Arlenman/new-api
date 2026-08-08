package cloudflare

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

func newCloudflareStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "cloudflare-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "@cf-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

func TestCloudflareStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "usage only", body: "data: {\"choices\":[],\"usage\":{\"total_tokens\":1}}\n\n"},
		{name: "stop frame only", body: "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newCloudflareStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			apiErr, usage := cfStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestCloudflareStreamHandlerForwardsBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newCloudflareStreamTestContext(body)

	apiErr, usage := cfStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestCloudflareStreamHandlerReturnsScannerErrorAfterBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newCloudflareStreamTestContext("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n")
	resp.Body = io.NopCloser(io.MultiReader(resp.Body, iotest.ErrReader(errors.New("truncated upstream"))))

	apiErr, usage := cfStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestCloudflareStreamHandlerPreservesClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newCloudflareStreamTestContext("")
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	resp.Body = io.NopCloser(iotest.ErrReader(context.Canceled))

	apiErr, usage := cfStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Empty(t, recorder.Body.String())
}

func TestCloudflareStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "space", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" \"}}]}\n\n", want: `"content":" "`},
		{name: "newline", body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\\n\"}}]}\n\n", want: `"content":"\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newCloudflareStreamTestContext(tt.body)

			apiErr, usage := cfStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestCloudflareStreamHandlerKeepsWhitespaceBeforeBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":" "}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	c, recorder, resp, info := newCloudflareStreamTestContext(body)

	apiErr, usage := cfStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), `"content":" "`)
	assert.Contains(t, recorder.Body.String(), "hello")
}
