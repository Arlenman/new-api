package zhipu

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type zhipuStreamResponseRecorder struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func (r *zhipuStreamResponseRecorder) CloseNotify() <-chan bool {
	return r.closeNotify
}

func newZhipuStreamTestContext(body string) (*gin.Context, *zhipuStreamResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := &zhipuStreamResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "zhipu-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "chatglm-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

type zhipuTruncatingReadCloser struct {
	reader *strings.Reader
	done   bool
}

func (r *zhipuTruncatingReadCloser) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.ErrUnexpectedEOF
	}
	r.done = true
	return r.reader.Read(p)
}

func (r *zhipuTruncatingReadCloser) Close() error {
	return nil
}

type zhipuBlockingReadCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (r *zhipuBlockingReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *zhipuBlockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestZhipuStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "meta only", body: `meta: {"request_id":"req-1","task_status":"SUCCESS","usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newZhipuStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := zhipuStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestZhipuStreamHandlerRejectsTruncatedUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newZhipuStreamTestContext("")
	resp.Body = &zhipuTruncatingReadCloser{reader: strings.NewReader("data: hello\n")}

	usage, apiErr := zhipuStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestZhipuStreamHandlerStopsOnClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, recorder, resp, info := newZhipuStreamTestContext("")
	c.Request = c.Request.WithContext(ctx)
	body := &zhipuBlockingReadCloser{started: make(chan struct{}), closed: make(chan struct{})}
	resp.Body = body

	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	resultCh := make(chan result, 1)
	go func() {
		usage, apiErr := zhipuStreamHandler(c, info, resp)
		resultCh <- result{usage: usage, err: apiErr}
	}()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not start reading upstream")
	}
	cancel()

	select {
	case result := <-resultCh:
		require.Nil(t, result.err)
		assert.Empty(t, recorder.Body.String())
	case <-time.After(time.Second):
		t.Fatal("stream handler did not stop after client cancellation")
	}
}

func TestZhipuStreamHandlerForwardsBusinessEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		"data: hello",
		`meta: {"request_id":"req-1","task_status":"SUCCESS","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		"",
	}, "\n")
	c, recorder, resp, info := newZhipuStreamTestContext(body)

	usage, apiErr := zhipuStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TotalTokens)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestZhipuStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "space", body: "data: \n\n", want: `"content":" "`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newZhipuStreamTestContext(tt.body)

			usage, apiErr := zhipuStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestZhipuStreamHandlerKeepsWhitespaceBeforeBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		"data: ",
		"data: hello",
		`meta: {"request_id":"req-1","task_status":"SUCCESS","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		"",
	}, "\n")
	c, recorder, resp, info := newZhipuStreamTestContext(body)

	usage, apiErr := zhipuStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), `"content":" "`)
	assert.Contains(t, recorder.Body.String(), "hello")
}
