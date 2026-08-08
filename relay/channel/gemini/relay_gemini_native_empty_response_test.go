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

func newNativeGeminiStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:streamGenerateContent", nil)
	c.Set(common.RequestIdKey, "gemini-native-empty-response-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
		IsStream:    true,
		DisablePing: true,
	}
	return c, recorder, resp, info
}

func TestGeminiNativeStreamHandlerRejectsEmptyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "done only", body: "data: [DONE]\n\n"},
		{name: "empty json event", body: "data: {}\n\ndata: [DONE]\n\n"},
		{name: "finish candidate only", body: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"STOP\"}]}\n\ndata: [DONE]\n\n"},
		{name: "usage only", body: "data: {\"usageMetadata\":{\"promptTokenCount\":1,\"totalTokenCount\":1}}\n\ndata: [DONE]\n\n"},
		{name: "prompt feedback only", body: "data: {\"promptFeedback\":{\"safetyRatings\":[{\"category\":\"HARM_CATEGORY_DANGEROUS_CONTENT\",\"probability\":\"LOW\"}]}}\n\ndata: [DONE]\n\n"},
		{name: "thought signature only", body: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thoughtSignature\":\"signature\"}]},\"finishReason\":\"STOP\"}]}\n\ndata: [DONE]\n\n"},
		{name: "safety metadata only", body: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"SAFETY\",\"safetyRatings\":[{\"category\":\"HARM_CATEGORY_HATE_SPEECH\",\"probability\":\"MEDIUM\"}]}]}\n\ndata: [DONE]\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newNativeGeminiStreamTestContext(tt.body)
			info.ReceivedResponseCount = 9

			usage, apiErr := GeminiTextGenerationStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Zero(t, info.ReceivedResponseCount)
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestGeminiNativeStreamHandlerForwardsBusinessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}]}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newNativeGeminiStreamTestContext(body)

	usage, apiErr := GeminiTextGenerationStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1, info.ReceivedResponseCount)
	assert.Contains(t, recorder.Body.String(), "hello")
}
