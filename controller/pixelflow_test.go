package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPixelFlowTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/api/pixelflow/session-token-sync", PixelFlowSessionTokenSync)
	router.GET("/api/pixelflow/session-token-sync/data", func(c *gin.Context) {
		c.Set("id", 7)
		PixelFlowSessionTokenSyncData(c)
	})
	return router
}

func seedPixelFlowUser(t *testing.T, dbUserID int, username string) {
	t.Helper()

	user := &model.User{
		Id:          dbUserID,
		Username:    username,
		Password:    "unused",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, model.DB.Create(user).Error)
}

func performPixelFlowRequest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestPixelFlowSessionTokenSyncServesAuthBridgeWithoutEmbeddingCredentials(t *testing.T) {
	router := newPixelFlowTestRouter()

	recorder := performPixelFlowRequest(router, "/api/pixelflow/session-token-sync?origin=http%3A%2F%2Flocalhost%3A3030")

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "fetch('/api/user/auth/refresh'")
	assert.Contains(t, body, "/api/pixelflow/session-token-sync/data?origin=")
	assert.Contains(t, body, "Authorization: 'Bearer ' + auth.access_token")
	assert.Contains(t, body, "window.opener.postMessage(message, targetOrigin)")
	assert.NotContains(t, body, `"tokens"`)
}

func TestPixelFlowSessionTokenSyncRejectsUntrustedOrigin(t *testing.T) {
	router := newPixelFlowTestRouter()

	for _, path := range []string{
		"/api/pixelflow/session-token-sync?origin=https%3A%2F%2Fevil.example.com",
		"/api/pixelflow/session-token-sync/data?origin=https%3A%2F%2Fevil.example.com",
	} {
		recorder := performPixelFlowRequest(router, path)
		assert.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "不允许同步到该站点")
	}
}

func TestPixelFlowSessionTokenSyncDataReturnsOnlyCurrentUserTokens(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	seedPixelFlowUser(t, 7, "pixel-user")
	seedToken(t, db, 7, "绘画密钥", "raw-token-key")
	seedToken(t, db, 8, "其他用户密钥", "other-token-key")
	router := newPixelFlowTestRouter()

	recorder := performPixelFlowRequest(router, "/api/pixelflow/session-token-sync/data?origin=http%3A%2F%2Flocalhost%3A3030")

	require.Equal(t, http.StatusOK, recorder.Code)
	var message pixelFlowSessionSyncMessage
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &message))
	assert.Equal(t, "pixelflow:newapi-token-sync", message.Type)
	assert.Equal(t, "http://localhost:3030", message.Origin)
	assert.Equal(t, 7, message.Payload.Binding.UserID)
	assert.Equal(t, "pixel-user", message.Payload.Binding.Username)
	require.Len(t, message.Payload.Tokens, 1)
	assert.Equal(t, "raw-token-key", message.Payload.Tokens[0].Key)
	assert.False(t, strings.Contains(recorder.Body.String(), "other-token-key"))
	assert.False(t, strings.Contains(recorder.Body.String(), "其他用户密钥"))
}
