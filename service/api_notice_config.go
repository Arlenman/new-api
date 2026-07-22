package service

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// Keep the existing encryption purpose so API keys already saved by the old
// HMAC-labelled UI remain decryptable after switching transport auth to Bearer.
const apiNoticeAPIKeyPurpose = "api-notice-hmac-secret"

// ApiNoticeRuntimeConfig is the server-side configuration used by every
// api-notice operation. Regular configuration responses expose only key status
// and a mask; the root-only reveal endpoint returns APIKey only on demand.
type ApiNoticeRuntimeConfig struct {
	BaseURL       string
	APIKey        string
	APIKeySource  string
	BaseURLSource string
}

func GetApiNoticeConfig() (dto.ApiNoticeConfig, error) {
	runtime, err := loadApiNoticeRuntimeConfig()
	if err != nil {
		return dto.ApiNoticeConfig{}, err
	}
	masked := ""
	if runtime.APIKey != "" {
		masked = "••••••••"
	}
	return dto.ApiNoticeConfig{
		BaseURL:           runtime.BaseURL,
		APIKeyConfigured:  runtime.APIKey != "",
		APIKeyMasked:      masked,
		APIKeySource:      runtime.APIKeySource,
		PersistentStorage: common.HasPersistentCryptoSecret(),
	}, nil
}

func RevealApiNoticeAPIKey() (dto.ApiNoticeAPIKeyReveal, error) {
	runtime, err := loadApiNoticeRuntimeConfig()
	if err != nil {
		return dto.ApiNoticeAPIKeyReveal{}, err
	}
	if strings.TrimSpace(runtime.APIKey) == "" {
		return dto.ApiNoticeAPIKeyReveal{}, ErrApiNoticeAPIKeyNotConfigured
	}
	return dto.ApiNoticeAPIKeyReveal{
		APIKey:       runtime.APIKey,
		APIKeySource: runtime.APIKeySource,
	}, nil
}

func UpdateApiNoticeConfig(input dto.ApiNoticeConfigUpdate) (dto.ApiNoticeConfig, error) {
	baseURL, err := normalizeApiNoticeBaseURL(input.BaseURL)
	if err != nil {
		return dto.ApiNoticeConfig{}, err
	}

	stored, err := getStoredApiNoticeConfig()
	if err != nil {
		return dto.ApiNoticeConfig{}, err
	}
	encryptedAPIKey := ""
	if stored != nil {
		encryptedAPIKey = stored.EncryptedAPIKey
	}
	if input.ClearAPIKey {
		encryptedAPIKey = ""
	}
	if strings.TrimSpace(input.APIKey) != "" {
		if !common.HasPersistentCryptoSecret() {
			return dto.ApiNoticeConfig{}, errors.New("persistent crypto secret is required to save API key")
		}
		encryptedAPIKey, err = common.EncryptSecret(apiNoticeAPIKeyPurpose, input.APIKey)
		if err != nil {
			return dto.ApiNoticeConfig{}, fmt.Errorf("encrypt api-notice API key: %w", err)
		}
	}
	if _, err = model.SaveApiNoticeConfig(baseURL, encryptedAPIKey); err != nil {
		return dto.ApiNoticeConfig{}, err
	}
	return GetApiNoticeConfig()
}

func loadApiNoticeRuntimeConfig() (ApiNoticeRuntimeConfig, error) {
	stored, err := getStoredApiNoticeConfig()
	if err != nil {
		return ApiNoticeRuntimeConfig{}, err
	}

	baseURLValue := os.Getenv("API_NOTICE_BASE_URL")
	baseURLSource := "environment"
	if stored != nil && strings.TrimSpace(stored.BaseURL) != "" {
		baseURLValue = stored.BaseURL
		baseURLSource = "database"
	}
	baseURL, err := normalizeApiNoticeBaseURL(baseURLValue)
	if err != nil {
		return ApiNoticeRuntimeConfig{}, err
	}

	apiKey, source, err := loadApiNoticeAPIKeyWithStored(stored)
	if err != nil && !errors.Is(err, ErrApiNoticeAPIKeyNotConfigured) {
		return ApiNoticeRuntimeConfig{}, err
	}
	return ApiNoticeRuntimeConfig{
		BaseURL:       baseURL,
		APIKey:        apiKey,
		APIKeySource:  source,
		BaseURLSource: baseURLSource,
	}, nil
}

func getStoredApiNoticeConfig() (*model.ApiNoticeConfig, error) {
	if model.DB == nil {
		return nil, nil
	}
	return model.GetApiNoticeConfig()
}

func loadApiNoticeAPIKeyWithStored(stored *model.ApiNoticeConfig) (string, string, error) {
	apiKey, source, err := loadApiNoticeAPIKeyFromEnvironmentOrFile()
	if err == nil {
		return apiKey, source, nil
	}
	if !errors.Is(err, ErrApiNoticeAPIKeyNotConfigured) {
		return "", "", err
	}

	if stored != nil && strings.TrimSpace(stored.EncryptedAPIKey) != "" {
		apiKey, err = common.DecryptSecret(apiNoticeAPIKeyPurpose, stored.EncryptedAPIKey)
		if err != nil {
			return "", "", fmt.Errorf("decrypt api-notice API key: %w", err)
		}
		if strings.TrimSpace(apiKey) != "" {
			return apiKey, "database", nil
		}
	}

	return "", "", ErrApiNoticeAPIKeyNotConfigured
}
