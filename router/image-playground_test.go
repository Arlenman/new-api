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

func TestImagePlaygroundSessionAuthWorksWithoutDashboardHeaderAndAuthFailuresAreNotCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("image-playground-test-secret"))))
	engine.Use(middleware.Cache())
	engine.GET("/login-fixture", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 123)
		session.Set("status", common.UserStatusEnabled)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.GET(
		imagePlaygroundRoute+"/*filepath",
		middleware.DisableCache(),
		middleware.TokenOrUserAuth(),
		tool.serve,
	)

	unauthenticated := performImagePlaygroundRequest(engine, imagePlaygroundRoute+"/")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", unauthenticated.Header().Get("Cache-Control"))

	login := performImagePlaygroundRequest(engine, "/login-fixture")
	require.Equal(t, http.StatusNoContent, login.Code)
	require.NotEmpty(t, login.Result().Cookies())

	authenticatedRequest := httptest.NewRequest(http.MethodGet, imagePlaygroundRoute+"/?new_api_user=456", nil)
	authenticatedRequest.AddCookie(login.Result().Cookies()[0])
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
