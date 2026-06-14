package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newPixelFlowSessionRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	store := cookie.NewStore([]byte("pixelflow-test-secret"))
	router.Use(sessions.Sessions("session", store))
	router.GET("/session-setup", func(c *gin.Context) {
		userId, _ := strconv.Atoi(c.Query("user_id"))
		session := sessions.Default(c)
		session.Set("id", userId)
		session.Set("username", "pixel-user")
		session.Set("role", common.RoleCommonUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "ok")
	})
	router.GET("/api/pixelflow/session-token-sync", PixelFlowSessionTokenSync)

	return router
}

func seedPixelFlowUser(t *testing.T, dbUserId int, username string) {
	t.Helper()

	user := &model.User{
		Id:          dbUserId,
		Username:    username,
		Password:    "unused",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
}

func performPixelFlowSessionSyncRequest(t *testing.T, router *gin.Engine, origin string, sessionUserId int) *httptest.ResponseRecorder {
	t.Helper()

	target := "/api/pixelflow/session-token-sync?origin=" + origin
	request := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()

	if sessionUserId > 0 {
		setupRequest := httptest.NewRequest(
			http.MethodGet,
			"/session-setup?user_id="+strconv.Itoa(sessionUserId),
			nil,
		)
		setupRecorder := httptest.NewRecorder()
		router.ServeHTTP(setupRecorder, setupRequest)
		for _, cookie := range setupRecorder.Result().Cookies() {
			request.AddCookie(cookie)
		}
	}

	router.ServeHTTP(recorder, request)

	return recorder
}

func TestPixelFlowSessionTokenSyncRequiresLoggedInSession(t *testing.T) {
	router := newPixelFlowSessionRouter()

	recorder := performPixelFlowSessionSyncRequest(t, router, "http%3A%2F%2Flocalhost%3A3030", 0)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "请先登录 NewAPI") {
		t.Fatalf("expected login hint, got %s", recorder.Body.String())
	}
}

func TestPixelFlowSessionTokenSyncRejectsUntrustedOrigin(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to migrate user table: %v", err)
	}
	seedPixelFlowUser(t, 7, "pixel-user")
	router := newPixelFlowSessionRouter()

	recorder := performPixelFlowSessionSyncRequest(t, router, "https%3A%2F%2Fevil.example.com", 7)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "不允许同步到该站点") {
		t.Fatalf("expected origin rejection, got %s", recorder.Body.String())
	}
}

func TestPixelFlowSessionTokenSyncPostsCurrentUserTokensToAllowedOrigin(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to migrate user table: %v", err)
	}
	seedPixelFlowUser(t, 7, "pixel-user")
	seedToken(t, db, 7, "绘画密钥", "raw-token-key")
	seedToken(t, db, 8, "其他用户密钥", "other-token-key")
	router := newPixelFlowSessionRouter()

	recorder := performPixelFlowSessionSyncRequest(t, router, "http%3A%2F%2Flocalhost%3A3030", 7)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "window.opener.postMessage") {
		t.Fatalf("expected postMessage page, got %s", body)
	}
	if !strings.Contains(body, `"origin":"http://localhost:3030"`) {
		t.Fatalf("expected target origin in payload, got %s", body)
	}
	if !strings.Contains(body, `"userId":7`) || !strings.Contains(body, `"username":"pixel-user"`) {
		t.Fatalf("expected current user binding, got %s", body)
	}
	if !strings.Contains(body, `"key":"raw-token-key"`) {
		t.Fatalf("expected current user raw token key, got %s", body)
	}
	if strings.Contains(body, "other-token-key") || strings.Contains(body, "其他用户密钥") {
		t.Fatalf("response leaked another user's token: %s", body)
	}
}
