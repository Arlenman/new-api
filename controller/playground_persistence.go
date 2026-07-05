package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	playgroundSessionHeader    = "X-Playground-Session-Id"
	playgroundMessageKeyHeader = "X-Playground-Message-Key"
	playgroundAsyncHeader      = "X-Playground-Async"
	playgroundAsyncTimeout     = 10 * time.Minute
)

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
	if w.wroteHeader {
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
	if c == nil || c.Request == nil {
		return false
	}
	sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
	messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))
	if sessionID == "" || messageKey == "" {
		return false
	}
	return true
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

func runAsyncPlaygroundImage(c *gin.Context, userID int, sessionID string, messageKey string) {
	writer, _ := c.Writer.(*playgroundImageCaptureWriter)
	if writer == nil {
		writer = newPlaygroundImageCaptureWriter(c.Writer)
		c.Writer = writer
	}

	PlaygroundRelay(c, types.RelayFormatOpenAIImage)

	status := writer.Status()
	body := writer.body.Bytes()
	contentType := strings.ToLower(writer.Header().Get("Content-Type"))
	if status >= 200 && status < 300 && len(body) > 0 {
		var response dto.ImageResponse
		if strings.Contains(contentType, "text/event-stream") {
			streamResponse, err := playgroundImageResponseFromStream(body)
			if err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, err.Error(), time.Now().UnixMilli())
				return
			}
			rawResponse, err := common.Marshal(streamResponse)
			if err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, "invalid image stream response", time.Now().UnixMilli())
				return
			}
			rewritten, err := rewritePlaygroundImageResponse(c, rawResponse)
			if err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, fmt.Sprintf("failed to persist playground image: %s", err.Error()), time.Now().UnixMilli())
				return
			}
			if err := common.Unmarshal(rewritten, &response); err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, "invalid image stream response", time.Now().UnixMilli())
				return
			}
		} else {
			rewritten, err := rewritePlaygroundImageResponse(c, body)
			if err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, fmt.Sprintf("failed to persist playground image: %s", err.Error()), time.Now().UnixMilli())
				return
			}
			if err := common.Unmarshal(rewritten, &response); err != nil {
				_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, "invalid image response", time.Now().UnixMilli())
				return
			}
		}
		if len(response.Data) == 0 {
			_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, "empty image response", time.Now().UnixMilli())
			return
		}
		content := buildPlaygroundImageResponseContent(response)
		if strings.TrimSpace(content) == "" {
			_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, "empty image response", time.Now().UnixMilli())
			return
		}
		_ = model.CompletePlaygroundImageMessage(userID, sessionID, messageKey, content, time.Now().UnixMilli())
		return
	}

	_ = model.FailPlaygroundImageMessage(userID, sessionID, messageKey, playgroundImageErrorFromResponse(status, body), time.Now().UnixMilli())
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
			return payload.Error.Message
		}
		if strings.TrimSpace(payload.Message) != "" {
			return payload.Message
		}
	}
	return strings.TrimSpace(common.LocalLogPreview(string(body)))
}

type playgroundImageStreamPayload struct {
	Type          string          `json:"type"`
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
	fallbackImages := make([]dto.ImageData, 0)
	streamError := ""

	for _, event := range events {
		if event == "" || event == "[DONE]" {
			continue
		}

		var imageResponse dto.ImageResponse
		if err := common.Unmarshal(common.StringToByteSlice(event), &imageResponse); err == nil && len(imageResponse.Data) > 0 {
			if imageResponse.Created > 0 {
				response.Created = imageResponse.Created
			}
			completedImages = append(completedImages, imageResponse.Data...)
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

		if len(payload.Error) > 0 {
			streamError = playgroundImageStreamErrorMessage(payload)
			continue
		}

		image := dto.ImageData{
			Url:           strings.TrimSpace(payload.Url),
			B64Json:       strings.TrimSpace(payload.B64Json),
			RevisedPrompt: strings.TrimSpace(payload.RevisedPrompt),
		}
		if image.Url == "" && image.B64Json == "" && image.RevisedPrompt == "" {
			continue
		}

		if isPlaygroundImageCompletedEvent(payload.Type) {
			completedImages = append(completedImages, image)
		} else {
			fallbackImages = append(fallbackImages, image)
		}
	}

	switch {
	case len(completedImages) > 0:
		response.Data = completedImages
	case len(fallbackImages) > 0:
		response.Data = []dto.ImageData{fallbackImages[len(fallbackImages)-1]}
	case streamError != "":
		return response, fmt.Errorf("%s", streamError)
	default:
		return response, fmt.Errorf("empty image stream response")
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
	return eventType == "" || strings.Contains(eventType, "completed") || strings.Contains(eventType, "complete")
}

func playgroundImageStreamErrorMessage(payload playgroundImageStreamPayload) string {
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg
	}
	if len(payload.Error) == 0 {
		return "image stream returned an error"
	}
	var nested struct {
		Message string `json:"message"`
	}
	if err := common.Unmarshal(payload.Error, &nested); err == nil {
		if msg := strings.TrimSpace(nested.Message); msg != "" {
			return msg
		}
	}
	if msg := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); msg != "" {
		return msg
	}
	return "image stream returned an error"
}

func writeCapturedPlaygroundImageResponse(c *gin.Context, writer *playgroundImageCaptureWriter) {
	status := writer.Status()
	body := writer.body.Bytes()
	contentType := writer.Header().Get("Content-Type")
	if contentType == "" {
		contentType = gin.MIMEJSON
	}

	if status >= 200 && status < 300 && len(body) > 0 && !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		rewritten, err := rewritePlaygroundImageResponse(c, body)
		if err != nil {
			c.Writer.Header().Del("Content-Length")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("failed to persist playground image: %s", err.Error()),
				},
			})
			return
		}
		body = rewritten
	}

	for key, values := range writer.Header() {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		c.Writer.Header().Del(key)
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	c.Data(status, contentType, body)
}

func rewritePlaygroundImageResponse(c *gin.Context, raw []byte) ([]byte, error) {
	var response dto.ImageResponse
	if err := common.Unmarshal(raw, &response); err != nil {
		return raw, nil
	}
	if len(response.Data) == 0 {
		return raw, nil
	}

	userID := c.GetInt("id")
	sessionID := strings.TrimSpace(c.GetHeader(playgroundSessionHeader))
	messageKey := strings.TrimSpace(c.GetHeader(playgroundMessageKeyHeader))

	for idx := range response.Data {
		item := &response.Data[idx]
		switch {
		case item.B64Json != "":
			file, err := model.PersistPlaygroundImageBase64(userID, sessionID, messageKey, item.B64Json, "")
			if err != nil {
				return nil, err
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		case strings.HasPrefix(item.Url, "data:image/"):
			file, err := model.PersistPlaygroundImageBase64(userID, sessionID, messageKey, item.Url, "")
			if err != nil {
				return nil, err
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		case strings.HasPrefix(item.Url, "http://") || strings.HasPrefix(item.Url, "https://"):
			file, err := persistPlaygroundImageURL(userID, sessionID, messageKey, item.Url)
			if err != nil {
				return nil, err
			}
			item.Url = model.PlaygroundFileURL(file.ID)
			item.B64Json = ""
		}
	}

	return common.Marshal(response)
}

func persistPlaygroundImageURL(userID int, sessionID string, messageKey string, imageURL string) (*model.PlaygroundFile, error) {
	resp, err := service.DoDownloadRequest(imageURL, "playground_image_persist")
	if err != nil {
		return nil, fmt.Errorf("download playground image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download playground image: HTTP %d", resp.StatusCode)
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

func SavePlaygroundSessionMessagesAPI(c *gin.Context) {
	var req playgroundMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
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
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
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
	err := json.Unmarshal(raw, &response)
	return response, err
}
