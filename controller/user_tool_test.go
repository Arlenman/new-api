package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userToolAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    model.JSONValue `json:"data"`
}

type userToolBootstrapTestData struct {
	Items []struct {
		ID       string          `json:"id"`
		Kind     string          `json:"kind"`
		Key      string          `json:"key"`
		Revision int64           `json:"revision"`
		Payload  model.JSONValue `json:"payload"`
	} `json:"items"`
	Cursor      int64  `json:"cursor"`
	NextAfterID string `json:"next_after_id"`
	HasMore     bool   `json:"has_more"`
}

type userToolChangesTestData struct {
	Changes []model.UserToolChange `json:"changes"`
	Items   []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Key      string `json:"key"`
		Revision int64  `json:"revision"`
	} `json:"items"`
	NextCursor int64 `json:"next_cursor"`
	HasMore    bool  `json:"has_more"`
}

func setupUserToolControllerTest(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.Token{},
		&model.UserToolItem{},
		&model.UserToolAsset{},
		&model.UserToolItemAsset{},
		&model.UserToolPreference{},
		&model.UserToolChange{},
		&model.UserToolMutationReceipt{},
		&model.UserToolRuntimeSession{},
	))
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())
}

func TestCreateUserToolRuntimeSessionReturnsScopedCredentialAndTokenMetadata(t *testing.T) {
	setupUserToolControllerTest(t)

	const rawKey = "backend-only-user-tool-key-1234567890"
	token := &model.Token{
		UserId:         101,
		Key:            rawKey,
		Name:           "test2",
		Status:         common.TokenStatusEnabled,
		CreatedTime:    common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          "A组",
	}
	require.NoError(t, model.DB.Create(token).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolImagePlayground}}
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user-tools/image-playground/runtime-session",
		bytes.NewBufferString(fmt.Sprintf(`{"token_id":%d}`, token.Id)),
	)
	CreateUserToolRuntimeSession(context)

	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	assert.NotContains(t, recorder.Body.String(), rawKey)

	var data struct {
		Credential string `json:"credential"`
		ExpiresAt  int64  `json:"expires_at"`
		Token      struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			MaskedKey    string `json:"masked_key"`
			Group        string `json:"group"`
			DisplayLabel string `json:"display_label"`
		} `json:"token"`
	}
	require.NoError(t, common.Unmarshal(envelope.Data, &data))
	assert.True(t, strings.HasPrefix(data.Credential, "utrs_"))
	assert.NotEqual(t, rawKey, data.Credential)
	assert.Greater(t, data.ExpiresAt, time.Now().UnixMilli())
	assert.Equal(t, token.Id, data.Token.ID)
	assert.Equal(t, "test2", data.Token.Name)
	assert.Equal(t, model.MaskTokenKey(rawKey), data.Token.MaskedKey)
	assert.Equal(t, "A组", data.Token.Group)
	assert.Equal(t, "test2 · A组", data.Token.DisplayLabel)

	resolvedSession, resolvedToken, err := model.ResolveUserToolRuntimeToken(data.Credential)
	require.NoError(t, err)
	assert.Equal(t, model.UserToolImagePlayground, resolvedSession.Tool)
	assert.Equal(t, token.Id, resolvedToken.Id)
	assert.Equal(t, "A组", resolvedToken.Group)

	var storedSession model.UserToolRuntimeSession
	require.NoError(t, model.DB.First(&storedSession).Error)
	assert.NotEqual(t, data.Credential, storedSession.ID)
	assert.NotContains(t, fmt.Sprintf("%+v", storedSession), rawKey)
}

func TestCreateUserToolRuntimeSessionRejectsAnotherUsersToken(t *testing.T) {
	setupUserToolControllerTest(t)

	otherToken := &model.Token{
		UserId:         202,
		Key:            "other-user-tool-key-1234567890",
		Name:           "other",
		Status:         common.TokenStatusEnabled,
		CreatedTime:    common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	require.NoError(t, model.DB.Create(otherToken).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user-tools/infinite-canvas/runtime-session",
		bytes.NewBufferString(fmt.Sprintf(`{"token_id":%d}`, otherToken.Id)),
	)
	CreateUserToolRuntimeSession(context)

	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserToolRuntimeSession{}).Count(&sessionCount).Error)
	assert.Zero(t, sessionCount)
}

func TestUserToolSyncBootstrapAndChangesContract(t *testing.T) {
	setupUserToolControllerTest(t)

	syncRecorder := httptest.NewRecorder()
	syncContext, _ := gin.CreateTestContext(syncRecorder)
	syncContext.Set("id", 101)
	syncContext.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	syncContext.Request = httptest.NewRequest(http.MethodPost, "/api/user-tools/infinite-canvas/sync", bytes.NewBufferString(`{
		"mutations":[{"client_mutation_id":"create-canvas-1","kind":"project","key":"canvas-1","schema_version":1,"base_revision":0,"status":"ready","payload":{"name":"Canvas 1"},"asset_ids":[]}]
	}`))
	SyncUserTool(syncContext)

	var syncEnvelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(syncRecorder.Body.Bytes(), &syncEnvelope))
	require.True(t, syncEnvelope.Success, syncEnvelope.Message)
	var syncData struct {
		Results []struct {
			ClientMutationID string `json:"client_mutation_id"`
			Kind             string `json:"kind"`
			Key              string `json:"key"`
			Result           string `json:"result"`
			Item             struct {
				Revision int64 `json:"revision"`
			} `json:"item"`
		} `json:"results"`
		Cursor int64 `json:"cursor"`
	}
	require.NoError(t, common.Unmarshal(syncEnvelope.Data, &syncData))
	require.Len(t, syncData.Results, 1)
	assert.Equal(t, "create-canvas-1", syncData.Results[0].ClientMutationID)
	assert.Equal(t, "project", syncData.Results[0].Kind)
	assert.Equal(t, "canvas-1", syncData.Results[0].Key)
	assert.Equal(t, model.UserToolMutationResultApplied, syncData.Results[0].Result)
	assert.EqualValues(t, 1, syncData.Results[0].Item.Revision)
	assert.Positive(t, syncData.Cursor)

	bootstrapRecorder := httptest.NewRecorder()
	bootstrapContext, _ := gin.CreateTestContext(bootstrapRecorder)
	bootstrapContext.Set("id", 101)
	bootstrapContext.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	bootstrapContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/infinite-canvas/bootstrap", nil)
	GetUserToolBootstrap(bootstrapContext)

	var bootstrapEnvelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(bootstrapRecorder.Body.Bytes(), &bootstrapEnvelope))
	require.True(t, bootstrapEnvelope.Success, bootstrapEnvelope.Message)
	var bootstrapData struct {
		Items []struct {
			Key      string `json:"key"`
			Revision int64  `json:"revision"`
			Payload  struct {
				Name string `json:"name"`
			} `json:"payload"`
		} `json:"items"`
		Cursor int64 `json:"cursor"`
	}
	require.NoError(t, common.Unmarshal(bootstrapEnvelope.Data, &bootstrapData))
	require.Len(t, bootstrapData.Items, 1)
	assert.Equal(t, "canvas-1", bootstrapData.Items[0].Key)
	assert.Equal(t, "Canvas 1", bootstrapData.Items[0].Payload.Name)
	assert.Equal(t, syncData.Cursor, bootstrapData.Cursor)

	changesRecorder := httptest.NewRecorder()
	changesContext, _ := gin.CreateTestContext(changesRecorder)
	changesContext.Set("id", 101)
	changesContext.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	changesContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/infinite-canvas/changes?cursor=0&limit=10", nil)
	GetUserToolChanges(changesContext)

	var changesEnvelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(changesRecorder.Body.Bytes(), &changesEnvelope))
	require.True(t, changesEnvelope.Success, changesEnvelope.Message)
	var changesData struct {
		Changes []model.UserToolChange `json:"changes"`
		Items   []struct {
			Key string `json:"key"`
		} `json:"items"`
		NextCursor int64 `json:"next_cursor"`
		HasMore    bool  `json:"has_more"`
	}
	require.NoError(t, common.Unmarshal(changesEnvelope.Data, &changesData))
	require.Len(t, changesData.Changes, 1)
	require.Len(t, changesData.Items, 1)
	assert.Equal(t, "canvas-1", changesData.Items[0].Key)
	assert.Equal(t, syncData.Cursor, changesData.NextCursor)
	assert.False(t, changesData.HasMore)
}

func TestUserToolAssetContentSupportsOwnerETagAndRange(t *testing.T) {
	setupUserToolControllerTest(t)
	asset, err := model.StoreUserToolAsset(101, "sample.txt", "text/plain", "", bytes.NewBufferString("abcdef"))
	require.NoError(t, err)

	otherRecorder := httptest.NewRecorder()
	otherContext, _ := gin.CreateTestContext(otherRecorder)
	otherContext.Set("id", 202)
	otherContext.Params = gin.Params{{Key: "id", Value: asset.ID}}
	otherContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+asset.ID+"/content", nil)
	GetUserToolAssetContent(otherContext)
	assert.Equal(t, http.StatusNotFound, otherRecorder.Code)

	rangeRecorder := httptest.NewRecorder()
	rangeContext, _ := gin.CreateTestContext(rangeRecorder)
	rangeContext.Set("id", 101)
	rangeContext.Params = gin.Params{{Key: "id", Value: asset.ID}}
	rangeContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+asset.ID+"/content", nil)
	rangeContext.Request.Header.Set("Range", "bytes=1-3")
	GetUserToolAssetContent(rangeContext)
	assert.Equal(t, http.StatusPartialContent, rangeRecorder.Code)
	assert.Equal(t, "bcd", rangeRecorder.Body.String())
	assert.Equal(t, `"`+asset.Sha256+`"`, rangeRecorder.Header().Get("ETag"))
	assert.Equal(t, "nosniff", rangeRecorder.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "private, max-age=0, must-revalidate", rangeRecorder.Header().Get("Cache-Control"))
	assert.Equal(t, `attachment; filename=sample.txt`, rangeRecorder.Header().Get("Content-Disposition"))

	notModifiedRecorder := httptest.NewRecorder()
	notModifiedContext, _ := gin.CreateTestContext(notModifiedRecorder)
	notModifiedContext.Set("id", 101)
	notModifiedContext.Params = gin.Params{{Key: "id", Value: asset.ID}}
	notModifiedContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+asset.ID+"/content", nil)
	notModifiedContext.Request.Header.Set("If-None-Match", `"`+asset.Sha256+`"`)
	GetUserToolAssetContent(notModifiedContext)
	assert.Equal(t, http.StatusNotModified, notModifiedRecorder.Code)
	assert.Empty(t, notModifiedRecorder.Body.String())

	svgAsset, err := model.StoreUserToolAsset(101, "vector.svg", "image/svg+xml", "", bytes.NewBufferString("<svg>"))
	require.NoError(t, err)
	svgRecorder := httptest.NewRecorder()
	svgContext, _ := gin.CreateTestContext(svgRecorder)
	svgContext.Set("id", 101)
	svgContext.Params = gin.Params{{Key: "id", Value: svgAsset.ID}}
	svgContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+svgAsset.ID+"/content", nil)
	GetUserToolAssetContent(svgContext)
	assert.Equal(t, http.StatusOK, svgRecorder.Code)
	assert.Equal(t, "attachment; filename=vector.svg", svgRecorder.Header().Get("Content-Disposition"))

	pngAsset, err := model.StoreUserToolAsset(101, "preview.png", "image/png", "", bytes.NewBufferString("png-bytes"))
	require.NoError(t, err)
	pngRecorder := httptest.NewRecorder()
	pngContext, _ := gin.CreateTestContext(pngRecorder)
	pngContext.Set("id", 101)
	pngContext.Params = gin.Params{{Key: "id", Value: pngAsset.ID}}
	pngContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+pngAsset.ID+"/content", nil)
	GetUserToolAssetContent(pngContext)
	assert.Equal(t, http.StatusOK, pngRecorder.Code)
	assert.Equal(t, "inline; filename=preview.png", pngRecorder.Header().Get("Content-Disposition"))
	assert.Equal(t, "image/png", pngRecorder.Header().Get("Content-Type"))
}

func TestUserToolSyncReturnsPerMutationResultsAndContinuesAfterError(t *testing.T) {
	setupUserToolControllerTest(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user-tools/infinite-canvas/sync", bytes.NewBufferString(`{
		"mutations":[
			{"client_mutation_id":"create-first","kind":"project","key":"first","payload":{"name":"First"}},
			{"client_mutation_id":"","kind":"project","key":"invalid","payload":{"name":"Invalid"}},
			{"client_mutation_id":"create-third","kind":"project","key":"third","payload":{"name":"Third"}}
		]
	}`))
	SyncUserTool(context)

	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	var data struct {
		Results []struct {
			ClientMutationID string `json:"client_mutation_id"`
			Result           string `json:"result"`
			Message          string `json:"message"`
			Item             *struct {
				Revision int64 `json:"revision"`
			} `json:"item"`
		} `json:"results"`
	}
	require.NoError(t, common.Unmarshal(envelope.Data, &data))
	require.Len(t, data.Results, 3)
	assert.Equal(t, model.UserToolMutationResultApplied, data.Results[0].Result)
	assert.Equal(t, "error", data.Results[1].Result)
	assert.Equal(t, "invalid client mutation id", data.Results[1].Message)
	assert.Nil(t, data.Results[1].Item)
	assert.Equal(t, model.UserToolMutationResultApplied, data.Results[2].Result)

	items, err := model.ListUserToolItems(101, model.UserToolInfiniteCanvas, false)
	require.NoError(t, err)
	assert.Len(t, items, 2)
}

func TestUserToolSyncRejectsSensitivePayloadWithoutPersistence(t *testing.T) {
	setupUserToolControllerTest(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user-tools/infinite-canvas/sync", bytes.NewBufferString(`{
		"mutations":[{
			"client_mutation_id":"malicious-client",
			"kind":"project",
			"key":"malicious-item",
			"payload":{"nested":{"apiKey":"sk-proj-abcdefghijk1234567890"}}
		}]
	}`))
	SyncUserTool(context)

	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	var data struct {
		Results []struct {
			Result  string `json:"result"`
			Message string `json:"message"`
		} `json:"results"`
	}
	require.NoError(t, common.Unmarshal(envelope.Data, &data))
	require.Len(t, data.Results, 1)
	assert.Equal(t, "error", data.Results[0].Result)
	assert.Equal(t, model.ErrUserToolSensitivePayload.Error(), data.Results[0].Message)

	var itemCount int64
	require.NoError(t, model.DB.Model(&model.UserToolItem{}).Count(&itemCount).Error)
	assert.Zero(t, itemCount)

	var receiptCount int64
	require.NoError(t, model.DB.Model(&model.UserToolMutationReceipt{}).Count(&receiptCount).Error)
	assert.Zero(t, receiptCount)
}

func TestUserToolSyncRejectsOversizedBody(t *testing.T) {
	setupUserToolControllerTest(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user-tools/infinite-canvas/sync",
		bytes.NewReader(make([]byte, maxUserToolSyncRequestBytes+1)),
	)
	SyncUserTool(context)
	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestUserToolBootstrapPaginatesWithStableSnapshotCursor(t *testing.T) {
	setupUserToolControllerTest(t)

	for index := 1; index <= 4; index++ {
		seedUserToolItemAndChange(t, model.UserToolItem{
			ID:            fmt.Sprintf("uti_%03d", index),
			UserID:        101,
			Tool:          model.UserToolInfiniteCanvas,
			Kind:          "project",
			ItemKey:       fmt.Sprintf("canvas-%03d", index),
			SchemaVersion: 1,
			Revision:      1,
			Payload:       model.JSONValue(fmt.Sprintf(`{"name":"Canvas %d"}`, index)),
			CreatedTime:   int64(index),
			UpdatedTime:   int64(index),
		})
	}

	firstEnvelope, firstPage := getUserToolBootstrapForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/bootstrap?limit=2",
	)
	require.Len(t, firstPage.Items, 2)
	assert.Equal(t, []string{"uti_001", "uti_002"}, []string{firstPage.Items[0].ID, firstPage.Items[1].ID})
	assert.Equal(t, "uti_002", firstPage.NextAfterID)
	assert.True(t, firstPage.HasMore)
	assert.Positive(t, firstPage.Cursor)

	updated, err := model.ApplyUserToolMutation(101, model.UserToolInfiniteCanvas, model.UserToolMutation{
		ClientMutationID: "update-during-bootstrap",
		Kind:             "project",
		ItemKey:          "canvas-004",
		SchemaVersion:    1,
		BaseRevision:     1,
		Payload:          model.JSONValue(`{"name":"Canvas 4 updated"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Item)
	assert.EqualValues(t, 2, updated.Item.Revision)

	secondTarget := fmt.Sprintf(
		"/api/user-tools/infinite-canvas/bootstrap?limit=2&after_id=%s&snapshot_cursor=%d",
		firstPage.NextAfterID,
		firstPage.Cursor,
	)
	secondEnvelope, secondPage := getUserToolBootstrapForTest(t, 101, model.UserToolInfiniteCanvas, secondTarget)
	require.Len(t, secondPage.Items, 2)
	assert.Equal(t, []string{"uti_003", "uti_004"}, []string{secondPage.Items[0].ID, secondPage.Items[1].ID})
	assert.Equal(t, "uti_004", secondPage.NextAfterID)
	assert.False(t, secondPage.HasMore)
	assert.Equal(t, firstPage.Cursor, secondPage.Cursor)
	assert.Less(t, len(firstEnvelope.Data), maxUserToolBootstrapPayloadBytes)
	assert.Less(t, len(secondEnvelope.Data), maxUserToolBootstrapPayloadBytes)

	_, changes := getUserToolChangesForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		fmt.Sprintf("/api/user-tools/infinite-canvas/changes?cursor=%d&limit=10", firstPage.Cursor),
	)
	require.Len(t, changes.Changes, 1)
	require.Len(t, changes.Items, 1)
	assert.Equal(t, "canvas-004", changes.Items[0].Key)
	assert.EqualValues(t, 2, changes.Items[0].Revision)
	assert.Greater(t, changes.NextCursor, firstPage.Cursor)
	assert.False(t, changes.HasMore)
}

func TestUserToolBootstrapSplitsPagesAtFourMegabytePayloadBudget(t *testing.T) {
	setupUserToolControllerTest(t)

	largePayload := model.JSONValue(`{"blob":"` + strings.Repeat("x", 2*1024*1024) + `"}`)
	for index := 1; index <= 2; index++ {
		seedUserToolItemAndChange(t, model.UserToolItem{
			ID:            fmt.Sprintf("uti_large_%d", index),
			UserID:        101,
			Tool:          model.UserToolInfiniteCanvas,
			Kind:          "blob",
			ItemKey:       fmt.Sprintf("large-%d", index),
			SchemaVersion: 1,
			Revision:      1,
			Payload:       largePayload,
			CreatedTime:   int64(index),
			UpdatedTime:   int64(index),
		})
	}

	firstEnvelope, firstPage := getUserToolBootstrapForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/bootstrap?limit=200",
	)
	require.Len(t, firstPage.Items, 1)
	assert.Equal(t, "uti_large_1", firstPage.Items[0].ID)
	assert.True(t, firstPage.HasMore)
	assert.Equal(t, "uti_large_1", firstPage.NextAfterID)
	assert.Less(t, len(firstEnvelope.Data), maxUserToolBootstrapPayloadBytes)

	secondTarget := fmt.Sprintf(
		"/api/user-tools/infinite-canvas/bootstrap?limit=200&after_id=%s&snapshot_cursor=%d",
		firstPage.NextAfterID,
		firstPage.Cursor,
	)
	secondEnvelope, secondPage := getUserToolBootstrapForTest(t, 101, model.UserToolInfiniteCanvas, secondTarget)
	require.Len(t, secondPage.Items, 1)
	assert.Equal(t, "uti_large_2", secondPage.Items[0].ID)
	assert.False(t, secondPage.HasMore)
	assert.Equal(t, firstPage.Cursor, secondPage.Cursor)
	assert.Less(t, len(secondEnvelope.Data), maxUserToolBootstrapPayloadBytes)
}

func TestUserToolChangesLimit1000PreservesHasMoreBoundary(t *testing.T) {
	setupUserToolControllerTest(t)

	item := &model.UserToolItem{
		ID: "uti_changes_boundary", UserID: 101, Tool: model.UserToolInfiniteCanvas,
		Kind: "project", ItemKey: "boundary", SchemaVersion: 1, Revision: 1001,
		Payload: model.JSONValue(`{"name":"Boundary"}`), CreatedTime: 1, UpdatedTime: 1001,
	}
	require.NoError(t, model.DB.Create(item).Error)

	changes := make([]model.UserToolChange, 1001)
	for index := range changes {
		changes[index] = model.UserToolChange{
			UserID: 101, Tool: model.UserToolInfiniteCanvas, ItemID: item.ID,
			Operation: "upsert", Revision: int64(index + 1), CreatedAt: int64(index + 1),
		}
	}
	require.NoError(t, model.DB.CreateInBatches(&changes, 100).Error)

	_, firstPage := getUserToolChangesForTest(t, 101, model.UserToolInfiniteCanvas, "/api/user-tools/infinite-canvas/changes?cursor=0&limit=1000")
	require.Len(t, firstPage.Changes, 1000)
	assert.Len(t, firstPage.Items, 1)
	assert.True(t, firstPage.HasMore)
	assert.EqualValues(t, 1000, firstPage.NextCursor)

	_, secondPage := getUserToolChangesForTest(t, 101, model.UserToolInfiniteCanvas, fmt.Sprintf("/api/user-tools/infinite-canvas/changes?cursor=%d&limit=1000", firstPage.NextCursor))
	require.Len(t, secondPage.Changes, 1)
	assert.Len(t, secondPage.Items, 1)
	assert.False(t, secondPage.HasMore)
	assert.EqualValues(t, 1001, secondPage.NextCursor)
}

func TestUserToolChangesPaginationAndScopeIsolation(t *testing.T) {
	setupUserToolControllerTest(t)

	applyUserToolMutationForControllerTest(t, 101, model.UserToolInfiniteCanvas, "owner-canvas-1", "setting", "owner-canvas-1", `{"scope":"owner-canvas-1"}`)
	applyUserToolMutationForControllerTest(t, 202, model.UserToolInfiniteCanvas, "other-canvas-1", "setting", "other-canvas-1", `{"scope":"other-canvas-1"}`)
	applyUserToolMutationForControllerTest(t, 101, model.UserToolImagePlayground, "owner-image-1", "setting", "owner-image-1", `{"scope":"owner-image-1"}`)
	applyUserToolMutationForControllerTest(t, 101, model.UserToolInfiniteCanvas, "owner-canvas-2", "plugin-record", "owner-canvas-2", `{"scope":"owner-canvas-2"}`)
	applyUserToolMutationForControllerTest(t, 202, model.UserToolInfiniteCanvas, "other-canvas-2", "plugin-record", "other-canvas-2", `{"scope":"other-canvas-2"}`)
	applyUserToolMutationForControllerTest(t, 101, model.UserToolInfiniteCanvas, "owner-canvas-3", "project", "owner-canvas-3", `{"scope":"owner-canvas-3"}`)

	_, firstPage := getUserToolChangesForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/changes?cursor=0&limit=2",
	)
	require.Len(t, firstPage.Changes, 2)
	assert.True(t, firstPage.HasMore)
	assert.Positive(t, firstPage.NextCursor)
	assert.ElementsMatch(t, []string{"owner-canvas-1", "owner-canvas-2"}, userToolChangeItemKeys(firstPage.Items))
	for _, change := range firstPage.Changes {
		assert.Equal(t, 101, change.UserID)
		assert.Equal(t, model.UserToolInfiniteCanvas, change.Tool)
	}

	_, secondPage := getUserToolChangesForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		fmt.Sprintf("/api/user-tools/infinite-canvas/changes?cursor=%d&limit=2", firstPage.NextCursor),
	)
	require.Len(t, secondPage.Changes, 1)
	assert.False(t, secondPage.HasMore)
	assert.Greater(t, secondPage.NextCursor, firstPage.NextCursor)
	assert.Equal(t, []string{"owner-canvas-3"}, userToolChangeItemKeys(secondPage.Items))

	_, ownerBootstrap := getUserToolBootstrapForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/bootstrap?limit=20",
	)
	assert.ElementsMatch(t, []string{"owner-canvas-1", "owner-canvas-2", "owner-canvas-3"}, userToolBootstrapItemKeys(ownerBootstrap.Items))

	_, otherUserChanges := getUserToolChangesForTest(
		t,
		202,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/changes?cursor=0&limit=20",
	)
	assert.ElementsMatch(t, []string{"other-canvas-1", "other-canvas-2"}, userToolChangeItemKeys(otherUserChanges.Items))

	_, otherToolChanges := getUserToolChangesForTest(
		t,
		101,
		model.UserToolImagePlayground,
		"/api/user-tools/image-playground/changes?cursor=0&limit=20",
	)
	assert.Equal(t, []string{"owner-image-1"}, userToolChangeItemKeys(otherToolChanges.Items))
}

func TestUserToolSyncRoundTripsSettingAndPluginRecordKinds(t *testing.T) {
	setupUserToolControllerTest(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", 101)
	context.Params = gin.Params{{Key: "tool", Value: model.UserToolInfiniteCanvas}}
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user-tools/infinite-canvas/sync", bytes.NewBufferString(`{
		"mutations":[
			{"client_mutation_id":"create-setting","kind":"setting","key":"shared-key","payload":{"theme":"dark"}},
			{"client_mutation_id":"create-plugin-record","kind":"plugin-record","key":"shared-key","payload":{"enabled":true}}
		]
	}`))
	SyncUserTool(context)

	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	var syncData struct {
		Results []struct {
			Kind   string `json:"kind"`
			Key    string `json:"key"`
			Result string `json:"result"`
		} `json:"results"`
	}
	require.NoError(t, common.Unmarshal(envelope.Data, &syncData))
	require.Len(t, syncData.Results, 2)
	assert.Equal(t, "setting", syncData.Results[0].Kind)
	assert.Equal(t, "shared-key", syncData.Results[0].Key)
	assert.Equal(t, model.UserToolMutationResultApplied, syncData.Results[0].Result)
	assert.Equal(t, "plugin-record", syncData.Results[1].Kind)
	assert.Equal(t, "shared-key", syncData.Results[1].Key)
	assert.Equal(t, model.UserToolMutationResultApplied, syncData.Results[1].Result)

	_, bootstrap := getUserToolBootstrapForTest(
		t,
		101,
		model.UserToolInfiniteCanvas,
		"/api/user-tools/infinite-canvas/bootstrap?limit=20",
	)
	require.Len(t, bootstrap.Items, 2)
	assert.ElementsMatch(t, []string{"setting", "plugin-record"}, []string{bootstrap.Items[0].Kind, bootstrap.Items[1].Kind})
	assert.Equal(t, "shared-key", bootstrap.Items[0].Key)
	assert.Equal(t, "shared-key", bootstrap.Items[1].Key)
}

func seedUserToolItemAndChange(t *testing.T, item model.UserToolItem) {
	t.Helper()
	require.NoError(t, model.DB.Create(&item).Error)
	require.NoError(t, model.DB.Create(&model.UserToolChange{
		UserID:    item.UserID,
		Tool:      item.Tool,
		ItemID:    item.ID,
		Operation: "upsert",
		Revision:  item.Revision,
		CreatedAt: item.UpdatedTime,
	}).Error)
}

func applyUserToolMutationForControllerTest(t *testing.T, userID int, tool, mutationID, kind, key, payload string) {
	t.Helper()
	result, err := model.ApplyUserToolMutation(userID, tool, model.UserToolMutation{
		ClientMutationID: mutationID,
		Kind:             kind,
		ItemKey:          key,
		Payload:          model.JSONValue(payload),
	})
	require.NoError(t, err)
	require.NotNil(t, result.Item)
	assert.Equal(t, model.UserToolMutationResultApplied, result.Result)
}

func getUserToolBootstrapForTest(t *testing.T, userID int, tool, target string) (userToolAPIResponse, userToolBootstrapTestData) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", userID)
	context.Params = gin.Params{{Key: "tool", Value: tool}}
	context.Request = httptest.NewRequest(http.MethodGet, target, nil)
	GetUserToolBootstrap(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	var data userToolBootstrapTestData
	require.NoError(t, common.Unmarshal(envelope.Data, &data))
	return envelope, data
}

func getUserToolChangesForTest(t *testing.T, userID int, tool, target string) (userToolAPIResponse, userToolChangesTestData) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("id", userID)
	context.Params = gin.Params{{Key: "tool", Value: tool}}
	context.Request = httptest.NewRequest(http.MethodGet, target, nil)
	GetUserToolChanges(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope userToolAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, envelope.Message)
	var data userToolChangesTestData
	require.NoError(t, common.Unmarshal(envelope.Data, &data))
	return envelope, data
}

func userToolBootstrapItemKeys(items []struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Key      string          `json:"key"`
	Revision int64           `json:"revision"`
	Payload  model.JSONValue `json:"payload"`
}) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

func userToolChangeItemKeys(items []struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
}) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}
