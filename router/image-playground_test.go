package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImagePlaygroundHandlerServesOnlyBuiltToolAssetsWithExpectedCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dist := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dist, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>tool index</html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "sw.js"), []byte("service worker"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "assets", "index-CtXnX3Xp.js"), []byte("hashed asset"), 0o644))
	buildInfo, err := common.Marshal(map[string]string{
		"version":  "0.7.0",
		"commit":   "a10477581b3d43ac98d39777e4445625a9db113d",
		"built_at": "2026-07-17T00:00:00.000Z",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "build-info.json"), buildInfo, 0o644))

	tool, err := loadImagePlayground(dist)
	require.NoError(t, err)
	assert.Equal(t, "0.7.0", tool.buildInfo.Version)
	assert.Equal(t, "a10477581b3d43ac98d39777e4445625a9db113d", tool.buildInfo.Commit)

	engine := gin.New()
	engine.GET("/_tools/gpt-image-playground/*filepath", tool.serve)

	index := performImagePlaygroundRequest(engine, "/_tools/gpt-image-playground/")
	assert.Equal(t, http.StatusOK, index.Code)
	assert.Equal(t, "no-cache", index.Header().Get("Cache-Control"))
	assert.Contains(t, index.Body.String(), "tool index")
	assert.Equal(t, "frame-ancestors 'self'", index.Header().Get("Content-Security-Policy"))

	asset := performImagePlaygroundRequest(engine, "/_tools/gpt-image-playground/assets/index-CtXnX3Xp.js")
	assert.Equal(t, http.StatusOK, asset.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", asset.Header().Get("Cache-Control"))
	assert.Equal(t, "hashed asset", asset.Body.String())

	serviceWorker := performImagePlaygroundRequest(engine, "/_tools/gpt-image-playground/sw.js")
	assert.Equal(t, http.StatusOK, serviceWorker.Code)
	assert.Equal(t, "no-cache", serviceWorker.Header().Get("Cache-Control"))
	assert.Equal(t, "/_tools/gpt-image-playground/", serviceWorker.Header().Get("Service-Worker-Allowed"))

	missing := performImagePlaygroundRequest(engine, "/_tools/gpt-image-playground/not-found")
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.NotContains(t, missing.Body.String(), "tool index")
}

func TestImagePlaygroundAccessTokenAuthIgnoresForgedDashboardHeaderAndAuthFailuresAreNotCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	previousDatabaseType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "image-playground-access-token-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.SetMainDatabaseType(previousDatabaseType)
		_ = sqlDB.Close()
	})

	user := &model.User{
		Id: 123, Username: "image-playground-owner", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "image-playground-owner-aff",
	}
	require.NoError(t, db.Create(user).Error)
	forgedUser := &model.User{
		Id: 456, Username: "image-playground-forged", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "image-playground-forged-aff",
	}
	require.NoError(t, db.Create(forgedUser).Error)
	bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "image-playground-router-test")
	require.NoError(t, err)

	dist := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>tool index</html>"), 0o644))
	buildInfo, err := common.Marshal(map[string]string{
		"version": "0.7.0",
		"commit":  "a10477581b3d43ac98d39777e4445625a9db113d",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "build-info.json"), buildInfo, 0o644))
	tool, err := loadImagePlayground(dist)
	require.NoError(t, err)

	engine := gin.New()
	engine.Use(middleware.Cache())
	engine.GET(
		imagePlaygroundRoute+"/*filepath",
		middleware.DisableCache(),
		middleware.TokenOrUserAuth(),
		tool.serve,
	)

	unauthenticated := performImagePlaygroundRequest(engine, imagePlaygroundRoute+"/")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", unauthenticated.Header().Get("Cache-Control"))

	authenticatedRequest := httptest.NewRequest(http.MethodGet, imagePlaygroundRoute+"/?new_api_user=456", nil)
	authenticatedRequest.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
	authenticatedRequest.Header.Set("X-Auth-Session", bundle.Session.SID)
	authenticatedRequest.Header.Set("New-Api-User", "456")
	authenticated := httptest.NewRecorder()
	engine.ServeHTTP(authenticated, authenticatedRequest)

	assert.Equal(t, http.StatusOK, authenticated.Code)
	assert.Contains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=123")
	assert.NotContains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=456")
	assert.Equal(t, "no-cache", authenticated.Header().Get("Cache-Control"))
	assert.Contains(t, authenticated.Body.String(), "tool index")
}

func TestLoadImagePlaygroundRejectsIncompleteDistribution(t *testing.T) {
	dist := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("index"), 0o644))

	_, err := loadImagePlayground(dist)
	require.ErrorContains(t, err, "build-info.json")
}

func performImagePlaygroundRequest(engine http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	engine.ServeHTTP(recorder, request)
	return recorder
}
