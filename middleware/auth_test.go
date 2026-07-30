package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardAuthMiddlewareTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	previousSQLitePath := common.SQLitePath
	previousMasterNode := common.IsMasterNode
	t.Setenv("SQL_DSN", "")
	common.SQLitePath = ":memory:"
	common.IsMasterNode = false
	require.NoError(t, model.InitDB())
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Token{}, &model.TokenIP{}))
	common.RedisEnabled = false
	common.SessionSecret = "middleware-auth-test-secret"
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = previousDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.SQLitePath = previousSQLitePath
		common.IsMasterNode = previousMasterNode
	})
}

func issueExpiredDashboardAccessToken(t *testing.T, identity service.AuthIdentity) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":       "new-api",
		"aud":       []string{"new-api-dashboard"},
		"sub":       fmt.Sprintf("%d", identity.UserID),
		"token_use": "access",
		"sid":       identity.SessionID,
		"uv":        identity.UserAuthVersion,
		"sv":        identity.SessionVersion,
		"exp":       time.Now().Add(-time.Minute).Unix(),
		"nbf":       time.Now().Add(-2 * time.Minute).Unix(),
		"iat":       time.Now().Add(-2 * time.Minute).Unix(),
	}
	mac := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, err := mac.Write([]byte("new-api/auth/access/v1"))
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(mac.Sum(nil))
	require.NoError(t, err)
	return token
}

func tamperDashboardToken(token string) string {
	tamperAt := len(token) - 2
	replacement := "x"
	if token[tamperAt] == 'x' {
		replacement = "y"
	}
	return token[:tamperAt] + replacement + token[tamperAt+1:]
}

func createMiddlewarePATUser(t *testing.T, username, token string) *model.User {
	t.Helper()
	user := &model.User{
		Username: username, Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1,
		AffCode: "middleware-aff-" + username,
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func TestUserAuthAllowsOpaqueDottedPAT(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user := createMiddlewarePATUser(t, "dotted-pat-user", "opaque.key.with-dots")
	router := gin.New()
	router.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("id")})
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer opaque.key.with-dots")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	var body struct {
		ID int `json:"id"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, user.Id, body.ID)
}

func TestUserAuthNeverFallsBackForRecognizedInvalidInternalJWT(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	identity := service.AuthIdentity{UserID: 42, SessionID: "session-42", UserAuthVersion: 1, SessionVersion: 1}
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	tampered := tamperDashboardToken(token)
	createMiddlewarePATUser(t, "jwt-fallback-user", tampered)
	router := gin.New()
	router.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+tampered)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_UNAUTHORIZED")
}

func TestTryUserAuthCredentialClassification(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	gin.SetMode(gin.TestMode)

	patUser := createMiddlewarePATUser(t, "optional-pat-user", "optional.pat.with-dots")
	internalUser := createMiddlewarePATUser(t, "optional-session-user", "unrelated-pat")
	now := time.Now().Unix()
	session := &model.UserSession{
		SID:             "optional-auth-session",
		UserID:          internalUser.Id,
		Version:         1,
		UserAuthVersion: internalUser.AuthVersion,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     "refresh-hash",
		LoginMethod:     "password",
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}
	require.NoError(t, model.CreateUserSession(session))
	identity := service.AuthIdentity{
		UserID:          internalUser.Id,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	accessToken, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	securityProof, _, err := service.IssueSecurityProof(identity, "2fa", []string{"channel.key.read"})
	require.NoError(t, err)
	externalToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "external-issuer",
		"aud": "external-audience",
		"exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString([]byte("external-secret"))
	require.NoError(t, err)

	router := gin.New()
	router.GET("/optional", TryUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":               c.GetInt("id"),
			"use_access_token": c.GetBool("use_access_token"),
		})
	})

	tests := []struct {
		name          string
		token         string
		wantStatus    int
		wantUserID    int
		wantPAT       bool
		wantErrorCode string
	}{
		{name: "no authorization header", wantStatus: http.StatusOK},
		{name: "opaque unmatched credential", token: "opaque-relay-key", wantStatus: http.StatusOK},
		{name: "dotted unmatched credential", token: "ordinary.key.with-dots", wantStatus: http.StatusOK},
		{name: "third party jwt", token: externalToken, wantStatus: http.StatusOK},
		{name: "valid pat", token: "optional.pat.with-dots", wantStatus: http.StatusOK, wantUserID: patUser.Id, wantPAT: true},
		{name: "valid internal access jwt", token: accessToken, wantStatus: http.StatusOK, wantUserID: internalUser.Id},
		{name: "expired internal access jwt", token: issueExpiredDashboardAccessToken(t, identity), wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_TOKEN_EXPIRED"},
		{name: "tampered internal access jwt", token: tamperDashboardToken(accessToken), wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_UNAUTHORIZED"},
		{name: "security proof used as access", token: securityProof, wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_UNAUTHORIZED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/optional", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, test.wantStatus, response.Code)
			if test.wantErrorCode != "" {
				assert.Contains(t, response.Body.String(), test.wantErrorCode)
				return
			}
			var body struct {
				ID             int  `json:"id"`
				UseAccessToken bool `json:"use_access_token"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.wantUserID, body.ID)
			assert.Equal(t, test.wantPAT, body.UseAccessToken)
		})
	}

	requiredRouter := gin.New()
	requiredRouter.GET("/required", UserAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	requiredRequest := httptest.NewRequest(http.MethodGet, "/required", nil)
	requiredRequest.Header.Set("Authorization", "Bearer ordinary-unmatched-key")
	requiredResponse := httptest.NewRecorder()
	requiredRouter.ServeHTTP(requiredResponse, requiredRequest)
	assert.Equal(t, http.StatusUnauthorized, requiredResponse.Code, "required dashboard authentication must not adopt optional-auth fallback semantics")

	var patUserQueries int
	forcedCacheError := errors.New("forced PAT user cache lookup failure")
	const callbackName = "test:optional-auth-pat-user-cache-failure"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		patUserQueries++
		if patUserQueries == 2 {
			tx.AddError(forcedCacheError)
		}
	}))
	cacheFailureRequest := httptest.NewRequest(http.MethodGet, "/optional", nil)
	cacheFailureRequest.Header.Set("Authorization", "Bearer optional.pat.with-dots")
	cacheFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(cacheFailureResponse, cacheFailureRequest)
	model.DB.Callback().Query().Remove(callbackName)
	assert.Equal(t, http.StatusInternalServerError, cacheFailureResponse.Code)
	assert.Contains(t, cacheFailureResponse.Body.String(), "AUTH_INTERNAL_ERROR")

	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	databaseFailureRequest := httptest.NewRequest(http.MethodGet, "/optional", nil)
	databaseFailureRequest.Header.Set("Authorization", "Bearer database-failure-key")
	databaseFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(databaseFailureResponse, databaseFailureRequest)
	assert.Equal(t, http.StatusInternalServerError, databaseFailureResponse.Code)
	assert.Contains(t, databaseFailureResponse.Body.String(), "AUTH_INTERNAL_ERROR")
}

type playgroundAuthContextResponse struct {
	UserID                  int    `json:"user_id"`
	Group                   string `json:"group"`
	TokenID                 int    `json:"token_id"`
	TokenKey                string `json:"token_key"`
	SessionID               string `json:"session_id"`
	AuthVersion             int64  `json:"auth_version"`
	SessionVersion          int64  `json:"session_version"`
	HasDashboardIdentity    bool   `json:"has_dashboard_identity"`
	DashboardIdentityUserID int    `json:"dashboard_identity_user_id"`
}

func newPlaygroundAuthTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/pg/chat/completions", PlaygroundAuth(), func(c *gin.Context) {
		identity, hasDashboardIdentity := GetAuthIdentity(c)
		c.JSON(http.StatusOK, playgroundAuthContextResponse{
			UserID:                  c.GetInt("id"),
			Group:                   c.GetString("group"),
			TokenID:                 c.GetInt("token_id"),
			TokenKey:                c.GetString("token_key"),
			SessionID:               c.GetString("session_id"),
			AuthVersion:             c.GetInt64("auth_version"),
			SessionVersion:          c.GetInt64("session_version"),
			HasDashboardIdentity:    hasDashboardIdentity,
			DashboardIdentityUserID: identity.UserID,
		})
	})
	return router
}

func createPlaygroundAuthSession(t *testing.T, username string) (*model.User, service.AuthIdentity) {
	t.Helper()
	user := createMiddlewarePATUser(t, username, "unrelated-"+username+"-pat")
	now := time.Now().Unix()
	session := &model.UserSession{
		SID:             "playground-" + username + "-session",
		UserID:          user.Id,
		Version:         3,
		UserAuthVersion: user.AuthVersion,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     "refresh-" + username,
		LoginMethod:     "password",
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}
	require.NoError(t, model.CreateUserSession(session))
	return user, service.AuthIdentity{
		UserID:          user.Id,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
}

func createPlaygroundRelayToken(t *testing.T, userID int, key string) *model.Token {
	t.Helper()
	token := &model.Token{
		UserId:         userID,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		Name:           "playground-test-token",
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          "default",
		CreatedTime:    time.Now().Unix(),
		AccessedTime:   time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(token).Error)
	return token
}

func registerPlaygroundTokenQueryCounter(t *testing.T) *int {
	t.Helper()
	count := 0
	callbackName := "test:playground-auth-token-query-counter"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			count++
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Query().Remove(callbackName) })
	return &count
}

func TestPlaygroundAuthAllowsDashboardAccessJWTWithoutTokenLookup(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user, identity := createPlaygroundAuthSession(t, "playground-dashboard-user")
	accessToken, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	tokenQueries := registerPlaygroundTokenQueryCounter(t)

	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	newPlaygroundAuthTestRouter().ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	var body playgroundAuthContextResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, user.Id, body.UserID)
	assert.Equal(t, user.Group, body.Group)
	assert.Equal(t, identity.SessionID, body.SessionID)
	assert.Equal(t, identity.UserAuthVersion, body.AuthVersion)
	assert.Equal(t, identity.SessionVersion, body.SessionVersion)
	assert.True(t, body.HasDashboardIdentity)
	assert.Equal(t, user.Id, body.DashboardIdentityUserID)
	assert.Zero(t, body.TokenID)
	assert.Empty(t, body.TokenKey)
	assert.Zero(t, *tokenQueries)
}

func TestPlaygroundAuthPreservesRelayAPIKeyAuthentication(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user := createMiddlewarePATUser(t, "playground-api-key-user", "unrelated-playground-api-pat")
	token := createPlaygroundRelayToken(t, user.Id, "playgroundrelay")

	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk-playgroundrelay")
	response := httptest.NewRecorder()
	newPlaygroundAuthTestRouter().ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	var body playgroundAuthContextResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, user.Id, body.UserID)
	assert.Equal(t, "default", body.Group)
	assert.Equal(t, token.Id, body.TokenID)
	assert.Equal(t, token.Key, body.TokenKey)
	assert.False(t, body.HasDashboardIdentity)
	assert.Empty(t, body.SessionID)
}

func TestPlaygroundAuthRejectsExpiredDashboardAccessJWTWithoutFallback(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user, identity := createPlaygroundAuthSession(t, "playground-expired-user")
	expiredToken := issueExpiredDashboardAccessToken(t, identity)
	createPlaygroundRelayToken(t, user.Id, strings.Split(expiredToken, "-")[0])
	tokenQueries := registerPlaygroundTokenQueryCounter(t)

	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer "+expiredToken)
	response := httptest.NewRecorder()
	newPlaygroundAuthTestRouter().ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_TOKEN_EXPIRED")
	assert.Zero(t, *tokenQueries)
}

func TestPlaygroundAuthRejectsTamperedDashboardAccessJWTWithoutFallback(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user, identity := createPlaygroundAuthSession(t, "playground-tampered-user")
	accessToken, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	tamperedToken := tamperDashboardToken(accessToken)
	user.AccessToken = &tamperedToken
	require.NoError(t, model.DB.Save(user).Error)
	createPlaygroundRelayToken(t, user.Id, strings.Split(tamperedToken, "-")[0])
	tokenQueries := registerPlaygroundTokenQueryCounter(t)

	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer "+tamperedToken)
	response := httptest.NewRecorder()
	newPlaygroundAuthTestRouter().ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_UNAUTHORIZED")
	assert.Zero(t, *tokenQueries)
}

func TestPlaygroundAuthRejectsRevokedAndMissingDashboardSessions(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user, revokedIdentity := createPlaygroundAuthSession(t, "playground-revoked-user")
	require.NoError(t, model.DB.Model(&model.UserSession{}).
		Where("sid = ?", revokedIdentity.SessionID).
		Updates(map[string]any{
			"status":     model.UserSessionStatusRevoked,
			"revoked_at": time.Now().Unix(),
		}).Error)
	missingIdentity := service.AuthIdentity{
		UserID:          user.Id,
		SessionID:       "playground-missing-session",
		UserAuthVersion: user.AuthVersion,
		SessionVersion:  1,
	}

	for _, test := range []struct {
		name     string
		identity service.AuthIdentity
	}{
		{name: "revoked session", identity: revokedIdentity},
		{name: "missing session", identity: missingIdentity},
	} {
		t.Run(test.name, func(t *testing.T) {
			accessToken, _, err := service.IssueAccessToken(test.identity)
			require.NoError(t, err)
			tokenQueries := registerPlaygroundTokenQueryCounter(t)
			request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
			request.Header.Set("Authorization", "Bearer "+accessToken)
			response := httptest.NewRecorder()
			newPlaygroundAuthTestRouter().ServeHTTP(response, request)

			assert.Equal(t, http.StatusUnauthorized, response.Code)
			assert.Contains(t, response.Body.String(), "AUTH_SESSION_REVOKED")
			assert.Zero(t, *tokenQueries)
		})
	}
}

func TestPlaygroundAuthPreservesNoAuthorizationBehavior(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	response := httptest.NewRecorder()
	newPlaygroundAuthTestRouter().ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_UNAUTHORIZED")
}
