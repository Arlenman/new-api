package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestUserToolRoutesUseAccessTokenIdentityInsteadOfForgedDashboardUserHeader(t *testing.T) {
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
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSession{},
		&model.Token{},
		&model.UserToolItem{},
		&model.UserToolAsset{},
		&model.UserToolItemAsset{},
		&model.UserToolPreference{},
		&model.UserToolChange{},
		&model.UserToolMutationReceipt{},
		&model.UserToolRuntimeSession{},
	))
	model.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "user-tool-router-access-token-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.SetMainDatabaseType(previousDatabaseType)
		_ = sqlDB.Close()
	})

	owner := &model.User{
		Id: 101, Username: "user-tool-owner", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "user-tool-owner-aff",
	}
	forgedUser := &model.User{
		Id: 202, Username: "user-tool-forged", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
		AuthVersion: 1, AffCode: "user-tool-forged-aff",
	}
	require.NoError(t, db.Create(owner).Error)
	require.NoError(t, db.Create(forgedUser).Error)
	bundle, err := service.CreateLoginSession(owner.Id, "password", "127.0.0.1", "user-tool-router-test")
	require.NoError(t, err)

	ownerToken := &model.Token{
		UserId: owner.Id, Key: "user-tool-owner-token-key", Status: common.TokenStatusEnabled,
		Name: "owner-token", ExpiredTime: -1, RemainQuota: 1000, UnlimitedQuota: true, Group: "default",
	}
	forgedToken := &model.Token{
		UserId: forgedUser.Id, Key: "user-tool-forged-token-key", Status: common.TokenStatusEnabled,
		Name: "forged-token", ExpiredTime: -1, RemainQuota: 1000, UnlimitedQuota: true, Group: "default",
	}
	require.NoError(t, db.Create(ownerToken).Error)
	require.NoError(t, db.Create(forgedToken).Error)
	_, err = model.SaveUserToolPreference(owner.Id, model.UserToolInfiniteCanvas, ownerToken.Id)
	require.NoError(t, err)
	_, err = model.SaveUserToolPreference(forgedUser.Id, model.UserToolInfiniteCanvas, forgedToken.Id)
	require.NoError(t, err)

	_, err = model.ApplyUserToolMutation(owner.Id, model.UserToolInfiniteCanvas, model.UserToolMutation{
		ClientMutationID: "owner-seed-mutation", Kind: "project", ItemKey: "owner-project",
		SchemaVersion: 1, Payload: model.JSONValue(`{"identity":"token-owner"}`),
	})
	require.NoError(t, err)
	_, err = model.ApplyUserToolMutation(forgedUser.Id, model.UserToolInfiniteCanvas, model.UserToolMutation{
		ClientMutationID: "forged-seed-mutation", Kind: "project", ItemKey: "forged-project",
		SchemaVersion: 1, Payload: model.JSONValue(`{"identity":"forged-header-user"}`),
	})
	require.NoError(t, err)
	forgedAsset, err := model.StoreUserToolAsset(forgedUser.Id, "forged.txt", "text/plain", "", strings.NewReader("forged asset"))
	require.NoError(t, err)

	engine := gin.New()
	registerUserToolRoutes(engine.Group("/api"))

	requestWithAccessToken := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
		request.Header.Set("X-Auth-Session", bundle.Session.SID)
		request.Header.Set("New-Api-User", "202")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder
	}

	t.Run("bootstrap and changes remain scoped to the token owner", func(t *testing.T) {
		for _, path := range []string{
			"/api/user-tools/infinite-canvas/bootstrap",
			"/api/user-tools/infinite-canvas/changes",
		} {
			recorder := requestWithAccessToken(http.MethodGet, path, "")
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			assert.Contains(t, recorder.Body.String(), "owner-project")
			assert.NotContains(t, recorder.Body.String(), "forged-project")
		}
	})

	t.Run("sync writes to the token owner", func(t *testing.T) {
		recorder := requestWithAccessToken(http.MethodPost, "/api/user-tools/infinite-canvas/sync", `{"mutations":[{"client_mutation_id":"route-owner-sync","kind":"project","key":"route-owner-project","schema_version":1,"payload":{"identity":"route-token-owner"}}]}`)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		_, err := model.GetUserToolItem(owner.Id, model.UserToolInfiniteCanvas, "project", "route-owner-project")
		require.NoError(t, err)
		_, err = model.GetUserToolItem(forgedUser.Id, model.UserToolInfiniteCanvas, "project", "route-owner-project")
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("tokens expose only the token owner records", func(t *testing.T) {
		recorder := requestWithAccessToken(http.MethodGet, "/api/user-tools/infinite-canvas/tokens", "")
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		assert.Contains(t, recorder.Body.String(), "owner-token")
		assert.NotContains(t, recorder.Body.String(), "forged-token")
	})

	t.Run("preferences read and update the token owner record", func(t *testing.T) {
		getRecorder := requestWithAccessToken(http.MethodGet, "/api/user-tools/infinite-canvas/preferences", "")
		require.Equal(t, http.StatusOK, getRecorder.Code)
		var getResponse struct {
			Success bool `json:"success"`
			Data    struct {
				SelectedTokenID int `json:"selected_token_id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(getRecorder.Body.Bytes(), &getResponse))
		require.True(t, getResponse.Success)
		assert.Equal(t, ownerToken.Id, getResponse.Data.SelectedTokenID)

		body, err := common.Marshal(map[string]int{"selected_token_id": ownerToken.Id})
		require.NoError(t, err)
		updateRecorder := requestWithAccessToken(http.MethodPut, "/api/user-tools/infinite-canvas/preferences", string(body))
		require.Equal(t, http.StatusOK, updateRecorder.Code)
		var updateResponse struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(updateRecorder.Body.Bytes(), &updateResponse))
		require.True(t, updateResponse.Success)
		ownerPreference, err := model.GetUserToolPreference(owner.Id, model.UserToolInfiniteCanvas)
		require.NoError(t, err)
		assert.Equal(t, ownerToken.Id, ownerPreference.SelectedTokenID)
		forgedPreference, err := model.GetUserToolPreference(forgedUser.Id, model.UserToolInfiniteCanvas)
		require.NoError(t, err)
		assert.Equal(t, forgedToken.Id, forgedPreference.SelectedTokenID)
	})

	t.Run("runtime session belongs to the token owner", func(t *testing.T) {
		body, err := common.Marshal(map[string]int{"token_id": ownerToken.Id})
		require.NoError(t, err)
		recorder := requestWithAccessToken(http.MethodPost, "/api/user-tools/infinite-canvas/runtime-session", string(body))
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Credential string `json:"credential"`
				Token      struct {
					ID int `json:"id"`
				} `json:"token"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		assert.Equal(t, ownerToken.Id, response.Data.Token.ID)
		session, err := model.ResolveUserToolRuntimeSession(response.Data.Credential)
		require.NoError(t, err)
		assert.Equal(t, owner.Id, session.UserID)
	})

	t.Run("asset upload belongs to the token owner", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/user-tools/assets/uploads", strings.NewReader("owner asset"))
		request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
		request.Header.Set("X-Auth-Session", bundle.Session.SID)
		request.Header.Set("New-Api-User", "202")
		request.Header.Set("Content-Type", "text/plain")
		request.Header.Set("X-File-Name", "owner.txt")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		var stored model.UserToolAsset
		require.NoError(t, db.First(&stored, "id = ?", response.Data.ID).Error)
		assert.Equal(t, owner.Id, stored.UserID)
	})

	t.Run("asset content cannot cross into the forged header user", func(t *testing.T) {
		recorder := requestWithAccessToken(http.MethodGet, "/api/user-tools/assets/"+forgedAsset.ID+"/content", "")
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})
}
