package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGeminiEmptyResponseTestContext(body string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "gemini-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
		IsStream:    stream,
		RelayFormat: types.RelayFormatOpenAI,
		DisablePing: true,
	}
	return c, recorder, resp, info
}

func TestGeminiChatHandlerRejectsEmptyCandidatesBeforeWritingClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, body := range []string{`{}`, `{"candidates":[]}`} {
		c, recorder, resp, info := newGeminiEmptyResponseTestContext(body, false)

		usage, apiErr := GeminiChatHandler(c, info, resp)

		require.NotNil(t, usage)
		require.NotNil(t, apiErr)
		assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
		assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
		assert.Equal(t, "gemini_empty_candidates", common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
		assert.Empty(t, recorder.Body.String())
	}
}

func TestGeminiChatHandlerPreservesPromptFeedbackBlockReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, resp, info := newGeminiEmptyResponseTestContext(
		`{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`,
		false,
	)

	usage, apiErr := GeminiChatHandler(c, info, resp)

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodePromptBlocked, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "gemini_block_reason=SAFETY", common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
	assert.Empty(t, recorder.Body.String())
}

func TestGeminiChatStreamHandlerRejectsDoneOnlyBeforeSyntheticFinalOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	c, recorder, resp, info := newGeminiEmptyResponseTestContext("data: [DONE]\n\n", true)
	info.ShouldIncludeUsage = true

	usage, apiErr := GeminiChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Empty(t, recorder.Body.String(), "empty upstream streams must not emit final usage or terminal events")
}
