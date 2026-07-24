package service

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReconcileUpstreamKeyLinksNormalizesBaseURLAndAggregatesStatuses(t *testing.T) {
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reconcile-upstream-links.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	createChannel := func(baseURL string, key string, status int) {
		require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: key, Status: status}).Error)
	}
	createChannel("https://upstream.example/v1/", "sk-precedence", common.ChannelStatusManuallyDisabled)
	createChannel("https://UPSTREAM.example:443/api", "sk-precedence", common.ChannelStatusAutoDisabled)
	createChannel("https://upstream.example/api/v1", "sk-precedence", common.ChannelStatusEnabled)
	createChannel("https://upstream.example", "sk-auto", common.ChannelStatusAutoDisabled)
	createChannel("https://upstream.example/", "sk-disabled", common.ChannelStatusManuallyDisabled)
	createChannel("https://upstream.example", "sk-unused\nsk-multi", common.ChannelStatusEnabled)
	createChannel("https://other.example", "sk-other", common.ChannelStatusEnabled)

	snapshot := UpstreamSnapshot{Provider: UpstreamProviderNewAPI, Keys: []UpstreamKey{
		{ID: 1, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", "precedence")},
		{ID: 2, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", "auto")},
		{ID: 3, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", "disabled")},
		{ID: 4, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://other.example", "other")},
		{ID: 5, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", "multi")},
		{ID: 6, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", "precedence")},
	}}
	require.NoError(t, reconcileUpstreamKeyLinks("https://UPSTREAM.example:443/api/v1/", &snapshot))

	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[0].InUseStatus)
	assert.True(t, snapshot.Keys[0].Linked)
	assert.True(t, snapshot.Keys[0].Active)
	assert.Equal(t, UpstreamKeyInUseStatusAutoDisabled, snapshot.Keys[1].InUseStatus)
	assert.True(t, snapshot.Keys[1].Linked)
	assert.False(t, snapshot.Keys[1].Active)
	assert.Equal(t, UpstreamKeyInUseStatusDisabled, snapshot.Keys[2].InUseStatus)
	assert.True(t, snapshot.Keys[2].Linked)
	assert.Equal(t, UpstreamKeyInUseStatusUnlinked, snapshot.Keys[3].InUseStatus)
	assert.False(t, snapshot.Keys[3].Linked)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[4].InUseStatus)
	assert.True(t, snapshot.Keys[4].Linked)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[5].InUseStatus)

	summary := summarizeUpstreamKeyLinks(snapshot.Keys)
	assert.Equal(t, UpstreamKeyLinkSummary{Total: 5, Linked: 4, Enabled: 2, AutoDisabled: 1, Disabled: 1, Unlinked: 1}, summary)
	encoded, err := common.Marshal(snapshot)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "sk-precedence")
	assert.NotContains(t, string(encoded), "sk-auto")
}

func TestReconcileUpstreamKeyLinksKeepsSub2APIPrefixExact(t *testing.T) {
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reconcile-sub2api-prefix.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	baseURL := "https://sub2.example"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: "sk-strict", Status: common.ChannelStatusEnabled}).Error)
	snapshot := UpstreamSnapshot{Provider: UpstreamProviderSub2API, Keys: []UpstreamKey{
		{ID: 1, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderSub2API, baseURL, "strict")},
		{ID: 2, KeyFingerprint: UpstreamKeyFingerprintForProvider(UpstreamProviderSub2API, baseURL, "sk-strict")},
	}}

	require.NoError(t, reconcileUpstreamKeyLinks(baseURL, &snapshot))
	assert.False(t, snapshot.Keys[0].Linked)
	assert.Equal(t, UpstreamKeyInUseStatusUnlinked, snapshot.Keys[0].InUseStatus)
	assert.True(t, snapshot.Keys[1].Linked)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[1].InUseStatus)
}

func TestLinkUpstreamChannelKeysRecoversFingerprintsAndPersistsFreshState(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalHTTPClient := httpClient
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "link-upstream-keys.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "link-upstream-key-test-secret"
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		httpClient = originalHTTPClient
		common.CryptoSecret = originalCryptoSecret
	})

	const fullKey = "link-secret-value"
	localBaseURL := "https://UPSTREAM.example:443/api/v1/"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &localBaseURL, Key: "sk-" + fullKey, Status: common.ChannelStatusEnabled}).Error)

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":1000000}}`, nil), nil
		case "/api/token":
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":1,"items":[{"id":7,"name":"linked key","key":"link********alue","group":"default","status":1}]}}`, nil), nil
		case "/api/token/batch/keys":
			require.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			assert.Equal(t, []int64{7}, payload.IDs)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"7":"`+fullKey+`"}}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	encrypted, err := common.EncryptSecret("upstream-channel-password", "management-token")
	require.NoError(t, err)
	previousSnapshot, err := common.Marshal(UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Keys:     []UpstreamKey{{ID: 7, Imported: true}},
		Groups:   []UpstreamGroup{{Name: "default", Ratio: 1}},
		Ratios:   map[string]float64{"default": 1},
	})
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         model.UpstreamBaseURLHash("https://upstream.example"),
		Provider:            UpstreamProviderNewAPI,
		AuthType:            model.UpstreamAuthTypeAccessToken,
		Username:            "42",
		PasswordCiphertext:  encrypted,
		SnapshotJSON:        string(previousSnapshot),
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(row).Error)

	updatedRow, snapshot, summary, err := LinkUpstreamChannelKeys(context.Background(), row.Id)
	require.NoError(t, err)
	require.NotNil(t, updatedRow)
	require.Len(t, snapshot.Keys, 1)
	assert.Equal(t, UpstreamKeyFingerprintForProvider(UpstreamProviderNewAPI, "https://upstream.example", fullKey), snapshot.Keys[0].KeyFingerprint)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[0].InUseStatus)
	assert.True(t, snapshot.Keys[0].Linked)
	assert.Equal(t, UpstreamKeyLinkSummary{Total: 1, Linked: 1, Enabled: 1}, summary)

	var persisted UpstreamSnapshot
	require.NoError(t, common.UnmarshalJsonStr(updatedRow.SnapshotJSON, &persisted))
	require.Len(t, persisted.Keys, 1)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, persisted.Keys[0].InUseStatus)
	assert.Equal(t, "default", persisted.Keys[0].Group)
	assert.NotContains(t, updatedRow.SnapshotJSON, fullKey)
}

func TestLinkUpstreamChannelKeysRevealsOnlyPossibleLocalMatches(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalHTTPClient := httpClient
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "link-upstream-candidates.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "link-upstream-candidate-test-secret"
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		httpClient = originalHTTPClient
		common.CryptoSecret = originalCryptoSecret
	})

	localBaseURL := "https://UPSTREAM.example:443/api/v1/"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &localBaseURL, Key: "sk-first-matching-key-A001", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{BaseURL: &localBaseURL, Key: "sk-second-matching-key-B002", Status: common.ChannelStatusAutoDisabled}).Error)
	otherBaseURL := "https://other.example"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &otherBaseURL, Key: "sk-skip04-unused-Z004", Status: common.ChannelStatusEnabled}).Error)

	items := []map[string]any{
		{"id": 1, "name": "first", "key": "firs****...A001", "group": "default", "status": 1},
		{"id": 2, "name": "second", "key": "seco****...B002", "group": "default", "status": 1},
		{"id": 3, "name": "same visible edges", "key": "firs****...A001", "group": "default", "status": 1},
	}
	for keyID := 4; keyID <= 25; keyID++ {
		items = append(items, map[string]any{
			"id":     keyID,
			"name":   fmt.Sprintf("unrelated-%d", keyID),
			"key":    fmt.Sprintf("skip%02d****...Z%03d", keyID, keyID),
			"group":  "default",
			"status": 1,
		})
	}
	encodedKeyList, err := common.Marshal(map[string]any{
		"success": true,
		"data": map[string]any{
			"page":      1,
			"page_size": 100,
			"total":     len(items),
			"items":     items,
		},
	})
	require.NoError(t, err)

	requestedKeyIDs := make([]int64, 0, 3)
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":1000000}}`, nil), nil
		case "/api/token":
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, string(encodedKeyList), nil), nil
		case "/api/token/batch/keys":
			require.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			requestedKeyIDs = append(requestedKeyIDs, payload.IDs...)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"1":"first-matching-key-A001","2":"second-matching-key-B002","3":"first-different-key-A001"}}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	encrypted, err := common.EncryptSecret("upstream-channel-password", "management-token")
	require.NoError(t, err)
	previousSnapshot, err := common.Marshal(UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Groups:   []UpstreamGroup{{Name: "default", Ratio: 1}},
		Ratios:   map[string]float64{"default": 1},
	})
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         model.UpstreamBaseURLHash("https://upstream.example"),
		Provider:            UpstreamProviderNewAPI,
		AuthType:            model.UpstreamAuthTypeAccessToken,
		Username:            "42",
		PasswordCiphertext:  encrypted,
		SnapshotJSON:        string(previousSnapshot),
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(row).Error)

	updatedRow, snapshot, summary, err := LinkUpstreamChannelKeys(context.Background(), row.Id)
	require.NoError(t, err)
	require.NotNil(t, updatedRow)
	require.Len(t, snapshot.Keys, 25)
	assert.Equal(t, []int64{1, 2, 3}, requestedKeyIDs)
	assert.Equal(t, UpstreamKeyLinkSummary{Total: 25, Linked: 2, Enabled: 1, AutoDisabled: 1, Unlinked: 23}, summary)
	assert.Equal(t, UpstreamKeyInUseStatusEnabled, snapshot.Keys[0].InUseStatus)
	assert.Equal(t, UpstreamKeyInUseStatusAutoDisabled, snapshot.Keys[1].InUseStatus)
	assert.Equal(t, UpstreamKeyInUseStatusUnlinked, snapshot.Keys[2].InUseStatus)
	for _, key := range snapshot.Keys[3:] {
		assert.Empty(t, key.KeyFingerprint)
		assert.Equal(t, UpstreamKeyInUseStatusUnlinked, key.InUseStatus)
	}
}
