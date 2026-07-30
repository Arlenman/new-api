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

func TestInfiniteCanvasHandlerServesOnlyBuiltToolAssetsWithExpectedCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dist := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dist, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>canvas index</html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "sw.js"), []byte("service worker"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "assets", "index-CtXnX3Xp.js"), []byte("hashed asset"), 0o644))
	buildInfo, err := common.Marshal(map[string]string{
		"version":  "0.9.0",
		"commit":   "bdca6b0a5c193b8c85dfbf7c6a433d62f02da9df",
		"built_at": "2026-07-18T00:00:00.000Z",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "build-info.json"), buildInfo, 0o644))

	tool, err := loadInfiniteCanvas(dist)
	require.NoError(t, err)
	assert.Equal(t, "0.9.0", tool.buildInfo.Version)
	assert.Equal(t, "bdca6b0a5c193b8c85dfbf7c6a433d62f02da9df", tool.buildInfo.Commit)

	engine := gin.New()
	engine.GET(infiniteCanvasRoute+"/*filepath", tool.serve)

	index := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/")
	assert.Equal(t, http.StatusOK, index.Code)
	assert.Equal(t, "no-cache", index.Header().Get("Cache-Control"))
	assert.Contains(t, index.Body.String(), "canvas index")
	assert.Equal(t, "frame-ancestors 'self'", index.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "same-origin", index.Header().Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", index.Header().Get("X-Content-Type-Options"))

	asset := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/assets/index-CtXnX3Xp.js")
	assert.Equal(t, http.StatusOK, asset.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", asset.Header().Get("Cache-Control"))
	assert.Equal(t, "hashed asset", asset.Body.String())

	serviceWorker := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/sw.js")
	assert.Equal(t, http.StatusOK, serviceWorker.Code)
	assert.Equal(t, "no-cache", serviceWorker.Header().Get("Cache-Control"))
	assert.Equal(t, infiniteCanvasRoute+"/", serviceWorker.Header().Get("Service-Worker-Allowed"))

	traversal := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/../build-info.json")
	assert.Equal(t, http.StatusNotFound, traversal.Code)
	for _, pagePath := range []string{
		"/image",
		"/video",
		"/assets",
		"/prompts",
		"/canvas",
		"/canvas/project-123",
		"/config",
	} {
		page := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+pagePath)
		assert.Equal(t, http.StatusOK, page.Code, pagePath)
		assert.Equal(t, "text/html; charset=utf-8", page.Header().Get("Content-Type"), pagePath)
		assert.Equal(t, "no-cache", page.Header().Get("Cache-Control"), pagePath)
		assert.Contains(t, page.Body.String(), "canvas index", pagePath)
	}

	for _, missingPath := range []string{
		"/not-found",
		"/assets/not-found.js",
		"/canvas/project-123/extra",
	} {
		missing := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+missingPath)
		assert.Equal(t, http.StatusNotFound, missing.Code, missingPath)
		assert.NotContains(t, missing.Body.String(), "canvas index", missingPath)
	}
}

func TestInfiniteCanvasAccessTokenAuthIgnoresForgedDashboardHeaderAndAuthFailuresAreNotCached(t *testing.T) {
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
	common.SessionSecret = "infinite-canvas-access-token-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.SetMainDatabaseType(previousDatabaseType)
		_ = sqlDB.Close()
	})

	user := &model.User{
		Id: 123, Username: "infinite-canvas-owner", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "infinite-canvas-owner-aff",
	}
	require.NoError(t, db.Create(user).Error)
	forgedUser := &model.User{
		Id: 456, Username: "infinite-canvas-forged", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "infinite-canvas-forged-aff",
	}
	require.NoError(t, db.Create(forgedUser).Error)
	bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "infinite-canvas-router-test")
	require.NoError(t, err)

	dist := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>canvas index</html>"), 0o644))
	buildInfo, err := common.Marshal(map[string]string{
		"version": "0.9.0",
		"commit":  "bdca6b0a5c193b8c85dfbf7c6a433d62f02da9df",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "build-info.json"), buildInfo, 0o644))
	tool, err := loadInfiniteCanvas(dist)
	require.NoError(t, err)

	engine := gin.New()
	engine.Use(middleware.Cache())
	engine.GET(
		infiniteCanvasRoute+"/*filepath",
		middleware.DisableCache(),
		middleware.UserToolAssetAuth(model.UserToolInfiniteCanvas),
		tool.serve,
	)

	unauthenticated := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", unauthenticated.Header().Get("Cache-Control"))

	authenticatedRequest := httptest.NewRequest(http.MethodGet, infiniteCanvasRoute+"/?new_api_user=456", nil)
	authenticatedRequest.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
	authenticatedRequest.Header.Set("X-Auth-Session", bundle.Session.SID)
	authenticatedRequest.Header.Set("New-Api-User", "456")
	authenticated := httptest.NewRecorder()
	engine.ServeHTTP(authenticated, authenticatedRequest)

	assert.Equal(t, http.StatusOK, authenticated.Code)
	assert.Contains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=123")
	assert.NotContains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=456")
	assert.Equal(t, "no-cache", authenticated.Header().Get("Cache-Control"))
	assert.Contains(t, authenticated.Body.String(), "canvas index")

	cookieRequest := httptest.NewRequest(http.MethodGet, infiniteCanvasRoute+"/?new_api_user=456", nil)
	cookieRequest.AddCookie(&http.Cookie{
		Name:  "new_api_user_tool_access",
		Value: bundle.AccessToken,
	})
	cookieRequest.Header.Set("New-Api-User", "456")
	cookieAuthenticated := httptest.NewRecorder()
	engine.ServeHTTP(cookieAuthenticated, cookieRequest)

	assert.Equal(t, http.StatusOK, cookieAuthenticated.Code)
	assert.Contains(t, cookieAuthenticated.Body.String(), "window.__NEW_API_USER_ID__=123")
	assert.NotContains(t, cookieAuthenticated.Body.String(), "window.__NEW_API_USER_ID__=456")
	assert.Equal(t, "no-cache", cookieAuthenticated.Header().Get("Cache-Control"))
	assert.Contains(t, cookieAuthenticated.Body.String(), "canvas index")

	opaqueCookieRequest := httptest.NewRequest(http.MethodGet, infiniteCanvasRoute+"/", nil)
	opaqueCookieRequest.AddCookie(&http.Cookie{
		Name:  service.UserToolAccessCookieName,
		Value: "opaque-dashboard-pat-or-relay-key",
	})
	opaqueCookieResponse := httptest.NewRecorder()
	engine.ServeHTTP(opaqueCookieResponse, opaqueCookieRequest)
	assert.Equal(t, http.StatusUnauthorized, opaqueCookieResponse.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", opaqueCookieResponse.Header().Get("Cache-Control"))
}

func TestLoadInfiniteCanvasRejectsIncompleteDistribution(t *testing.T) {
	dist := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("index"), 0o644))

	_, err := loadInfiniteCanvas(dist)
	require.ErrorContains(t, err, "build-info.json")
}

func performInfiniteCanvasRequest(engine http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	engine.ServeHTTP(recorder, request)
	return recorder
}
