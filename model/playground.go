package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

const (
	defaultPlaygroundFileDir    = "data/playground-files"
	playgroundFileURLPrefix     = "/api/playground/files/"
	playgroundFileURLSuffix     = "/content"
	playgroundImageRetryText    = "图片生成未完成，可以点击重新生成。"
	defaultPlaygroundImageSize  = "auto"
	defaultPlaygroundMaxImageMB = 64
)

var markdownDataImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\((data:image/[^;)]+;base64,[^)]+)\)`)

type PlaygroundSession struct {
	ID          string         `json:"id" gorm:"primaryKey;type:varchar(64)"`
	UserID      int            `json:"user_id" gorm:"primaryKey;index"`
	Title       string         `json:"title" gorm:"type:varchar(255)"`
	CreatedTime int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedTime int64          `json:"updated_at" gorm:"bigint;index"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
	Messages    []JSONValue    `json:"messages,omitempty" gorm:"-"`
}

type PlaygroundMessage struct {
	ID          int64          `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID      int            `json:"user_id" gorm:"index"`
	SessionID   string         `json:"session_id" gorm:"type:varchar(64);index"`
	MessageKey  string         `json:"key" gorm:"type:varchar(64);index"`
	Role        string         `json:"role" gorm:"type:varchar(32);index"`
	Mode        string         `json:"mode" gorm:"type:varchar(32);index"`
	SortOrder   int            `json:"sort_order" gorm:"index"`
	Payload     JSONValue      `json:"payload" gorm:"type:json"`
	CreatedTime int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedTime int64          `json:"updated_at" gorm:"bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type PlaygroundFile struct {
	ID          string         `json:"id" gorm:"primaryKey;type:varchar(64)"`
	UserID      int            `json:"user_id" gorm:"index"`
	SessionID   string         `json:"session_id" gorm:"type:varchar(64);index"`
	MessageKey  string         `json:"message_key" gorm:"type:varchar(64);index"`
	Filename    string         `json:"filename" gorm:"type:varchar(255)"`
	ContentType string         `json:"content_type" gorm:"type:varchar(100)"`
	SizeBytes   int64          `json:"size_bytes"`
	Sha256      string         `json:"sha256" gorm:"type:char(64);index"`
	StoragePath string         `json:"storage_path" gorm:"type:varchar(512)"`
	CreatedTime int64          `json:"created_at" gorm:"bigint;index"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type playgroundMessagePayload struct {
	Key      string `json:"key"`
	From     string `json:"from"`
	Mode     string `json:"mode"`
	Versions []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	} `json:"versions"`
}

func PlaygroundFileDir() string {
	dir := strings.TrimSpace(os.Getenv("PLAYGROUND_FILE_DIR"))
	if dir == "" {
		dir = defaultPlaygroundFileDir
	}
	return filepath.Clean(dir)
}

func (f PlaygroundFile) AbsolutePath() string {
	if filepath.IsAbs(f.StoragePath) {
		return f.StoragePath
	}
	return filepath.Join(PlaygroundFileDir(), f.StoragePath)
}

func PlaygroundFileURL(fileID string) string {
	return playgroundFileURLPrefix + fileID + playgroundFileURLSuffix
}

func UpsertPlaygroundSession(userID int, sessionID string, title string, createdAt int64, updatedAt int64) (*PlaygroundSession, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "pgs_" + common.GetUUID()
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New conversation"
	}
	now := time.Now().UnixMilli()
	if createdAt <= 0 {
		createdAt = now
	}
	if updatedAt <= 0 {
		updatedAt = createdAt
	}

	session := &PlaygroundSession{
		ID:          sessionID,
		UserID:      userID,
		Title:       title,
		CreatedTime: createdAt,
		UpdatedTime: updatedAt,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing PlaygroundSession
		err := tx.Unscoped().Where("id = ? AND user_id = ?", sessionID, userID).First(&existing).Error
		if err == nil {
			return tx.Unscoped().Model(&existing).Updates(map[string]any{
				"title":        title,
				"updated_time": updatedAt,
				"deleted_at":   nil,
			}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(session).Error
	})
	if err != nil {
		return nil, err
	}

	return session, nil
}

func ListPlaygroundSessions(userID int) ([]PlaygroundSession, error) {
	var sessions []PlaygroundSession
	if err := DB.Where("user_id = ?", userID).Order("updated_time DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	for idx := range sessions {
		messages, err := listPlaygroundMessagePayloads(userID, sessions[idx].ID)
		if err != nil {
			return nil, err
		}
		sessions[idx].Messages = messages
	}
	return sessions, nil
}

func RenamePlaygroundSession(userID int, sessionID string, title string) (*PlaygroundSession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("title is required")
	}
	var session PlaygroundSession
	if err := DB.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return nil, err
	}
	session.Title = title
	session.UpdatedTime = time.Now().UnixMilli()
	if err := DB.Save(&session).Error; err != nil {
		return nil, err
	}
	session.Messages, _ = listPlaygroundMessagePayloads(userID, sessionID)
	return &session, nil
}

func SoftDeletePlaygroundSession(userID int, sessionID string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND id = ?", userID, sessionID).Delete(&PlaygroundSession{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND session_id = ?", userID, sessionID).Delete(&PlaygroundMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND session_id = ?", userID, sessionID).Delete(&PlaygroundFile{}).Error
	})
}

func SavePlaygroundSessionMessages(userID int, sessionID string, messages []JSONValue) (*PlaygroundSession, error) {
	var session PlaygroundSession
	if err := DB.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	processed := make([]playgroundMessagePayload, 0, len(messages))
	records := make([]PlaygroundMessage, 0, len(messages))
	for idx, raw := range messages {
		message, payload, err := normalizePlaygroundMessagePayload(userID, sessionID, raw)
		if err != nil {
			return nil, err
		}
		payload, err = repairPlaygroundImagePayloadFromFiles(userID, sessionID, PlaygroundMessage{
			UserID:     userID,
			SessionID:  sessionID,
			MessageKey: message.Key,
			Role:       message.From,
			Mode:       message.Mode,
			Payload:    payload,
		})
		if err != nil {
			return nil, err
		}
		if repairedPayload, changed := repairPlaygroundImageErrorPayload(payload); changed {
			payload = repairedPayload
		}
		message.Mode = playgroundPayloadMode(payload, message.Mode)
		processed = append(processed, message)
		records = append(records, PlaygroundMessage{
			UserID:      userID,
			SessionID:   sessionID,
			MessageKey:  message.Key,
			Role:        message.From,
			Mode:        message.Mode,
			SortOrder:   idx,
			Payload:     payload,
			CreatedTime: now,
			UpdatedTime: now,
		})
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var lockedSession PlaygroundSession
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", sessionID, userID).First(&lockedSession).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND session_id = ?", userID, sessionID).Delete(&PlaygroundMessage{}).Error; err != nil {
			return err
		}
		if len(records) > 0 {
			if err := tx.Create(&records).Error; err != nil {
				return err
			}
		}
		session.UpdatedTime = now
		if len(processed) > 0 {
			session.Title = inferPlaygroundSessionTitle(session.Title, processed)
		}
		return tx.Model(&PlaygroundSession{}).
			Where("id = ? AND user_id = ?", sessionID, userID).
			Updates(map[string]any{"updated_time": session.UpdatedTime, "title": session.Title}).Error
	})
	if err != nil {
		return nil, err
	}

	session.Messages = make([]JSONValue, 0, len(records))
	for _, record := range records {
		session.Messages = append(session.Messages, record.Payload)
	}
	return &session, nil
}

func CompletePlaygroundImageMessage(userID int, sessionID string, messageKey string, content string, completedAt int64) error {
	return updatePlaygroundImageMessage(userID, sessionID, messageKey, completedAt, func(payload map[string]any) {
		payload["status"] = "complete"
		payload["mode"] = "image"
		payload["isReasoningStreaming"] = false
		payload["completedAt"] = completedAt
		updatePlaygroundPayloadFirstVersion(payload, content)

		imageGeneration := playgroundPayloadMap(payload, "imageGeneration")
		ensurePlaygroundImageGenerationPayload(payload, imageGeneration, "complete")
		imageGeneration["status"] = "complete"
		imageGeneration["completedAt"] = completedAt
		delete(imageGeneration, "error")
		payload["imageGeneration"] = imageGeneration
	})
}

func FailPlaygroundImageMessage(userID int, sessionID string, messageKey string, errorMessage string, completedAt int64) error {
	if strings.TrimSpace(errorMessage) == "" {
		errorMessage = "image generation failed"
	}
	return updatePlaygroundImageMessage(userID, sessionID, messageKey, completedAt, func(payload map[string]any) {
		payload["status"] = "complete"
		payload["mode"] = "image"
		payload["isReasoningStreaming"] = false
		payload["completedAt"] = completedAt
		updatePlaygroundPayloadFirstVersion(payload, playgroundImageRetryText)

		imageGeneration := playgroundPayloadMap(payload, "imageGeneration")
		ensurePlaygroundImageGenerationPayload(payload, imageGeneration, "retryable")
		imageGeneration["status"] = "retryable"
		imageGeneration["completedAt"] = completedAt
		delete(imageGeneration, "error")
		payload["imageGeneration"] = imageGeneration
	})
}

func updatePlaygroundImageMessage(userID int, sessionID string, messageKey string, completedAt int64, mutate func(map[string]any)) error {
	messageKey = strings.TrimSpace(messageKey)
	if messageKey == "" {
		return errors.New("message key is required")
	}
	if completedAt <= 0 {
		completedAt = time.Now().UnixMilli()
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var message PlaygroundMessage
		err := tx.Where("user_id = ? AND session_id = ? AND message_key = ?", userID, sessionID, messageKey).First(&message).Error
		if err != nil {
			return err
		}

		var payload map[string]any
		if len(message.Payload) > 0 {
			_ = json.Unmarshal(message.Payload, &payload)
		}
		if payload == nil {
			payload = map[string]any{}
		}
		if _, ok := payload["key"].(string); !ok {
			payload["key"] = messageKey
		}
		if _, ok := payload["from"].(string); !ok {
			payload["from"] = "assistant"
		}
		mutate(payload)

		normalized, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Model(&PlaygroundMessage{}).
			Where("id = ? AND user_id = ? AND session_id = ?", message.ID, userID, sessionID).
			Updates(map[string]any{
				"payload":      JSONValue(normalized),
				"mode":         "image",
				"role":         "assistant",
				"updated_time": completedAt,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&PlaygroundSession{}).
			Where("id = ? AND user_id = ?", sessionID, userID).
			Update("updated_time", completedAt).Error
	})
}

func updatePlaygroundPayloadFirstVersion(payload map[string]any, content string) {
	versions, _ := payload["versions"].([]any)
	if len(versions) == 0 {
		payload["versions"] = []any{
			map[string]any{
				"id":      "image-result",
				"content": content,
			},
		}
		return
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		version = map[string]any{}
		versions[0] = version
	}
	if _, ok := version["id"].(string); !ok {
		version["id"] = "image-result"
	}
	version["content"] = content
	payload["versions"] = versions
}

func ensurePlaygroundImageGenerationPayload(payload map[string]any, imageGeneration map[string]any, status string) {
	if strings.TrimSpace(asString(imageGeneration["taskId"])) == "" {
		messageKey := asString(payload["key"])
		if strings.TrimSpace(messageKey) == "" {
			messageKey = "server"
		}
		imageGeneration["taskId"] = "image-" + messageKey
	}
	if _, ok := imageGeneration["prompt"].(string); !ok {
		imageGeneration["prompt"] = ""
	}
	if strings.TrimSpace(asString(imageGeneration["size"])) == "" {
		imageGeneration["size"] = defaultPlaygroundImageSize
	}
	if strings.TrimSpace(asString(imageGeneration["status"])) == "" {
		imageGeneration["status"] = status
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func playgroundPayloadMap(payload map[string]any, key string) map[string]any {
	if value, ok := payload[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func ImportPlaygroundSessions(userID int, sessions []PlaygroundSession) ([]PlaygroundSession, error) {
	for _, session := range sessions {
		createdAt := session.CreatedTime
		if createdAt == 0 {
			createdAt = time.Now().UnixMilli()
		}
		updatedAt := session.UpdatedTime
		if updatedAt == 0 {
			updatedAt = createdAt
		}
		upserted, err := UpsertPlaygroundSession(userID, session.ID, session.Title, createdAt, updatedAt)
		if err != nil {
			return nil, err
		}
		if len(session.Messages) > 0 {
			if _, err := SavePlaygroundSessionMessages(userID, upserted.ID, session.Messages); err != nil {
				return nil, err
			}
		}
	}
	return ListPlaygroundSessions(userID)
}

func PersistPlaygroundImageBase64(userID int, sessionID string, messageKey string, b64 string, contentType string) (*PlaygroundFile, error) {
	contentTypeFromDataURL := ""
	if strings.HasPrefix(b64, "data:") {
		parsedContentType, clean, err := decodeDataURLImage(b64)
		if err != nil {
			return nil, err
		}
		contentTypeFromDataURL = parsedContentType
		b64 = clean
	}
	b64 = strings.TrimSpace(b64)
	if int64(base64.StdEncoding.DecodedLen(len(b64))) > playgroundMaxImageBytes() {
		return nil, errors.New("playground image exceeds maximum size")
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode playground image base64: %w", err)
	}
	if contentType == "" {
		contentType = contentTypeFromDataURL
	}
	if contentType == "" {
		contentType = "image/png"
	}
	if contentType == "application/octet-stream" {
		contentType = "image/png"
	}
	return PersistPlaygroundImageBytes(userID, sessionID, messageKey, decoded, contentType)
}

func PersistPlaygroundImageBytes(userID int, sessionID string, messageKey string, data []byte, contentType string) (*PlaygroundFile, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if len(data) == 0 {
		return nil, errors.New("empty playground image")
	}
	if int64(len(data)) > playgroundMaxImageBytes() {
		return nil, errors.New("playground image exceeds maximum size")
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("invalid playground image content type: %s", contentType)
	}

	now := time.Now()
	fileID := "pgf_" + common.GetUUID()
	ext := extensionForContentType(contentType)
	filename := fileID + ext
	relativePath := filepath.Join(fmt.Sprintf("%d", userID), now.Format("20060102"), filename)
	absolutePath := filepath.Join(PlaygroundFileDir(), relativePath)

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(absolutePath, data, 0644); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	file := &PlaygroundFile{
		ID:          fileID,
		UserID:      userID,
		SessionID:   sessionID,
		MessageKey:  messageKey,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
		Sha256:      hex.EncodeToString(sum[:]),
		StoragePath: relativePath,
		CreatedTime: now.UnixMilli(),
	}
	if err := DB.Create(file).Error; err != nil {
		_ = os.Remove(absolutePath)
		return nil, err
	}
	return file, nil
}

func playgroundMaxImageBytes() int64 {
	limitMB := constant.MaxFileDownloadMB
	if limitMB <= 0 {
		limitMB = defaultPlaygroundMaxImageMB
	}
	return int64(limitMB) * 1024 * 1024
}

func ResolvePlaygroundFileForUser(userID int, fileID string) (*PlaygroundFile, string, error) {
	var file PlaygroundFile
	err := DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", errors.New("playground file not found")
		}
		return nil, "", err
	}
	path := file.AbsolutePath()
	if _, err := os.Stat(path); err != nil {
		return nil, "", err
	}
	return &file, path, nil
}

func listPlaygroundMessagePayloads(userID int, sessionID string) ([]JSONValue, error) {
	var messages []PlaygroundMessage
	if err := DB.Where("user_id = ? AND session_id = ?", userID, sessionID).
		Order("created_time DESC").
		Order("updated_time DESC").
		Order("id DESC").
		Find(&messages).Error; err != nil {
		return nil, err
	}

	deduplicated := make([]PlaygroundMessage, 0, len(messages))
	seenMessageKeys := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if message.MessageKey != "" {
			if _, exists := seenMessageKeys[message.MessageKey]; exists {
				continue
			}
			seenMessageKeys[message.MessageKey] = struct{}{}
		}
		deduplicated = append(deduplicated, message)
	}
	sort.SliceStable(deduplicated, func(i, j int) bool {
		if deduplicated[i].SortOrder == deduplicated[j].SortOrder {
			return deduplicated[i].ID < deduplicated[j].ID
		}
		return deduplicated[i].SortOrder < deduplicated[j].SortOrder
	})

	payloads := make([]JSONValue, 0, len(deduplicated))
	for _, message := range deduplicated {
		payload, err := repairPlaygroundImagePayloadFromFiles(userID, sessionID, message)
		if err != nil {
			return nil, err
		}
		if normalized, changed := normalizePlaygroundImageGenerationPayloadMetadata(payload, message.MessageKey, "complete"); changed {
			payload = normalized
			persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, payload, time.Now().UnixMilli())
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func repairPlaygroundImagePayloadFromFiles(userID int, sessionID string, message PlaygroundMessage) (JSONValue, error) {
	if message.Role != "assistant" || len(message.Payload) == 0 {
		return message.Payload, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return nil, err
	}

	imageGeneration, _ := payload["imageGeneration"].(map[string]any)
	payloadMode, _ := payload["mode"].(string)
	messageKey, _ := payload["key"].(string)
	if strings.TrimSpace(messageKey) == "" {
		messageKey = message.MessageKey
	}
	if message.Mode != "image" && payloadMode != "image" && imageGeneration == nil {
		if repaired, changed := repairPlaygroundImageErrorPayload(message.Payload); changed {
			if message.ID > 0 {
				now := time.Now().UnixMilli()
				persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, repaired, now)
			}
			return repaired, nil
		}
		return message.Payload, nil
	}

	if strings.Contains(string(message.Payload), playgroundFileURLPrefix) {
		if normalized, changed := normalizePlaygroundImageGenerationPayloadMetadata(message.Payload, messageKey, "complete"); changed {
			if message.ID > 0 {
				persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, normalized, time.Now().UnixMilli())
			}
			return normalized, nil
		}
		return message.Payload, nil
	}

	if strings.TrimSpace(messageKey) == "" {
		return message.Payload, nil
	}

	var files []PlaygroundFile
	if err := DB.Where("user_id = ? AND session_id = ? AND message_key = ?", userID, sessionID, messageKey).
		Order("created_time ASC").
		Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) == 0 {
		if repaired, changed := repairPlaygroundImageErrorPayload(message.Payload); changed {
			if message.ID > 0 {
				now := time.Now().UnixMilli()
				persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, repaired, now)
			}
			return repaired, nil
		}
		if normalized, changed := normalizePlaygroundImageGenerationPayloadMetadata(message.Payload, messageKey, "pending"); changed {
			if message.ID > 0 {
				persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, normalized, time.Now().UnixMilli())
			}
			return normalized, nil
		}
		return message.Payload, nil
	}

	imageMarkdown := buildPlaygroundFileMarkdown(files)
	payload["mode"] = "image"
	versions, _ := payload["versions"].([]any)
	if len(versions) == 0 {
		payload["versions"] = []any{
			map[string]any{
				"id":      "recovered",
				"content": imageMarkdown,
			},
		}
	} else {
		version, ok := versions[0].(map[string]any)
		if !ok {
			version = map[string]any{}
			versions[0] = version
		}
		if _, ok := version["id"].(string); !ok {
			version["id"] = "recovered"
		}
		version["content"] = imageMarkdown
		payload["versions"] = versions
	}

	completedAt := files[len(files)-1].CreatedTime
	payload["status"] = "complete"
	if _, ok := payload["completedAt"]; !ok {
		payload["completedAt"] = completedAt
	}
	if imageGeneration == nil {
		imageGeneration = map[string]any{}
	}
	ensurePlaygroundImageGenerationPayload(payload, imageGeneration, "complete")
	imageGeneration["status"] = "complete"
	imageGeneration["completedAt"] = completedAt
	delete(imageGeneration, "error")
	payload["imageGeneration"] = imageGeneration

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	normalizedPayload := JSONValue(normalized)
	if message.ID > 0 && string(normalizedPayload) != string(message.Payload) {
		persistPlaygroundMessagePayloadIfChanged(userID, sessionID, message, normalizedPayload, completedAt)
	}
	return normalizedPayload, nil
}

func normalizePlaygroundImageGenerationPayloadMetadata(raw JSONValue, fallbackKey string, defaultStatus string) (JSONValue, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw, false
	}
	imageGeneration, ok := payload["imageGeneration"].(map[string]any)
	if !ok {
		return raw, false
	}
	if strings.TrimSpace(asString(payload["key"])) == "" && strings.TrimSpace(fallbackKey) != "" {
		payload["key"] = fallbackKey
	}
	if strings.TrimSpace(asString(defaultStatus)) == "" {
		defaultStatus = defaultPlaygroundImageGenerationStatus(payload)
	}
	ensurePlaygroundImageGenerationPayload(payload, imageGeneration, defaultStatus)
	payload["mode"] = "image"
	payload["imageGeneration"] = imageGeneration

	normalized, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	normalizedPayload := JSONValue(normalized)
	return normalizedPayload, string(normalizedPayload) != string(raw)
}

func defaultPlaygroundImageGenerationStatus(payload map[string]any) string {
	switch asString(payload["status"]) {
	case "complete":
		return "complete"
	case "error":
		return "error"
	default:
		return "pending"
	}
}

func persistPlaygroundMessagePayloadIfChanged(userID int, sessionID string, message PlaygroundMessage, payload JSONValue, updatedAt int64) {
	if message.ID <= 0 || string(payload) == string(message.Payload) {
		return
	}
	if updatedAt <= 0 {
		updatedAt = time.Now().UnixMilli()
	}
	mode := playgroundPayloadMode(payload, message.Mode)
	_ = DB.Model(&PlaygroundMessage{}).
		Where("id = ? AND user_id = ? AND session_id = ?", message.ID, userID, sessionID).
		Updates(map[string]any{
			"payload":      payload,
			"mode":         mode,
			"updated_time": updatedAt,
		}).Error
}

func repairPlaygroundImageErrorPayload(raw JSONValue) (JSONValue, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw, false
	}

	status, _ := payload["status"].(string)
	payloadMode, _ := payload["mode"].(string)
	imageGeneration, _ := payload["imageGeneration"].(map[string]any)
	if payloadMode != "image" && imageGeneration == nil {
		return raw, false
	}
	imageStatus, _ := imageGeneration["status"].(string)
	content := playgroundPayloadFirstVersionContent(payload)
	if status != "error" && imageStatus != "error" && !strings.Contains(content, "Request error occurred") {
		return raw, false
	}

	payload["status"] = "complete"
	payload["mode"] = "image"
	payload["isReasoningStreaming"] = false
	updatePlaygroundPayloadFirstVersion(payload, playgroundImageRetryText)
	if imageGeneration == nil {
		imageGeneration = map[string]any{}
	}
	ensurePlaygroundImageGenerationPayload(payload, imageGeneration, "retryable")
	if strings.TrimSpace(imageStatus) == "" || imageStatus == "error" {
		imageGeneration["status"] = "retryable"
	}
	delete(imageGeneration, "error")
	payload["imageGeneration"] = imageGeneration

	normalized, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return JSONValue(normalized), true
}

func playgroundPayloadFirstVersionContent(payload map[string]any) string {
	versions, _ := payload["versions"].([]any)
	if len(versions) == 0 {
		return ""
	}
	version, _ := versions[0].(map[string]any)
	content, _ := version["content"].(string)
	return content
}

func playgroundPayloadMode(payload JSONValue, fallback string) string {
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return fallback
	}
	mode, _ := payloadMap["mode"].(string)
	if strings.TrimSpace(mode) == "" {
		return fallback
	}
	return mode
}

func buildPlaygroundFileMarkdown(files []PlaygroundFile) string {
	images := make([]string, 0, len(files))
	for idx, file := range files {
		images = append(images, fmt.Sprintf("![Generated image %d](%s)", idx+1, PlaygroundFileURL(file.ID)))
	}
	return strings.Join(images, "\n\n")
}

func normalizePlaygroundMessagePayload(userID int, sessionID string, raw JSONValue) (playgroundMessagePayload, JSONValue, error) {
	var message playgroundMessagePayload
	if err := json.Unmarshal(raw, &message); err != nil {
		return message, nil, err
	}
	if message.Key == "" {
		message.Key = "msg_" + common.GetUUID()
	}
	if message.From == "" {
		message.From = "assistant"
	}
	if message.Mode == "" {
		message.Mode = "chat"
	}

	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil {
		return message, nil, err
	}
	changed := sanitizePlaygroundAttachmentDataURLs(payloadMap)

	if len(message.Versions) > 0 {
		rawVersions, ok := payloadMap["versions"].([]any)
		if ok {
			for idx := range rawVersions {
				version, ok := rawVersions[idx].(map[string]any)
				if !ok {
					continue
				}
				content, ok := version["content"].(string)
				if !ok {
					continue
				}
				rewritten, err := rewriteInlineBase64Images(userID, sessionID, message.Key, content)
				if err != nil {
					return message, nil, err
				}
				if rewritten != content {
					changed = true
				}
				version["content"] = rewritten
				if idx < len(message.Versions) {
					message.Versions[idx].Content = rewritten
				}
			}
		}
	}

	if !changed {
		return message, raw, nil
	}
	normalized, err := json.Marshal(payloadMap)
	if err != nil {
		return message, nil, err
	}
	return message, JSONValue(normalized), nil
}

func sanitizePlaygroundAttachmentDataURLs(payload map[string]any) bool {
	attachments, ok := payload["attachments"].([]any)
	if !ok {
		return false
	}

	changed := false
	for _, rawAttachment := range attachments {
		attachment, ok := rawAttachment.(map[string]any)
		if !ok {
			continue
		}
		url, _ := attachment["url"].(string)
		if !strings.HasPrefix(strings.ToLower(url), "data:") {
			continue
		}
		delete(attachment, "url")
		changed = true
	}
	return changed
}

func rewriteInlineBase64Images(userID int, sessionID string, messageKey string, content string) (string, error) {
	var firstErr error
	rewritten := markdownDataImagePattern.ReplaceAllStringFunc(content, func(match string) string {
		if firstErr != nil {
			return match
		}
		parts := markdownDataImagePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		file, err := PersistPlaygroundImageBase64(userID, sessionID, messageKey, parts[2], "")
		if err != nil {
			firstErr = err
			return match
		}
		return fmt.Sprintf("![%s](%s)", parts[1], PlaygroundFileURL(file.ID))
	})
	if firstErr != nil {
		return "", firstErr
	}
	return rewritten, nil
}

func decodeDataURLImage(dataURL string) (string, string, error) {
	prefix, payload, ok := strings.Cut(dataURL, ",")
	if !ok {
		return "", "", errors.New("invalid data url")
	}
	contentType := strings.TrimPrefix(prefix, "data:")
	contentType = strings.TrimSuffix(contentType, ";base64")
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mediaType
	}
	if !strings.HasPrefix(contentType, "image/") {
		return "", "", fmt.Errorf("invalid data url content type: %s", contentType)
	}
	return contentType, payload, nil
}

func extensionForContentType(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		extensions, err := mime.ExtensionsByType(contentType)
		if err == nil && len(extensions) > 0 {
			sort.Strings(extensions)
			return extensions[0]
		}
		return ".img"
	}
}

func inferPlaygroundSessionTitle(current string, messages []playgroundMessagePayload) string {
	if strings.TrimSpace(current) != "" && current != "New conversation" {
		return current
	}
	for _, message := range messages {
		if message.From != "user" || len(message.Versions) == 0 {
			continue
		}
		title := strings.TrimSpace(message.Versions[0].Content)
		if title == "" {
			continue
		}
		title = strings.Join(strings.Fields(title), " ")
		if len([]rune(title)) > 32 {
			runes := []rune(title)
			title = string(runes[:32]) + "..."
		}
		return title
	}
	return current
}
