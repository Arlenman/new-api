package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	playgroundSessionHeader               = "X-Playground-Session-Id"
	playgroundMessageKeyHeader            = "X-Playground-Message-Key"
	playgroundAsyncHeader                 = "X-Playground-Async"
	playgroundAsyncTimeout                = 10 * time.Minute
	maxPlaygroundPersistenceRequestBytes  = 128 * 1024 * 1024
	playgroundImageTerminalUpdateAttempts = 5
	playgroundImageTerminalUpdateInterval = 100 * time.Millisecond
)

type playgroundImageTerminalState string

const (
	playgroundImageTerminalStateComplete playgroundImageTerminalState = "complete"
	playgroundImageTerminalStateFailed   playgroundImageTerminalState = "failed"
)

type playgroundImageMetadata struct {
	ImageCount         int
	HasURL             bool
	HasB64JSON         bool
	DownloadStatusCode int
	ErrorSummary       string
}

type playgroundImageProcessingError struct {
	Stage              string
	Message            string
	StatusCode         int
	DownloadStatusCode int
	Cause              error
}

type playgroundImageDownloadStatusError struct {
	StatusCode int
}

func (e *playgroundImageDownloadStatusError) Error() string {
	return fmt.Sprintf("download playground image: HTTP %d", e.StatusCode)
}

func (e *playgroundImageProcessingError) Error() string {
	if e == nil {
		return "playground image processing failed"
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Message, e.Cause.Error())
}

func newPlaygroundImageProcessingError(stage string, statusCode int, message string, cause error) *playgroundImageProcessingError {
	return &playgroundImageProcessingError{
		Stage:      stage,
		Message:    message,
		StatusCode: statusCode,
		Cause:      cause,
	}
}

func newPlaygroundImagePersistenceError(err error) *playgroundImageProcessingError {
	message := "failed to persist playground image"
	var statusErr *playgroundImageDownloadStatusError
	if errors.As(err, &statusErr) {
		message = fmt.Sprintf("failed to persist playground image: HTTP %d", statusErr.StatusCode)
	}
	processingErr := newPlaygroundImageProcessingError(
		"persist_image",
		http.StatusInternalServerError,
		message,
		err,
	)
	if statusErr != nil {
		processingErr.DownloadStatusCode = statusErr.StatusCode
	}
	return processingErr
}

type playgroundImageCaptureWriter struct {
	gin.ResponseWriter
	body        bytes.Buffer
	header      http.Header
	status      int
	wroteHeader bool
}

type playgroundSessionRequest struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	CreatedAtSnake int64  `json:"created_at"`
	UpdatedAtSnake int64  `json:"updated_at"`
}

type playgroundMessagesRequest struct {
	Messages []model.JSONValue `json:"messages"`
}

type playgroundImportRequest struct {
	Sessions []playgroundSessionImportItem `json:"sessions"`
}

type playgroundSessionImportItem struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Messages       []model.JSONValue `json:"messages"`
	CreatedAt      int64             `json:"createdAt"`
	UpdatedAt      int64             `json:"updatedAt"`
	CreatedAtSnake int64             `json:"created_at"`
	UpdatedAtSnake int64             `json:"updated_at"`
}

type playgroundSessionResponse struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Messages  []model.JSONValue `json:"messages"`
	CreatedAt int64             `json:"createdAt"`
	UpdatedAt int64             `json:"updatedAt"`
}

type playgroundImageAcceptedResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Status string `json:"status"`
}

func newPlaygroundImageCaptureWriter(writer gin.ResponseWriter) *playgroundImageCaptureWriter {
	return &playgroundImageCaptureWriter{
		ResponseWriter: writer,
		header:         make(http.Header),
		status:         http.StatusOK,
	}
}

func (w *playgroundImageCaptureWriter) Header() http.Header {
	return w.header
}

func (w *playgroundImageCaptureWriter) WriteHeader(statusCode int) {
	// Gin uses negative status codes for body-only renders (for example,
	// streaming chunks rendered with c.Render(-1, ...)). Its native response
	// writer ignores those values, so the capture writer must do the same.
	if statusCode <= 0 || w.wroteHeader {
		return
	}
	w.status = statusCode
	w.wroteHeader = true
}

func (w *playgroundImageCaptureWriter) WriteHeaderNow() {
	if !w.wroteHeader {
		w.WriteHeader(w.status)
	}
}

func (w *playgroundImageCaptureWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *playgroundImageCaptureWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *playgroundImageCaptureWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *playgroundImageCaptureWriter) Size() int {
	return w.body.Len()
}

func (w *playgroundImageCaptureWriter) Written() bool {
	return w.wroteHeader
}

func (w *playgroundImageCaptureWriter) Flush() {
}

func (w *playgroundImageCaptureWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

func shouldRunPlaygroundImageAsync(c *gin.Context) bool {
	if c == nil || c.Request == nil || !strings.EqualFold(strings.TrimSpace(c.GetHeader(playgroundAsyncHeader)), "true") {
		return false
	}
	sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
	messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))
	return sessionID != "" && messageKey != ""
}

func startAsyncPlaygroundImage(c *gin.Context) {
	userID := c.GetInt("id")
	sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
	messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))

	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	bodyBytes, err := bodyStorage.Bytes()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	request, cancel := clonePlaygroundAsyncRequest(c.Request, bodyBytes)
	ctx := c.Copy()
	ctx.Request = request
	ctx.Writer = newPlaygroundImageCaptureWriter(c.Writer)
	asyncStorage, err := common.CreateBodyStorage(bodyBytes)
	if err != nil {
		common.ApiError(c, err)
		cancel()
		return
	}
	ctx.Set(common.KeyBodyStorage, asyncStorage)

	for _, key := range []string{
		playgroundAsyncHeader,
		playgroundSessionHeader,
		playgroundMessageKeyHeader,
	} {
		ctx.Request.Header.Set(key, c.GetHeader(key))
	}

	go func() {
		defer cancel()
		defer common.CleanupBodyStorage(ctx)
		runAsyncPlaygroundImage(ctx, userID, sessionID, messageKey)
	}()

	c.JSON(http.StatusAccepted, playgroundImageAcceptedResponse{
		ID:     "pgimg_" + common.GetUUID(),
		Object: "playground.image_generation.task",
		Status: "pending",
	})
}

func clonePlaygroundAsyncRequest(request *http.Request, body []byte) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), playgroundAsyncTimeout)
	cloned := request.Clone(ctx)
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	cloned.ContentLength = int64(len(body))
	cloned.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return cloned, cancel
}

func persistPlaygroundImageTerminalState(
	c *gin.Context,
	userID int,
	sessionID string,
	messageKey string,
	stage string,
	state playgroundImageTerminalState,
	value string,
	completedAt int64,
) bool {
	var firstErr error
	for attempt := 1; attempt <= playgroundImageTerminalUpdateAttempts; attempt++ {
		var err error
		switch state {
		case playgroundImageTerminalStateComplete:
			err = model.CompletePlaygroundImageMessage(userID, sessionID, messageKey, value, completedAt)
		case playgroundImageTerminalStateFailed:
			err = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, value, completedAt)
		default:
			err = fmt.Errorf("unsupported playground image terminal state %q", state)
		}

		if err == nil {
			if attempt > 1 {
				logger.LogWarn(c, fmt.Sprintf(
					"playground image terminal update recovered user_id=%d session_id=%q message_key=%q stage=%q state=%q attempt=%d first_error=%q",
					userID,
					sessionID,
					messageKey,
					stage,
					state,
					attempt,
					firstErr,
				))
			}
			return true
		}
		if firstErr == nil {
			firstErr = err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) || attempt == playgroundImageTerminalUpdateAttempts {
			logger.LogError(c, fmt.Sprintf(
				"playground image terminal update failed user_id=%d session_id=%q message_key=%q stage=%q state=%q attempt=%d error=%q first_error=%q",
				userID,
				sessionID,
				messageKey,
				stage,
				state,
				attempt,
				err,
				firstErr,
			))
			return false
		}
		time.Sleep(playgroundImageTerminalUpdateInterval)
	}
	return false
}

func inspectPlaygroundImageData(items []dto.ImageData) playgroundImageMetadata {
	metadata := playgroundImageMetadata{}
	for _, item := range items {
		hasURL := strings.TrimSpace(item.Url) != ""
		hasB64JSON := strings.TrimSpace(item.B64Json) != ""
		metadata.HasURL = metadata.HasURL || hasURL
		metadata.HasB64JSON = metadata.HasB64JSON || hasB64JSON
		if hasURL || hasB64JSON {
			metadata.ImageCount++
		}
	}
	return metadata
}

func decodeAndValidatePlaygroundImageResponse(raw []byte) (dto.ImageResponse, playgroundImageMetadata, error) {
	var response dto.ImageResponse
	if err := common.Unmarshal(raw, &response); err != nil {
		return response, playgroundImageMetadata{}, newPlaygroundImageProcessingError(
			"decode_response",
			http.StatusBadGateway,
			"failed to decode playground image response",
			errors.New("invalid JSON response"),
		)
	}

	metadata := inspectPlaygroundImageData(response.Data)
	if metadata.ImageCount == 0 {
		return response, metadata, newPlaygroundImageProcessingError(
			"validate_image_data",
			http.StatusBadGateway,
			"playground image response did not include a valid url or b64_json",
			nil,
		)
	}

	validImages := make([]dto.ImageData, 0, metadata.ImageCount)
	for _, item := range response.Data {
		item.Url = strings.TrimSpace(item.Url)
		item.B64Json = strings.TrimSpace(item.B64Json)
		if item.Url == "" && item.B64Json == "" {
			continue
		}
		validImages = append(validImages, item)
	}
	response.Data = validImages
	return response, metadata, nil
}

func playgroundImageMetadataFromRelay(c *gin.Context) playgroundImageMetadata {
	metadata, ok := common.GetContextKeyType[relaycommon.ImageFailureMetadata](c, constant.ContextKeyImageFailureMetadata)
	if !ok {
		return playgroundImageMetadata{}
	}
	return playgroundImageMetadata{
		ImageCount:   int(metadata.ImageCount),
		HasURL:       metadata.HasURL,
		HasB64JSON:   metadata.HasB64JSON,
		ErrorSummary: metadata.ErrorSummary,
	}
}

func playgroundImageRelayFailure(c *gin.Context, statusCode int, body []byte) *playgroundImageProcessingError {
	stage := "relay"
	message := ""
	if metadata, ok := common.GetContextKeyType[relaycommon.ImageFailureMetadata](c, constant.ContextKeyImageFailureMetadata); ok {
		switch metadata.Stage {
		case "parse_stream", "validate_image_data", "decode_response", "relay":
			stage = metadata.Stage
		}
		message = metadata.ErrorSummary
	}
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	if message == "" {
		message = playgroundImageErrorFromResponse(statusCode, body)
	}
	return newPlaygroundImageProcessingError(stage, statusCode, relaycommon.SanitizeImageErrorSummary(message), nil)
}

func playgroundImageDownloadStatusCode(err error) int {
	var processingErr *playgroundImageProcessingError
	if errors.As(err, &processingErr) {
		return processingErr.DownloadStatusCode
	}
	return 0
}

func playgroundImageProcessingErrorDetails(err error) (string, int, string) {
	var processingErr *playgroundImageProcessingError
	if errors.As(err, &processingErr) {
		statusCode := processingErr.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}
		return processingErr.Stage, statusCode, relaycommon.SanitizeImageErrorSummary(processingErr.Message)
	}
	return "relay", http.StatusInternalServerError, "playground image processing failed"
}

func recordPlaygroundImageFailure(
	c *gin.Context,
	stage string,
	statusCode int,
	contentType string,
	metadata playgroundImageMetadata,
	message string,
	recordErrorLog bool,
) {
	requestID := ""
	channelID := 0
	modelName := ""
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
		channelID = c.GetInt("channel_id")
		modelName = c.GetString("original_model")
	}
	errorSummary := relaycommon.SanitizeImageErrorSummary(message)
	logMessage := fmt.Sprintf(
		"playground image failure request_id=%s channel_id=%d model=%s stage=%s status=%d content_type=%q image_count=%d has_url=%t has_b64_json=%t error_summary=%q",
		requestID,
		channelID,
		modelName,
		stage,
		statusCode,
		contentType,
		metadata.ImageCount,
		metadata.HasURL,
		metadata.HasB64JSON,
		errorSummary,
	)
	if metadata.DownloadStatusCode > 0 {
		logMessage += fmt.Sprintf(" download_status_code=%d", metadata.DownloadStatusCode)
	}
	logger.LogError(c, logMessage)

	// Playground image failures must remain visible in the usage log even when
	// the optional global relay error log switch is disabled. These failures can
	// happen after the relay has returned (stream validation, image persistence,
	// or billing settlement), so the task record and process log alone are not a
	// sufficient request audit trail.
	if !recordErrorLog || c == nil || model.LOG_DB == nil {
		return
	}
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	other := map[string]interface{}{
		"stage":         stage,
		"status_code":   statusCode,
		"content_type":  contentType,
		"image_count":   metadata.ImageCount,
		"has_url":       metadata.HasURL,
		"has_b64_json":  metadata.HasB64JSON,
		"error_summary": errorSummary,
	}
	if metadata.DownloadStatusCode > 0 {
		other["download_status_code"] = metadata.DownloadStatusCode
	}
	if taskID := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyPlaygroundImageTaskID)); taskID != "" {
		other["task_id"] = taskID
	}
	model.RecordErrorLog(
		c,
		c.GetInt("id"),
		channelID,
		modelName,
		c.GetString("token_name"),
		fmt.Sprintf("playground image processing failed at %s: %s", stage, errorSummary),
		c.GetInt("token_id"),
		int(time.Since(startTime).Seconds()),
		common.GetContextKeyBool(c, constant.ContextKeyIsStream),
		c.GetString("group"),
		other,
	)
}

func writePlaygroundImageProcessingError(c *gin.Context, err error) {
	stage, statusCode, message := playgroundImageProcessingErrorDetails(err)
	c.Writer.Header().Del("Content-Length")
	c.Writer.Header().Del("Transfer-Encoding")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "playground_image_error",
			"code":    "playground_image_" + stage,
			"stage":   stage,
		},
	})
}

func failAsyncPlaygroundImage(
	c *gin.Context,
	userID int,
	sessionID string,
	messageKey string,
	contentType string,
	metadata playgroundImageMetadata,
	err error,
) {
	stage, statusCode, message := playgroundImageProcessingErrorDetails(err)
	metadata.DownloadStatusCode = playgroundImageDownloadStatusCode(err)
	recordPlaygroundImageFailure(c, stage, statusCode, contentType, metadata, message, stage != "relay")
	persistPlaygroundImageTerminalState(
		c,
		userID,
		sessionID,
		messageKey,
		stage,
		playgroundImageTerminalStateFailed,
		message,
		time.Now().UnixMilli(),
	)
}

func runAsyncPlaygroundImage(c *gin.Context, userID int, sessionID string, messageKey string) {
	writer, _ := c.Writer.(*playgroundImageCaptureWriter)
	if writer == nil {
		writer = newPlaygroundImageCaptureWriter(c.Writer)
		c.Writer = writer
	}
	defer service.RefundPlaygroundImageBilling(c)

	PlaygroundRelay(c, types.RelayFormatOpenAIImage)

	status := writer.Status()
	body := writer.body.Bytes()
	contentType := strings.ToLower(writer.Header().Get("Content-Type"))
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		failAsyncPlaygroundImage(
			c,
			userID,
			sessionID,
			messageKey,
			contentType,
			playgroundImageMetadataFromRelay(c),
			playgroundImageRelayFailure(c, status, body),
		)
		return
	}
	if len(body) == 0 {
		failAsyncPlaygroundImage(
			c,
			userID,
			sessionID,
			messageKey,
			contentType,
			playgroundImageMetadata{},
			newPlaygroundImageProcessingError(
				"validate_image_data",
				http.StatusBadGateway,
				"playground image response did not include a valid url or b64_json",
				nil,
			),
		)
		return
	}

	var response dto.ImageResponse
	var metadata playgroundImageMetadata
	if strings.Contains(contentType, "text/event-stream") {
		streamResponse, err := playgroundImageResponseFromStream(body)
		if err != nil {
			failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, err)
			return
		}
		metadata = inspectPlaygroundImageData(streamResponse.Data)
		rawResponse, err := common.Marshal(streamResponse)
		if err != nil {
			processingErr := newPlaygroundImageProcessingError(
				"decode_response",
				http.StatusInternalServerError,
				"failed to encode playground image response",
				errors.New("invalid image response"),
			)
			failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, processingErr)
			return
		}
		body = rawResponse
		contentType = gin.MIMEJSON
	}

	rewritten, err := rewritePlaygroundImageResponse(c, body)
	if err != nil {
		if _, decodedMetadata, decodeErr := decodeAndValidatePlaygroundImageResponse(body); decodeErr == nil {
			metadata = decodedMetadata
		}
		failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, err)
		return
	}
	response, metadata, err = decodeAndValidatePlaygroundImageResponse(rewritten)
	if err != nil {
		failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, err)
		return
	}

	content := buildPlaygroundImageResponseContent(response)
	if strings.TrimSpace(content) == "" {
		processingErr := newPlaygroundImageProcessingError(
			"validate_image_data",
			http.StatusBadGateway,
			"playground image response did not include renderable image data",
			nil,
		)
		failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, processingErr)
		return
	}
	if !persistPlaygroundImageTerminalState(
		c,
		userID,
		sessionID,
		messageKey,
		"complete",
		playgroundImageTerminalStateComplete,
		content,
		time.Now().UnixMilli(),
	) {
		processingErr := newPlaygroundImageProcessingError(
			"persist_image",
			http.StatusInternalServerError,
			"failed to persist completed playground image state",
			nil,
		)
		recordPlaygroundImageFailure(c, processingErr.Stage, processingErr.StatusCode, contentType, metadata, processingErr.Message, true)
		return
	}
	if _, err := service.FinalizePlaygroundImageBilling(c); err != nil {
		processingErr := newPlaygroundImageProcessingError(
			"settle_billing",
			http.StatusInternalServerError,
			"failed to settle playground image billing",
			err,
		)
		failAsyncPlaygroundImage(c, userID, sessionID, messageKey, contentType, metadata, processingErr)
	}
}

func buildPlaygroundImageResponseContent(response dto.ImageResponse) string {
	images := make([]string, 0, len(response.Data))
	revisedPrompts := make([]string, 0, len(response.Data))
	for idx, item := range response.Data {
		if strings.TrimSpace(item.Url) != "" {
			images = append(images, fmt.Sprintf("![Generated image %d](%s)", idx+1, item.Url))
		}
		if strings.TrimSpace(item.RevisedPrompt) != "" {
			revisedPrompts = append(revisedPrompts, strings.TrimSpace(item.RevisedPrompt))
		}
	}
	switch {
	case len(images) == 0:
		return strings.Join(revisedPrompts, "\n\n")
	case len(revisedPrompts) == 0:
		return strings.Join(images, "\n\n")
	default:
		return strings.Join(images, "\n\n") + "\n\n" + strings.Join(revisedPrompts, "\n\n")
	}
}

func playgroundImageErrorFromResponse(status int, body []byte) string {
	if len(body) == 0 {
		return fmt.Sprintf("image generation failed with HTTP %d", status)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(body, &payload); err == nil {
		if strings.TrimSpace(payload.Error.Message) != "" {
			return relaycommon.SanitizeImageErrorSummary(payload.Error.Message)
		}
		if strings.TrimSpace(payload.Message) != "" {
			return relaycommon.SanitizeImageErrorSummary(payload.Message)
		}
	}
	return fmt.Sprintf("image generation failed with HTTP %d", status)
}

type playgroundImageStreamPayload struct {
	Type          string          `json:"type"`
	Data          []dto.ImageData `json:"data"`
	Url           string          `json:"url"`
	B64Json       string          `json:"b64_json"`
	RevisedPrompt string          `json:"revised_prompt"`
	Created       int64           `json:"created"`
	CreatedAt     int64           `json:"created_at"`
	Error         json.RawMessage `json:"error"`
	Message       string          `json:"message"`
}

func playgroundImageResponseFromStream(body []byte) (dto.ImageResponse, error) {
	events := playgroundImageStreamDataEvents(body)
	response := dto.ImageResponse{}
	completedImages := make([]dto.ImageData, 0)
	hasPartialImage := false
	sawCompletedEvent := false
	streamError := ""

	for _, event := range events {
		if event == "" || event == "[DONE]" {
			continue
		}

		var payload playgroundImageStreamPayload
		if err := common.Unmarshal(common.StringToByteSlice(event), &payload); err != nil {
			continue
		}

		if payload.Created > 0 && response.Created == 0 {
			response.Created = payload.Created
		}
		if payload.CreatedAt > 0 && response.Created == 0 {
			response.Created = payload.CreatedAt
		}

		if len(payload.Error) > 0 || strings.EqualFold(strings.TrimSpace(payload.Type), "error") || strings.EqualFold(strings.TrimSpace(payload.Type), "upstream_error") {
			streamError = playgroundImageStreamErrorMessage(payload)
			continue
		}

		completedEvent := isPlaygroundImageCompletedEvent(payload.Type)
		if completedEvent {
			sawCompletedEvent = true
		}
		completedDataCount := 0
		for _, item := range payload.Data {
			item.Url = strings.TrimSpace(item.Url)
			item.B64Json = strings.TrimSpace(item.B64Json)
			if item.Url == "" && item.B64Json == "" {
				continue
			}
			if completedEvent {
				completedImages = append(completedImages, item)
				completedDataCount++
			} else {
				hasPartialImage = true
			}
		}

		image := dto.ImageData{
			Url:           strings.TrimSpace(payload.Url),
			B64Json:       strings.TrimSpace(payload.B64Json),
			RevisedPrompt: strings.TrimSpace(payload.RevisedPrompt),
		}
		if (image.Url == "" && image.B64Json == "") || completedDataCount > 0 {
			continue
		}
		if completedEvent {
			completedImages = append(completedImages, image)
		} else {
			hasPartialImage = true
		}
	}

	switch {
	case streamError != "":
		return response, newPlaygroundImageProcessingError(
			"parse_stream",
			http.StatusBadGateway,
			"playground image stream returned an error: "+streamError,
			nil,
		)
	case len(completedImages) > 0:
		response.Data = completedImages
	case sawCompletedEvent:
		return response, newPlaygroundImageProcessingError(
			"validate_image_data",
			http.StatusBadGateway,
			"completed playground image stream event did not include a valid url or b64_json",
			nil,
		)
	case hasPartialImage:
		return response, newPlaygroundImageProcessingError(
			"parse_stream",
			http.StatusBadGateway,
			"playground image stream did not complete an image",
			nil,
		)
	default:
		return response, newPlaygroundImageProcessingError(
			"parse_stream",
			http.StatusBadGateway,
			"empty image stream response",
			nil,
		)
	}

	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	return response, nil
}

func playgroundImageStreamDataEvents(body []byte) []string {
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	frames := strings.Split(normalized, "\n\n")
	events := make([]string, 0, len(frames))

	for _, frame := range frames {
		dataLines := make([]string, 0)
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if len(dataLines) > 0 {
			events = append(events, strings.Join(dataLines, "\n"))
		}
	}

	return events
}

func isPlaygroundImageCompletedEvent(eventType string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	return eventType == "" || eventType == "image_generation.completed" || eventType == "image_edit.completed"
}

func playgroundImageStreamErrorMessage(payload playgroundImageStreamPayload) string {
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return relaycommon.SanitizeImageErrorSummary(msg)
	}
	if len(payload.Error) == 0 {
		return "image stream returned an error"
	}
	var nested struct {
		Message string `json:"message"`
	}
	if err := common.Unmarshal(payload.Error, &nested); err == nil {
		if msg := strings.TrimSpace(nested.Message); msg != "" {
			return relaycommon.SanitizeImageErrorSummary(msg)
		}
	}
	return "image stream returned an error"
}

func writeCapturedPlaygroundImageResponse(c *gin.Context, writer *playgroundImageCaptureWriter) {
	defer service.RefundPlaygroundImageBilling(c)

	status := writer.Status()
	body := writer.body.Bytes()
	contentType := writer.Header().Get("Content-Type")
	if contentType == "" {
		contentType = gin.MIMEJSON
	}

	isEventStream := strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
	if status >= http.StatusOK && status < http.StatusMultipleChoices && isEventStream {
		streamResponse, err := playgroundImageResponseFromStream(body)
		if err != nil {
			stage, errorStatus, message := playgroundImageProcessingErrorDetails(err)
			metadata := inspectPlaygroundImageData(streamResponse.Data)
			recordPlaygroundImageFailure(c, stage, errorStatus, contentType, metadata, message, true)
			var processingErr *playgroundImageProcessingError
			if !errors.As(err, &processingErr) {
				err = newPlaygroundImageProcessingError(stage, errorStatus, message, nil)
			}
			writePlaygroundImageProcessingError(c, err)
			return
		}
		body, err = common.Marshal(streamResponse)
		if err != nil {
			processingErr := newPlaygroundImageProcessingError(
				"decode_response",
				http.StatusInternalServerError,
				"failed to encode playground image response",
				errors.New("invalid image response"),
			)
			metadata := inspectPlaygroundImageData(streamResponse.Data)
			recordPlaygroundImageFailure(c, processingErr.Stage, status, contentType, metadata, processingErr.Message, true)
			writePlaygroundImageProcessingError(c, processingErr)
			return
		}
		contentType = gin.MIMEJSON
	} else if (status < http.StatusOK || status >= http.StatusMultipleChoices) && isEventStream {
		// Streaming headers may already have been staged before the relay detects
		// an upstream failure. PlaygroundRelay serializes that failure as JSON;
		// do not expose a JSON error body as text/event-stream to the embedded tool.
		contentType = gin.MIMEJSON
	}

	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		if len(body) == 0 {
			processingErr := newPlaygroundImageProcessingError(
				"validate_image_data",
				http.StatusBadGateway,
				"playground image response did not include a valid url or b64_json",
				nil,
			)
			recordPlaygroundImageFailure(c, processingErr.Stage, status, contentType, playgroundImageMetadata{}, processingErr.Message, true)
			writePlaygroundImageProcessingError(c, processingErr)
			return
		}

		_, metadata, err := decodeAndValidatePlaygroundImageResponse(body)
		if err != nil {
			stage, _, message := playgroundImageProcessingErrorDetails(err)
			recordPlaygroundImageFailure(c, stage, status, contentType, metadata, message, true)
			writePlaygroundImageProcessingError(c, err)
			return
		}

		sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
		messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))
		if sessionID != "" && messageKey != "" {
			rewritten, err := rewritePlaygroundImageResponse(c, body)
			if err != nil {
				stage, _, message := playgroundImageProcessingErrorDetails(err)
				metadata.DownloadStatusCode = playgroundImageDownloadStatusCode(err)
				recordPlaygroundImageFailure(c, stage, status, contentType, metadata, message, true)
				writePlaygroundImageProcessingError(c, err)
				return
			}
			body = rewritten
			_, metadata, err = decodeAndValidatePlaygroundImageResponse(body)
			if err != nil {
				stage, _, message := playgroundImageProcessingErrorDetails(err)
				recordPlaygroundImageFailure(c, stage, status, contentType, metadata, message, true)
				writePlaygroundImageProcessingError(c, err)
				return
			}
		}
		if _, err := service.FinalizePlaygroundImageBilling(c); err != nil {
			processingErr := newPlaygroundImageProcessingError(
				"settle_billing",
				http.StatusInternalServerError,
				"failed to settle playground image billing",
				err,
			)
			recordPlaygroundImageFailure(c, processingErr.Stage, status, contentType, metadata, processingErr.Message, true)
			writePlaygroundImageProcessingError(c, processingErr)
			return
		}
	} else {
		processingErr := playgroundImageRelayFailure(c, status, body)
		recordErrorLog := processingErr.Stage != "relay" || !common.GetContextKeyBool(c, constant.ContextKeyChannelErrorLogRecorded)
		recordPlaygroundImageFailure(
			c,
			processingErr.Stage,
			processingErr.StatusCode,
			contentType,
			playgroundImageMetadataFromRelay(c),
			processingErr.Message,
			recordErrorLog,
		)
	}

	for key, values := range writer.Header() {
		if strings.EqualFold(key, "Content-Length") ||
			strings.EqualFold(key, "Content-Type") ||
			strings.EqualFold(key, "Transfer-Encoding") {
			continue
		}
		c.Writer.Header().Del(key)
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.Header().Del("Content-Type")
	c.Writer.Header().Del("Transfer-Encoding")
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	c.Data(status, contentType, body)
}

func rewritePlaygroundImageResponse(c *gin.Context, raw []byte) ([]byte, error) {
	response, _, err := decodeAndValidatePlaygroundImageResponse(raw)
	if err != nil {
		return nil, err
	}

	userID := c.GetInt("id")
	sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
	messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))

	for idx := range response.Data {
		item := &response.Data[idx]
		switch {
		case item.B64Json != "":
			file, persistErr := model.PersistPlaygroundImageBase64(userID, sessionID, messageKey, item.B64Json, "")
			if persistErr != nil {
				return nil, newPlaygroundImagePersistenceError(persistErr)
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		case strings.HasPrefix(item.Url, "data:image/"):
			file, persistErr := model.PersistPlaygroundImageBase64(userID, sessionID, messageKey, item.Url, "")
			if persistErr != nil {
				return nil, newPlaygroundImagePersistenceError(persistErr)
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		case strings.HasPrefix(item.Url, "http://") || strings.HasPrefix(item.Url, "https://"):
			file, persistErr := persistPlaygroundImageURL(userID, sessionID, messageKey, item.Url)
			if persistErr != nil {
				return nil, newPlaygroundImagePersistenceError(persistErr)
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		}
	}

	metadata := inspectPlaygroundImageData(response.Data)
	if metadata.ImageCount == 0 {
		return nil, newPlaygroundImageProcessingError(
			"validate_image_data",
			http.StatusBadGateway,
			"persisted playground image response did not include a valid url or b64_json",
			nil,
		)
	}

	rewritten, err := common.Marshal(response)
	if err != nil {
		return nil, newPlaygroundImageProcessingError(
			"decode_response",
			http.StatusInternalServerError,
			"failed to encode playground image response",
			errors.New("invalid image response"),
		)
	}
	return rewritten, nil
}

func persistPlaygroundImageURL(userID int, sessionID string, messageKey string, imageURL string) (*model.PlaygroundFile, error) {
	resp, err := service.DoDownloadRequest(imageURL, "playground_image_persist")
	if err != nil {
		return nil, errors.New("download playground image request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &playgroundImageDownloadStatusError{StatusCode: resp.StatusCode}
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mediaType
	}
	if contentType != "application/octet-stream" && !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("invalid playground image content type: %s", contentType)
	}

	maxBytes := int64(constant.MaxFileDownloadMB * 1024 * 1024)
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("playground image exceeds maximum size")
	}
	buffer := bytes.NewBuffer(nil)
	written, err := io.Copy(buffer, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read playground image: %w", err)
	}
	if written > maxBytes {
		return nil, fmt.Errorf("playground image exceeds maximum size")
	}
	if contentType == "application/octet-stream" {
		contentType = http.DetectContentType(buffer.Bytes())
	}
	return model.PersistPlaygroundImageBytes(userID, sessionID, messageKey, buffer.Bytes(), contentType)
}

func GetPlaygroundSessions(c *gin.Context) {
	sessions, err := model.ListPlaygroundSessions(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sessions": toPlaygroundSessionResponses(sessions)})
}

func CreatePlaygroundSessionAPI(c *gin.Context) {
	var req playgroundSessionRequest
	_ = c.ShouldBindJSON(&req)
	createdAt := req.CreatedAt
	if createdAt == 0 {
		createdAt = req.CreatedAtSnake
	}
	updatedAt := req.UpdatedAt
	if updatedAt == 0 {
		updatedAt = req.UpdatedAtSnake
	}
	session, err := model.UpsertPlaygroundSession(c.GetInt("id"), req.ID, req.Title, createdAt, updatedAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, toPlaygroundSessionResponse(*session))
}

func UpdatePlaygroundSessionAPI(c *gin.Context) {
	var req playgroundSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	session, err := model.RenamePlaygroundSession(c.GetInt("id"), c.Param("id"), req.Title)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, toPlaygroundSessionResponse(*session))
}

func DeletePlaygroundSessionAPI(c *gin.Context) {
	if err := model.SoftDeletePlaygroundSession(c.GetInt("id"), c.Param("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func bindPlaygroundPersistenceJSON(c *gin.Context, target any) bool {
	if c.Request.ContentLength > maxPlaygroundPersistenceRequestBytes {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return false
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPlaygroundPersistenceRequestBytes)
	if err := c.ShouldBindJSON(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return false
		}
		common.ApiErrorMsg(c, "invalid request")
		return false
	}
	return true
}

func SavePlaygroundSessionMessagesAPI(c *gin.Context) {
	var req playgroundMessagesRequest
	if !bindPlaygroundPersistenceJSON(c, &req) {
		return
	}
	session, err := model.SavePlaygroundSessionMessages(c.GetInt("id"), c.Param("id"), req.Messages)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, toPlaygroundSessionResponse(*session))
}

func ImportPlaygroundSessionsAPI(c *gin.Context) {
	var req playgroundImportRequest
	if !bindPlaygroundPersistenceJSON(c, &req) {
		return
	}
	sessions := make([]model.PlaygroundSession, 0, len(req.Sessions))
	for _, item := range req.Sessions {
		createdAt := item.CreatedAt
		if createdAt == 0 {
			createdAt = item.CreatedAtSnake
		}
		updatedAt := item.UpdatedAt
		if updatedAt == 0 {
			updatedAt = item.UpdatedAtSnake
		}
		sessions = append(sessions, model.PlaygroundSession{
			ID:          item.ID,
			Title:       item.Title,
			Messages:    item.Messages,
			CreatedTime: createdAt,
			UpdatedTime: updatedAt,
		})
	}
	imported, err := model.ImportPlaygroundSessions(c.GetInt("id"), sessions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sessions": toPlaygroundSessionResponses(imported)})
}

func GetPlaygroundFileContent(c *gin.Context) {
	file, path, err := model.ResolvePlaygroundFileForUser(c.GetInt("id"), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
		return
	}
	c.Header("Content-Type", file.ContentType)
	c.Header("Cache-Control", "private, max-age=31536000")
	c.File(path)
}

func toPlaygroundSessionResponses(sessions []model.PlaygroundSession) []playgroundSessionResponse {
	responses := make([]playgroundSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		responses = append(responses, toPlaygroundSessionResponse(session))
	}
	return responses
}

func toPlaygroundSessionResponse(session model.PlaygroundSession) playgroundSessionResponse {
	if session.Messages == nil {
		session.Messages = []model.JSONValue{}
	}
	return playgroundSessionResponse{
		ID:        session.ID,
		Title:     session.Title,
		Messages:  session.Messages,
		CreatedAt: session.CreatedTime,
		UpdatedAt: session.UpdatedTime,
	}
}

func decodePlaygroundSessionResponse(raw json.RawMessage) (playgroundSessionResponse, error) {
	var response playgroundSessionResponse
	err := common.Unmarshal(raw, &response)
	return response, err
}
