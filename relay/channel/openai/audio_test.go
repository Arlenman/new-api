package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type partialErrorReadCloser struct {
	data []byte
}

func (r *partialErrorReadCloser) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, io.ErrUnexpectedEOF
}

func (r *partialErrorReadCloser) Close() error {
	return nil
}

type blockingReadCloser struct {
	data      []byte
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingReadCloser(data string) *blockingReadCloser {
	return &blockingReadCloser{
		data:    []byte(data),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *blockingReadCloser) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return 0, io.EOF
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func newOpenaiTTSTestContext(t *testing.T, body string, isStream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)

	contentType := "audio/pcm"
	if isStream {
		contentType = "text/event-stream"
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    isStream,
		DisablePing: true,
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		Request:     &dto.AudioRequest{ResponseFormat: "pcm"},
	}
	return c, recorder, resp, info
}

func TestOpenaiTTSHandlerRejectsStreamWithoutAudioData(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "comment only", body: ": PING\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "audio done only", body: "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}\n\ndata: [DONE]\n\n"},
		{name: "empty audio delta", body: "data: {\"type\":\"speech.audio.delta\",\"audio\":\"\"}\n\ndata: [DONE]\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newOpenaiTTSTestContext(t, tt.body, true)

			usage, apiErr := OpenaiTTSHandler(c, resp, info)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.False(t, types.IsSkipRetryError(apiErr))
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiTTSHandlerForwardsActualStreamAudio(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"speech.audio.done","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
		``,
		`data: {"type":"speech.audio.delta","audio":"YXVkaW8="}`,
		``,
		`data: {"type":"speech.audio.done","usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newOpenaiTTSTestContext(t, body, true)

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 4, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
	assert.Equal(t, 9, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), `data: {"type":"speech.audio.delta","audio":"YXVkaW8="}`)
	assert.Contains(t, recorder.Body.String(), `data: {"type":"speech.audio.done","usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}`)
	assert.NotContains(t, recorder.Body.String(), `"input_tokens":1`)
}

func TestOpenaiTTSHandlerKeepsActualAudioOnEOF(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	c, recorder, resp, info := newOpenaiTTSTestContext(t, "data: {\"type\":\"speech.audio.delta\",\"audio\":\"YXVkaW8=\"}\n\n", true)

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), `data: {"type":"speech.audio.delta","audio":"YXVkaW8="}`)
}

func TestOpenaiTTSHandlerRejectsEmptyNonStreamBody(t *testing.T) {
	c, recorder, resp, info := newOpenaiTTSTestContext(t, "", false)

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiTTSHandlerRejectsPartialNonStreamBodyReadErrorBeforeWriting(t *testing.T) {
	c, recorder, resp, info := newOpenaiTTSTestContext(t, "", false)
	resp.Body = &partialErrorReadCloser{data: []byte("partial audio")}
	resp.Header.Set("X-Upstream-Header", "must-not-be-copied")

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("X-Upstream-Header"))
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiTTSHandlerReturnsUsageWithoutEmptyResponseWhenClientCancelsBeforeAudio(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	c, recorder, resp, info := newOpenaiTTSTestContext(t, "", true)
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	body := newBlockingReadCloser("data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}\n\n")
	resp.Body = body

	type result struct {
		usage  *dto.Usage
		apiErr *types.NewAPIError
	}
	resultCh := make(chan result, 1)
	go func() {
		usage, apiErr := OpenaiTTSHandler(c, resp, info)
		resultCh <- result{usage: usage, apiErr: apiErr}
	}()

	<-body.started
	cancel()

	got := <-resultCh
	require.NotNil(t, got.usage)
	require.Nil(t, got.apiErr)
	assert.Equal(t, 1, got.usage.PromptTokens)
	assert.Equal(t, 2, got.usage.CompletionTokens)
	assert.Equal(t, 3, got.usage.TotalTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiTTSDoResponsePropagatesEmptyResponse(t *testing.T) {
	c, recorder, resp, info := newOpenaiTTSTestContext(t, "", false)

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiTTSHandlerForwardsNonStreamAudio(t *testing.T) {
	body := "\x00\x01\x02\x03"
	c, recorder, resp, info := newOpenaiTTSTestContext(t, body, false)

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, body, recorder.Body.String())
}
