package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	UpstreamChannelStatusUnconfigured = "unconfigured"
	UpstreamChannelStatusReady        = "ready"
	UpstreamChannelStatusError        = "error"
	UpstreamChannelDefaultMultiplier  = 1
	UpstreamAuthTypePassword          = "password"
	UpstreamAuthTypeAccessToken       = "access_token"
)

type UpstreamChannel struct {
	Id                  int     `json:"id"`
	Name                string  `json:"name" gorm:"type:varchar(255)"`
	BaseURL             string  `json:"base_url" gorm:"type:text;not null"`
	BaseURLHash         string  `json:"-" gorm:"type:char(64);uniqueIndex"`
	Provider            string  `json:"provider" gorm:"type:varchar(32);index"`
	AuthType            string  `json:"auth_type" gorm:"type:varchar(32)"`
	Priority            int64   `json:"priority" gorm:"index"`
	SelectedGroup       string  `json:"selected_group" gorm:"type:varchar(255)"`
	Username            string  `json:"username" gorm:"type:varchar(255)"`
	Note                string  `json:"note" gorm:"type:text"`
	PasswordCiphertext  string  `json:"-" gorm:"type:text"`
	Balance             float64 `json:"balance"`
	BalanceUpdatedTime  int64   `json:"balance_updated_time" gorm:"bigint"`
	BalanceThreshold    float64 `json:"balance_threshold"`
	Multiplier          float64 `json:"multiplier"`
	AutoRefreshInterval int     `json:"auto_refresh_interval"`
	LowBalanceNotified  bool    `json:"-"`
	LastSyncTime        int64   `json:"last_sync_time" gorm:"bigint"`
	LastError           string  `json:"last_error" gorm:"type:text"`
	Status              string  `json:"status" gorm:"type:varchar(32);index"`
	SnapshotJSON        string  `json:"-" gorm:"type:text"`
	CreatedAt           int64   `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt           int64   `json:"updated_at" gorm:"autoUpdateTime"`
}

func NormalizeUpstreamAuthType(authType string) string {
	authType = strings.ToLower(strings.TrimSpace(authType))
	if authType == "" {
		return UpstreamAuthTypePassword
	}
	return authType
}

func UpstreamBaseURLHash(baseURL string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(baseURL)))
	return hex.EncodeToString(digest[:])
}

func UpstreamKeyFingerprint(key string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(digest[:])
}

func UpstreamChannelDefaultName(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		hostname := strings.TrimSuffix(parsed.Hostname(), ".")
		if net.ParseIP(hostname) != nil {
			return hostname
		}
		parts := strings.Split(hostname, ".")
		index := 0
		for index < len(parts)-1 {
			switch strings.ToLower(parts[index]) {
			case "api", "www", "ai", "sub", "sub2api", "gateway", "vip", "openai":
				index++
			default:
				return parts[index]
			}
		}
		if index < len(parts) && parts[index] != "" {
			return parts[index]
		}
		if hostname != "" {
			return hostname
		}
	}
	return trimmed
}

func EnsureUpstreamChannels(baseURLs []string) ([]*UpstreamChannel, error) {
	if len(baseURLs) == 0 {
		return []*UpstreamChannel{}, nil
	}

	rows := make([]*UpstreamChannel, 0, len(baseURLs))
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, baseURL := range baseURLs {
			hash := UpstreamBaseURLHash(baseURL)
			var row UpstreamChannel
			err := tx.Where("base_url_hash = ?", hash).First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				row = UpstreamChannel{
					Name:                UpstreamChannelDefaultName(baseURL),
					BaseURL:             baseURL,
					BaseURLHash:         hash,
					Provider:            "auto",
					AuthType:            UpstreamAuthTypePassword,
					Multiplier:          UpstreamChannelDefaultMultiplier,
					AutoRefreshInterval: 300,
					Status:              UpstreamChannelStatusUnconfigured,
				}
				if err = tx.Create(&row).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				name := strings.TrimSpace(row.Name)
				legacyDefaultName := ""
				previousDefaultName := ""
				if parsed, parseErr := url.Parse(baseURL); parseErr == nil {
					legacyDefaultName = parsed.Host
					hostname := strings.TrimSuffix(parsed.Hostname(), ".")
					parts := strings.Split(hostname, ".")
					if len(parts) > 1 {
						previousDefaultName = parts[1]
					} else {
						previousDefaultName = hostname
					}
				}
				useDefaultName := name == "" || name == legacyDefaultName || name == previousDefaultName
				effectiveMultiplier := row.EffectiveMultiplier()
				effectiveAuthType := row.EffectiveAuthType()
				if row.BaseURL == baseURL && !useDefaultName && row.Multiplier == effectiveMultiplier && row.AuthType == effectiveAuthType {
					rows = append(rows, &row)
					continue
				}
				updates := map[string]any{"base_url": baseURL}
				if useDefaultName {
					updates["name"] = UpstreamChannelDefaultName(baseURL)
				}
				if row.Multiplier != effectiveMultiplier {
					updates["multiplier"] = effectiveMultiplier
				}
				if row.AuthType != effectiveAuthType {
					updates["auth_type"] = effectiveAuthType
				}
				if err = tx.Model(&row).Updates(updates).Error; err != nil {
					return err
				}
				row.BaseURL = baseURL
				if useDefaultName {
					row.Name = UpstreamChannelDefaultName(baseURL)
				}
				row.Multiplier = effectiveMultiplier
				row.AuthType = effectiveAuthType
			}
			rows = append(rows, &row)
		}
		return nil
	})
	return rows, err
}

func CreateUpstreamChannelConfig(row UpstreamChannel) (*UpstreamChannel, error) {
	row.Name = strings.TrimSpace(row.Name)
	if row.Name == "" {
		row.Name = UpstreamChannelDefaultName(row.BaseURL)
	}
	row.BaseURLHash = UpstreamBaseURLHash(row.BaseURL)
	if row.Provider == "" {
		row.Provider = "auto"
	}
	row.AuthType = NormalizeUpstreamAuthType(row.AuthType)
	if row.Multiplier <= 0 || math.IsNaN(row.Multiplier) || math.IsInf(row.Multiplier, 0) {
		row.Multiplier = UpstreamChannelDefaultMultiplier
	}
	if row.Status == "" {
		row.Status = UpstreamChannelStatusUnconfigured
	}
	var count int64
	if err := DB.Model(&UpstreamChannel{}).Where("base_url_hash = ?", row.BaseURLHash).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, errors.New("upstream channel already exists")
	}
	if err := DB.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func normalizeUpstreamChannelPriorities(db *gorm.DB) error {
	return db.Model(&UpstreamChannel{}).Where("priority IS NULL").UpdateColumn("priority", 0).Error
}

func ListUpstreamChannels() ([]*UpstreamChannel, error) {
	var rows []*UpstreamChannel
	err := DB.Order("COALESCE(priority, 0) desc").Order("id asc").Find(&rows).Error
	for _, row := range rows {
		if row != nil {
			row.Multiplier = row.EffectiveMultiplier()
			row.AuthType = row.EffectiveAuthType()
		}
	}
	return rows, err
}

func GetUpstreamChannelByID(id int) (*UpstreamChannel, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream channel id")
	}
	var row UpstreamChannel
	if err := DB.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	row.Multiplier = row.EffectiveMultiplier()
	row.AuthType = row.EffectiveAuthType()
	return &row, nil
}

func UpdateUpstreamChannelConfig(id int, name string, provider string, authType string, username string, passwordCiphertext *string, balanceThreshold float64, multiplier float64, autoRefreshInterval int, priority int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var row UpstreamChannel
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"name":                  name,
			"provider":              provider,
			"auth_type":             NormalizeUpstreamAuthType(authType),
			"priority":              priority,
			"username":              username,
			"balance_threshold":     balanceThreshold,
			"multiplier":            multiplier,
			"auto_refresh_interval": autoRefreshInterval,
		}
		loginIdentityChanged := row.Provider != provider || row.EffectiveAuthType() != NormalizeUpstreamAuthType(authType) || row.Username != username || passwordCiphertext != nil
		if passwordCiphertext != nil {
			updates["password_ciphertext"] = *passwordCiphertext
		}
		if loginIdentityChanged {
			updates["balance"] = 0
			updates["balance_updated_time"] = 0
			updates["low_balance_notified"] = false
			updates["last_sync_time"] = 0
			updates["last_error"] = ""
			updates["snapshot_json"] = ""
			updates["status"] = UpstreamChannelStatusUnconfigured
		}
		return tx.Model(&UpstreamChannel{}).Where("id = ?", id).Updates(updates).Error
	})
}

func PinUpstreamChannel(id int) (*UpstreamChannel, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream channel id")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var target UpstreamChannel
		if err := tx.First(&target, "id = ?", id).Error; err != nil {
			return err
		}

		var highest UpstreamChannel
		if err := lockForUpdate(tx).Order("COALESCE(priority, 0) desc").Order("id asc").First(&highest).Error; err != nil {
			return err
		}
		if highest.Priority >= math.MaxInt32 {
			return errors.New("upstream channel priority has reached the maximum")
		}
		return tx.Model(&UpstreamChannel{}).Where("id = ?", id).Update("priority", highest.Priority+1).Error
	})
	if err != nil {
		return nil, err
	}
	return GetUpstreamChannelByID(id)
}

func GetChannelKeyStatesByBaseURL(baseURL string) (map[string]bool, error) {
	var channels []Channel
	if err := DB.Select("key", "status").Where("base_url = ?", baseURL).Find(&channels).Error; err != nil {
		return nil, err
	}
	states := make(map[string]bool, len(channels))
	for _, channel := range channels {
		key := strings.TrimSpace(channel.Key)
		if key == "" {
			continue
		}
		fingerprint := UpstreamKeyFingerprint(key)
		states[fingerprint] = states[fingerprint] || channel.Status == common.ChannelStatusEnabled
	}
	return states, nil
}

func UpdateUpstreamChannelSnapshot(id int, snapshotJSON string) error {
	return DB.Model(&UpstreamChannel{}).Where("id = ?", id).Update("snapshot_json", snapshotJSON).Error
}

func UpdateUpstreamChannelNote(id int, note string) error {
	return DB.Model(&UpstreamChannel{}).Where("id = ?", id).Update("note", note).Error
}

func UpdateUpstreamChannelSelectedGroup(id int, selectedGroup string) error {
	return DB.Model(&UpstreamChannel{}).Where("id = ?", id).Update("selected_group", selectedGroup).Error
}

func SaveUpstreamChannelRefresh(id int, provider string, snapshotJSON string, balance float64, refreshedAt int64, lowBalanceNotified bool) error {
	return DB.Model(&UpstreamChannel{}).Where("id = ?", id).Updates(map[string]any{
		"provider":             provider,
		"snapshot_json":        snapshotJSON,
		"balance":              balance,
		"balance_updated_time": refreshedAt,
		"last_sync_time":       refreshedAt,
		"low_balance_notified": lowBalanceNotified,
		"last_error":           "",
		"status":               UpstreamChannelStatusReady,
	}).Error
}

func SaveUpstreamChannelRefreshError(id int, message string, attemptedAt int64) error {
	return DB.Model(&UpstreamChannel{}).Where("id = ?", id).Updates(map[string]any{
		"last_sync_time": attemptedAt,
		"last_error":     message,
		"status":         UpstreamChannelStatusError,
	}).Error
}

func ListDueUpstreamChannels(now int64, limit int) ([]*UpstreamChannel, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []*UpstreamChannel
	err := DB.
		Where("password_ciphertext <> ''").
		Where("username <> ''").
		Where("auto_refresh_interval > 0").
		Where("last_sync_time = 0 OR last_sync_time + auto_refresh_interval <= ?", now).
		Order("id asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func GetExplicitChannelBaseURLs() ([]string, error) {
	var baseURLs []string
	err := DB.Model(&Channel{}).
		Where("base_url IS NOT NULL AND base_url <> ''").
		Pluck("base_url", &baseURLs).Error
	return baseURLs, err
}

type ExplicitChannelSource struct {
	BaseURL string `gorm:"column:base_url"`
	Status  int    `gorm:"column:status"`
}

func ListExplicitChannelSources() ([]ExplicitChannelSource, error) {
	var sources []ExplicitChannelSource
	err := DB.Model(&Channel{}).
		Select("base_url", "status").
		Where("base_url IS NOT NULL AND base_url <> ''").
		Scan(&sources).Error
	return sources, err
}

func (row *UpstreamChannel) HasPassword() bool {
	return row != nil && strings.TrimSpace(row.PasswordCiphertext) != ""
}

func (row *UpstreamChannel) EffectiveMultiplier() float64 {
	if row == nil || row.Multiplier <= 0 || math.IsNaN(row.Multiplier) || math.IsInf(row.Multiplier, 0) {
		return UpstreamChannelDefaultMultiplier
	}
	return row.Multiplier
}

func (row *UpstreamChannel) EffectiveAuthType() string {
	if row == nil {
		return UpstreamAuthTypePassword
	}
	return NormalizeUpstreamAuthType(row.AuthType)
}

func (row *UpstreamChannel) DecryptPassword() (string, error) {
	if !row.HasPassword() {
		return "", errors.New("upstream password is not configured")
	}
	return common.DecryptSecret("upstream-channel-password", row.PasswordCiphertext)
}

type UpsertImportedUpstreamChannelsResult struct {
	ChannelIDs []int
	Imported   int
	Updated    int
}

func UpsertImportedUpstreamChannels(channels []Channel) (UpsertImportedUpstreamChannelsResult, error) {
	if len(channels) == 0 {
		return UpsertImportedUpstreamChannelsResult{ChannelIDs: []int{}}, nil
	}
	result := UpsertImportedUpstreamChannelsResult{ChannelIDs: make([]int, 0, len(channels))}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for i := range channels {
			channel := &channels[i]
			baseURL := channel.GetBaseURL()
			var existing Channel
			err := tx.Where("base_url = ?", baseURL).Where(&Channel{Key: channel.Key}).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err = tx.Create(channel).Error; err != nil {
					return err
				}
				if strings.TrimSpace(channel.Models) != "" {
					if err = channel.AddAbilities(tx); err != nil {
						return err
					}
				}
				result.Imported++
				result.ChannelIDs = append(result.ChannelIDs, channel.Id)
				continue
			}
			if err != nil {
				return err
			}

			channel.Id = existing.Id
			updates := map[string]any{
				"type":       channel.Type,
				"key":        channel.Key,
				"status":     channel.Status,
				"name":       channel.Name,
				"base_url":   baseURL,
				"models":     channel.Models,
				"group":      channel.Group,
				"tag":        channel.Tag,
				"priority":   channel.Priority,
				"weight":     channel.Weight,
				"test_model": channel.TestModel,
				"auto_ban":   channel.AutoBan,
				"remark":     channel.Remark,
			}
			if err = tx.Model(&Channel{}).Where("id = ?", existing.Id).Updates(updates).Error; err != nil {
				return err
			}
			if strings.TrimSpace(channel.Models) == "" {
				if err = tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error; err != nil {
					return err
				}
			} else if err = channel.UpdateAbilities(tx); err != nil {
				return err
			}
			result.Updated++
			result.ChannelIDs = append(result.ChannelIDs, channel.Id)
		}
		return nil
	})
	return result, err
}
