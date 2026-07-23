package service

import (
	"context"
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

	snapshot := UpstreamSnapshot{Keys: []UpstreamKey{
		{ID: 1, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://upstream.example", "sk-precedence")},
		{ID: 2, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://upstream.example", "sk-auto")},
		{ID: 3, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://upstream.example", "sk-disabled")},
		{ID: 4, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://other.example", "sk-other")},
		{ID: 5, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://upstream.example", "sk-multi")},
		{ID: 6, KeyFingerprint: model.UpstreamChannelKeyFingerprint("https://upstream.example", "sk-precedence")},
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

	const fullKey = "sk-link-secret-value"
	localBaseURL := "https://UPSTREAM.example:443/api/v1/"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &localBaseURL, Key: fullKey, Status: common.ChannelStatusEnabled}).Error)

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":1000000}}`, nil), nil
		case "/api/token":
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":1,"items":[{"id":7,"name":"linked key","key":"sk-link...alue","group":"default","status":1}]}}`, nil), nil
		case "/api/token/7/key":
			require.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			assert.Equal(t, "42", request.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"`+fullKey+`"}}`, nil), nil
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
	assert.Equal(t, model.UpstreamChannelKeyFingerprint("https://upstream.example", fullKey), snapshot.Keys[0].KeyFingerprint)
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
