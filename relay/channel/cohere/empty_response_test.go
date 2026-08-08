package cohere

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

type cohereStreamResponseRecorder struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func (r *cohereStreamResponseRecorder) CloseNotify() <-chan bool {
	return r.closeNotify
}

func newCohereStreamTestContext(body string) (*gin.Context, *cohereStreamResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := &cohereStreamResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "cohere-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "command-test"},
		IsStream:    true,
	}
	return c, recorder, resp, info
}

type cohereTruncatingReadCloser struct {
	reader *strings.Reader
	done   bool
}

func (r *cohereTruncatingReadCloser) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.ErrUnexpectedEOF
	}
	r.done = true
	return r.reader.Read(p)
}

func (r *cohereTruncatingReadCloser) Close() error {
	return nil
}

type cohereBlockingReadCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (r *cohereBlockingReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *cohereBlockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestCohereStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "blank lines", body: "\n\r\n"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "[DONE]\n\n"},
		{name: "finish event only", body: `{"is_finished":true,"finish_reason":"COMPLETE"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newCohereStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := cohereStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestCohereStreamHandlerRejectsTruncatedUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newCohereStreamTestContext("")
	resp.Body = &cohereTruncatingReadCloser{reader: strings.NewReader("{\"is_finished\":false,\"text\":\"hello\"}\n")}

	usage, apiErr := cohereStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestCohereStreamHandlerStopsOnClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, recorder, resp, info := newCohereStreamTestContext("")
	c.Request = c.Request.WithContext(ctx)
	body := &cohereBlockingReadCloser{started: make(chan struct{}), closed: make(chan struct{})}
	resp.Body = body

	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	resultCh := make(chan result, 1)
	go func() {
		usage, apiErr := cohereStreamHandler(c, info, resp)
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

func TestCohereStreamHandlerForwardsBusinessEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`{"is_finished":false,"event_type":"text-generation","text":"hello"}`,
		`{"is_finished":true,"finish_reason":"COMPLETE","response":{"meta":{"billed_units":{"input_tokens":1,"output_tokens":2}}}}`,
	}, "\n")
	c, recorder, resp, info := newCohereStreamTestContext(body)

	usage, apiErr := cohereStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, usage.PromptTokens)
	assert.Equal(t, 2, usage.CompletionTokens)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestCohereStreamHandlerForwardsWhitespaceWithoutCountingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "space", body: `{"is_finished":false,"event_type":"text-generation","text":" "}` + "\n", want: `"content":" "`},
		{name: "newline", body: `{"is_finished":false,"event_type":"text-generation","text":"\n"}` + "\n", want: `"content":"\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newCohereStreamTestContext(tt.body)

			usage, apiErr := cohereStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestCohereStreamHandlerKeepsWhitespaceBeforeBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.Join([]string{
		`{"is_finished":false,"event_type":"text-generation","text":" "}`,
		`{"is_finished":false,"event_type":"text-generation","text":"hello"}`,
		`{"is_finished":true,"finish_reason":"COMPLETE","response":{"meta":{"billed_units":{"input_tokens":1,"output_tokens":2}}}}`,
	}, "\n")
	c, recorder, resp, info := newCohereStreamTestContext(body)

	usage, apiErr := cohereStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), `"content":" "`)
	assert.Contains(t, recorder.Body.String(), "hello")
}
