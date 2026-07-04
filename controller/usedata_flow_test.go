package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

type tokenTagQuotaResponse struct {
	Success bool                      `json:"success"`
	Message string                    `json:"message"`
	Data    []model.TokenTagQuotaData `json:"data"`
}

type tokenTagOptionsResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    []model.TokenTag `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenTag{}, &model.TokenTagBinding{}, &model.QuotaData{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123", AffCode: "alice-aff"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "bob", Password: "password123", AffCode: "bob-aff"}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    1,
		Username:  "alice",
		TokenId:   11,
		TokenName: "primary",
		Type:      model.LogTypeConsume,
		CreatedAt: 1199,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    2,
		Username:  "bob",
		TokenId:   22,
		TokenName: "backup",
		Type:      model.LogTypeConsume,
		CreatedAt: 1299,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

func TestGetUserTokenTagQuotaDatesAllowsRangesLongerThanOneMonth(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.ReplaceTokenTags(1, 11, []string{"Client A"}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/token-tags/self?start_timestamp=1000&end_timestamp=5200000", nil)

	GetUserTokenTagQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload tokenTagQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "Client A", payload.Data[0].TagName)
	require.Equal(t, 40, payload.Data[0].TokenUsed)
	require.Equal(t, 100, payload.Data[0].Quota)
	require.EqualValues(t, 1199, payload.Data[0].LastUsedAt)
}

func TestGetAllTokenTagQuotaDatesFiltersByUsernameAndTag(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.ReplaceTokenTags(1, 11, []string{"Client A", "Shared"}))
	require.NoError(t, model.ReplaceTokenTags(2, 22, []string{"Client B"}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/token-tags?start_timestamp=1000&end_timestamp=2000&username=alice&token_tag=Client%20A", nil)

	GetAllTokenTagQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload tokenTagQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "Client A", payload.Data[0].TagName)
	require.EqualValues(t, 1199, payload.Data[0].LastUsedAt)
}

func TestGetUserTokenTagQuotaDatesFiltersByTag(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.ReplaceTokenTags(1, 11, []string{"Client A", "Shared"}))
	require.NoError(t, model.ReplaceTokenTags(2, 22, []string{"Client A"}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/token-tags/self?start_timestamp=1000&end_timestamp=2000&token_tag=Client%20A", nil)

	GetUserTokenTagQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload tokenTagQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "Client A", payload.Data[0].TagName)
	require.Empty(t, payload.Data[0].Username)
	require.EqualValues(t, 1199, payload.Data[0].LastUsedAt)
}

func TestGetTokenTagOptionsScopesByRoleAndUsername(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.ReplaceTokenTags(1, 11, []string{"Client A", "Shared"}))
	require.NoError(t, model.ReplaceTokenTags(2, 22, []string{"Client B"}))

	adminRecorder := httptest.NewRecorder()
	adminCtx, _ := gin.CreateTestContext(adminRecorder)
	adminCtx.Set("role", common.RoleAdminUser)
	adminCtx.Request = httptest.NewRequest(http.MethodGet, "/api/data/token-tags/options?username=alice", nil)

	GetTokenTagOptions(adminCtx)

	require.Equal(t, http.StatusOK, adminRecorder.Code)
	var adminPayload tokenTagOptionsResponse
	require.NoError(t, common.Unmarshal(adminRecorder.Body.Bytes(), &adminPayload))
	require.True(t, adminPayload.Success, adminPayload.Message)
	require.Len(t, adminPayload.Data, 2)
	for _, option := range adminPayload.Data {
		require.Equal(t, 1, option.UserID)
	}

	selfRecorder := httptest.NewRecorder()
	selfCtx, _ := gin.CreateTestContext(selfRecorder)
	selfCtx.Set("id", 2)
	selfCtx.Set("role", common.RoleCommonUser)
	selfCtx.Request = httptest.NewRequest(http.MethodGet, "/api/data/token-tags/options", nil)

	GetTokenTagOptions(selfCtx)

	require.Equal(t, http.StatusOK, selfRecorder.Code)
	var selfPayload tokenTagOptionsResponse
	require.NoError(t, common.Unmarshal(selfRecorder.Body.Bytes(), &selfPayload))
	require.True(t, selfPayload.Success, selfPayload.Message)
	require.Len(t, selfPayload.Data, 1)
	require.Equal(t, "Client B", selfPayload.Data[0].Name)
}
