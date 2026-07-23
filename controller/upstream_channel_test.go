package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

func setupUpstreamChannelControllerTest(t *testing.T) (*gin.Engine, *model.UpstreamChannel) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-controller.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "upstream-controller-test-secret"
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.CryptoSecret = originalCryptoSecret
	})

	row := &model.UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         model.UpstreamBaseURLHash("https://upstream.example"),
		Provider:            "auto",
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusUnconfigured,
	}
	require.NoError(t, db.Create(row).Error)

	engine := gin.New()
	engine.GET("/api/upstream-channels/", GetUpstreamChannels)
	engine.POST("/api/upstream-channels/", CreateUpstreamChannel)
	engine.PUT("/api/upstream-channels/:id", UpdateUpstreamChannelConfig)
	engine.POST("/api/upstream-channels/:id/pin", PinUpstreamChannel)
	engine.POST("/api/upstream-channels/:id/refresh", RefreshUpstreamChannel)
	engine.PATCH("/api/upstream-channels/:id/note", UpdateUpstreamChannelNote)
	engine.PATCH("/api/upstream-channels/:id/selected-group", UpdateUpstreamChannelSelectedGroup)
	engine.PATCH("/api/upstream-channels/:id/default-test-model", UpdateUpstreamChannelDefaultTestModel)
	engine.POST("/api/upstream-channels/:id/keys/link", LinkUpstreamChannelKeys)
	engine.PATCH("/api/upstream-channels/:id/keys/:key_id/group", UpdateUpstreamChannelKeyGroup)
	engine.POST("/api/upstream-channels/:id/keys/import", ImportUpstreamChannelKeys)
	engine.POST("/api/upstream-channels/:id/keys/models", FetchUpstreamChannelKeyModels)
	return engine, row
}

func TestUpdateUpstreamChannelKeyGroupRejectsUnsupportedProvider(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	row.Provider = service.UpstreamProviderOther
	fingerprint := model.UpstreamChannelKeyFingerprint(row.BaseURL, "sk-linked")
	row.SnapshotJSON = `{"provider":"other","keys":[{"id":7,"name":"key","linked":true,"active":true,"in_use_status":"enabled","key_fingerprint":"` + fingerprint + `"}],"groups":[],"ratios":{},"models":[]}`
	require.NoError(t, model.DB.Save(row).Error)

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/upstream-channels/"+strconv.Itoa(row.Id)+"/keys/7/group",
		bytes.NewBufferString(`{"group":"premium"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			InUseKeyCount int                       `json:"in_use_key_count"`
			Snapshot      *service.UpstreamSnapshot `json:"snapshot"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "provider other")
	require.NotNil(t, response.Data.Snapshot)
	require.Len(t, response.Data.Snapshot.Keys, 1)
	assert.Equal(t, int64(7), response.Data.Snapshot.Keys[0].ID)
	assert.Equal(t, 1, response.Data.InUseKeyCount)
}

func TestLinkUpstreamChannelKeysErrorResponseKeepsInUseKeyCount(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	fingerprint := model.UpstreamChannelKeyFingerprint(row.BaseURL, "sk-linked")
	row.SnapshotJSON = `{"provider":"new-api","keys":[{"id":7,"linked":true,"active":true,"in_use_status":"enabled","key_fingerprint":"` + fingerprint + `"}],"groups":[],"ratios":{},"models":[]}`
	require.NoError(t, model.DB.Save(row).Error)

	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/keys/link", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			InUseKeyCount int `json:"in_use_key_count"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, 1, response.Data.InUseKeyCount)
}

func TestUpstreamChannelTurnstileErrorResponseIncludesRecoveryCode(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	encrypted, err := common.EncryptSecret("upstream-channel-password", "plain-text-password")
	require.NoError(t, err)
	row.Provider = service.UpstreamProviderNewAPI
	row.AuthType = model.UpstreamAuthTypePassword
	row.Username = "yunqi"
	row.PasswordCiphertext = encrypted
	row.LastError = service.ErrNewAPITurnstileRequiresAccessToken.Error()
	row.Status = model.UpstreamChannelStatusError

	engine.GET("/turnstile-error", func(c *gin.Context) {
		respondUpstreamChannelError(
			c,
			service.ErrNewAPITurnstileRequiresAccessToken,
			buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)),
		)
	})
	request := httptest.NewRequest(http.MethodGet, "/turnstile-error", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		ErrorCode string `json:"error_code"`
		Data      struct {
			ID            int    `json:"id"`
			Username      string `json:"username"`
			HasPassword   bool   `json:"has_password"`
			LastError     string `json:"last_error"`
			LastErrorCode string `json:"last_error_code"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, service.UpstreamErrorCodeTurnstileRequiresAccessToken, response.ErrorCode)
	assert.Contains(t, response.Message, "management access token")
	assert.Equal(t, row.Id, response.Data.ID)
	assert.Equal(t, "yunqi", response.Data.Username)
	assert.True(t, response.Data.HasPassword)
	assert.Equal(t, service.UpstreamErrorCodeTurnstileRequiresAccessToken, response.Data.LastErrorCode)
	assert.Contains(t, response.Data.LastError, "management access token")
	assert.NotContains(t, recorder.Body.String(), "plain-text-password")
}

func TestUpdateUpstreamChannelSelectedGroupPersistsAvailableGroup(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	snapshotJSON := `{"provider":"new-api","balance":10,"account":{"id":1,"username":"root","balance":10},"keys":[],"groups":[{"name":"gpt-pro","ratio":0.12}],"ratios":{"gpt-pro":0.12},"retrieved_at":0}`
	require.NoError(t, model.UpdateUpstreamChannelSnapshot(row.Id, snapshotJSON))

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/upstream-channels/"+strconv.Itoa(row.Id)+"/selected-group",
		bytes.NewBufferString(`{"selected_group":" gpt-pro "}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			SelectedGroup string `json:"selected_group"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "gpt-pro", response.Data.SelectedGroup)

	updated, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, "gpt-pro", updated.SelectedGroup)

	refreshedSnapshotJSON := `{"provider":"new-api","balance":9,"account":{"id":1,"username":"root","balance":9},"keys":[],"groups":[{"name":"gpt","ratio":0.085}],"ratios":{"gpt":0.085},"retrieved_at":1}`
	require.NoError(t, model.UpdateUpstreamChannelSnapshot(row.Id, refreshedSnapshotJSON))

	request = httptest.NewRequest(http.MethodGet, "/api/upstream-channels/", nil)
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var listResponse struct {
		Success bool `json:"success"`
		Data    []struct {
			SelectedGroup string                    `json:"selected_group"`
			Snapshot      *service.UpstreamSnapshot `json:"snapshot"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data, 1)
	assert.Equal(t, "gpt-pro", listResponse.Data[0].SelectedGroup)
	require.NotNil(t, listResponse.Data[0].Snapshot)
	require.Len(t, listResponse.Data[0].Snapshot.Groups, 1)
	assert.Equal(t, "gpt", listResponse.Data[0].Snapshot.Groups[0].Name)
}

func TestUpdateUpstreamChannelSelectedGroupTrimsLegacySnapshotName(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	snapshotJSON := `{"provider":"sub2api","balance":10,"account":{"id":1,"username":"root","balance":10},"keys":[],"groups":[{"id":24,"name":"005 ","ratio":0.05}],"ratios":{"24":0.05},"retrieved_at":0}`
	require.NoError(t, model.UpdateUpstreamChannelSnapshot(row.Id, snapshotJSON))

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/upstream-channels/"+strconv.Itoa(row.Id)+"/selected-group",
		bytes.NewBufferString(`{"selected_group":"005"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			SelectedGroup string `json:"selected_group"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "005", response.Data.SelectedGroup)

	updated, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, "005", updated.SelectedGroup)
}

func TestUpdateUpstreamChannelSelectedGroupRejectsUnavailableGroup(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	snapshotJSON := `{"provider":"new-api","balance":10,"account":{"id":1,"username":"root","balance":10},"keys":[],"groups":[{"name":"gpt","ratio":0.085}],"ratios":{"gpt":0.085},"retrieved_at":0}`
	require.NoError(t, model.UpdateUpstreamChannelSnapshot(row.Id, snapshotJSON))

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/upstream-channels/"+strconv.Itoa(row.Id)+"/selected-group",
		bytes.NewBufferString(`{"selected_group":"gpt-pro"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "selected upstream group is unavailable", response.Message)
}

func TestUpdateUpstreamChannelDefaultTestModelPersistsAvailableModel(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	row.SnapshotJSON = `{"provider":"new-api","models":[{"id":"gpt-4o-mini","pricing":[]},{"id":"claude-3-5-sonnet","pricing":[]}]}`
	require.NoError(t, model.DB.Save(row).Error)

	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/default-test-model", bytes.NewBufferString(`{"default_test_model":"  gpt-4o-mini  "}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			DefaultTestModel string `json:"default_test_model"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "gpt-4o-mini", response.Data.DefaultTestModel)

	updated, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", updated.DefaultTestModel)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/upstream-channels/", nil)
	listRecorder := httptest.NewRecorder()
	engine.ServeHTTP(listRecorder, listRequest)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse struct {
		Success bool `json:"success"`
		Data    []struct {
			DefaultTestModel string `json:"default_test_model"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data, 1)
	assert.Equal(t, "gpt-4o-mini", listResponse.Data[0].DefaultTestModel)
}

func TestUpdateUpstreamChannelDefaultTestModelRejectsUnavailableModel(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	row.SnapshotJSON = `{"provider":"new-api","models":[{"id":"gpt-4o-mini","pricing":[]}]}`
	require.NoError(t, model.DB.Save(row).Error)

	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/default-test-model", bytes.NewBufferString(`{"default_test_model":"missing-model"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "default test model is unavailable", response.Message)
}

func TestUpdateUpstreamChannelDefaultTestModelCanClearSelection(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	row.DefaultTestModel = "gpt-4o-mini"
	require.NoError(t, model.DB.Save(row).Error)

	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/default-test-model", bytes.NewBufferString(`{"default_test_model":""}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			DefaultTestModel string `json:"default_test_model"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Empty(t, response.Data.DefaultTestModel)
}

func TestUpdateUpstreamChannelDefaultTestModelRejectsOversizedValue(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	payload := `{"default_test_model":"` + strings.Repeat("x", upstreamChannelNameMaxLength+1) + `"}`
	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/default-test-model", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, errInvalidUpstreamDefaultTestModel.Error(), response.Message)
}

func TestCreateUpstreamChannelNormalizesBaseURLAndDefaultsName(t *testing.T) {
	t.Setenv("SESSION_SECRET", "persistent-session-secret")
	engine, _ := setupUpstreamChannelControllerTest(t)
	payload := `{"base_url":" https://api.SyncAPI.dpdns.org/v1/ ","name":" ","provider":"other","priority":7,"username":"root","password":"plain-text-password","balance_threshold":10,"auto_refresh_interval":600}`
	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "plain-text-password")
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ID          int     `json:"id"`
			Name        string  `json:"name"`
			BaseURL     string  `json:"base_url"`
			HasPassword bool    `json:"has_password"`
			Multiplier  float64 `json:"multiplier"`
			Provider    string  `json:"provider"`
			Priority    int64   `json:"priority"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "syncapi", response.Data.Name)
	assert.Equal(t, "https://api.syncapi.dpdns.org", response.Data.BaseURL)
	assert.True(t, response.Data.HasPassword)
	assert.Equal(t, float64(model.UpstreamChannelDefaultMultiplier), response.Data.Multiplier)
	assert.Equal(t, "other", response.Data.Provider)
	assert.Equal(t, int64(7), response.Data.Priority)

	created, err := model.GetUpstreamChannelByID(response.Data.ID)
	require.NoError(t, err)
	assert.Equal(t, "syncapi", created.Name)
	assert.Equal(t, "other", created.Provider)
	assert.Equal(t, int64(7), created.Priority)
	password, err := created.DecryptPassword()
	require.NoError(t, err)
	assert.Equal(t, "plain-text-password", password)
}

func TestCreateUpstreamChannelAllowsMultipleAccountsForSameBaseURL(t *testing.T) {
	t.Setenv("SESSION_SECRET", "persistent-session-secret")
	engine, _ := setupUpstreamChannelControllerTest(t)
	baseURL := "https://multi-account.example"
	payloads := []string{
		`{"base_url":"` + baseURL + `","name":"Account one","provider":"sub2api","auth_type":"password","username":"one@example.com","password":"password-one"}`,
		`{"base_url":"` + baseURL + `","name":"Account two","provider":"sub2api","auth_type":"password","username":"two@example.com","password":"password-two"}`,
	}

	ids := make([]int, 0, len(payloads))
	for _, payload := range payloads {
		request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		ids = append(ids, response.Data.ID)
	}

	assert.NotEqual(t, ids[0], ids[1])
	var rows []model.UpstreamChannel
	require.NoError(t, model.DB.Where("base_url = ?", baseURL).Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "one@example.com", rows[0].Username)
	assert.Equal(t, "two@example.com", rows[1].Username)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/upstream-channels/", nil)
	listRecorder := httptest.NewRecorder()
	engine.ServeHTTP(listRecorder, listRequest)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse struct {
		Success bool `json:"success"`
		Data    []struct {
			ID       int    `json:"id"`
			BaseURL  string `json:"base_url"`
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	listedAccounts := make(map[string]int)
	for _, row := range listResponse.Data {
		if row.BaseURL == baseURL {
			listedAccounts[row.Username] = row.ID
		}
	}
	assert.Equal(t, map[string]int{
		"one@example.com": ids[0],
		"two@example.com": ids[1],
	}, listedAccounts)
}

func TestCreateUpstreamChannelRejectsInvalidConfiguration(t *testing.T) {
	engine, _ := setupUpstreamChannelControllerTest(t)
	tests := []struct {
		name    string
		payload string
		message string
	}{
		{name: "base url", payload: `{"base_url":"ftp://invalid.example","username":"root","password":"secret"}`, message: "invalid upstream base URL"},
		{name: "name", payload: `{"base_url":"https://manual.example","name":"` + strings.Repeat("n", 256) + `","username":"root","password":"secret"}`, message: "upstream channel name must not exceed 255 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, tt.message, response.Message)
		})
	}
}

func TestCreateUpstreamChannelStoresManagementAccessTokenWithoutExposingIt(t *testing.T) {
	t.Setenv("SESSION_SECRET", "persistent-session-secret")
	engine, _ := setupUpstreamChannelControllerTest(t)
	const managementToken = "management-secret"
	payload := `{"base_url":"https://token-auth.example","provider":"new-api","auth_type":"access_token","username":"42","password":"` + managementToken + `","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`
	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), managementToken)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ID          int    `json:"id"`
			AuthType    string `json:"auth_type"`
			HasPassword bool   `json:"has_password"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, model.UpstreamAuthTypeAccessToken, response.Data.AuthType)
	assert.True(t, response.Data.HasPassword)

	created, err := model.GetUpstreamChannelByID(response.Data.ID)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamAuthTypeAccessToken, created.EffectiveAuthType())
	storedToken, err := created.DecryptPassword()
	require.NoError(t, err)
	assert.Equal(t, managementToken, storedToken)
}

func TestCreateUpstreamChannelStoresSub2APIAccessTokenWithoutExposingIt(t *testing.T) {
	t.Setenv("SESSION_SECRET", "persistent-session-secret")
	engine, _ := setupUpstreamChannelControllerTest(t)
	const accessToken = "sub2-access-secret"
	payload := `{"base_url":"https://sub2-token.example","provider":"sub2api","auth_type":"access_token","username":"owner@example.com","password":"` + accessToken + `","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`
	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), accessToken)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ID          int    `json:"id"`
			Provider    string `json:"provider"`
			AuthType    string `json:"auth_type"`
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, service.UpstreamProviderSub2API, response.Data.Provider)
	assert.Equal(t, model.UpstreamAuthTypeAccessToken, response.Data.AuthType)
	assert.Equal(t, "owner@example.com", response.Data.Username)
	assert.True(t, response.Data.HasPassword)

	created, err := model.GetUpstreamChannelByID(response.Data.ID)
	require.NoError(t, err)
	storedToken, err := created.DecryptPassword()
	require.NoError(t, err)
	assert.Equal(t, accessToken, storedToken)
}

func TestCreateUpstreamChannelRejectsInvalidManagementAccessTokenConfiguration(t *testing.T) {
	engine, _ := setupUpstreamChannelControllerTest(t)
	tests := []struct {
		name    string
		payload string
		message string
	}{
		{
			name:    "provider",
			payload: `{"base_url":"https://auto-token.example","provider":"auto","auth_type":"access_token","username":"42","password":"token","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`,
			message: "access token authentication is only supported for new-api and sub2api upstream channels",
		},
		{
			name:    "numeric user id",
			payload: `{"base_url":"https://invalid-user.example","provider":"new-api","auth_type":"access_token","username":"yunqi","password":"token","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`,
			message: "management access token authentication requires a positive numeric upstream user ID",
		},
		{
			name:    "token",
			payload: `{"base_url":"https://missing-token.example","provider":"new-api","auth_type":"access_token","username":"42","password":"","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`,
			message: "enter a new password or access token when changing the authentication method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, tt.message, response.Message)
		})
	}
}

func TestUpdateUpstreamChannelRequiresNewSecretWhenAuthenticationMethodChanges(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	encrypted, err := common.EncryptSecret("upstream-channel-password", "saved-password")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(row).Updates(map[string]any{
		"provider":            service.UpstreamProviderNewAPI,
		"auth_type":           model.UpstreamAuthTypePassword,
		"username":            "yunqi",
		"password_ciphertext": encrypted,
	}).Error)

	payload := `{"provider":"new-api","auth_type":"access_token","username":"42","password":"","balance_threshold":0,"multiplier":1,"auto_refresh_interval":300}`
	request := httptest.NewRequest(http.MethodPut, "/api/upstream-channels/"+strconv.Itoa(row.Id), bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "enter a new password or access token when changing the authentication method", response.Message)
}

func TestCreateUpstreamChannelAllowsSavingWithoutCredentialsOrPersistentCryptoSecret(t *testing.T) {
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("CRYPTO_SECRET", "")
	engine, _ := setupUpstreamChannelControllerTest(t)
	payload := `{"base_url":"https://aimuxr.com","name":"","provider":"auto","username":"","password":"","balance_threshold":0,"auto_refresh_interval":300}`
	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "aimuxr", response.Data.Name)
	assert.Empty(t, response.Data.Username)
	assert.False(t, response.Data.HasPassword)
	assert.Equal(t, model.UpstreamChannelStatusUnconfigured, response.Data.Status)

	created, err := model.GetUpstreamChannelByID(response.Data.ID)
	require.NoError(t, err)
	assert.Empty(t, created.PasswordCiphertext)
}

func TestUpdateUpstreamChannelConfigRejectsInvalidValues(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	tests := []struct {
		name    string
		payload string
		message string
	}{
		{
			name:    "provider",
			payload: `{"provider":"unknown","username":"root","balance_threshold":0,"auto_refresh_interval":300}`,
			message: "provider must be auto, new-api, sub2api, or other",
		},
		{
			name:    "priority",
			payload: `{"provider":"new-api","priority":2147483648,"username":"root","balance_threshold":0,"auto_refresh_interval":300}`,
			message: "upstream channel priority must be between -2147483648 and 2147483647",
		},
		{
			name:    "threshold",
			payload: `{"provider":"new-api","username":"root","balance_threshold":-1,"auto_refresh_interval":300}`,
			message: "balance threshold must be between 0 and 1000000000",
		},
		{
			name:    "multiplier zero",
			payload: `{"provider":"new-api","username":"root","balance_threshold":0,"multiplier":0,"auto_refresh_interval":300}`,
			message: "channel multiplier must be greater than 0, at most 1000000000, and have no more than 2 decimal places",
		},
		{
			name:    "multiplier precision",
			payload: `{"provider":"new-api","username":"root","balance_threshold":0,"multiplier":1.001,"auto_refresh_interval":300}`,
			message: "channel multiplier must be greater than 0, at most 1000000000, and have no more than 2 decimal places",
		},
		{
			name:    "refresh interval",
			payload: `{"provider":"new-api","username":"root","balance_threshold":0,"auto_refresh_interval":59}`,
			message: "auto refresh interval must be 0 or between 60 and 86400 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/upstream-channels/"+strconv.Itoa(row.Id), bytes.NewBufferString(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, tt.message, response.Message)
		})
	}
}

func TestUpdateUpstreamChannelConfigEncryptsPasswordAndDoesNotExposeIt(t *testing.T) {
	t.Setenv("SESSION_SECRET", "persistent-session-secret")
	engine, row := setupUpstreamChannelControllerTest(t)
	payload := `{"name":"Primary upstream","provider":"other","priority":9,"username":"root","password":"plain-text-password","balance_threshold":10,"multiplier":1.25,"auto_refresh_interval":600}`
	request := httptest.NewRequest(http.MethodPut, "/api/upstream-channels/"+strconv.Itoa(row.Id), bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "plain-text-password")
	assert.NotContains(t, strings.ToLower(recorder.Body.String()), "ciphertext")
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string  `json:"name"`
			HasPassword bool    `json:"has_password"`
			Multiplier  float64 `json:"multiplier"`
			Provider    string  `json:"provider"`
			Priority    int64   `json:"priority"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "Primary upstream", response.Data.Name)
	assert.True(t, response.Data.HasPassword)
	assert.Equal(t, 1.25, response.Data.Multiplier)
	assert.Equal(t, "other", response.Data.Provider)
	assert.Equal(t, int64(9), response.Data.Priority)

	updated, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.NotEmpty(t, updated.PasswordCiphertext)
	assert.NotEqual(t, "plain-text-password", updated.PasswordCiphertext)
	assert.Equal(t, "Primary upstream", updated.Name)
	assert.Equal(t, 1.25, updated.Multiplier)
	assert.Equal(t, "other", updated.Provider)
	assert.Equal(t, int64(9), updated.Priority)
	password, err := updated.DecryptPassword()
	require.NoError(t, err)
	assert.Equal(t, "plain-text-password", password)
}

func TestUpdateUpstreamChannelConfigRejectsPasswordWhenCryptoSecretIsEphemeral(t *testing.T) {
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("CRYPTO_SECRET", "")
	engine, row := setupUpstreamChannelControllerTest(t)
	payload := `{"provider":"new-api","username":"root","password":"plain-text-password","balance_threshold":0,"auto_refresh_interval":300}`
	request := httptest.NewRequest(http.MethodPut, "/api/upstream-channels/"+strconv.Itoa(row.Id), bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "SESSION_SECRET or CRYPTO_SECRET must be configured before saving upstream passwords", response.Message)
}

func TestUpdateUpstreamChannelNoteTrimsAndPersists(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/note", bytes.NewBufferString(`{"note":"  billing owner  "}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Note string `json:"note"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "billing owner", response.Data.Note)
	updated, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, "billing owner", updated.Note)
}

func TestUpdateUpstreamChannelNoteRejectsOversizedValue(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/note", bytes.NewBufferString(`{"note":"`+strings.Repeat("x", upstreamChannelNoteMaxLength+1)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, errInvalidUpstreamNote.Error(), response.Message)
}

func TestImportUpstreamChannelKeysRejectsInvalidConfiguration(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	tests := []struct {
		name    string
		payload string
		message string
	}{
		{name: "empty groups", payload: `{"key_ids":[1],"groups":[]}`, message: "select at least one channel group"},
		{name: "invalid auto ban", payload: `{"key_ids":[1],"auto_ban":2}`, message: "auto ban must be 0 or 1"},
		{name: "negative weight", payload: `{"key_ids":[1],"weight":-1}`, message: "channel weight must be between 0 and 2147483647"},
		{name: "invalid key id", payload: `{"key_ids":[0]}`, message: "upstream key ids must be positive"},
		{name: "long remark", payload: `{"key_ids":[1],"remark":"` + strings.Repeat("r", 256) + `"}`, message: "channel remark must not exceed 255 characters"},
		{name: "invalid model name", payload: `{"key_ids":[1],"models":["gpt-4o,bad"]}`, message: "model names must not contain commas"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/keys/import", bytes.NewBufferString(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, tt.message, response.Message)
		})
	}
}

func TestPinUpstreamChannelRaisesPriority(t *testing.T) {
	engine, row := setupUpstreamChannelControllerTest(t)
	other := &model.UpstreamChannel{
		BaseURL:     "https://higher.example",
		BaseURLHash: model.UpstreamBaseURLHash("https://higher.example"),
		Priority:    8,
	}
	require.NoError(t, model.DB.Create(other).Error)

	request := httptest.NewRequest(http.MethodPost, "/api/upstream-channels/"+strconv.Itoa(row.Id)+"/pin", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Priority int64 `json:"priority"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, int64(9), response.Data.Priority)
}
