package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserToolRuntimeRequestAllowed(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		method string
		path   string
		want   bool
	}{
		{name: "image generation", tool: model.UserToolImagePlayground, method: http.MethodPost, path: "/pg/v1/images/generations", want: true},
		{name: "image edit", tool: model.UserToolImagePlayground, method: http.MethodPost, path: "/pg/images/edits", want: true},
		{name: "image agent response", tool: model.UserToolImagePlayground, method: http.MethodPost, path: "/pg/v1/responses", want: true},
		{name: "image tool cannot use chat relay", tool: model.UserToolImagePlayground, method: http.MethodPost, path: "/v1/chat/completions", want: false},
		{name: "image tool cannot use playground chat", tool: model.UserToolImagePlayground, method: http.MethodPost, path: "/pg/chat/completions", want: false},
		{name: "canvas image generation", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/pg/v1/images/generations", want: true},
		{name: "canvas responses", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/v1/responses", want: true},
		{name: "canvas speech", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/v1/audio/speech", want: true},
		{name: "canvas create video", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/v1/videos", want: true},
		{name: "canvas list models", tool: model.UserToolInfiniteCanvas, method: http.MethodGet, path: "/v1/models", want: true},
		{name: "canvas poll video", tool: model.UserToolInfiniteCanvas, method: http.MethodGet, path: "/v1/videos/task_123", want: true},
		{name: "canvas fetch video content", tool: model.UserToolInfiniteCanvas, method: http.MethodGet, path: "/v1/videos/task_123/content", want: true},
		{name: "canvas cannot use embeddings", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/v1/embeddings", want: false},
		{name: "canvas cannot use nested video path", tool: model.UserToolInfiniteCanvas, method: http.MethodGet, path: "/v1/videos/task_123/content/extra", want: false},
		{name: "canvas cannot mutate model route", tool: model.UserToolInfiniteCanvas, method: http.MethodPost, path: "/v1/models", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, userToolRuntimeRequestAllowed(test.tool, test.method, test.path))
		})
	}
}

func TestUserUploadRateLimitUsesAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = redisEnabled })

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		var userID int
		_, err := fmt.Sscanf(c.GetHeader("X-Test-User"), "%d", &userID)
		require.NoError(t, err)
		c.Set("id", userID)
	})
	engine.POST("/upload", UserUploadRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for requestNumber := 0; requestNumber < common.UploadRateLimitNum; requestNumber++ {
		request := httptest.NewRequest(http.MethodPost, "/upload", nil)
		request.Header.Set("X-Test-User", "910001")
		request.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", requestNumber+1)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}

	limitedRequest := httptest.NewRequest(http.MethodPost, "/upload", nil)
	limitedRequest.Header.Set("X-Test-User", "910001")
	limitedRequest.RemoteAddr = "198.51.100.1:1234"
	limitedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(limitedRecorder, limitedRequest)
	assert.Equal(t, http.StatusTooManyRequests, limitedRecorder.Code)

	otherUserRequest := httptest.NewRequest(http.MethodPost, "/upload", nil)
	otherUserRequest.Header.Set("X-Test-User", "910002")
	otherUserRequest.RemoteAddr = limitedRequest.RemoteAddr
	otherUserRecorder := httptest.NewRecorder()
	engine.ServeHTTP(otherUserRecorder, otherUserRequest)
	assert.Equal(t, http.StatusNoContent, otherUserRecorder.Code)
}
