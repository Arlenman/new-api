package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestInfiniteCanvasSessionAuthWorksWithoutDashboardHeaderAndAuthFailuresAreNotCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("infinite-canvas-test-secret"))))
	engine.Use(middleware.Cache())
	engine.GET("/login-fixture", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 123)
		session.Set("status", common.UserStatusEnabled)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.GET(
		infiniteCanvasRoute+"/*filepath",
		middleware.DisableCache(),
		middleware.TokenOrUserAuth(),
		tool.serve,
	)

	unauthenticated := performInfiniteCanvasRequest(engine, infiniteCanvasRoute+"/")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", unauthenticated.Header().Get("Cache-Control"))

	login := performInfiniteCanvasRequest(engine, "/login-fixture")
	require.Equal(t, http.StatusNoContent, login.Code)
	require.NotEmpty(t, login.Result().Cookies())

	authenticatedRequest := httptest.NewRequest(http.MethodGet, infiniteCanvasRoute+"/?new_api_user=456", nil)
	authenticatedRequest.AddCookie(login.Result().Cookies()[0])
	authenticated := httptest.NewRecorder()
	engine.ServeHTTP(authenticated, authenticatedRequest)

	assert.Equal(t, http.StatusOK, authenticated.Code)
	assert.Contains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=123")
	assert.NotContains(t, authenticated.Body.String(), "window.__NEW_API_USER_ID__=456")
	assert.Equal(t, "no-cache", authenticated.Header().Get("Cache-Control"))
	assert.Contains(t, authenticated.Body.String(), "canvas index")
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
