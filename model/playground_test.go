package model

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/require"
)

func setupPlaygroundModelTest(t *testing.T) string {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&PlaygroundSession{}, &PlaygroundMessage{}, &PlaygroundFile{}))

	dir := t.TempDir()
	t.Setenv("PLAYGROUND_FILE_DIR", dir)
	return dir
}

func requireImageGenerationMetadata(t *testing.T, payload map[string]any, taskID string, status string) {
	t.Helper()

	imageGeneration, ok := payload["imageGeneration"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, taskID, imageGeneration["taskId"])
	require.Equal(t, "", imageGeneration["prompt"])
	require.Equal(t, defaultPlaygroundImageSize, imageGeneration["size"])
	require.Equal(t, status, imageGeneration["status"])
}

func TestSavePlaygroundSessionMessagesConvertsInlineBase64Images(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "session-inline", "Inline image", 1000, 1000)
	require.NoError(t, err)

	inlineImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	messages := []JSONValue{
		JSONValue(`{"key":"user-1","from":"user","versions":[{"id":"v1","content":"make image"}]}`),
		JSONValue(`{"key":"assistant-1","from":"assistant","mode":"image","versions":[{"id":"v1","content":"![Generated image](` + inlineImage + `)"}]}`),
	}

	saved, err := SavePlaygroundSessionMessages(1, session.ID, messages)
	require.NoError(t, err)
	require.Len(t, saved.Messages, 2)

	payload := string(saved.Messages[1])
	require.NotContains(t, payload, "data:image/png;base64")
	require.NotContains(t, payload, base64.StdEncoding.EncodeToString([]byte("png-bytes")))
	require.Contains(t, payload, "/api/playground/files/")
	require.Contains(t, payload, "/content")

	var files []PlaygroundFile
	require.NoError(t, DB.Find(&files).Error)
	require.Len(t, files, 1)
	require.Equal(t, 1, files[0].UserID)
	require.Equal(t, session.ID, files[0].SessionID)
	require.Equal(t, "assistant-1", files[0].MessageKey)

	content, err := os.ReadFile(files[0].AbsolutePath())
	require.NoError(t, err)
	require.Equal(t, []byte("png-bytes"), content)
}

func TestPlaygroundSessionListIsScopedToUserAndOrdered(t *testing.T) {
	setupPlaygroundModelTest(t)

	_, err := UpsertPlaygroundSession(1, "older", "Older", 1000, 1000)
	require.NoError(t, err)
	_, err = UpsertPlaygroundSession(2, "other-user", "Other", 2000, 2000)
	require.NoError(t, err)
	_, err = UpsertPlaygroundSession(1, "newer", "Newer", 3000, 3000)
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, "newer", sessions[0].ID)
	require.Equal(t, "older", sessions[1].ID)
}

func TestListPlaygroundSessionsDeduplicatesConcurrentMessageSnapshots(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "duplicate-snapshot", "Duplicate snapshot", 1000, 2000)
	require.NoError(t, err)

	records := []PlaygroundMessage{
		{
			UserID:      1,
			SessionID:   session.ID,
			MessageKey:  "assistant-pdf",
			Role:        "assistant",
			Mode:        "image",
			SortOrder:   1,
			Payload:     JSONValue(`{"key":"assistant-pdf","from":"assistant","mode":"image","status":"complete","imageGeneration":{"status":"retryable"},"versions":[{"id":"old","content":"图片生成未完成，可以点击重新生成。"}]}`),
			CreatedTime: 1000,
			UpdatedTime: 1000,
		},
		{
			UserID:      1,
			SessionID:   session.ID,
			MessageKey:  "assistant-pdf",
			Role:        "assistant",
			Mode:        "chat",
			SortOrder:   1,
			Payload:     JSONValue(`{"key":"assistant-pdf","from":"assistant","mode":"chat","status":"complete","versions":[{"id":"new","content":"PDF summary"}]}`),
			CreatedTime: 2000,
			UpdatedTime: 2000,
		},
	}
	require.NoError(t, DB.Create(&records).Error)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 1)
	require.Contains(t, string(sessions[0].Messages[0]), "PDF summary")
	require.NotContains(t, string(sessions[0].Messages[0]), playgroundImageRetryText)
}

func TestSoftDeletePlaygroundSessionRemovesFileAccess(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "delete-me", "Delete me", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	require.NoError(t, SoftDeletePlaygroundSession(1, session.ID))

	_, _, err = ResolvePlaygroundFileForUser(1, file.ID)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "not found"))
}

func TestPersistPlaygroundImageBytesRejectsOversizedImages(t *testing.T) {
	dir := setupPlaygroundModelTest(t)

	previousLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		constant.MaxFileDownloadMB = previousLimit
	})

	_, err := PersistPlaygroundImageBytes(1, "session-large", "assistant-large", make([]byte, 1024*1024+1), "image/png")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum size")

	var count int64
	require.NoError(t, DB.Model(&PlaygroundFile{}).Count(&count).Error)
	require.Zero(t, count)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestPersistPlaygroundImageBase64RejectsOversizedImagesBeforeWriting(t *testing.T) {
	dir := setupPlaygroundModelTest(t)

	previousLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		constant.MaxFileDownloadMB = previousLimit
	})

	oversized := base64.StdEncoding.EncodeToString(make([]byte, 1024*1024+1))
	_, err := PersistPlaygroundImageBase64(1, "session-large", "assistant-large", oversized, "image/png")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum size")

	var count int64
	require.NoError(t, DB.Model(&PlaygroundFile{}).Count(&count).Error)
	require.Zero(t, count)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestListPlaygroundSessionsRepairsPersistedImageNetworkError(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "repair-me", "Repair me", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "user-1",
		Role:       "user",
		Mode:       "image",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"user-1","from":"user","mode":"image","versions":[{"id":"v1","content":"cute cat"}]}`,
		),
	}).Error
	require.NoError(t, err)
	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-1",
		Role:       "assistant",
		Mode:       "image",
		SortOrder:  1,
		Payload: JSONValue(
			`{"key":"assistant-1","from":"assistant","mode":"image","status":"error","imageGeneration":{"status":"error","error":"Network Error"},"versions":[{"id":"v1","content":"Request error occurred: Network Error"}]}`,
		),
	}).Error
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 2)

	var repaired map[string]any
	require.NoError(t, json.Unmarshal(sessions[0].Messages[1], &repaired))
	require.Equal(t, "complete", repaired["status"])
	require.NotContains(t, string(sessions[0].Messages[1]), "Network Error")
	require.Contains(t, string(sessions[0].Messages[1]), PlaygroundFileURL(file.ID))
	require.Contains(t, string(sessions[0].Messages[1]), `"status":"complete"`)

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-1").First(&persisted).Error)
	require.NotContains(t, string(persisted.Payload), "Network Error")
	require.Contains(t, string(persisted.Payload), PlaygroundFileURL(file.ID))
}

func TestSavePlaygroundSessionMessagesRepairsPersistedImageNetworkError(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "save-repair", "Save repair", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	messages := []JSONValue{
		JSONValue(`{"key":"user-1","from":"user","mode":"image","versions":[{"id":"v1","content":"cute cat"}]}`),
		JSONValue(`{"key":"assistant-1","from":"assistant","mode":"image","status":"error","imageGeneration":{"status":"error","error":"Network Error"},"versions":[{"id":"v1","content":"Request error occurred: Network Error"}]}`),
	}

	saved, err := SavePlaygroundSessionMessages(1, session.ID, messages)
	require.NoError(t, err)
	require.Len(t, saved.Messages, 2)
	require.NotContains(t, string(saved.Messages[1]), "Network Error")
	require.Contains(t, string(saved.Messages[1]), PlaygroundFileURL(file.ID))
	require.Contains(t, string(saved.Messages[1]), `"status":"complete"`)

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-1").First(&persisted).Error)
	require.NotContains(t, string(persisted.Payload), "Network Error")
	require.Contains(t, string(persisted.Payload), PlaygroundFileURL(file.ID))
}

func TestListPlaygroundSessionsRepairsPendingImageWithPersistedFile(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "repair-pending", "Repair pending", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-1",
		Role:       "assistant",
		Mode:       "image",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"assistant-1","from":"assistant","mode":"image","status":"loading","imageGeneration":{"status":"pending"},"versions":[{"id":"v1","content":""}]}`,
		),
	}).Error
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 1)

	var repaired map[string]any
	require.NoError(t, json.Unmarshal(sessions[0].Messages[0], &repaired))
	require.Equal(t, "image", repaired["mode"])
	require.Equal(t, "complete", repaired["status"])
	require.Contains(t, string(sessions[0].Messages[0]), PlaygroundFileURL(file.ID))

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-1").First(&persisted).Error)
	require.Equal(t, "image", persisted.Mode)
	require.Contains(t, string(persisted.Payload), PlaygroundFileURL(file.ID))
}

func TestListPlaygroundSessionsCompletesPartialImageMetadataWithPersistedFileURL(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "repair-partial-metadata", "Repair partial metadata", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-1",
		Role:       "assistant",
		Mode:       "image",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"assistant-1","from":"assistant","mode":"image","status":"complete","imageGeneration":{"status":"complete"},"versions":[{"id":"v1","content":"![Generated image](` + PlaygroundFileURL(file.ID) + `)"}]}`,
		),
	}).Error
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 1)

	var repaired map[string]any
	require.NoError(t, json.Unmarshal(sessions[0].Messages[0], &repaired))
	require.Equal(t, "complete", repaired["status"])
	require.Equal(t, "image", repaired["mode"])
	require.Contains(t, string(sessions[0].Messages[0]), PlaygroundFileURL(file.ID))
	requireImageGenerationMetadata(t, repaired, "image-assistant-1", "complete")

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-1").First(&persisted).Error)
	require.Contains(t, string(persisted.Payload), `"taskId":"image-assistant-1"`)
	require.Contains(t, string(persisted.Payload), `"size":"auto"`)
}

func TestSavePlaygroundSessionMessagesRepairsLegacyImageMessageWithoutMode(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "repair-legacy", "Repair legacy", 1000, 1000)
	require.NoError(t, err)
	file, err := PersistPlaygroundImageBytes(1, session.ID, "assistant-1", []byte("image"), "image/png")
	require.NoError(t, err)

	messages := []JSONValue{
		JSONValue(`{"key":"assistant-1","from":"assistant","status":"error","imageGeneration":{"status":"error","error":"Network Error"},"versions":[{"id":"v1","content":"Request error occurred: Network Error"}]}`),
	}

	saved, err := SavePlaygroundSessionMessages(1, session.ID, messages)
	require.NoError(t, err)
	require.Len(t, saved.Messages, 1)
	require.NotContains(t, string(saved.Messages[0]), "Network Error")
	require.Contains(t, string(saved.Messages[0]), PlaygroundFileURL(file.ID))
	require.Contains(t, string(saved.Messages[0]), `"mode":"image"`)

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-1").First(&persisted).Error)
	require.Equal(t, "image", persisted.Mode)
	require.Contains(t, string(persisted.Payload), PlaygroundFileURL(file.ID))
}

func TestSavePlaygroundSessionMessagesKeepsChatRequestErrorsInChatMode(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "pdf-chat-error", "PDF chat error", 1000, 1000)
	require.NoError(t, err)

	messages := []JSONValue{
		JSONValue(`{"key":"user-pdf","from":"user","mode":"chat","attachments":[{"filename":"book.pdf","mediaType":"application/pdf","extractionStatus":"empty"}],"versions":[{"id":"v1","content":"这本电子书主要讲什么内容？"}]}`),
		JSONValue(`{"key":"assistant-pdf","from":"assistant","mode":"chat","status":"error","versions":[{"id":"v2","content":"Request error occurred: PDF could not be processed"}]}`),
	}

	saved, err := SavePlaygroundSessionMessages(1, session.ID, messages)
	require.NoError(t, err)
	require.Len(t, saved.Messages, 2)

	var assistant map[string]any
	require.NoError(t, json.Unmarshal(saved.Messages[1], &assistant))
	require.Equal(t, "chat", assistant["mode"])
	require.Equal(t, "error", assistant["status"])
	require.Nil(t, assistant["imageGeneration"])
	require.Contains(t, string(saved.Messages[1]), "PDF could not be processed")
	require.NotContains(t, string(saved.Messages[1]), playgroundImageRetryText)

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-pdf").First(&persisted).Error)
	require.Equal(t, "chat", persisted.Mode)
}

func TestListPlaygroundSessionsConvertsImageGatewayTimeoutToRetryableMessage(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "retryable-timeout", "Retryable timeout", 1000, 1000)
	require.NoError(t, err)

	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-timeout",
		Role:       "assistant",
		Mode:       "image",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"assistant-timeout","from":"assistant","mode":"image","status":"error","imageGeneration":{"status":"error","error":"Request failed with status code 524"},"versions":[{"id":"v1","content":"Request error occurred: Request failed with status code 524"}]}`,
		),
	}).Error
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 1)

	var repaired map[string]any
	require.NoError(t, json.Unmarshal(sessions[0].Messages[0], &repaired))
	require.Equal(t, "complete", repaired["status"])
	require.Equal(t, "image", repaired["mode"])
	require.NotContains(t, string(sessions[0].Messages[0]), "Request error occurred")
	require.NotContains(t, string(sessions[0].Messages[0]), "status code 524")
	require.Contains(t, string(sessions[0].Messages[0]), playgroundImageRetryText)
	require.Contains(t, string(sessions[0].Messages[0]), `"status":"retryable"`)
	requireImageGenerationMetadata(t, repaired, "image-assistant-timeout", "retryable")
}

func TestListPlaygroundSessionsKeepsLegacyChatGatewayTimeoutInChatMode(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "legacy-timeout", "Legacy timeout", 1000, 1000)
	require.NoError(t, err)

	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-legacy-timeout",
		Role:       "assistant",
		Mode:       "chat",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"assistant-legacy-timeout","from":"assistant","mode":"chat","status":"error","versions":[{"id":"v1","content":"Request error occurred: Request failed with status code 524"}]}`,
		),
	}).Error
	require.NoError(t, err)

	sessions, err := ListPlaygroundSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Len(t, sessions[0].Messages, 1)

	var loaded map[string]any
	require.NoError(t, json.Unmarshal(sessions[0].Messages[0], &loaded))
	require.Equal(t, "error", loaded["status"])
	require.Equal(t, "chat", loaded["mode"])
	require.Nil(t, loaded["imageGeneration"])
	require.Contains(t, string(sessions[0].Messages[0]), "status code 524")
	require.NotContains(t, string(sessions[0].Messages[0]), playgroundImageRetryText)

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-legacy-timeout").First(&persisted).Error)
	require.Equal(t, "chat", persisted.Mode)
}

func TestFailPlaygroundImageMessageStoresRetryableMessage(t *testing.T) {
	setupPlaygroundModelTest(t)

	session, err := UpsertPlaygroundSession(1, "fail-retryable", "Fail retryable", 1000, 1000)
	require.NoError(t, err)
	err = DB.Create(&PlaygroundMessage{
		UserID:     1,
		SessionID:  session.ID,
		MessageKey: "assistant-timeout",
		Role:       "assistant",
		Mode:       "image",
		SortOrder:  0,
		Payload: JSONValue(
			`{"key":"assistant-timeout","from":"assistant","mode":"image","status":"loading","imageGeneration":{"status":"pending"},"versions":[{"id":"v1","content":""}]}`,
		),
	}).Error
	require.NoError(t, err)

	require.NoError(t, FailPlaygroundImageMessage(1, session.ID, "assistant-timeout", "Request failed with status code 524", 2000))

	var persisted PlaygroundMessage
	require.NoError(t, DB.Where("message_key = ?", "assistant-timeout").First(&persisted).Error)
	require.NotContains(t, string(persisted.Payload), "Request error occurred")
	require.NotContains(t, string(persisted.Payload), "status code 524")
	require.Contains(t, string(persisted.Payload), playgroundImageRetryText)
	require.Contains(t, string(persisted.Payload), `"status":"complete"`)
	require.Contains(t, string(persisted.Payload), `"status":"retryable"`)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(persisted.Payload, &payload))
	requireImageGenerationMetadata(t, payload, "image-assistant-timeout", "retryable")
}
