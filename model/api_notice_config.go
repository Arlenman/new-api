package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const apiNoticeConfigID int64 = 1

// ApiNoticeConfig stores the singleton server-side connection configuration.
// EncryptedAPIKey keeps the legacy database column name for compatibility with
// API keys already saved by the earlier HMAC-labelled implementation.
type ApiNoticeConfig struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	BaseURL         string `json:"base_url" gorm:"type:varchar(512);not null"`
	EncryptedAPIKey string `json:"-" gorm:"column:encrypted_hmac_secret;type:text;not null"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint;index"`
}

func GetApiNoticeConfig() (*ApiNoticeConfig, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	var config ApiNoticeConfig
	if err := DB.First(&config, "id = ?", apiNoticeConfigID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

func SaveApiNoticeConfig(baseURL, encryptedAPIKey string) (*ApiNoticeConfig, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	now := common.GetTimestamp()
	returnConfig := &ApiNoticeConfig{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing ApiNoticeConfig
		err := tx.First(&existing, "id = ?", apiNoticeConfigID).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			returnConfig = &ApiNoticeConfig{
				ID:              apiNoticeConfigID,
				BaseURL:         baseURL,
				EncryptedAPIKey: encryptedAPIKey,
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			return tx.Create(returnConfig).Error
		case err != nil:
			return err
		default:
			updates := map[string]any{
				"base_url":              baseURL,
				"encrypted_hmac_secret": encryptedAPIKey,
				"updated_at":            now,
			}
			if err := tx.Model(&ApiNoticeConfig{}).Where("id = ?", apiNoticeConfigID).Updates(updates).Error; err != nil {
				return err
			}
			existing.BaseURL = baseURL
			existing.EncryptedAPIKey = encryptedAPIKey
			existing.UpdatedAt = now
			returnConfig = &existing
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return returnConfig, nil
}
