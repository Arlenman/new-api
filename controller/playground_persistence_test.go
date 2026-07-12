package controller

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type playgroundAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupPlaygroundControllerTest(t *testing.T) string {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PlaygroundSession{}, &model.PlaygroundMessage{}, &model.PlaygroundFile{}))
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

func TestPlaygroundImageCaptureWriterDoesNotFlushOriginalResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	captureWriter := newPlaygroundImageCaptureWriter(ctx.Writer)

	captureWriter.Flush()

	require.False(t, recorder.Flushed)
}

func TestShouldRunPlaygroundImageAsyncForSessionMessageHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Request.Header.Set("X-Playground-Session-Id", "session-image")
	ctx.Request.Header.Set("X-Playground-Message-Key", "assistant-image")

	require.True(t, shouldRunPlaygroundImageAsync(ctx))
}

func TestShouldNotRunPlaygroundImageAsyncWithoutSessionMessageHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)

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
