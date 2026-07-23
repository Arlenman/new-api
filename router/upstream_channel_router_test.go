package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpstreamChannelRoutesRejectNonRootSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("upstream-channel-test"))))
	engine.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 7)
		session.Set("username", "admin")
		session.Set("role", common.RoleAdminUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	registerUpstreamChannelRoutes(engine.Group("/api"))

	setupRecorder := httptest.NewRecorder()
	engine.ServeHTTP(setupRecorder, httptest.NewRequest(http.MethodGet, "/session", nil))
	require.Equal(t, http.StatusNoContent, setupRecorder.Code)

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/upstream-channels/"},
		{method: http.MethodPost, path: "/api/upstream-channels/"},
		{method: http.MethodPost, path: "/api/upstream-channels/refresh"},
		{method: http.MethodGet, path: "/api/upstream-channels/priority-schedule"},
		{method: http.MethodPut, path: "/api/upstream-channels/priority-schedule"},
		{method: http.MethodPost, path: "/api/upstream-channels/priority-schedule/run"},
		{method: http.MethodGet, path: "/api/upstream-channels/priority-schedule/tasks"},
		{method: http.MethodDelete, path: "/api/upstream-channels/priority-schedule/tasks"},
		{method: http.MethodPut, path: "/api/upstream-channels/1"},
		{method: http.MethodDelete, path: "/api/upstream-channels/1"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/pin"},
		{method: http.MethodPatch, path: "/api/upstream-channels/1/note"},
		{method: http.MethodPatch, path: "/api/upstream-channels/1/selected-group"},
		{method: http.MethodPatch, path: "/api/upstream-channels/1/default-test-model"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-balance"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-keys"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-groups"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/link"},
		{method: http.MethodPatch, path: "/api/upstream-channels/1/keys/1/group"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/import"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/models"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/1"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.Header.Set("New-Api-User", "7")
			for _, sessionCookie := range setupRecorder.Result().Cookies() {
				request.AddCookie(sessionCookie)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
		})
	}
}

func TestDeleteUpstreamChannelRouteAllowsReferencedConfigurations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-delete-route.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	inUseBaseURL := "https://used-upstream.example"
	localChannel := &model.Channel{
		Key:     "used-upstream-key",
		BaseURL: &inUseBaseURL,
		Status:  common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(localChannel).Error)
	inUseChannel := &model.UpstreamChannel{
		Name:               "used upstream",
		BaseURL:            inUseBaseURL,
		BaseURLHash:        model.UpstreamBaseURLHash(inUseBaseURL),
		Provider:           "new-api",
		Username:           "root",
		PasswordCiphertext: "secret",
		Status:             model.UpstreamChannelStatusReady,
	}
	unusedChannel := &model.UpstreamChannel{
		Name:        "unused upstream",
		BaseURL:     "https://unused-upstream.example",
		BaseURLHash: model.UpstreamBaseURLHash("https://unused-upstream.example"),
		Provider:    "new-api",
		Status:      model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(inUseChannel).Error)
	require.NoError(t, db.Create(unusedChannel).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("upstream-channel-delete-test"))))
	engine.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 1)
		session.Set("username", "root")
		session.Set("role", common.RoleRootUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	registerUpstreamChannelRoutes(engine.Group("/api"))

	setupRecorder := httptest.NewRecorder()
	engine.ServeHTTP(setupRecorder, httptest.NewRequest(http.MethodGet, "/session", nil))
	require.Equal(t, http.StatusNoContent, setupRecorder.Code)

	requestWithRootSession := func(method string, path string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("New-Api-User", "1")
		for _, sessionCookie := range setupRecorder.Result().Cookies() {
			request.AddCookie(sessionCookie)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
		return recorder
	}

	for _, upstreamChannelID := range []int{unusedChannel.Id, inUseChannel.Id} {
		recorder := requestWithRootSession(http.MethodDelete, "/api/upstream-channels/"+strconv.Itoa(upstreamChannelID))
		var response struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
	}

	var storedLocalChannel model.Channel
	require.NoError(t, db.First(&storedLocalChannel, localChannel.Id).Error)
	assert.Equal(t, "used-upstream-key", storedLocalChannel.Key)
	assert.Equal(t, common.ChannelStatusEnabled, storedLocalChannel.Status)

	var suppressed model.UpstreamChannel
	require.NoError(t, db.First(&suppressed, inUseChannel.Id).Error)
	require.NotNil(t, suppressed.SuppressedAt)
	assert.Empty(t, suppressed.PasswordCiphertext)

	listRecorder := requestWithRootSession(http.MethodGet, "/api/upstream-channels/")
	var listResponse struct {
		Success bool `json:"success"`
		Data    []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	assert.Empty(t, listResponse.Data)

	var rowCount int64
	require.NoError(t, db.Model(&model.UpstreamChannel{}).Where("base_url_hash = ?", model.UpstreamBaseURLHash(inUseBaseURL)).Count(&rowCount).Error)
	assert.Equal(t, int64(1), rowCount)
}

func TestUpstreamChannelRoutesAllowRootSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-route.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{Key: "root-route-test", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Key: "unrelated-route-test", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}).Error)
	fingerprint := model.UpstreamChannelKeyFingerprint(baseURL, "root-route-test")
	snapshotJSON := `{"provider":"new-api","balance":0,"account":{"id":1,"username":"root","balance":0},"keys":[{"id":7,"name":"route-key","masked_key":"sk-...test","status":"1","imported":false,"key_fingerprint":"` + fingerprint + `"}],"groups":[],"ratios":{},"retrieved_at":0}`
	require.NoError(t, db.Create(&model.UpstreamChannel{
		BaseURL:             baseURL,
		BaseURLHash:         model.UpstreamBaseURLHash(baseURL),
		Provider:            "new-api",
		SnapshotJSON:        snapshotJSON,
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusReady,
	}).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("upstream-channel-root-test"))))
	engine.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 1)
		session.Set("username", "root")
		session.Set("role", common.RoleRootUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	registerUpstreamChannelRoutes(engine.Group("/api"))

	setupRecorder := httptest.NewRecorder()
	engine.ServeHTTP(setupRecorder, httptest.NewRequest(http.MethodGet, "/session", nil))
	require.Equal(t, http.StatusNoContent, setupRecorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/api/upstream-channels/", nil)
	request.Header.Set("New-Api-User", "1")
	for _, sessionCookie := range setupRecorder.Result().Cookies() {
		request.AddCookie(sessionCookie)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    []struct {
			BaseURL                  string `json:"base_url"`
			HasPassword              bool   `json:"has_password"`
			SourceChannelCount       int    `json:"source_channel_count"`
			ActiveSourceChannelCount int    `json:"active_source_channel_count"`
			InUseKeyCount            int    `json:"in_use_key_count"`
			Priority                 int64  `json:"priority"`
			Snapshot                 *struct {
				Keys []struct {
					Imported       bool   `json:"imported"`
					Active         bool   `json:"active"`
					KeyFingerprint string `json:"key_fingerprint"`
				} `json:"keys"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 1)
	assert.Equal(t, "https://upstream.example", response.Data[0].BaseURL)
	assert.False(t, response.Data[0].HasPassword)
	assert.Equal(t, 2, response.Data[0].SourceChannelCount)
	assert.Equal(t, 2, response.Data[0].ActiveSourceChannelCount)
	assert.Equal(t, 1, response.Data[0].InUseKeyCount)
	assert.Zero(t, response.Data[0].Priority)
	require.NotNil(t, response.Data[0].Snapshot)
	require.Len(t, response.Data[0].Snapshot.Keys, 1)
	assert.True(t, response.Data[0].Snapshot.Keys[0].Imported)
	assert.True(t, response.Data[0].Snapshot.Keys[0].Active)
	assert.Equal(t, fingerprint, response.Data[0].Snapshot.Keys[0].KeyFingerprint)
	assert.Contains(t, recorder.Body.String(), "key_fingerprint")
	assert.NotContains(t, recorder.Body.String(), "root-route-test")
	assert.NotContains(t, recorder.Body.String(), "unrelated-route-test")
}
