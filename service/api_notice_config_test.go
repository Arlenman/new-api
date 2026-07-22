package service

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRevealApiNoticeAPIKeyReturnsDecryptedDatabaseKey(t *testing.T) {
	originalDB := model.DB
	originalCryptoSecret := common.CryptoSecret
	t.Setenv("API_NOTICE_API_KEY", "")
	t.Setenv("API_NOTICE_API_KEY_FILE", "")
	common.CryptoSecret = "api-notice-reveal-test-secret"
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api-notice-config.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ApiNoticeConfig{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		common.CryptoSecret = originalCryptoSecret
	})

	encrypted, err := common.EncryptSecret(apiNoticeAPIKeyPurpose, "database-api-key")
	require.NoError(t, err)
	_, err = model.SaveApiNoticeConfig("http://api-notice.test", encrypted)
	require.NoError(t, err)

	revealed, err := RevealApiNoticeAPIKey()
	require.NoError(t, err)
	assert.Equal(t, "database-api-key", revealed.APIKey)
	assert.Equal(t, "database", revealed.APIKeySource)
}

func TestRevealApiNoticeAPIKeyRejectsMissingKey(t *testing.T) {
	originalDB := model.DB
	t.Setenv("API_NOTICE_API_KEY", "")
	t.Setenv("API_NOTICE_API_KEY_FILE", "")
	model.DB = nil
	t.Cleanup(func() {
		model.DB = originalDB
	})

	_, err := RevealApiNoticeAPIKey()
	assert.ErrorIs(t, err, ErrApiNoticeAPIKeyNotConfigured)
}
