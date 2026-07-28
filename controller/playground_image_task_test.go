package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupManagedPlaygroundImageTaskTest(t *testing.T) {
	t.Helper()
	setupPlaygroundControllerTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserToolImageTask{}))
}

func newManagedPlaygroundImageTaskContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)
	ctx.Request.Header.Set(playgroundAsyncHeader, "true")
	ctx.Request.Header.Set(playgroundClientTaskIDHeader, "local-task-1")
	ctx.Set("id", 1)
	ctx.Set("token_id", 99)
	common.SetContextKey(ctx, constant.ContextKeyUserTool, model.UserToolImagePlayground)
	return ctx, recorder
}

func TestShouldRunManagedPlaygroundImageAsyncForRuntimeGenerationAndEdit(t *testing.T) {
	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	assert.True(t, shouldRunManagedPlaygroundImageAsync(ctx))

	for _, path := range []string{"/pg/images/edits", "/pg/v1/images/edits"} {
		ctx.Request.URL.Path = path
		ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		assert.True(t, shouldRunManagedPlaygroundImageAsync(ctx), path)
	}

	ctx.Request.URL.Path = "/pg/v1/images/generations"
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)
	common.SetContextKey(ctx, constant.ContextKeyUserTool, model.UserToolInfiniteCanvas)
	assert.False(t, shouldRunManagedPlaygroundImageAsync(ctx))

	common.SetContextKey(ctx, constant.ContextKeyUserTool, model.UserToolImagePlayground)
	ctx.Request.Header.Del(playgroundAsyncHeader)
	assert.False(t, shouldRunManagedPlaygroundImageAsync(ctx))
}

func TestCreateManagedPlaygroundImageTaskIsIdempotentAndDetectsChangedBody(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	body := []byte(`{"model":"gpt-image-2","prompt":"cat"}`)
	created, existing, err := createManagedPlaygroundImageTask(ctx, body)
	require.NoError(t, err)
	assert.False(t, existing)
	assert.Equal(t, "local-task-1", created.ClientTaskID)
	assert.Equal(t, 99, created.TokenID)

	retried, existing, err := createManagedPlaygroundImageTask(ctx, body)
	require.NoError(t, err)
	assert.True(t, existing)
	assert.Equal(t, created.TaskID, retried.TaskID)

	_, _, err = createManagedPlaygroundImageTask(ctx, []byte(`{"model":"gpt-image-2","prompt":"dog"}`))
	assert.ErrorIs(t, err, model.ErrUserToolImageTaskIdempotencyConflict)
}

func TestCreateManagedPlaygroundImageEditTaskStoresCredentialFreeRequestEnvelope(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	body := []byte("--test\r\nContent-Disposition: form-data; name=\"image[]\"; filename=\"secret.png\"\r\nContent-Type: image/png\r\n\r\nraw-image-bytes-must-not-be-persisted\r\n--test--\r\n")
	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/edits", "")
	ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")

	task, existing, err := createManagedPlaygroundImageTask(ctx, body)
	require.NoError(t, err)
	assert.False(t, existing)
	assert.NotContains(t, string(task.RequestSnapshot), "raw-image-bytes-must-not-be-persisted")
	assert.NotContains(t, string(task.RequestSnapshot), "secret.png")

	var snapshot struct {
		RequestPath string `json:"request_path"`
		ContentType string `json:"content_type"`
		BodySHA256  string `json:"body_sha256"`
		BodySize    int    `json:"body_size"`
	}
	require.NoError(t, common.Unmarshal(task.RequestSnapshot, &snapshot))
	digest := sha256.Sum256(body)
	assert.Equal(t, "/pg/v1/images/edits", snapshot.RequestPath)
	assert.Equal(t, "multipart/form-data", snapshot.ContentType)
	assert.Equal(t, hex.EncodeToString(digest[:]), snapshot.BodySHA256)
	assert.Equal(t, len(body), snapshot.BodySize)

	retried, existing, err := createManagedPlaygroundImageTask(ctx, body)
	require.NoError(t, err)
	assert.True(t, existing)
	assert.Equal(t, task.TaskID, retried.TaskID)

	_, _, err = createManagedPlaygroundImageTask(ctx, append(append([]byte(nil), body...), 'x'))
	assert.ErrorIs(t, err, model.ErrUserToolImageTaskIdempotencyConflict)
}

func TestGetPlaygroundImageTaskRecoversSettledFinalizingResult(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "recover-finalizing-task",
		TokenID:         99,
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)
	workerID := "worker-before-restart"
	now := time.Now().UnixMilli()
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, workerID, now+60_000, now)
	require.NoError(t, err)
	require.True(t, claimed)
	result := model.JSONValue(`{"data":[{"url":"/pg/image-files/pgf_recover/content"}]}`)
	require.NoError(t, model.PrepareUserToolImageTaskCompletion(task.TaskID, workerID, "pgf_recover", result, now+1))
	require.NoError(t, model.MarkUserToolImageTaskPreConsumed(task.TaskID, "request-recover-finalizing", 140000, now+2))
	require.NoError(t, model.MarkUserToolImageTaskSettled(task.TaskID, 140000, now+3))

	ctx, recorder := newManagedPlaygroundImageTaskContext(http.MethodGet, "/pg/image-tasks/"+task.TaskID, "")
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetPlaygroundImageTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response playgroundImageTaskResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, model.UserToolImageTaskStatusCompleted, response.Status)
	require.NotNil(t, response.Result)
	require.Len(t, response.Result.Data, 1)
	assert.Equal(t, "/pg/image-files/pgf_recover/content", response.Result.Data[0].Url)

	stored, err := model.GetUserToolImageTask(1, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.UserToolImageTaskStatusCompleted, stored.Status)
	assert.Equal(t, model.UserToolImageTaskBillingStatusSettled, stored.BillingStatus)
}

func TestGetPlaygroundImageTaskReturnsCompletedLocalResultForOwner(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "completed-local-task",
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.CompleteUserToolImageTaskWithResult(
		task.TaskID,
		"worker-a",
		"pgf_local",
		model.JSONValue(`{"created":123,"data":[{"url":"/api/playground/files/pgf_local/content"}]}`),
		2000,
	))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/pg/image-tasks/"+task.TaskID, nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", 1)
	GetPlaygroundImageTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		ClientTaskID string `json:"client_task_id"`
		StatusURL    string `json:"status_url"`
		Result       struct {
			Data []struct {
				URL     string `json:"url"`
				B64JSON string `json:"b64_json"`
			} `json:"data"`
		} `json:"result"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, task.TaskID, response.ID)
	assert.Equal(t, model.UserToolImageTaskStatusCompleted, response.Status)
	assert.Equal(t, "completed-local-task", response.ClientTaskID)
	assert.Equal(t, "/pg/image-tasks/"+task.TaskID, response.StatusURL)
	require.Len(t, response.Result.Data, 1)
	assert.Equal(t, "/pg/image-files/pgf_local/content", response.Result.Data[0].URL)
	assert.Empty(t, response.Result.Data[0].B64JSON)
}

func TestGetManagedPlaygroundImageFileContentIsOwnerScoped(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	file, err := model.PersistPlaygroundImageBytes(1, "managed-session", "managed-message", []byte("managed-image"), "image/png")
	require.NoError(t, err)

	tests := []struct {
		name       string
		userID     int
		wantStatus int
		wantBody   string
	}{
		{name: "owner", userID: 1, wantStatus: http.StatusOK, wantBody: "managed-image"},
		{name: "other user", userID: 2, wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/pg/image-files/"+file.ID+"/content", nil)
			ctx.Params = gin.Params{{Key: "id", Value: file.ID}}
			ctx.Set("id", test.userID)

			GetManagedPlaygroundImageFileContent(ctx)

			assert.Equal(t, test.wantStatus, recorder.Code)
			if test.wantBody != "" {
				assert.Equal(t, test.wantBody, recorder.Body.String())
				assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
				assert.Equal(t, "private, max-age=31536000", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestGetPlaygroundImageTaskIsOwnerScopedAndDoesNotLeakInvalidResult(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "sensitive-result-task",
		RequestSnapshot: model.JSONValue(`{"prompt":"cat"}`),
	})
	require.NoError(t, err)
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.CompleteUserToolImageTaskWithResult(
		task.TaskID,
		"worker-a",
		"",
		model.JSONValue(`{"data":[{"url":"https://sensitive.example/image.png","b64_json":"secret-base64"}]}`),
		2000,
	))

	for _, test := range []struct {
		name       string
		userID     int
		wantStatus int
	}{
		{name: "other owner", userID: 2, wantStatus: http.StatusNotFound},
		{name: "invalid completed snapshot", userID: 1, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/pg/image-tasks/"+task.TaskID, nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
			ctx.Set("id", test.userID)
			GetPlaygroundImageTask(ctx)

			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), "sensitive.example")
			assert.NotContains(t, recorder.Body.String(), "secret-base64")
		})
	}
}

func TestGetPlaygroundImageTaskReturnsStructuredTerminalFailure(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "failed-task",
		RequestSnapshot: model.JSONValue(`{"prompt":"cat"}`),
	})
	require.NoError(t, err)
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = model.FailUserToolImageTaskTerminal(task.TaskID, "worker-a", "playground_image_persist_image", "failed to persist playground image: HTTP 404", 2000)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/pg/image-tasks/"+task.TaskID, nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", 1)
	GetPlaygroundImageTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Stage   string `json:"stage"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, model.UserToolImageTaskStatusFailed, response.Status)
	assert.Equal(t, "failed to persist playground image: HTTP 404", response.Error.Message)
	assert.Equal(t, "playground_image_persist_image", response.Error.Code)
	assert.Equal(t, "persist_image", response.Error.Stage)
}

func TestGetPlaygroundImageTaskKeepsPreparedCompletionInternal(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "prepared-task",
		RequestSnapshot: model.JSONValue(`{"prompt":"cat"}`),
	})
	require.NoError(t, err)
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, "worker-a", 3000, 1000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.PrepareUserToolImageTaskCompletion(
		task.TaskID,
		"worker-a",
		"pgf_local",
		model.JSONValue(`{"data":[{"url":"/api/playground/files/pgf_local/content"}]}`),
		2000,
	))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/pg/image-tasks/"+task.TaskID, nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", 1)
	GetPlaygroundImageTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Status string             `json:"status"`
		Result *dto.ImageResponse `json:"result"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, model.UserToolImageTaskStatusRunning, response.Status)
	assert.Nil(t, response.Result)
}

func TestFailManagedPlaygroundImageTaskRecordsSingleTerminalFailureWhenGlobalErrorLoggingDisabled(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "managed-failure-log-task",
		TokenID:         99,
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)

	workerID := "worker-failure-log"
	now := time.Now().UnixMilli()
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, workerID, now+60_000, now)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	ctx.Set(common.RequestIdKey, "request-managed-parse-stream")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	ctx.Set("username", "alice")
	ctx.Set("token_name", "playground-token")
	ctx.Set("group", "default")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())

	processingErr := newPlaygroundImageProcessingError(
		"parse_stream",
		http.StatusBadGateway,
		"playground image stream did not include a completed image",
		nil,
	)
	metadata := playgroundImageMetadata{ImageCount: 1, HasB64JSON: true}

	failManagedPlaygroundImageTask(ctx, task.TaskID, workerID, "text/event-stream", metadata, processingErr)
	failManagedPlaygroundImageTask(ctx, task.TaskID, workerID, "text/event-stream", metadata, processingErr)

	stored, err := model.GetUserToolImageTask(1, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.UserToolImageTaskStatusFailed, stored.Status)
	assert.Equal(t, "playground_image_parse_stream", stored.ErrorCode)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where(
		"request_id = ? AND type = ?",
		"request-managed-parse-stream",
		model.LogTypeError,
	).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 168, logs[0].ChannelId)
	assert.Equal(t, "gpt-image-2", logs[0].ModelName)
	assert.Contains(t, logs[0].Content, "parse_stream")

	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, task.TaskID, other["task_id"])
	assert.Equal(t, "parse_stream", other["stage"])
	assert.EqualValues(t, http.StatusBadGateway, other["status_code"])
	assert.Equal(t, "text/event-stream", other["content_type"])
	assert.EqualValues(t, 1, other["image_count"])
	assert.Equal(t, false, other["has_url"])
	assert.Equal(t, true, other["has_b64_json"])
}

func TestManagedPlaygroundImageRelayFailurePreservesAdaptorStage(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "managed-validate-stage-task",
		TokenID:         99,
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)
	workerID := "worker-validate-stage"
	now := time.Now().UnixMilli()
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, workerID, now+60_000, now)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	ctx.Set(common.RequestIdKey, "request-managed-validate-stage")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	ctx.Set("username", "alice")
	ctx.Set("token_name", "playground-token")
	ctx.Set("group", "default")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyImageFailureMetadata, relaycommon.ImageFailureMetadata{
		Stage:       "validate_image_data",
		StatusCode:  http.StatusOK,
		ContentType: gin.MIMEJSON,
		ImageCount:  0,
	})

	processingErr := playgroundImageRelayFailure(ctx, http.StatusInternalServerError, []byte(`{"error":{"message":"upstream image response did not include a valid url or b64_json"}}`))
	assert.Equal(t, "validate_image_data", processingErr.Stage)
	assert.Equal(t, http.StatusInternalServerError, processingErr.StatusCode)

	failManagedPlaygroundImageTask(ctx, task.TaskID, workerID, gin.MIMEJSON, playgroundImageMetadata{}, processingErr)

	stored, err := model.GetUserToolImageTask(1, task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.UserToolImageTaskStatusFailed, stored.Status)
	assert.Equal(t, "playground_image_validate_image_data", stored.ErrorCode)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where(
		"request_id = ? AND type = ?",
		"request-managed-validate-stage",
		model.LogTypeError,
	).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "validate_image_data")
}

func TestFailManagedPlaygroundImageTaskRelayFallbackRecordsErrorWhenChannelLogMissing(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "managed-relay-fallback-log-task",
		TokenID:         99,
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)
	workerID := "worker-relay-fallback-log"
	now := time.Now().UnixMilli()
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, workerID, now+60_000, now)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	ctx.Set(common.RequestIdKey, "request-managed-relay-fallback")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	ctx.Set("username", "alice")
	ctx.Set("token_name", "playground-token")
	ctx.Set("group", "default")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())

	processingErr := newPlaygroundImageProcessingError(
		"relay",
		http.StatusBadGateway,
		"playground image relay failed",
		nil,
	)
	failManagedPlaygroundImageTask(ctx, task.TaskID, workerID, "text/plain", playgroundImageMetadata{}, processingErr)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where(
		"request_id = ? AND type = ?",
		"request-managed-relay-fallback",
		model.LogTypeError,
	).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "relay")
}

func TestFailManagedPlaygroundImageTaskDoesNotDuplicateRecordedChannelError(t *testing.T) {
	setupManagedPlaygroundImageTaskTest(t)

	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })

	task, _, err := model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          1,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "managed-relay-dedup-log-task",
		TokenID:         99,
		RequestSnapshot: model.JSONValue(`{"model":"gpt-image-2","prompt":"cat"}`),
	})
	require.NoError(t, err)
	workerID := "worker-relay-dedup-log"
	now := time.Now().UnixMilli()
	_, claimed, err := model.ClaimUserToolImageTask(task.TaskID, workerID, now+60_000, now)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, _ := newManagedPlaygroundImageTaskContext(http.MethodPost, "/pg/v1/images/generations", `{"model":"gpt-image-2","prompt":"cat"}`)
	ctx.Set(common.RequestIdKey, "request-managed-relay-dedup")
	ctx.Set("channel_id", 168)
	ctx.Set("original_model", "gpt-image-2")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyChannelErrorLogRecorded, true)

	processingErr := newPlaygroundImageProcessingError(
		"relay",
		http.StatusBadGateway,
		"playground image relay failed",
		nil,
	)
	failManagedPlaygroundImageTask(ctx, task.TaskID, workerID, "text/plain", playgroundImageMetadata{}, processingErr)

	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where(
		"request_id = ? AND type = ?",
		"request-managed-relay-dedup",
		model.LogTypeError,
	).Count(&count).Error)
	assert.Zero(t, count)
}
