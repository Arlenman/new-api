package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

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
	assert.Contains(t, rangeRecorder.Header().Get("Cache-Control"), "private")

	notModifiedRecorder := httptest.NewRecorder()
	notModifiedContext, _ := gin.CreateTestContext(notModifiedRecorder)
	notModifiedContext.Set("id", 101)
	notModifiedContext.Params = gin.Params{{Key: "id", Value: asset.ID}}
	notModifiedContext.Request = httptest.NewRequest(http.MethodGet, "/api/user-tools/assets/"+asset.ID+"/content", nil)
	notModifiedContext.Request.Header.Set("If-None-Match", `"`+asset.Sha256+`"`)
	GetUserToolAssetContent(notModifiedContext)
	assert.Equal(t, http.StatusNotModified, notModifiedRecorder.Code)
	assert.Empty(t, notModifiedRecorder.Body.String())
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
