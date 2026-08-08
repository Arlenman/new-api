package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	playgroundClientTaskIDHeader     = "X-Playground-Client-Task-Id"
	playgroundImageTaskObject        = "playground.image_generation.task"
	playgroundImageTaskURLPrefix     = "/pg/image-tasks/"
	playgroundImageFileURLPrefix     = "/pg/image-files/"
	playgroundPersistedFileURLPrefix = "/api/playground/files/"
	playgroundImageFileURLSuffix     = "/content"
	playgroundImageTaskLeaseSkew     = 30 * time.Second
)

type playgroundImageTaskErrorResponse struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Stage   string `json:"stage"`
}

type playgroundImageTaskResponse struct {
	ID           string                            `json:"id"`
	Object       string                            `json:"object"`
	Status       string                            `json:"status"`
	ClientTaskID string                            `json:"client_task_id"`
	StatusURL    string                            `json:"status_url"`
	Result       *dto.ImageResponse                `json:"result,omitempty"`
	Error        *playgroundImageTaskErrorResponse `json:"error,omitempty"`
}

func shouldRunManagedPlaygroundImageAsync(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return false
	}
	if common.GetContextKeyString(c, constant.ContextKeyUserTool) != model.UserToolImagePlayground {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetHeader(playgroundAsyncHeader)), "true") {
		return false
	}
	switch c.Request.URL.Path {
	case "/pg/images/generations", "/pg/v1/images/generations", "/pg/images/edits", "/pg/v1/images/edits":
		return true
	default:
		return false
	}
}

func startManagedPlaygroundImageTask(c *gin.Context) {
	clientTaskID := strings.TrimSpace(c.GetHeader(playgroundClientTaskIDHeader))
	if clientTaskID == "" {
		writeManagedPlaygroundImageTaskError(c, http.StatusBadRequest, "playground image client task id is required", "playground_image_task_invalid", "task")
		return
	}

	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		writeManagedPlaygroundImageTaskError(c, http.StatusBadRequest, "failed to read playground image request", "playground_image_task_invalid", "task")
		return
	}
	body, err := bodyStorage.Bytes()
	if err != nil {
		writeManagedPlaygroundImageTaskError(c, http.StatusBadRequest, "failed to read playground image request", "playground_image_task_invalid", "task")
		return
	}

	task, _, err := createManagedPlaygroundImageTask(c, body)
	if err != nil {
		if errors.Is(err, model.ErrUserToolImageTaskIdempotencyConflict) {
			writeManagedPlaygroundImageTaskError(c, http.StatusConflict, "client task id was already used for a different image request", "playground_image_task_conflict", "task")
			return
		}
		writeManagedPlaygroundImageTaskError(c, http.StatusBadRequest, "failed to create playground image task", "playground_image_task_invalid", "task")
		return
	}

	if task.Status == model.UserToolImageTaskStatusPending || task.Status == model.UserToolImageTaskStatusRunning {
		request, cancel := clonePlaygroundAsyncRequest(c.Request, body)
		ctx := c.Copy()
		ctx.Request = request
		ctx.Writer = newPlaygroundImageCaptureWriter(c.Writer)
		asyncStorage, storageErr := common.CreateBodyStorage(body)
		if storageErr != nil {
			cancel()
			writeManagedPlaygroundImageTaskError(c, http.StatusInternalServerError, "failed to prepare playground image task", "playground_image_task_storage", "task")
			return
		}
		ctx.Set(common.KeyBodyStorage, asyncStorage)

		go func() {
			defer cancel()
			defer common.CleanupBodyStorage(ctx)
			runManagedPlaygroundImageTask(ctx, task.TaskID, "pgworker_"+common.GetUUID())
		}()
	}

	c.JSON(http.StatusAccepted, playgroundImageTaskResponse{
		ID:           task.TaskID,
		Object:       playgroundImageTaskObject,
		Status:       managedPlaygroundImageTaskPublicStatus(task.Status),
		ClientTaskID: task.ClientTaskID,
		StatusURL:    playgroundImageTaskURLPrefix + task.TaskID,
	})
}

func createManagedPlaygroundImageTask(c *gin.Context, body []byte) (*model.UserToolImageTask, bool, error) {
	if c == nil {
		return nil, false, errors.New("missing playground image context")
	}
	requestSnapshot := model.JSONValue(append([]byte(nil), body...))
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "multipart/form-data") {
		digest := sha256.Sum256(body)
		// Multipart edit bodies can contain user images. Persist only a replay-safe,
		// credential-free fingerprint. The in-process worker uses BodyStorage bytes;
		// after a process crash this envelope alone cannot replay the binary upload.
		envelope, err := common.Marshal(struct {
			RequestPath string `json:"request_path"`
			ContentType string `json:"content_type"`
			BodySHA256  string `json:"body_sha256"`
			BodySize    int    `json:"body_size"`
		}{
			RequestPath: c.Request.URL.Path,
			ContentType: mediaType,
			BodySHA256:  fmt.Sprintf("%x", digest[:]),
			BodySize:    len(body),
		})
		if err != nil {
			return nil, false, err
		}
		requestSnapshot = model.JSONValue(envelope)
	}
	return model.CreateOrGetUserToolImageTask(model.UserToolImageTaskInput{
		UserID:          c.GetInt("id"),
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    strings.TrimSpace(c.GetHeader(playgroundClientTaskIDHeader)),
		TokenID:         c.GetInt("token_id"),
		RequestSnapshot: requestSnapshot,
	})
}

func runManagedPlaygroundImageTask(c *gin.Context, taskID, workerID string) {
	now := time.Now()
	_, claimed, err := model.ClaimUserToolImageTask(
		taskID,
		workerID,
		now.Add(playgroundAsyncTimeout+playgroundImageTaskLeaseSkew).UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("claim playground image task failed task_id=%s error=%q", taskID, err))
		return
	}
	if !claimed {
		return
	}
	if strings.TrimSpace(c.GetString(common.RequestIdKey)) == "" {
		c.Set(common.RequestIdKey, taskID)
	}
	common.SetContextKey(c, constant.ContextKeyPlaygroundImageTaskID, taskID)

	writer, _ := c.Writer.(*playgroundImageCaptureWriter)
	if writer == nil {
		writer = newPlaygroundImageCaptureWriter(c.Writer)
		c.Writer = writer
	}
	defer service.RefundPlaygroundImageBilling(c)

	PlaygroundRelay(c, types.RelayFormatOpenAIImage)

	status := writer.Status()
	body := writer.body.Bytes()
	contentType := writer.Header().Get("Content-Type")
	metadata := playgroundImageMetadata{}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		metadata = playgroundImageMetadataFromRelay(c)
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, playgroundImageRelayFailure(c, status, body))
		return
	}
	if len(body) == 0 {
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, newPlaygroundImageProcessingError(
			"validate_image_data",
			http.StatusBadGateway,
			"playground image response did not include a valid url or b64_json",
			nil,
		))
		return
	}

	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		streamResponse, streamErr := playgroundImageResponseFromStream(body)
		if streamErr != nil {
			metadata = inspectPlaygroundImageData(streamResponse.Data)
			failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, streamErr)
			return
		}
		metadata = inspectPlaygroundImageData(streamResponse.Data)
		body, err = common.Marshal(streamResponse)
		if err != nil {
			failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, newPlaygroundImageProcessingError(
				"decode_response",
				http.StatusInternalServerError,
				"failed to encode playground image response",
				errors.New("invalid image response"),
			))
			return
		}
		contentType = gin.MIMEJSON
	}

	rewritten, err := rewritePlaygroundImageResponse(c, body)
	if err != nil {
		if _, decodedMetadata, decodeErr := decodeAndValidatePlaygroundImageResponse(body); decodeErr == nil {
			metadata = decodedMetadata
		}
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, err)
		return
	}
	response, metadata, err := decodeAndValidatePlaygroundImageResponse(rewritten)
	if err != nil {
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, err)
		return
	}
	if err = normalizeManagedPlaygroundImageResult(&response); err != nil {
		processingErr := newPlaygroundImageProcessingError(
			"persist_image",
			http.StatusInternalServerError,
			"playground image result was not persisted to local storage",
			err,
		)
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, processingErr)
		return
	}

	resultSnapshot, err := common.Marshal(response)
	if err != nil {
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, newPlaygroundImageProcessingError(
			"decode_response",
			http.StatusInternalServerError,
			"failed to encode playground image task result",
			errors.New("invalid image response"),
		))
		return
	}
	resultItemID := firstManagedPlaygroundImageFileID(response)
	if err = prepareManagedPlaygroundImageTask(taskID, workerID, resultItemID, model.JSONValue(resultSnapshot)); err != nil {
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, newPlaygroundImageProcessingError(
			"persist_image",
			http.StatusInternalServerError,
			"failed to persist playground image task result",
			err,
		))
		return
	}
	if _, err = service.FinalizePlaygroundImageBilling(c); err != nil {
		if errors.Is(err, service.ErrManagedPlaygroundImageSettlementAudit) {
			var auditErr error
			for attempt := 0; attempt < playgroundImageTerminalUpdateAttempts; attempt++ {
				_, auditErr = service.RetryPlaygroundImageSettlementAudit(c)
				if auditErr == nil {
					break
				}
				time.Sleep(playgroundImageTerminalUpdateInterval)
			}
			if auditErr == nil {
				if completeErr := completeManagedPlaygroundImageTask(taskID, workerID); completeErr != nil {
					logger.LogError(c, fmt.Sprintf(
						"complete playground image task after settlement audit retry failed request_id=%s task_id=%s channel_id=%d model=%s stage=settle_billing error=%q",
						c.GetString(common.RequestIdKey),
						taskID,
						c.GetInt("channel_id"),
						c.GetString("original_model"),
						common.LocalLogPreview(completeErr.Error()),
					))
				}
				return
			}
			logger.LogError(c, fmt.Sprintf(
				"playground image settlement audit retry failed request_id=%s task_id=%s channel_id=%d model=%s stage=settle_billing error=%q",
				c.GetString(common.RequestIdKey),
				taskID,
				c.GetInt("channel_id"),
				c.GetString("original_model"),
				common.LocalLogPreview(auditErr.Error()),
			))
			return
		}
		failManagedPlaygroundImageTask(c, taskID, workerID, contentType, metadata, newPlaygroundImageProcessingError(
			"settle_billing",
			http.StatusInternalServerError,
			"failed to settle playground image billing",
			err,
		))
		return
	}

	if err = completeManagedPlaygroundImageTask(taskID, workerID); err != nil {
		logger.LogError(c, fmt.Sprintf("complete playground image task failed task_id=%s error=%q", taskID, err))
	}
}

func prepareManagedPlaygroundImageTask(taskID, workerID, resultItemID string, result model.JSONValue) error {
	var err error
	for attempt := 0; attempt < playgroundImageTerminalUpdateAttempts; attempt++ {
		err = model.PrepareUserToolImageTaskCompletion(taskID, workerID, resultItemID, result, time.Now().UnixMilli())
		if err == nil || errors.Is(err, model.ErrUserToolImageTaskLeaseLost) {
			return err
		}
		time.Sleep(playgroundImageTerminalUpdateInterval)
	}
	return err
}

func completeManagedPlaygroundImageTask(taskID, workerID string) error {
	var err error
	for attempt := 0; attempt < playgroundImageTerminalUpdateAttempts; attempt++ {
		err = model.CompletePreparedUserToolImageTask(taskID, workerID, time.Now().UnixMilli())
		if err == nil || errors.Is(err, model.ErrUserToolImageTaskLeaseLost) {
			return err
		}
		time.Sleep(playgroundImageTerminalUpdateInterval)
	}
	return err
}

func failManagedPlaygroundImageTask(
	c *gin.Context,
	taskID string,
	workerID string,
	contentType string,
	metadata playgroundImageMetadata,
	processingError error,
) {
	if _, refundErr := service.RefundPlaygroundImageBillingChecked(c); refundErr != nil {
		logger.LogError(c, fmt.Sprintf(
			"playground image refund failed request_id=%s task_id=%s channel_id=%d model=%s stage=refund error=%q",
			c.GetString(common.RequestIdKey),
			taskID,
			c.GetInt("channel_id"),
			c.GetString("original_model"),
			common.LocalLogPreview(refundErr.Error()),
		))
		recordPlaygroundImageFailure(
			c,
			"refund",
			http.StatusInternalServerError,
			contentType,
			metadata,
			"failed to refund playground image billing",
			true,
		)
	}
	stage, statusCode, message := playgroundImageProcessingErrorDetails(processingError)
	metadata.DownloadStatusCode = playgroundImageDownloadStatusCode(processingError)

	var err error
	for attempt := 0; attempt < playgroundImageTerminalUpdateAttempts; attempt++ {
		_, err = model.FailUserToolImageTaskTerminal(
			taskID,
			workerID,
			"playground_image_"+stage,
			message,
			time.Now().UnixMilli(),
		)
		if err == nil {
			recordErrorLog := stage != "relay"
			if c != nil {
				common.SetContextKey(c, constant.ContextKeyPlaygroundImageTaskID, taskID)
				recordErrorLog = recordErrorLog || !common.GetContextKeyBool(c, constant.ContextKeyChannelErrorLogRecorded)
			}
			recordPlaygroundImageFailure(c, stage, statusCode, contentType, metadata, message, recordErrorLog)
			return
		}
		if errors.Is(err, model.ErrUserToolImageTaskLeaseLost) {
			return
		}
		time.Sleep(playgroundImageTerminalUpdateInterval)
	}
	logger.LogError(c, fmt.Sprintf("fail playground image task terminal update failed task_id=%s stage=%s error=%q", taskID, stage, err))
}

func normalizeManagedPlaygroundImageResult(response *dto.ImageResponse) error {
	if response == nil || len(response.Data) == 0 {
		return errors.New("empty image result")
	}
	for i := range response.Data {
		item := &response.Data[i]
		if strings.TrimSpace(item.B64Json) != "" {
			return errors.New("image result still contains base64 data")
		}
		fileID, ok := managedPlaygroundImageFileID(item.Url)
		if !ok {
			return errors.New("image result contains a non-local URL")
		}
		item.Url = playgroundImageFileURLPrefix + fileID + playgroundImageFileURLSuffix
	}
	return nil
}

func managedPlaygroundImageFileID(rawURL string) (string, bool) {
	url := strings.TrimSpace(rawURL)
	for _, prefix := range []string{playgroundImageFileURLPrefix, playgroundPersistedFileURLPrefix} {
		if !strings.HasPrefix(url, prefix) || !strings.HasSuffix(url, playgroundImageFileURLSuffix) {
			continue
		}
		fileID := strings.TrimSuffix(strings.TrimPrefix(url, prefix), playgroundImageFileURLSuffix)
		if fileID != "" && !strings.Contains(fileID, "/") {
			return fileID, true
		}
	}
	return "", false
}

func firstManagedPlaygroundImageFileID(response dto.ImageResponse) string {
	if len(response.Data) == 0 {
		return ""
	}
	fileID, _ := managedPlaygroundImageFileID(response.Data[0].Url)
	return fileID
}

func GetManagedPlaygroundImageFileContent(c *gin.Context) {
	GetPlaygroundFileContent(c)
}

func GetPlaygroundImageTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	userID := c.GetInt("id")
	task, err := model.GetUserToolImageTask(userID, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeManagedPlaygroundImageTaskError(c, http.StatusNotFound, "playground image task not found", "playground_image_task_not_found", "task")
			return
		}
		writeManagedPlaygroundImageTaskError(c, http.StatusInternalServerError, "failed to load playground image task", "playground_image_task_query", "task")
		return
	}

	if task.Status == model.UserToolImageTaskStatusFinalizing {
		recoveredTask, _, recoverErr := model.CompleteSettledPreparedUserToolImageTask(userID, taskID, time.Now().UnixMilli())
		if recoverErr != nil {
			logger.LogError(c, fmt.Sprintf("recover settled playground image task failed task_id=%s error=%q", taskID, common.LocalLogPreview(recoverErr.Error())))
			writeManagedPlaygroundImageTaskError(c, http.StatusInternalServerError, "failed to recover playground image task", "playground_image_task_query", "task")
			return
		}
		if recoveredTask != nil {
			task = recoveredTask
		}
	}

	response := playgroundImageTaskResponse{
		ID:           task.TaskID,
		Object:       playgroundImageTaskObject,
		Status:       managedPlaygroundImageTaskPublicStatus(task.Status),
		ClientTaskID: task.ClientTaskID,
		StatusURL:    playgroundImageTaskURLPrefix + task.TaskID,
	}
	switch task.Status {
	case model.UserToolImageTaskStatusCompleted:
		var result dto.ImageResponse
		if err = common.Unmarshal(task.ResultSnapshot, &result); err != nil || normalizeManagedPlaygroundImageResult(&result) != nil {
			logger.LogError(c, fmt.Sprintf("invalid persisted playground image task result task_id=%s", task.TaskID))
			writeManagedPlaygroundImageTaskError(c, http.StatusInternalServerError, "playground image task result is unavailable", "playground_image_task_result_invalid", "decode_response")
			return
		}
		response.Result = &result
	case model.UserToolImageTaskStatusFailed:
		response.Error = &playgroundImageTaskErrorResponse{
			Message: task.ErrorMessage,
			Code:    task.ErrorCode,
			Stage:   playgroundImageTaskStageFromCode(task.ErrorCode),
		}
	}
	c.JSON(http.StatusOK, response)
}

func managedPlaygroundImageTaskPublicStatus(status string) string {
	if status == model.UserToolImageTaskStatusFinalizing {
		return model.UserToolImageTaskStatusRunning
	}
	return status
}

func playgroundImageTaskStageFromCode(code string) string {
	const prefix = "playground_image_"
	if strings.HasPrefix(code, prefix) {
		stage := strings.TrimPrefix(code, prefix)
		if stage != "" {
			return stage
		}
	}
	return "relay"
}

func writeManagedPlaygroundImageTaskError(c *gin.Context, status int, message, code, stage string) {
	c.JSON(status, gin.H{
		"error": playgroundImageTaskErrorResponse{
			Message: message,
			Code:    code,
			Stage:   stage,
		},
	})
}
