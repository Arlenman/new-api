package model

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyUpstreamChannelForMigration struct {
	ID          int    `gorm:"primaryKey"`
	BaseURLHash string `gorm:"type:char(64);uniqueIndex"`
}

func (legacyUpstreamChannelForMigration) TableName() string {
	return "upstream_channels"
}

func setupUpstreamChannelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-channel.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UpstreamChannel{}))
	DB = db
	t.Cleanup(func() { DB = originalDB })
	return db
}

func TestMigrateUpstreamChannelBaseURLHashIndexAllowsDuplicateBaseURLs(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-channel-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { DB = originalDB })

	require.NoError(t, db.AutoMigrate(&legacyUpstreamChannelForMigration{}))
	require.True(t, db.Migrator().HasIndex(&legacyUpstreamChannelForMigration{}, legacyUpstreamChannelBaseURLHashIndex))
	require.NoError(t, db.AutoMigrate(&UpstreamChannel{}))
	require.NoError(t, migrateUpstreamChannelBaseURLHashIndex(db))

	assert.False(t, db.Migrator().HasIndex(&UpstreamChannel{}, legacyUpstreamChannelBaseURLHashIndex))
	assert.True(t, db.Migrator().HasIndex(&UpstreamChannel{}, "idx_upstream_channel_base_url_hash"))
	require.NoError(t, db.Create(&UpstreamChannel{
		BaseURL:     "https://migration.example",
		BaseURLHash: UpstreamBaseURLHash("https://migration.example"),
		Username:    "first@example.com",
	}).Error)
	require.NoError(t, db.Create(&UpstreamChannel{
		BaseURL:     "https://migration.example",
		BaseURLHash: UpstreamBaseURLHash("https://migration.example"),
		Username:    "second@example.com",
	}).Error)
}

func TestEnsureUpstreamChannelsCreatesDefaultsAndPreservesConfiguration(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)

	rows, err := EnsureUpstreamChannels([]string{"https://api.xtokenmirror.cn", "https://api.syncapi.dpdns.org"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "xtokenmirror", rows[0].Name)
	assert.Equal(t, "syncapi", rows[1].Name)
	assert.Equal(t, "auto", rows[0].Provider)
	assert.Equal(t, 300, rows[0].AutoRefreshInterval)
	assert.Equal(t, float64(UpstreamChannelDefaultMultiplier), rows[0].Multiplier)
	assert.Equal(t, UpstreamChannelStatusUnconfigured, rows[0].Status)

	require.NoError(t, db.Model(&UpstreamChannel{}).Where("id = ?", rows[0].Id).Update("name", "api.xtokenmirror.cn").Error)
	rows, err = EnsureUpstreamChannels([]string{"https://api.xtokenmirror.cn"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "xtokenmirror", rows[0].Name)

	require.NoError(t, db.Model(&UpstreamChannel{}).Where("id = ?", rows[0].Id).Updates(map[string]any{
		"name":                "Primary upstream",
		"provider":            "sub2api",
		"username":            "owner@example.com",
		"password_ciphertext": "encrypted-value",
	}).Error)

	rows, err = EnsureUpstreamChannels([]string{"https://api.xtokenmirror.cn"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Primary upstream", rows[0].Name)
	assert.Equal(t, "sub2api", rows[0].Provider)
	assert.Equal(t, "owner@example.com", rows[0].Username)
	assert.Equal(t, "encrypted-value", rows[0].PasswordCiphertext)

	var count int64
	require.NoError(t, db.Model(&UpstreamChannel{}).Where("base_url_hash = ?", UpstreamBaseURLHash("https://api.xtokenmirror.cn")).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestUpstreamChannelLegacyAuthTypeDefaultsToPassword(t *testing.T) {
	row := &UpstreamChannel{}
	assert.Equal(t, UpstreamAuthTypePassword, row.EffectiveAuthType())
	assert.Equal(t, UpstreamAuthTypeAccessToken, NormalizeUpstreamAuthType(" ACCESS_TOKEN "))
}

func TestUpstreamChannelDefaultNameUsesFirstMeaningfulHostnameSegment(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{baseURL: "https://api.xtokenmirror.cn", want: "xtokenmirror"},
		{baseURL: "https://api.syncapi.dpdns.org/v1", want: "syncapi"},
		{baseURL: "https://api.ggbond686.online", want: "ggbond686"},
		{baseURL: "https://aimuxr.com", want: "aimuxr"},
		{baseURL: "https://www.aiwanwu.cc", want: "aiwanwu"},
		{baseURL: "https://gateway.example.com:8443/api", want: "example"},
		{baseURL: "http://127.0.0.1:3000", want: "127.0.0.1"},
		{baseURL: "http://localhost:3000", want: "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			assert.Equal(t, tt.want, UpstreamChannelDefaultName(tt.baseURL))
		})
	}
}

func TestGetChannelKeyStatesByBaseURLTracksDistinctKeyEnablement(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}))

	baseURL := "https://upstream.example"
	otherBaseURL := "https://other.example"
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-enabled", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-disabled", Status: common.ChannelStatusManuallyDisabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-duplicate", Status: common.ChannelStatusManuallyDisabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-duplicate", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-multi-one\nsk-multi-two", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: " ", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{BaseURL: &otherBaseURL, Key: "sk-other", Status: common.ChannelStatusEnabled}).Error)

	states, err := GetChannelKeyStatesByBaseURL(baseURL)
	require.NoError(t, err)
	require.Len(t, states, 5)
	assert.True(t, states[UpstreamChannelKeyFingerprint(baseURL, "sk-enabled")])
	assert.False(t, states[UpstreamChannelKeyFingerprint(baseURL, "sk-disabled")])
	assert.True(t, states[UpstreamChannelKeyFingerprint(baseURL, "sk-duplicate")])
	assert.True(t, states[UpstreamChannelKeyFingerprint(baseURL, "sk-multi-one")])
	assert.True(t, states[UpstreamChannelKeyFingerprint(baseURL, "sk-multi-two")])
	_, containsOther := states[UpstreamChannelKeyFingerprint(baseURL, "sk-other")]
	assert.False(t, containsOther)
}

func TestListChannelKeySourcesExpandsMultiKeyChannels(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}))
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&Channel{BaseURL: &baseURL, Key: "sk-first\n\nsk-second", Status: common.ChannelStatusAutoDisabled}).Error)

	sources, err := ListChannelKeySources()
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.Equal(t, []ChannelKeySource{
		{BaseURL: baseURL, Key: "sk-first", Status: common.ChannelStatusAutoDisabled},
		{BaseURL: baseURL, Key: "sk-second", Status: common.ChannelStatusAutoDisabled},
	}, sources)
}

func TestEnsureUpstreamChannelsMigratesPreviousGeneratedName(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	baseURL := "https://aimuxr.com"
	row := UpstreamChannel{
		Name:                "com",
		BaseURL:             baseURL,
		BaseURLHash:         UpstreamBaseURLHash(baseURL),
		Provider:            "auto",
		AutoRefreshInterval: 300,
		Status:              UpstreamChannelStatusUnconfigured,
	}
	require.NoError(t, db.Create(&row).Error)

	rows, err := EnsureUpstreamChannels([]string{baseURL})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "aimuxr", rows[0].Name)
}

func TestCreateAndListUpstreamChannelPreservesManualConfiguration(t *testing.T) {
	setupUpstreamChannelTestDB(t)

	row, err := CreateUpstreamChannelConfig(UpstreamChannel{
		BaseURL:             "https://manual.example/api",
		Name:                "Manual channel",
		Provider:            "new-api",
		Username:            "root",
		PasswordCiphertext:  "encrypted-value",
		BalanceThreshold:    10,
		AutoRefreshInterval: 600,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://manual.example/api", row.BaseURL)
	assert.Equal(t, "Manual channel", row.Name)
	assert.Equal(t, UpstreamChannelStatusUnconfigured, row.Status)
	assert.Equal(t, float64(UpstreamChannelDefaultMultiplier), row.Multiplier)

	rows, err := ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, row.Id, rows[0].Id)
}

func TestCreateUpstreamChannelConfigAllowsMultipleAccountsForSameBaseURL(t *testing.T) {
	setupUpstreamChannelTestDB(t)
	baseURL := "https://manual.example"

	first, err := CreateUpstreamChannelConfig(UpstreamChannel{
		BaseURL:            baseURL,
		Name:               "First account",
		Provider:           "sub2api",
		Username:           "first@example.com",
		PasswordCiphertext: "first-secret",
	})
	require.NoError(t, err)
	second, err := CreateUpstreamChannelConfig(UpstreamChannel{
		BaseURL:            baseURL,
		Name:               "Second account",
		Provider:           "sub2api",
		Username:           "second@example.com",
		PasswordCiphertext: "second-secret",
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.Id, second.Id)

	rows, err := ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "first@example.com", rows[0].Username)
	assert.Equal(t, "second@example.com", rows[1].Username)
}

func TestDeleteUpstreamChannelSuppressesDiscoveryAndManualCreateRestores(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	baseURL := "https://used-upstream.example"
	row := UpstreamChannel{
		Name:                "Old upstream",
		BaseURL:             baseURL,
		BaseURLHash:         UpstreamBaseURLHash(baseURL),
		Provider:            "new-api",
		AuthType:            UpstreamAuthTypeAccessToken,
		Priority:            9,
		SelectedGroup:       "old-group",
		Username:            "old-user",
		Note:                "old note",
		PasswordCiphertext:  "old-secret",
		Balance:             42,
		BalanceUpdatedTime:  100,
		BalanceThreshold:    10,
		Multiplier:          1.5,
		AutoRefreshInterval: 300,
		LowBalanceNotified:  true,
		LastSyncTime:        100,
		LastError:           "old error",
		Status:              UpstreamChannelStatusReady,
		SnapshotJSON:        `{"provider":"new-api"}`,
	}
	require.NoError(t, db.Create(&row).Error)

	require.NoError(t, DeleteUpstreamChannel(row.Id))
	_, err := GetUpstreamChannelByID(row.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	listed, err := ListUpstreamChannels()
	require.NoError(t, err)
	assert.Empty(t, listed)

	discovered, err := EnsureUpstreamChannels([]string{baseURL})
	require.NoError(t, err)
	assert.Empty(t, discovered)

	var suppressed UpstreamChannel
	require.NoError(t, db.First(&suppressed, row.Id).Error)
	require.NotNil(t, suppressed.SuppressedAt)
	assert.Empty(t, suppressed.Username)
	assert.Empty(t, suppressed.PasswordCiphertext)
	assert.Empty(t, suppressed.SnapshotJSON)
	assert.Zero(t, suppressed.Balance)

	restored, err := CreateUpstreamChannelConfig(UpstreamChannel{
		Name:                "Restored upstream",
		BaseURL:             baseURL,
		Provider:            "sub2api",
		AuthType:            UpstreamAuthTypeAccessToken,
		Username:            "new-user",
		PasswordCiphertext:  "new-secret",
		BalanceThreshold:    5,
		Multiplier:          1.25,
		AutoRefreshInterval: 600,
	})
	require.NoError(t, err)
	assert.Equal(t, row.Id, restored.Id)
	assert.Nil(t, restored.SuppressedAt)
	assert.Equal(t, "Restored upstream", restored.Name)
	assert.Equal(t, "sub2api", restored.Provider)
	assert.Equal(t, "new-user", restored.Username)
	assert.Equal(t, "new-secret", restored.PasswordCiphertext)
	assert.Equal(t, UpstreamChannelStatusUnconfigured, restored.Status)
	assert.Empty(t, restored.SnapshotJSON)
	assert.Zero(t, restored.Balance)
	assert.Zero(t, restored.LastSyncTime)
	assert.Empty(t, restored.LastError)

	listed, err = ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, row.Id, listed[0].Id)
}

func TestGetUpstreamChannelByIDUsesDefaultMultiplierForLegacyRows(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:     "https://legacy.example",
		BaseURLHash: UpstreamBaseURLHash("https://legacy.example"),
		Multiplier:  0,
	}
	require.NoError(t, db.Create(&row).Error)

	loaded, err := GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, float64(UpstreamChannelDefaultMultiplier), loaded.Multiplier)
}

func TestUpdateUpstreamChannelConfigClearsSnapshotWhenLoginIdentityChanges(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         UpstreamBaseURLHash("https://upstream.example"),
		Provider:            "new-api",
		Username:            "old-user",
		PasswordCiphertext:  "encrypted-value",
		Balance:             15,
		BalanceUpdatedTime:  100,
		BalanceThreshold:    3,
		AutoRefreshInterval: 300,
		LowBalanceNotified:  true,
		LastSyncTime:        100,
		Status:              UpstreamChannelStatusReady,
		SnapshotJSON:        `{"provider":"new-api","balance":15}`,
		DefaultTestModel:    "gpt-4o-mini",
	}
	require.NoError(t, db.Create(&row).Error)

	err := UpdateUpstreamChannelConfig(row.Id, "Renamed upstream", "sub2api", UpstreamAuthTypePassword, "new-user", nil, 3, 1.25, 300, 7)
	require.NoError(t, err)

	updated, err := GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, UpstreamChannelStatusUnconfigured, updated.Status)
	assert.Empty(t, updated.SnapshotJSON)
	assert.Empty(t, updated.DefaultTestModel)
	assert.Zero(t, updated.Balance)
	assert.Zero(t, updated.BalanceUpdatedTime)
	assert.Zero(t, updated.LastSyncTime)
	assert.False(t, updated.LowBalanceNotified)
	assert.Empty(t, updated.LastError)
	assert.Equal(t, "encrypted-value", updated.PasswordCiphertext)
	assert.Equal(t, "Renamed upstream", updated.Name)
	assert.Equal(t, 1.25, updated.Multiplier)
	assert.Equal(t, int64(7), updated.Priority)
}

func TestUpdateUpstreamChannelConfigClearsSnapshotWhenPasswordChanges(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         UpstreamBaseURLHash("https://upstream.example"),
		Provider:            "new-api",
		Username:            "root",
		PasswordCiphertext:  "old-encrypted-value",
		Balance:             15,
		BalanceUpdatedTime:  100,
		AutoRefreshInterval: 300,
		LowBalanceNotified:  true,
		LastSyncTime:        100,
		Status:              UpstreamChannelStatusReady,
		SnapshotJSON:        `{"provider":"new-api","balance":15}`,
		DefaultTestModel:    "gpt-4o-mini",
	}
	require.NoError(t, db.Create(&row).Error)
	newPassword := "new-encrypted-value"

	err := UpdateUpstreamChannelConfig(row.Id, row.Name, row.Provider, UpstreamAuthTypePassword, row.Username, &newPassword, 0, 1, 300, 0)
	require.NoError(t, err)

	updated, err := GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, UpstreamChannelStatusUnconfigured, updated.Status)
	assert.Empty(t, updated.SnapshotJSON)
	assert.Empty(t, updated.DefaultTestModel)
	assert.Zero(t, updated.Balance)
	assert.Zero(t, updated.BalanceUpdatedTime)
	assert.Zero(t, updated.LastSyncTime)
	assert.False(t, updated.LowBalanceNotified)
	assert.Equal(t, newPassword, updated.PasswordCiphertext)
}

func TestUpdateUpstreamChannelConfigPreservesSnapshotForRefreshSettingsOnly(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:             "https://upstream.example",
		BaseURLHash:         UpstreamBaseURLHash("https://upstream.example"),
		Provider:            "new-api",
		Username:            "root",
		PasswordCiphertext:  "encrypted-value",
		Balance:             15,
		BalanceUpdatedTime:  100,
		AutoRefreshInterval: 300,
		LastSyncTime:        100,
		Status:              UpstreamChannelStatusReady,
		SnapshotJSON:        `{"provider":"new-api","balance":15}`,
		DefaultTestModel:    "gpt-4o-mini",
	}
	require.NoError(t, db.Create(&row).Error)

	err := UpdateUpstreamChannelConfig(row.Id, row.Name, row.Provider, UpstreamAuthTypePassword, row.Username, nil, 10, 1.5, 600, 12)
	require.NoError(t, err)

	updated, err := GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, UpstreamChannelStatusReady, updated.Status)
	assert.Equal(t, row.SnapshotJSON, updated.SnapshotJSON)
	assert.Equal(t, "gpt-4o-mini", updated.DefaultTestModel)
	assert.Equal(t, float64(15), updated.Balance)
	assert.Equal(t, int64(100), updated.BalanceUpdatedTime)
	assert.Equal(t, int64(100), updated.LastSyncTime)
	assert.Equal(t, float64(10), updated.BalanceThreshold)
	assert.Equal(t, 1.5, updated.Multiplier)
	assert.Equal(t, 600, updated.AutoRefreshInterval)
	assert.Equal(t, int64(12), updated.Priority)
}

func TestListUpstreamChannelsOrdersByPriorityThenID(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	rows := []UpstreamChannel{
		{BaseURL: "https://first.example", BaseURLHash: UpstreamBaseURLHash("https://first.example"), Priority: 1},
		{BaseURL: "https://second.example", BaseURLHash: UpstreamBaseURLHash("https://second.example"), Priority: 3},
		{BaseURL: "https://third.example", BaseURLHash: UpstreamBaseURLHash("https://third.example"), Priority: 3},
	}
	require.NoError(t, db.Create(&rows).Error)

	listed, err := ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, listed, 3)
	assert.Equal(t, []int{rows[1].Id, rows[2].Id, rows[0].Id}, []int{listed[0].Id, listed[1].Id, listed[2].Id})
}

func TestPinUpstreamChannelSetsPriorityAboveCurrentMaximum(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	rows := []UpstreamChannel{
		{BaseURL: "https://first.example", BaseURLHash: UpstreamBaseURLHash("https://first.example"), Priority: 1},
		{BaseURL: "https://second.example", BaseURLHash: UpstreamBaseURLHash("https://second.example"), Priority: 1},
	}
	require.NoError(t, db.Create(&rows).Error)

	firstPinned, err := PinUpstreamChannel(rows[0].Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), firstPinned.Priority)

	secondPinned, err := PinUpstreamChannel(rows[1].Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), secondPinned.Priority)

	firstAfterSecondPin, err := GetUpstreamChannelByID(rows[0].Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), firstAfterSecondPin.Priority)

	listed, err := ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, []int{rows[1].Id, rows[0].Id}, []int{listed[0].Id, listed[1].Id})
}

func TestNormalizeUpstreamChannelPrioritiesRepairsLegacyNullValues(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	rows := []UpstreamChannel{
		{BaseURL: "https://legacy.example", BaseURLHash: UpstreamBaseURLHash("https://legacy.example")},
		{BaseURL: "https://current.example", BaseURLHash: UpstreamBaseURLHash("https://current.example"), Priority: 1},
	}
	require.NoError(t, db.Create(&rows).Error)
	require.NoError(t, db.Exec("UPDATE upstream_channels SET priority = NULL WHERE id = ?", rows[0].Id).Error)

	require.NoError(t, normalizeUpstreamChannelPriorities(db))

	var nullCount int64
	require.NoError(t, db.Model(&UpstreamChannel{}).Where("priority IS NULL").Count(&nullCount).Error)
	assert.Zero(t, nullCount)

	pinned, err := PinUpstreamChannel(rows[0].Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pinned.Priority)

	listed, err := ListUpstreamChannels()
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, rows[0].Id, listed[0].Id)
}

func TestPinUpstreamChannelRejectsPriorityOverflow(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:     "https://highest.example",
		BaseURLHash: UpstreamBaseURLHash("https://highest.example"),
		Priority:    math.MaxInt32,
	}
	require.NoError(t, db.Create(&row).Error)

	_, err := PinUpstreamChannel(row.Id)
	require.EqualError(t, err, "upstream channel priority has reached the maximum")
}

func TestListDueUpstreamChannelsUsesLastAttemptAndSkipsDisabledOrUnconfiguredRows(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	now := int64(1_000)
	rows := []UpstreamChannel{
		{
			BaseURL:             "https://never-attempted.example",
			BaseURLHash:         UpstreamBaseURLHash("https://never-attempted.example"),
			Username:            "root",
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
		},
		{
			BaseURL:             "https://due.example",
			BaseURLHash:         UpstreamBaseURLHash("https://due.example"),
			Username:            "root",
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
			LastSyncTime:        now - 300,
		},
		{
			BaseURL:             "https://not-due.example",
			BaseURLHash:         UpstreamBaseURLHash("https://not-due.example"),
			Username:            "root",
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
			LastSyncTime:        now - 299,
		},
		{
			BaseURL:             "https://disabled.example",
			BaseURLHash:         UpstreamBaseURLHash("https://disabled.example"),
			Username:            "root",
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 0,
		},
		{
			BaseURL:             "https://missing-username.example",
			BaseURLHash:         UpstreamBaseURLHash("https://missing-username.example"),
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
		},
		{
			BaseURL:             "https://sub2-access-token.example",
			BaseURLHash:         UpstreamBaseURLHash("https://sub2-access-token.example"),
			Provider:            "sub2api",
			AuthType:            UpstreamAuthTypeAccessToken,
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
		},
		{
			BaseURL:             "https://sub2-password-missing-username.example",
			BaseURLHash:         UpstreamBaseURLHash("https://sub2-password-missing-username.example"),
			Provider:            "sub2api",
			AuthType:            UpstreamAuthTypePassword,
			PasswordCiphertext:  "encrypted",
			AutoRefreshInterval: 300,
		},
		{
			BaseURL:             "https://unconfigured.example",
			BaseURLHash:         UpstreamBaseURLHash("https://unconfigured.example"),
			AutoRefreshInterval: 300,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	due, err := ListDueUpstreamChannels(now, 100)
	require.NoError(t, err)
	require.Len(t, due, 3)
	assert.Equal(t, "https://never-attempted.example", due[0].BaseURL)
	assert.Equal(t, "https://due.example", due[1].BaseURL)
	assert.Equal(t, "https://sub2-access-token.example", due[2].BaseURL)
}

func TestSaveUpstreamChannelRefreshErrorPreservesLastSuccessfulSnapshot(t *testing.T) {
	db := setupUpstreamChannelTestDB(t)
	row := UpstreamChannel{
		BaseURL:            "https://upstream.example",
		BaseURLHash:        UpstreamBaseURLHash("https://upstream.example"),
		Provider:           "new-api",
		Balance:            25,
		BalanceUpdatedTime: 100,
		LastSyncTime:       100,
		Status:             UpstreamChannelStatusReady,
		SnapshotJSON:       `{"provider":"new-api","balance":25}`,
	}
	require.NoError(t, db.Create(&row).Error)

	require.NoError(t, SaveUpstreamChannelRefreshError(row.Id, "temporary upstream error", 200))

	updated, err := GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	assert.Equal(t, UpstreamChannelStatusError, updated.Status)
	assert.Equal(t, "temporary upstream error", updated.LastError)
	assert.Equal(t, int64(200), updated.LastSyncTime)
	assert.Equal(t, int64(100), updated.BalanceUpdatedTime)
	assert.Equal(t, float64(25), updated.Balance)
	assert.Equal(t, row.SnapshotJSON, updated.SnapshotJSON)
}
