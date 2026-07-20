package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserToolRoutesRejectMismatchedUserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-tool-route-test"))))
	engine.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 101)
		session.Set("username", "owner")
		session.Set("role", common.RoleCommonUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	registerUserToolRoutes(engine.Group("/api"))

	setupRecorder := httptest.NewRecorder()
	engine.ServeHTTP(setupRecorder, httptest.NewRequest(http.MethodGet, "/session", nil))
	require.Equal(t, http.StatusNoContent, setupRecorder.Code)

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/user-tools/infinite-canvas/bootstrap"},
		{method: http.MethodGet, path: "/api/user-tools/infinite-canvas/changes"},
		{method: http.MethodPost, path: "/api/user-tools/infinite-canvas/sync"},
		{method: http.MethodGet, path: "/api/user-tools/infinite-canvas/preferences"},
		{method: http.MethodPut, path: "/api/user-tools/infinite-canvas/preferences"},
		{method: http.MethodPost, path: "/api/user-tools/infinite-canvas/runtime-session"},
		{method: http.MethodPost, path: "/api/user-tools/assets/uploads"},
		{method: http.MethodGet, path: "/api/user-tools/assets/uta_other/content"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("New-Api-User", "202")
			for _, sessionCookie := range setupRecorder.Result().Cookies() {
				request.AddCookie(sessionCookie)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
		})
	}
}
