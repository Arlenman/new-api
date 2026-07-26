package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type playgroundAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type playgroundRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn playgroundRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func setupPlaygroundControllerTest(t *testing.T) string {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PlaygroundSession{}, &model.PlaygroundMessage{}, &model.PlaygroundFile{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123", AffCode: "alice-playground"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "bob", Password: "password123", AffCode: "bob-playground"}).Error)

	dir := t.TempDir()
	t.Setenv("PLAYGROUND_FILE_DIR", dir)
	return dir
}

func TestRewritePlaygroundImageResponseConvertsBase64ToFileURL(t *testing.T) {
	setupPlaygroundControllerTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set("X-Playground-Session-Id", "session-image")
	ctx.Request.Header.Set("X-Playground-Message-Key", "assistant-image")

	response := dto.ImageResponse{
		Data: []dto.ImageData{
			{B64Json: base64.StdEncoding.EncodeToString([]byte("image-bytes")), RevisedPrompt: "revised"},
		},
		Created: 1234,
	}
	raw, err := common.Marshal(response)
	require.NoError(t, err)

	rewritten, err := rewritePlaygroundImageResponse(ctx, raw)
	require.NoError(t, err)

	var payload dto.ImageResponse
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].B64Json)
	require.Contains(t, payload.Data[0].Url, "/api/playground/files/")
	require.Contains(t, payload.Data[0].Url, "/content")
	require.Equal(t, "revised", payload.Data[0].RevisedPrompt)

	var files []model.PlaygroundFile
	require.NoError(t, model.DB.Find(&files).Error)
	require.Len(t, files, 1)
	require.Equal(t, "session-image", files[0].SessionID)
	require.Equal(t, "assistant-image", files[0].MessageKey)

	content, err := os.ReadFile(files[0].AbsolutePath())
	require.NoError(t, err)
	require.Equal(t, []byte("image-bytes"), content)
}

func TestRewritePlaygroundImageResponseRejectsMissingImageData(t *testing.T) {
	setupPlaygroundControllerTest(t)

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "empty data",
			raw:  `{"data":[]}`,
		},
		{
			name: "usage without data",
			raw:  `{"usage":{"input_tokens":444,"output_tokens":1756,"total_tokens":2200}}`,
		},
		{
			name: "data item only has revised prompt",
			raw:  `{"data":[{"revised_prompt":"draw a cat"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", 1)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
			ctx.Request.Header.Set(playgroundSessionHeader, "session-missing-image")
			ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-missing-image")

			rewritten, err := rewritePlaygroundImageResponse(ctx, []byte(tt.raw))

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "image")
			assert.Empty(t, rewritten)
		})
	}

	var files []model.PlaygroundFile
	require.NoError(t, model.DB.Find(&files).Error)
	assert.Empty(t, files)
}

func TestPlaygroundImageStreamResponseConvertsBase64ToFileURL(t *testing.T) {
	setupPlaygroundControllerTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set("X-Playground-Session-Id", "session-stream-image")
	ctx.Request.Header.Set("X-Playground-Message-Key", "assistant-stream-image")

	rawStream := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("preview-image")) + `"}`,
		``,
		`event: image_generation.completed`,
		`data: {"type":"image_generation.completed","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("final-image")) + `","revised_prompt":"stream revised","created_at":1234}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	streamResponse, err := playgroundImageResponseFromStream([]byte(rawStream))
	require.NoError(t, err)
	rawResponse, err := common.Marshal(streamResponse)
	require.NoError(t, err)

	rewritten, err := rewritePlaygroundImageResponse(ctx, rawResponse)
	require.NoError(t, err)

	var payload dto.ImageResponse
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	require.Equal(t, int64(1234), payload.Created)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].B64Json)
	require.Contains(t, payload.Data[0].Url, "/api/playground/files/")
	require.Equal(t, "stream revised", payload.Data[0].RevisedPrompt)

	var files []model.PlaygroundFile
	require.NoError(t, model.DB.Find(&files).Error)
	require.Len(t, files, 1)
	content, err := os.ReadFile(files[0].AbsolutePath())
	require.NoError(t, err)
	require.Equal(t, []byte("final-image"), content)
}

func TestPlaygroundImageStreamResponseRejectsPartialOnlyStream(t *testing.T) {
	rawStream := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("preview-image")) + `"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	response, err := playgroundImageResponseFromStream([]byte(rawStream))

	require.ErrorContains(t, err, "did not complete")
	require.Empty(t, response.Data)
}

func TestPlaygroundImageStreamResponseRejectsIncompleteEventWithImageData(t *testing.T) {
	rawStream := strings.Join([]string{
		`event: image_generation.incomplete`,
		`data: {"type":"image_generation.incomplete","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("unfinished-image")) + `"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	response, err := playgroundImageResponseFromStream([]byte(rawStream))

	require.ErrorContains(t, err, "did not complete")
	require.Empty(t, response.Data)
}

func TestPlaygroundImageStreamResponseRejectsCompletedEventWithoutImageData(t *testing.T) {
	rawStream := strings.Join([]string{
		`event: image_generation.completed`,
		`data: {"type":"image_generation.completed","revised_prompt":"draw a cat","usage":{"input_tokens":444,"output_tokens":1756,"total_tokens":2200}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	response, err := playgroundImageResponseFromStream([]byte(rawStream))

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "image")
	assert.Empty(t, response.Data)
}

func TestPlaygroundImageStreamResponsePrefersStreamErrorOverPartialImage(t *testing.T) {
	rawStream := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("preview-image")) + `"}`,
		``,
		`event: error`,
		`data: {"type":"error","error":{"message":"upstream stream failed"}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	response, err := playgroundImageResponseFromStream([]byte(rawStream))

	require.ErrorContains(t, err, "upstream stream failed")
	require.Empty(t, response.Data)
}

func TestPersistPlaygroundImageTerminalStateRetriesUntilPendingMessageExists(t *testing.T) {
	setupPlaygroundControllerTest(t)

	const (
		sessionID  = "session-delayed-pending"
		messageKey = "assistant-delayed-pending"
	)
	_, err := model.UpsertPlaygroundSession(1, sessionID, "Image session", 1000, 1000)
	require.NoError(t, err)

	insertErr := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		payload, marshalErr := common.Marshal(map[string]any{
			"key":      messageKey,
			"from":     "assistant",
			"mode":     "image",
			"status":   "loading",
			"versions": []map[string]any{{"id": "assistant-version", "content": ""}},
			"imageGeneration": map[string]any{
				"status": "pending",
				"prompt": "draw a cat",
			},
		})
		if marshalErr != nil {
			insertErr <- marshalErr
			return
		}
		_, replaceErr := model.SavePlaygroundSessionMessages(1, sessionID, []model.JSONValue{payload})
		insertErr <- replaceErr
	}()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	persistPlaygroundImageTerminalState(
		ctx,
		1,
		sessionID,
		messageKey,
		"complete",
		playgroundImageTerminalStateComplete,
		"![Generated image 1](/api/playground/files/file-1/content)",
		2000,
	)
	require.NoError(t, <-insertErr)

	var message model.PlaygroundMessage
	require.NoError(t, model.DB.Where("user_id = ? AND session_id = ? AND message_key = ?", 1, sessionID, messageKey).First(&message).Error)
	var payload struct {
		Status          string `json:"status"`
		ImageGeneration struct {
			Status string `json:"status"`
		} `json:"imageGeneration"`
		Versions []struct {
			Content string `json:"content"`
		} `json:"versions"`
	}
	require.NoError(t, common.Unmarshal(message.Payload, &payload))
	require.Equal(t, "complete", payload.Status)
	require.Equal(t, "complete", payload.ImageGeneration.Status)
	require.Contains(t, payload.Versions[0].Content, "/api/playground/files/file-1/content")
}

func TestWriteCapturedPlaygroundImageResponseUpdatesContentLengthAfterRewrite(t *testing.T) {
	setupPlaygroundControllerTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	originalWriter := ctx.Writer
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set("X-Playground-Session-Id", "session-image")
	ctx.Request.Header.Set("X-Playground-Message-Key", "assistant-image")

	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
	ctx.Writer = captureWriter
	ctx.Writer.Header().Set("Content-Type", gin.MIMEJSON)
	ctx.Writer.Header().Set("Content-Length", "999999")

	response := dto.ImageResponse{
		Data: []dto.ImageData{
			{B64Json: base64.StdEncoding.EncodeToString([]byte("image-bytes"))},
		},
	}
	raw, err := common.Marshal(response)
	require.NoError(t, err)
	_, err = ctx.Writer.Write(raw)
	require.NoError(t, err)

	ctx.Writer = originalWriter
	writeCapturedPlaygroundImageResponse(ctx, captureWriter)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotEqual(t, "999999", recorder.Header().Get("Content-Length"))
	require.Equal(t, recorder.Body.Len(), len(recorder.Body.Bytes()))
	require.Equal(t, strconv.Itoa(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
	require.NotContains(t, recorder.Body.String(), "b64_json")
	require.Contains(t, recorder.Body.String(), "/api/playground/files/")
}

func TestPlaygroundImageResponseFromStreamReturnsSafeUpstreamErrorSummary(t *testing.T) {
	longPayload := strings.Repeat("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo", 40)
	body := []byte("data: {\"type\":\"upstream_error\",\"error\":{\"message\":\"stream ID 77 INTERNAL_ERROR while fetching https://private.example/image?token=secret api_key=sk-sensitive Bearer sk-live b64_json=" + longPayload + "\"}}\n\n")

	_, err := playgroundImageResponseFromStream(body)
	require.Error(t, err)

	stage, statusCode, message := playgroundImageProcessingErrorDetails(err)
	assert.Equal(t, "parse_stream", stage)
	assert.Equal(t, http.StatusBadGateway, statusCode)
	assert.Contains(t, message, "INTERNAL_ERROR")
	assert.NotContains(t, message, "private.example")
	assert.NotContains(t, message, "secret")
	assert.NotContains(t, message, "sk-live")
	assert.NotContains(t, message, longPayload[:32])
	assert.LessOrEqual(t, len(message), 640)
}

func TestWriteCapturedPlaygroundImageResponseConvertsStreamToJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	originalWriter := ctx.Writer
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/v1/images/generations", nil)

	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
	ctx.Writer = captureWriter
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("Transfer-Encoding", "chunked")

	ctx.Render(-1, common.CustomEvent{Data: "event: image_generation.completed\n"})
	ctx.Render(-1, common.CustomEvent{Data: `data: {"type":"image_generation.completed","b64_json":"` + base64.StdEncoding.EncodeToString([]byte("final-image")) + `","revised_prompt":"stream revised","created_at":1234}`})
	ctx.Render(-1, common.CustomEvent{Data: `data: [DONE]`})
	require.Equal(t, http.StatusOK, captureWriter.Status())

	ctx.Writer = originalWriter
	writeCapturedPlaygroundImageResponse(ctx, captureWriter)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, gin.MIMEJSON, recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	require.Equal(t, strconv.Itoa(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
	require.NotContains(t, recorder.Body.String(), "event:")

	var response dto.ImageResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, int64(1234), response.Created)
	require.Len(t, response.Data, 1)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("final-image")), response.Data[0].B64Json)
	require.Empty(t, response.Data[0].Url)
	require.Equal(t, "stream revised", response.Data[0].RevisedPrompt)
}

func TestWriteCapturedPlaygroundImageResponseUsesJSONContentTypeForStreamError(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	originalWriter := ctx.Writer

	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
	ctx.Writer = captureWriter
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("Transfer-Encoding", "chunked")
	ctx.Writer.WriteHeader(http.StatusBadGateway)
	_, err := ctx.Writer.Write([]byte(`{"error":{"message":"empty image stream response"}}`))
	require.NoError(t, err)

	ctx.Writer = originalWriter
	writeCapturedPlaygroundImageResponse(ctx, captureWriter)

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Equal(t, gin.MIMEJSON, recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	require.Contains(t, recorder.Body.String(), "empty image stream response")
}

func TestWriteCapturedPlaygroundImageResponseRejectsEmptySuccessfulStream(t *testing.T) {
	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(common.RequestIdKey, "request-empty-stream")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	originalWriter := ctx.Writer

	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
	ctx.Writer = captureWriter
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("Transfer-Encoding", "chunked")
	ctx.Writer.WriteHeader(http.StatusOK)

	ctx.Writer = originalWriter
	writeCapturedPlaygroundImageResponse(ctx, captureWriter)

	require.GreaterOrEqual(t, recorder.Code, http.StatusInternalServerError)
	require.Less(t, recorder.Code, 600)
	require.Contains(t, recorder.Header().Get("Content-Type"), gin.MIMEJSON)
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	require.Contains(t, recorder.Body.String(), "empty image stream response")

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "request-empty-stream")
	assert.Contains(t, logOutput, "channel_id=168")
	assert.Contains(t, logOutput, "model=gpt-image-2")
	assert.Contains(t, logOutput, "stage=parse_stream")
}

func TestWriteCapturedPlaygroundImageResponseRejectsMalformedJSONWithDecodeLog(t *testing.T) {
	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(common.RequestIdKey, "request-decode-response")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set(playgroundSessionHeader, "session-decode-response")
	ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-decode-response")
	originalWriter := ctx.Writer

	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
	ctx.Writer = captureWriter
	ctx.Writer.Header().Set("Content-Type", gin.MIMEJSON)
	ctx.Writer.WriteHeader(http.StatusOK)
	_, err := ctx.Writer.Write([]byte(`{"data":[`))
	require.NoError(t, err)

	ctx.Writer = originalWriter
	writeCapturedPlaygroundImageResponse(ctx, captureWriter)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), gin.MIMEJSON)
	assert.NotContains(t, recorder.Body.String(), `{"data":[`)
	assert.Contains(t, strings.ToLower(recorder.Body.String()), "decode")

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "request-decode-response")
	assert.Contains(t, logOutput, "channel_id=168")
	assert.Contains(t, logOutput, "model=gpt-image-2")
	assert.Contains(t, logOutput, "stage=decode_response")
}

func TestWriteCapturedPlaygroundImageResponseReportsURLPersistence404(t *testing.T) {
	setupPlaygroundControllerTest(t)

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	originalWorkerURL := system_setting.WorkerUrl
	t.Cleanup(func() {
		*fetchSetting = originalFetchSetting
		system_setting.WorkerUrl = originalWorkerURL
	})
	fetchSetting.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""

	httpClient := service.GetHttpClient()
	if httpClient == nil {
		service.InitHttpClient()
		httpClient = service.GetHttpClient()
	}
	require.NotNil(t, httpClient)
	originalTransport := httpClient.Transport
	t.Cleanup(func() {
		httpClient.Transport = originalTransport
	})
	const imageURL = "https://images.example.invalid/missing.png"
	httpClient.Transport = playgroundRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, imageURL, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("not found")),
			Request:    request,
		}, nil
	})

	response := dto.ImageResponse{
		Data: []dto.ImageData{{Url: imageURL}},
	}
	raw, err := common.Marshal(response)
	require.NoError(t, err)

	t.Run("rewrite returns HTTP 404 without creating a file", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
		ctx.Request.Header.Set(playgroundSessionHeader, "session-url-404")
		ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-url-404")

		rewritten, rewriteErr := rewritePlaygroundImageResponse(ctx, raw)

		require.ErrorContains(t, rewriteErr, "HTTP 404")
		assert.Empty(t, rewritten)

		var files []model.PlaygroundFile
		require.NoError(t, model.DB.Find(&files).Error)
		assert.Empty(t, files)
	})

	t.Run("captured response returns structured error and stage log", func(t *testing.T) {
		originalErrorLogEnabled := constant.ErrorLogEnabled
		constant.ErrorLogEnabled = true
		t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })

		var logBuffer bytes.Buffer
		common.LogWriterMu.Lock()
		originalErrorWriter := gin.DefaultErrorWriter
		gin.DefaultErrorWriter = &logBuffer
		common.LogWriterMu.Unlock()
		t.Cleanup(func() {
			common.LogWriterMu.Lock()
			gin.DefaultErrorWriter = originalErrorWriter
			common.LogWriterMu.Unlock()
		})

		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set(common.RequestIdKey, "request-persist-404")
		ctx.Set("channel_id", 168)
		ctx.Set("original_model", "gpt-image-2")
		ctx.Set("id", 1)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
		ctx.Request.Header.Set(playgroundSessionHeader, "session-url-404")
		ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-url-404")
		originalWriter := ctx.Writer

		captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)
		ctx.Writer = captureWriter
		ctx.Writer.Header().Set("Content-Type", gin.MIMEJSON)
		ctx.Writer.WriteHeader(http.StatusOK)
		_, writeErr := ctx.Writer.Write(raw)
		require.NoError(t, writeErr)

		ctx.Writer = originalWriter
		writeCapturedPlaygroundImageResponse(ctx, captureWriter)

		assert.Equal(t, http.StatusInternalServerError, recorder.Code)
		assert.Contains(t, recorder.Header().Get("Content-Type"), gin.MIMEJSON)
		assert.Contains(t, recorder.Body.String(), "failed to persist playground image")
		assert.Contains(t, recorder.Body.String(), "HTTP 404")
		assert.NotContains(t, recorder.Body.String(), imageURL)

		logOutput := logBuffer.String()
		assert.Contains(t, logOutput, "request-persist-404")
		assert.Contains(t, logOutput, "channel_id=168")
		assert.Contains(t, logOutput, "model=gpt-image-2")
		assert.Contains(t, logOutput, "stage=persist_image")
		assert.Contains(t, logOutput, "download_status_code=404")
		assert.NotContains(t, logOutput, imageURL)

		var errorLog model.Log
		require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", "request-persist-404", model.LogTypeError).First(&errorLog).Error)
		assert.Equal(t, 168, errorLog.ChannelId)
		assert.Equal(t, "gpt-image-2", errorLog.ModelName)
		assert.Contains(t, errorLog.Content, "persist_image")
		assert.NotContains(t, errorLog.Content, imageURL)

		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(errorLog.Other, &other))
		assert.Equal(t, "persist_image", other["stage"])
		assert.EqualValues(t, http.StatusOK, other["status_code"])
		assert.Equal(t, gin.MIMEJSON, other["content_type"])
		assert.EqualValues(t, 1, other["image_count"])
		assert.Equal(t, true, other["has_url"])
		assert.Equal(t, false, other["has_b64_json"])
		assert.EqualValues(t, http.StatusNotFound, other["download_status_code"])
		assert.NotContains(t, errorLog.Other, imageURL)
	})
}

func TestPlaygroundImageCaptureWriterDoesNotFlushOriginalResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)

	captureWriter.Flush()

	require.False(t, recorder.Flushed)
}

func TestShouldRunPlaygroundImageAsyncWithExplicitHeaderAndSessionMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set(playgroundAsyncHeader, "true")
	ctx.Request.Header.Set(playgroundSessionHeader, "session-image")
	ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-image")

	require.True(t, shouldRunPlaygroundImageAsync(ctx))
}

func TestShouldNotRunPlaygroundImageAsyncWithoutExplicitHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set(playgroundSessionHeader, "session-image")
	ctx.Request.Header.Set(playgroundMessageKeyHeader, "assistant-image")

	require.False(t, shouldRunPlaygroundImageAsync(ctx))
}

func TestShouldNotRunPlaygroundImageAsyncWithoutSessionMessageHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set(playgroundAsyncHeader, "true")

	require.False(t, shouldRunPlaygroundImageAsync(ctx))
}

func TestGetPlaygroundFileContentRequiresOwner(t *testing.T) {
	setupPlaygroundControllerTest(t)

	file, err := model.PersistPlaygroundImageBytes(1, "session-1", "assistant-1", []byte("owner-image"), "image/png")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 2)
	ctx.Params = gin.Params{{Key: "id", Value: file.ID}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/playground/files/"+file.ID+"/content", nil)

	GetPlaygroundFileContent(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestSavePlaygroundSessionMessagesAPIImportsInlineBase64(t *testing.T) {
	setupPlaygroundControllerTest(t)

	_, err := model.UpsertPlaygroundSession(1, "session-api", "API session", 1000, 1000)
	require.NoError(t, err)

	inlineImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("api-image"))
	body := map[string]any{
		"messages": []map[string]any{
			{
				"key":  "assistant-api",
				"from": "assistant",
				"mode": "image",
				"versions": []map[string]any{
					{"id": "v1", "content": "![Generated image](" + inlineImage + ")"},
				},
			},
		},
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/playground/sessions/session-api/messages", body, 1)
	ctx.Params = gin.Params{{Key: "id", Value: "session-api"}}

	SavePlaygroundSessionMessagesAPI(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload playgroundAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	require.NotContains(t, string(payload.Data), "data:image/png;base64")
	require.NotContains(t, string(payload.Data), base64.StdEncoding.EncodeToString([]byte("api-image")))
	require.Contains(t, string(payload.Data), "/api/playground/files/")

	var files []model.PlaygroundFile
	require.NoError(t, model.DB.Find(&files).Error)
	require.Len(t, files, 1)
	require.False(t, strings.Contains(string(payload.Data), files[0].StoragePath))
}

func TestSavePlaygroundSessionMessagesAPIRemovesAttachmentDataURL(t *testing.T) {
	setupPlaygroundControllerTest(t)

	_, err := model.UpsertPlaygroundSession(1, "session-pdf", "PDF session", 1000, 1000)
	require.NoError(t, err)

	body := map[string]any{
		"messages": []map[string]any{
			{
				"key":  "user-pdf",
				"from": "user",
				"mode": "chat",
				"versions": []map[string]any{
					{"id": "v1", "content": "分析扫描件"},
				},
				"attachments": []map[string]any{
					{
						"url":              "data:application/pdf;base64,c2Nhbm5lZA==",
						"mediaType":        "application/pdf",
						"filename":         "scanned.pdf",
						"size":             12,
						"extractionStatus": "empty",
					},
				},
			},
		},
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/playground/sessions/session-pdf/messages", body, 1)
	ctx.Params = gin.Params{{Key: "id", Value: "session-pdf"}}

	SavePlaygroundSessionMessagesAPI(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload playgroundAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	require.NotContains(t, string(payload.Data), "data:application/pdf;base64")
	require.Contains(t, string(payload.Data), "scanned.pdf")

	var stored model.PlaygroundMessage
	require.NoError(t, model.DB.Where("user_id = ? AND session_id = ?", 1, "session-pdf").First(&stored).Error)
	require.NotContains(t, string(stored.Payload), "data:application/pdf;base64")
	require.Contains(t, string(stored.Payload), "scanned.pdf")
}

func TestSavePlaygroundSessionMessagesAPIRejectsOversizedRequest(t *testing.T) {
	setupPlaygroundControllerTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Params = gin.Params{{Key: "id", Value: "session-large"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/playground/sessions/session-large/messages", strings.NewReader(`{"messages":[]}`))
	ctx.Request.ContentLength = maxPlaygroundPersistenceRequestBytes + 1

	SavePlaygroundSessionMessagesAPI(ctx)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
