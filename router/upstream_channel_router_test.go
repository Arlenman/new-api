package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUpstreamChannelRouterTestDB(t *testing.T, filename string) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), filename)), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSession{},
		&model.Channel{},
		&model.UpstreamChannel{},
		&model.Log{},
	))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.SessionSecret = "upstream-channel-router-access-token-test-secret"
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createUpstreamChannelRouterSession(t *testing.T, db *gorm.DB, id int, username string, role int) (*model.User, *service.AuthBundle) {
	t.Helper()
	user := &model.User{
		Id: id, Username: username, Password: "unused-password-hash", Role: role,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: username + "-aff",
	}
	require.NoError(t, db.Create(user).Error)
	bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "upstream-channel-router-test")
	require.NoError(t, err)
	return user, bundle
}

func TestUpstreamChannelRoutesRejectNonRootAccessTokenWithForgedRootHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupUpstreamChannelRouterTestDB(t, "upstream-non-root-route.db")
	_, adminBundle := createUpstreamChannelRouterSession(t, db, 7, "upstream-admin", common.RoleAdminUser)
	forgedRoot, _ := createUpstreamChannelRouterSession(t, db, 8, "upstream-forged-root", common.RoleRootUser)

	engine := gin.New()
	registerUpstreamChannelRoutes(engine.Group("/api"))

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/upstream-channels/"},
		{method: http.MethodGet, path: "/api/upstream-channels/statistics?start_timestamp=100&end_timestamp=200"},
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
		{method: http.MethodPatch, path: "/api/upstream-channels/1/default-test-endpoint"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-balance"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-keys"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/refresh-groups"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/link"},
		{method: http.MethodPatch, path: "/api/upstream-channels/1/keys/1/group"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/import"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/models"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/1/test"},
		{method: http.MethodPost, path: "/api/upstream-channels/1/keys/1"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.Header.Set("Authorization", "Bearer "+adminBundle.AccessToken)
			request.Header.Set("X-Auth-Session", adminBundle.Session.SID)
			request.Header.Set("New-Api-User", strconv.Itoa(forgedRoot.Id))
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusForbidden, recorder.Code)
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
	db := setupUpstreamChannelRouterTestDB(t, "upstream-delete-route.db")
	_, rootBundle := createUpstreamChannelRouterSession(t, db, 1, "upstream-delete-root", common.RoleRootUser)
	forgedCommon, _ := createUpstreamChannelRouterSession(t, db, 2, "upstream-delete-forged-common", common.RoleCommonUser)

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
	registerUpstreamChannelRoutes(engine.Group("/api"))

	requestWithRootAccessToken := func(method string, path string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("Authorization", "Bearer "+rootBundle.AccessToken)
		request.Header.Set("X-Auth-Session", rootBundle.Session.SID)
		request.Header.Set("New-Api-User", strconv.Itoa(forgedCommon.Id))
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
		return recorder
	}

	for _, upstreamChannelID := range []int{unusedChannel.Id, inUseChannel.Id} {
		recorder := requestWithRootAccessToken(http.MethodDelete, "/api/upstream-channels/"+strconv.Itoa(upstreamChannelID))
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

	listRecorder := requestWithRootAccessToken(http.MethodGet, "/api/upstream-channels/")
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

func TestUpstreamChannelRoutesAllowRootAccessTokenWithForgedCommonHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupUpstreamChannelRouterTestDB(t, "upstream-route.db")
	_, rootBundle := createUpstreamChannelRouterSession(t, db, 1, "upstream-list-root", common.RoleRootUser)
	forgedCommon, _ := createUpstreamChannelRouterSession(t, db, 2, "upstream-list-forged-common", common.RoleCommonUser)

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
	registerUpstreamChannelRoutes(engine.Group("/api"))

	request := httptest.NewRequest(http.MethodGet, "/api/upstream-channels/", nil)
	request.Header.Set("Authorization", "Bearer "+rootBundle.AccessToken)
	request.Header.Set("X-Auth-Session", rootBundle.Session.SID)
	request.Header.Set("New-Api-User", strconv.Itoa(forgedCommon.Id))
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
