package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRefreshUpstreamChannelPersistsTurnstileRecoveryState(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalHTTPClient := httpClient
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "refresh-turnstile.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "refresh-turnstile-test-secret"
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/user/login", request.URL.Path)
		return jsonResponse(http.StatusOK, `{"success":false,"message":"Turnstile token 为空"}`, nil), nil
	})}
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		httpClient = originalHTTPClient
		common.CryptoSecret = originalCryptoSecret
	})

	encrypted, err := common.EncryptSecret("upstream-channel-password", "plain-text-password")
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		BaseURL:             "https://upstream.test",
		BaseURLHash:         model.UpstreamBaseURLHash("https://upstream.test"),
		Provider:            UpstreamProviderNewAPI,
		AuthType:            model.UpstreamAuthTypePassword,
		Username:            "yunqi",
		PasswordCiphertext:  encrypted,
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusUnconfigured,
	}
	require.NoError(t, db.Create(row).Error)

	refreshed, _, err := RefreshUpstreamChannel(context.Background(), row.Id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNewAPITurnstileRequiresAccessToken))
	require.NotNil(t, refreshed)
	assert.Equal(t, model.UpstreamChannelStatusError, refreshed.Status)
	assert.Equal(t, ErrNewAPITurnstileRequiresAccessToken.Error(), refreshed.LastError)
	assert.Equal(t, UpstreamErrorCodeTurnstileRequiresAccessToken, UpstreamErrorCodeFromMessage(refreshed.LastError))
	assert.NotZero(t, refreshed.LastSyncTime)
}

func TestNormalizeUpstreamBaseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "new api v1", in: " HTTPS://Example.COM:443/v1/ ", want: "https://example.com"},
		{name: "new api api", in: "https://example.com/api", want: "https://example.com"},
		{name: "sub2 api v1", in: "https://example.com/panel/api/v1/", want: "https://example.com/panel"},
		{name: "keeps deployment path", in: "http://127.0.0.1:3000/gateway/", want: "http://127.0.0.1:3000/gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeUpstreamBaseURL(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeUpstreamBaseURLRejectsUnsupportedScheme(t *testing.T) {
	_, err := NormalizeUpstreamBaseURL("file:///etc/passwd")
	require.Error(t, err)
}

func TestCollectExplicitUpstreamBaseURLsDeduplicates(t *testing.T) {
	urls := CollectExplicitUpstreamBaseURLs([]string{
		"https://example.com/v1",
		"https://EXAMPLE.com/api/",
		"",
		"https://second.example/api/v1",
	})

	require.Equal(t, []string{
		"https://example.com",
		"https://second.example",
	}, urls)
}

func TestBalanceNotificationTransition(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		balance   float64
		notified  bool
		wantSend  bool
		wantState bool
	}{
		{name: "zero disables notification", threshold: 0, balance: -1, notified: false, wantSend: false, wantState: false},
		{name: "first low balance sends", threshold: 10, balance: 9.99, notified: false, wantSend: true, wantState: true},
		{name: "repeated low balance is deduplicated", threshold: 10, balance: 5, notified: true, wantSend: false, wantState: true},
		{name: "recovery resets state", threshold: 10, balance: 10, notified: true, wantSend: false, wantState: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			send, state := BalanceNotificationTransition(tt.threshold, tt.balance, tt.notified)
			assert.Equal(t, tt.wantSend, send)
			assert.Equal(t, tt.wantState, state)
		})
	}
}

func TestDiscoverUpstreamChannelsDeduplicatesExplicitChannelBaseURLs(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "discover-upstream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	firstURL := "https://EXAMPLE.com/api/v1"
	secondURL := "https://example.com/v1"
	thirdURL := "https://second.example"
	require.NoError(t, db.Create(&model.Channel{Key: "first", BaseURL: &firstURL, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Key: "second", BaseURL: &secondURL, Status: common.ChannelStatusManuallyDisabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Key: "third", BaseURL: &thirdURL, Status: common.ChannelStatusAutoDisabled}).Error)

	firstHash := model.UpstreamBaseURLHash("https://example.com")
	require.NoError(t, db.Create(&model.UpstreamChannel{
		Name:                "Primary upstream",
		BaseURL:             "https://example.com",
		BaseURLHash:         firstHash,
		Provider:            "sub2api",
		Username:            "owner@example.com",
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusUnconfigured,
	}).Error)
	require.NoError(t, db.Create(&model.UpstreamChannel{
		Name:                "Manual upstream",
		BaseURL:             "https://manual.example",
		BaseURLHash:         model.UpstreamBaseURLHash("https://manual.example"),
		Provider:            UpstreamProviderAuto,
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusUnconfigured,
	}).Error)

	rows, stats, err := DiscoverUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, 2, stats["https://example.com"].Total)
	assert.Zero(t, stats["https://example.com"].Active)
	assert.Equal(t, 1, stats["https://second.example"].Total)
	assert.Zero(t, stats["https://second.example"].Active)
	assert.Zero(t, stats["https://manual.example"].Total)
	assert.Zero(t, stats["https://manual.example"].Active)
	assert.Equal(t, "Primary upstream", rows[0].Name)
	assert.Equal(t, "sub2api", rows[0].Provider)
	assert.Equal(t, "owner@example.com", rows[0].Username)
}

func TestDiscoverUpstreamChannelsCountsOnlyEnabledSnapshotKeysAsActive(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "discover-active-upstream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{Key: "sk-enabled", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Key: "sk-disabled", BaseURL: &baseURL, Status: common.ChannelStatusManuallyDisabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Key: "sk-unrelated", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}).Error)

	snapshotJSON, err := common.Marshal(UpstreamSnapshot{Keys: []UpstreamKey{
		{ID: 1, KeyFingerprint: model.UpstreamKeyFingerprint("sk-enabled")},
		{ID: 2, KeyFingerprint: model.UpstreamKeyFingerprint("sk-disabled")},
	}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.UpstreamChannel{
		BaseURL:             baseURL,
		BaseURLHash:         model.UpstreamBaseURLHash(baseURL),
		Provider:            UpstreamProviderNewAPI,
		SnapshotJSON:        string(snapshotJSON),
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusReady,
	}).Error)

	rows, stats, err := DiscoverUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, stats[baseURL].Total)
	assert.Equal(t, 1, stats[baseURL].Active)

	var snapshot UpstreamSnapshot
	require.NoError(t, common.UnmarshalJsonStr(rows[0].SnapshotJSON, &snapshot))
	require.Len(t, snapshot.Keys, 2)
	assert.True(t, snapshot.Keys[0].Imported)
	assert.True(t, snapshot.Keys[0].Active)
	assert.True(t, snapshot.Keys[1].Imported)
	assert.False(t, snapshot.Keys[1].Active)

	require.NoError(t, db.Model(&model.Channel{}).Where("key = ?", "sk-enabled").Update("status", common.ChannelStatusManuallyDisabled).Error)
	rows, stats, err = DiscoverUpstreamChannels()
	require.NoError(t, err)
	assert.Zero(t, stats[baseURL].Active)
	require.NoError(t, common.UnmarshalJsonStr(rows[0].SnapshotJSON, &snapshot))
	assert.False(t, snapshot.Keys[0].Active)

	require.NoError(t, db.Model(&model.Channel{}).Where("key = ?", "sk-enabled").Update("status", common.ChannelStatusAutoDisabled).Error)
	rows, stats, err = DiscoverUpstreamChannels()
	require.NoError(t, err)
	assert.Zero(t, stats[baseURL].Active)
	require.NoError(t, common.UnmarshalJsonStr(rows[0].SnapshotJSON, &snapshot))
	assert.False(t, snapshot.Keys[0].Active)

	require.NoError(t, db.Model(&model.Channel{}).Where("key = ?", "sk-enabled").Update("status", common.ChannelStatusEnabled).Error)
	rows, stats, err = DiscoverUpstreamChannels()
	require.NoError(t, err)
	assert.Equal(t, 1, stats[baseURL].Active)
	require.NoError(t, common.UnmarshalJsonStr(rows[0].SnapshotJSON, &snapshot))
	assert.True(t, snapshot.Keys[0].Active)
}

func TestMarkImportedUpstreamKeysUsesBaseURLAndFullKeyFingerprint(t *testing.T) {
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mark-imported-upstream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	baseURL := "https://upstream.example"
	otherBaseURL := "https://other.example"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: "sk-imported", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: "sk-disabled", Status: common.ChannelStatusManuallyDisabled}).Error)
	require.NoError(t, db.Create(&model.Channel{BaseURL: &otherBaseURL, Key: "sk-other", Status: common.ChannelStatusEnabled}).Error)

	snapshot := UpstreamSnapshot{Keys: []UpstreamKey{
		{ID: 1, KeyFingerprint: upstreamKeyFingerprint("sk-imported")},
		{ID: 2, KeyFingerprint: upstreamKeyFingerprint("sk-disabled")},
		{ID: 3, KeyFingerprint: upstreamKeyFingerprint("sk-not-imported")},
		{ID: 4, KeyFingerprint: upstreamKeyFingerprint("sk-other")},
	}}
	require.NoError(t, markImportedUpstreamKeys(baseURL, &snapshot))
	assert.True(t, snapshot.Keys[0].Imported)
	assert.True(t, snapshot.Keys[0].Active)
	assert.True(t, snapshot.Keys[1].Imported)
	assert.False(t, snapshot.Keys[1].Active)
	assert.False(t, snapshot.Keys[2].Imported)
	assert.False(t, snapshot.Keys[2].Active)
	assert.False(t, snapshot.Keys[3].Imported)
	assert.False(t, snapshot.Keys[3].Active)

	encoded, err := common.Marshal(snapshot)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "sk-imported")
	assert.Contains(t, string(encoded), "key_fingerprint")
}

func TestMarkImportedUpstreamKeysPreservesLegacyImportedState(t *testing.T) {
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mark-legacy-imported-upstream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: "sk-enabled", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{BaseURL: &baseURL, Key: "sk-disabled", Status: common.ChannelStatusManuallyDisabled}).Error)

	snapshot := UpstreamSnapshot{Keys: []UpstreamKey{
		{ID: 1, Imported: true},
		{ID: 2, Imported: true},
		{ID: 3, Imported: false},
	}}
	require.NoError(t, markImportedUpstreamKeys(baseURL, &snapshot))
	assert.True(t, snapshot.Keys[0].Imported)
	assert.True(t, snapshot.Keys[0].Active)
	assert.True(t, snapshot.Keys[1].Imported)
	assert.False(t, snapshot.Keys[1].Active)
	assert.False(t, snapshot.Keys[2].Imported)
	assert.False(t, snapshot.Keys[2].Active)
}

func TestImportUpstreamChannelKeysCreatesAndOverwritesChannels(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "import-upstream.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "import-upstream-test-secret"
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.CryptoSecret = originalCryptoSecret
	})

	baseURL := "https://upstream.test"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/user/login":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42}}`, nil), nil
		case "/api/token/7/key":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-imported-key"}}`, nil), nil
		case "/api/token/8/key":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-disabled-key"}}`, nil), nil
		case "/v1/models":
			switch r.Header.Get("Authorization") {
			case "Bearer sk-imported-key":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`, nil), nil
			case "Bearer sk-disabled-key":
				return jsonResponse(http.StatusOK, `{"data":[]}`, nil), nil
			default:
				return jsonResponse(http.StatusUnauthorized, `{}`, nil), nil
			}
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	passwordCiphertext, err := common.EncryptSecret("upstream-channel-password", "secret")
	require.NoError(t, err)
	snapshotJSON, err := common.Marshal(UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Keys: []UpstreamKey{
			{ID: 7, Name: "team-key"},
			{ID: 8},
		},
	})
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		Name:               "Friendly upstream",
		Priority:           17,
		BaseURL:            baseURL,
		BaseURLHash:        model.UpstreamBaseURLHash(baseURL),
		Provider:           UpstreamProviderNewAPI,
		Username:           "root",
		PasswordCiphertext: passwordCiphertext,
		SnapshotJSON:       string(snapshotJSON),
		Status:             model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(row).Error)

	groups := []string{" premium ", "default", "premium"}
	tag := " imported-tag "
	namePrefix := " Imported "
	priority := int64(12)
	weight := int64(34)
	testModel := " gpt-4o "
	autoBan := 0
	remark := " managed import "
	result, err := importUpstreamChannelKeys(context.Background(), client, row.Id, UpstreamKeyImportOptions{
		KeyIDs:     []int64{7, 7},
		Groups:     &groups,
		Tag:        &tag,
		NamePrefix: &namePrefix,
		Priority:   &priority,
		Weight:     &weight,
		TestModel:  &testModel,
		AutoBan:    &autoBan,
		Remark:     &remark,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Imported)
	assert.Equal(t, 0, result.Skipped)
	require.Len(t, result.ChannelIDs, 1)

	var imported model.Channel
	require.NoError(t, db.First(&imported, result.ChannelIDs[0]).Error)
	assert.Equal(t, constant.ChannelTypeOpenAI, imported.Type)
	assert.Equal(t, "sk-imported-key", imported.Key)
	assert.Equal(t, common.ChannelStatusEnabled, imported.Status)
	assert.Equal(t, "gpt-4o,gpt-4o-mini", imported.Models)
	assert.Equal(t, "premium,default", imported.Group)
	assert.Equal(t, "imported-tag", imported.GetTag())
	assert.Equal(t, "Imported-team-key", imported.Name)
	assert.Equal(t, int64(12), imported.GetPriority())
	assert.Equal(t, 34, imported.GetWeight())
	require.NotNil(t, imported.TestModel)
	assert.Equal(t, "gpt-4o", *imported.TestModel)
	assert.False(t, imported.GetAutoBan())
	require.NotNil(t, imported.Remark)
	assert.Equal(t, "managed import", *imported.Remark)
	assert.Equal(t, baseURL, imported.GetBaseURL())
	encodedResult, err := common.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encodedResult), "sk-imported-key")
	updatedRow, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	var updatedSnapshot UpstreamSnapshot
	require.NoError(t, common.UnmarshalJsonStr(updatedRow.SnapshotJSON, &updatedSnapshot))
	require.Len(t, updatedSnapshot.Keys, 2)
	assert.Equal(t, model.UpstreamKeyFingerprint("sk-imported-key"), updatedSnapshot.Keys[0].KeyFingerprint)
	assert.True(t, updatedSnapshot.Keys[0].Imported)
	assert.True(t, updatedSnapshot.Keys[0].Active)
	assert.Empty(t, updatedSnapshot.Keys[1].KeyFingerprint)
	assert.False(t, updatedSnapshot.Keys[1].Imported)
	assert.False(t, updatedSnapshot.Keys[1].Active)

	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", imported.Id).Updates(map[string]any{
		"test_time":            int64(101),
		"response_time":        202,
		"balance":              303.5,
		"balance_updated_time": int64(404),
		"used_quota":           int64(505),
	}).Error)

	disabledResult, err := importUpstreamChannelKeys(context.Background(), client, row.Id, UpstreamKeyImportOptions{
		KeyIDs: []int64{8},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, disabledResult.Imported)
	assert.Equal(t, 1, disabledResult.Disabled)
	require.Len(t, disabledResult.ChannelIDs, 1)
	var disabledChannel model.Channel
	require.NoError(t, db.First(&disabledChannel, disabledResult.ChannelIDs[0]).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, disabledChannel.Status)
	assert.Equal(t, "Friendly upstream-8", disabledChannel.Name)
	assert.Equal(t, "default", disabledChannel.Group)
	assert.Equal(t, "Friendly upstream", disabledChannel.GetTag())
	assert.Equal(t, int64(17), disabledChannel.GetPriority())
	assert.Zero(t, disabledChannel.GetWeight())
	assert.Nil(t, disabledChannel.TestModel)
	assert.True(t, disabledChannel.GetAutoBan())
	require.NotNil(t, disabledChannel.Remark)
	assert.Empty(t, *disabledChannel.Remark)
	disabledJSON, err := common.Marshal(disabledResult)
	require.NoError(t, err)
	assert.NotContains(t, string(disabledJSON), "sk-disabled-key")
	updatedRow, err = model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalJsonStr(updatedRow.SnapshotJSON, &updatedSnapshot))
	assert.Equal(t, model.UpstreamKeyFingerprint("sk-disabled-key"), updatedSnapshot.Keys[1].KeyFingerprint)
	assert.True(t, updatedSnapshot.Keys[1].Imported)
	assert.False(t, updatedSnapshot.Keys[1].Active)

	replacementTag := "replacement-tag"
	replacementPrefix := "Replacement"
	replacementPriority := int64(23)
	replacementWeight := int64(45)
	replacementTestModel := "gpt-4o-mini"
	replacementAutoBan := 1
	replacementRemark := "replacement remark"
	second, err := importUpstreamChannelKeys(context.Background(), client, row.Id, UpstreamKeyImportOptions{
		KeyIDs:     []int64{7},
		Groups:     &[]string{"replacement"},
		Tag:        &replacementTag,
		NamePrefix: &replacementPrefix,
		Priority:   &replacementPriority,
		Weight:     &replacementWeight,
		TestModel:  &replacementTestModel,
		Models:     &[]string{"gpt-4o-mini"},
		AutoBan:    &replacementAutoBan,
		Remark:     &replacementRemark,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, second.Imported)
	assert.Equal(t, 1, second.Updated)
	assert.Equal(t, 0, second.Skipped)
	assert.Equal(t, []int{imported.Id}, second.ChannelIDs)

	var overwritten model.Channel
	require.NoError(t, db.First(&overwritten, imported.Id).Error)
	assert.Equal(t, imported.Id, overwritten.Id)
	assert.Equal(t, imported.CreatedTime, overwritten.CreatedTime)
	assert.Equal(t, "Replacement-team-key", overwritten.Name)
	assert.Equal(t, "replacement", overwritten.Group)
	assert.Equal(t, "replacement-tag", overwritten.GetTag())
	assert.Equal(t, int64(23), overwritten.GetPriority())
	assert.Equal(t, 45, overwritten.GetWeight())
	assert.Equal(t, "gpt-4o-mini", overwritten.Models)
	require.NotNil(t, overwritten.TestModel)
	assert.Equal(t, "gpt-4o-mini", *overwritten.TestModel)
	assert.True(t, overwritten.GetAutoBan())
	require.NotNil(t, overwritten.Remark)
	assert.Equal(t, "replacement remark", *overwritten.Remark)
	assert.Equal(t, int64(101), overwritten.TestTime)
	assert.Equal(t, 202, overwritten.ResponseTime)
	assert.Equal(t, 303.5, overwritten.Balance)
	assert.Equal(t, int64(404), overwritten.BalanceUpdatedTime)
	assert.Equal(t, int64(505), overwritten.UsedQuota)

	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("base_url = ?", baseURL).Where(&model.Channel{Key: "sk-imported-key"}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)

	var abilities []model.Ability
	require.NoError(t, db.Where("channel_id = ?", imported.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "replacement", abilities[0].Group)
	assert.Equal(t, "gpt-4o-mini", abilities[0].Model)
	assert.Equal(t, "replacement-tag", *abilities[0].Tag)

	overwrittenJSON, err := common.Marshal(second)
	require.NoError(t, err)
	assert.NotContains(t, string(overwrittenJSON), "sk-imported-key")

	emptyModels := []string{}
	third, err := importUpstreamChannelKeys(context.Background(), client, row.Id, UpstreamKeyImportOptions{
		KeyIDs: []int64{7},
		Models: &emptyModels,
	})
	require.NoError(t, err)
	assert.Zero(t, third.Imported)
	assert.Equal(t, 1, third.Updated)
	assert.Equal(t, 1, third.Disabled)
	assert.Equal(t, []int{imported.Id}, third.ChannelIDs)

	var disabledOverwrite model.Channel
	require.NoError(t, db.First(&disabledOverwrite, imported.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, disabledOverwrite.Status)
	assert.Empty(t, disabledOverwrite.Models)
	assert.Equal(t, int64(101), disabledOverwrite.TestTime)
	assert.Equal(t, 202, disabledOverwrite.ResponseTime)
	assert.Equal(t, 303.5, disabledOverwrite.Balance)
	assert.Equal(t, int64(404), disabledOverwrite.BalanceUpdatedTime)
	assert.Equal(t, int64(505), disabledOverwrite.UsedQuota)

	var remainingAbilities int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", imported.Id).Count(&remainingAbilities).Error)
	assert.Zero(t, remainingAbilities)
}

func TestFetchUpstreamChannelKeyModelsMergesSelectedKeys(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalCryptoSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fetch-upstream-models.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "fetch-upstream-models-test-secret"
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.CryptoSecret = originalCryptoSecret
	})

	baseURL := "https://upstream.test"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/user/login":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42}}`, nil), nil
		case "/api/token/7/key":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-first"}}`, nil), nil
		case "/api/token/8/key":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-second"}}`, nil), nil
		case "/v1/models":
			switch r.Header.Get("Authorization") {
			case "Bearer sk-first":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o"},{"id":"shared"}]}`, nil), nil
			case "Bearer sk-second":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"claude-3-5-sonnet"},{"id":"shared"}]}`, nil), nil
			}
		}
		return jsonResponse(http.StatusNotFound, `{}`, nil), nil
	})}

	passwordCiphertext, err := common.EncryptSecret("upstream-channel-password", "secret")
	require.NoError(t, err)
	snapshotJSON, err := common.Marshal(UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Keys: []UpstreamKey{
			{ID: 7, Name: "first"},
			{ID: 8, Name: "second"},
		},
	})
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		BaseURL:            baseURL,
		BaseURLHash:        model.UpstreamBaseURLHash(baseURL),
		Provider:           UpstreamProviderNewAPI,
		Username:           "root",
		PasswordCiphertext: passwordCiphertext,
		SnapshotJSON:       string(snapshotJSON),
		Status:             model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(row).Error)

	models, err := fetchUpstreamChannelKeyModels(context.Background(), client, row.Id, []int64{8, 7, 8})
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-3-5-sonnet", "gpt-4o", "shared"}, models)

	_, err = fetchUpstreamChannelKeyModels(context.Background(), client, row.Id, []int64{9})
	require.EqualError(t, err, "upstream key 9 is not present in the latest snapshot")
}

func TestNormalizeUpstreamKeyImportOptionsUsesDefaults(t *testing.T) {
	normalized, err := normalizeUpstreamKeyImportOptions(UpstreamKeyImportOptions{
		KeyIDs: []int64{9, 9},
	}, "https://API.Example.com/v1", "Friendly upstream", 23)
	require.NoError(t, err)
	assert.Equal(t, []int64{9}, normalized.KeyIDs)
	assert.Equal(t, "default", normalized.Group)
	assert.Equal(t, "Friendly upstream", normalized.Tag)
	assert.Equal(t, "Friendly upstream", normalized.NamePrefix)
	assert.Equal(t, int64(23), normalized.Priority)
	assert.Zero(t, normalized.Weight)
	assert.Empty(t, normalized.TestModel)
	assert.Equal(t, 1, normalized.AutoBan)
	assert.Empty(t, normalized.Remark)

	customTag := "Custom tag"
	explicitPriority := int64(12)
	normalized, err = normalizeUpstreamKeyImportOptions(UpstreamKeyImportOptions{
		KeyIDs:   []int64{10},
		Tag:      &customTag,
		Priority: &explicitPriority,
	}, "https://API.Example.com/v1", "Friendly upstream", 23)
	require.NoError(t, err)
	assert.Equal(t, customTag, normalized.Tag)
	assert.Equal(t, customTag, normalized.NamePrefix)
	assert.Equal(t, explicitPriority, normalized.Priority)
}

func TestNormalizeUpstreamKeyImportOptionsRejectsInvalidValues(t *testing.T) {
	validID := []int64{1}
	emptyGroups := []string{" ", ""}
	negativeWeight := int64(-1)
	overflowWeight := int64(1 << 32)
	invalidAutoBan := 2
	emptyNamePrefix := " "
	longRemark := strings.Repeat("r", 256)
	longTestModel := strings.Repeat("m", 256)
	longGroup := []string{strings.Repeat("g", 65)}
	invalidModels := []string{"gpt-4o,bad"}
	longModels := []string{strings.Repeat("m", 256)}
	invalidPriority := int64(math.MaxInt32) + 1

	tests := []struct {
		name    string
		options UpstreamKeyImportOptions
		want    string
	}{
		{name: "missing key ids", options: UpstreamKeyImportOptions{}, want: "select at least one upstream key"},
		{name: "invalid key id", options: UpstreamKeyImportOptions{KeyIDs: []int64{0}}, want: "upstream key ids must be positive"},
		{name: "empty groups", options: UpstreamKeyImportOptions{KeyIDs: validID, Groups: &emptyGroups}, want: "select at least one channel group"},
		{name: "group storage length", options: UpstreamKeyImportOptions{KeyIDs: validID, Groups: &longGroup}, want: "channel groups must not exceed 64 characters"},
		{name: "empty name prefix", options: UpstreamKeyImportOptions{KeyIDs: validID, NamePrefix: &emptyNamePrefix}, want: "channel name prefix is required"},
		{name: "invalid priority", options: UpstreamKeyImportOptions{KeyIDs: validID, Priority: &invalidPriority}, want: "channel priority must be between -2147483648 and 2147483647"},
		{name: "negative weight", options: UpstreamKeyImportOptions{KeyIDs: validID, Weight: &negativeWeight}, want: "channel weight must be between 0 and 2147483647"},
		{name: "overflow weight", options: UpstreamKeyImportOptions{KeyIDs: validID, Weight: &overflowWeight}, want: "channel weight must be between 0 and 2147483647"},
		{name: "invalid auto ban", options: UpstreamKeyImportOptions{KeyIDs: validID, AutoBan: &invalidAutoBan}, want: "auto ban must be 0 or 1"},
		{name: "long test model", options: UpstreamKeyImportOptions{KeyIDs: validID, TestModel: &longTestModel}, want: "test model must not exceed 255 characters"},
		{name: "long remark", options: UpstreamKeyImportOptions{KeyIDs: validID, Remark: &longRemark}, want: "channel remark must not exceed 255 characters"},
		{name: "invalid model name", options: UpstreamKeyImportOptions{KeyIDs: validID, Models: &invalidModels}, want: "model names must not contain commas"},
		{name: "long model name", options: UpstreamKeyImportOptions{KeyIDs: validID, Models: &longModels}, want: "model names must not exceed 255 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeUpstreamKeyImportOptions(tt.options, "https://upstream.example", "Friendly upstream", 0)
			require.EqualError(t, err, tt.want)
		})
	}
}
