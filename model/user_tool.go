package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	UserToolImagePlayground = "image-playground"
	UserToolInfiniteCanvas  = "infinite-canvas"

	MaxUserToolKindLength             = 64
	MaxUserToolItemKeyLength          = 255
	MaxUserToolStatusLength           = 32
	MaxUserToolClientMutationIDLength = 128
	MaxUserToolMutationAssetIDs       = 64
	MaxUserToolMutationPayloadBytes   = 8 * 1024 * 1024
	MaxUserToolChangePageSize         = 1000

	defaultUserToolAssetDir = "data/user-tool-assets"
	userToolRuntimeTTL      = 15 * time.Minute
)

var supportedUserTools = map[string]struct{}{
	UserToolImagePlayground: {},
	UserToolInfiniteCanvas:  {},
}

// UserToolItem stores the account-owned metadata/state for one embedded tool item.
// Payload is deliberately TEXT-compatible JSON so all supported databases behave alike.
type UserToolItem struct {
	ID            string    `json:"id" gorm:"primaryKey;type:varchar(64);index:idx_user_tool_page,priority:3"`
	UserID        int       `json:"user_id" gorm:"index;uniqueIndex:uq_user_tool_item,priority:1;index:idx_user_tool_page,priority:1"`
	Tool          string    `json:"tool" gorm:"type:varchar(64);uniqueIndex:uq_user_tool_item,priority:2;index:idx_user_tool_updated,priority:2;index:idx_user_tool_page,priority:2"`
	Kind          string    `json:"kind" gorm:"type:varchar(64);uniqueIndex:uq_user_tool_item,priority:3"`
	ItemKey       string    `json:"key" gorm:"type:varchar(255);uniqueIndex:uq_user_tool_item,priority:4"`
	SchemaVersion int       `json:"schema_version"`
	Revision      int64     `json:"revision"`
	Status        string    `json:"status" gorm:"type:varchar(32);index"`
	Payload       JSONValue `json:"payload" gorm:"type:text"`
	CreatedTime   int64     `json:"created_at" gorm:"bigint;index"`
	UpdatedTime   int64     `json:"updated_at" gorm:"bigint;index:idx_user_tool_updated,priority:3"`
	Deleted       bool      `json:"deleted" gorm:"index"`
}

// UserToolAsset contains metadata for a local-folder object-storage file.
type UserToolAsset struct {
	ID          string `json:"id" gorm:"primaryKey;type:varchar(64)"`
	UserID      int    `json:"user_id" gorm:"index;uniqueIndex:uq_user_tool_asset_hash,priority:1"`
	Sha256      string `json:"sha256" gorm:"type:char(64);uniqueIndex:uq_user_tool_asset_hash,priority:2;index"`
	Filename    string `json:"filename" gorm:"type:varchar(255)"`
	ContentType string `json:"content_type" gorm:"type:varchar(150)"`
	SizeBytes   int64  `json:"size_bytes"`
	StoragePath string `json:"storage_path" gorm:"type:varchar(512)"`
	CreatedTime int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedTime int64  `json:"updated_at" gorm:"bigint"`
	Deleted     bool   `json:"deleted" gorm:"index"`
}

type UserToolItemAsset struct {
	ItemID  string `json:"item_id" gorm:"primaryKey;type:varchar(64)"`
	AssetID string `json:"asset_id" gorm:"primaryKey;type:varchar(64);index"`
	UserID  int    `json:"user_id" gorm:"index"`
}

type UserToolPreference struct {
	UserID          int    `json:"user_id" gorm:"primaryKey"`
	Tool            string `json:"tool" gorm:"primaryKey;type:varchar(64)"`
	SelectedTokenID int    `json:"selected_token_id"`
	UpdatedTime     int64  `json:"updated_at" gorm:"bigint"`
}

type UserToolChange struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    int    `json:"user_id" gorm:"index:idx_user_tool_changes,priority:1"`
	Tool      string `json:"tool" gorm:"type:varchar(64);index:idx_user_tool_changes,priority:2"`
	ItemID    string `json:"item_id" gorm:"type:varchar(64);index"`
	Operation string `json:"operation" gorm:"type:varchar(16)"`
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

// UserToolMutationReceipt makes a client mutation idempotent within one account and tool.
// Response stores the exact first result so retries cannot advance item revisions again.
type UserToolMutationReceipt struct {
	UserID           int       `json:"user_id" gorm:"primaryKey"`
	Tool             string    `json:"tool" gorm:"primaryKey;type:varchar(64)"`
	ClientMutationID string    `json:"client_mutation_id" gorm:"primaryKey;type:varchar(128)"`
	RequestHash      string    `json:"request_hash" gorm:"type:char(64)"`
	Response         JSONValue `json:"response" gorm:"type:text"`
	CreatedTime      int64     `json:"created_at" gorm:"bigint;index"`
}

// UserToolRuntimeSession is an opaque, short-lived credential mapped to a New API Token.
// The plaintext credential is returned only once and is never persisted.
type UserToolRuntimeSession struct {
	ID        string `json:"id" gorm:"primaryKey;type:char(64)"`
	UserID    int    `json:"user_id" gorm:"index"`
	TokenID   int    `json:"token_id" gorm:"index"`
	Tool      string `json:"tool" gorm:"type:varchar(64);index"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func IsSupportedUserTool(tool string) bool {
	_, ok := supportedUserTools[strings.TrimSpace(tool)]
	return ok
}

func UserToolAssetDir() string {
	dir := strings.TrimSpace(os.Getenv("USER_TOOL_ASSET_DIR"))
	if dir == "" {
		dir = defaultUserToolAssetDir
	}
	return filepath.Clean(dir)
}

func UserToolAssetAbsolutePath(asset UserToolAsset) string {
	if filepath.IsAbs(asset.StoragePath) {
		return asset.StoragePath
	}
	return filepath.Join(UserToolAssetDir(), asset.StoragePath)
}

func NewUserToolRuntimeSession(userID, tokenID int, tool string) (credential string, session *UserToolRuntimeSession, err error) {
	if userID <= 0 || tokenID <= 0 || !IsSupportedUserTool(tool) {
		return "", nil, errors.New("invalid user tool runtime session")
	}
	if _, err := ValidateUserToolToken(userID, tokenID); err != nil {
		return "", nil, err
	}
	randomPart, err := common.GenerateRandomCharsKey(48)
	if err != nil {
		return "", nil, err
	}
	credential = "utrs_" + randomPart
	digest := sha256.Sum256([]byte(credential))
	now := time.Now().UnixMilli()
	session = &UserToolRuntimeSession{
		ID:        hex.EncodeToString(digest[:]),
		UserID:    userID,
		TokenID:   tokenID,
		Tool:      strings.TrimSpace(tool),
		ExpiresAt: now + userToolRuntimeTTL.Milliseconds(),
		CreatedAt: now,
	}
	if err := DB.Create(session).Error; err != nil {
		return "", nil, err
	}
	return credential, session, nil
}

func ResolveUserToolRuntimeSession(credential string) (*UserToolRuntimeSession, error) {
	credential = strings.TrimSpace(credential)
	if !strings.HasPrefix(credential, "utrs_") {
		return nil, gorm.ErrRecordNotFound
	}
	digest := sha256.Sum256([]byte(credential))
	var session UserToolRuntimeSession
	if err := DB.Where("id = ?", hex.EncodeToString(digest[:])).First(&session).Error; err != nil {
		return nil, err
	}
	if session.ExpiresAt <= time.Now().UnixMilli() {
		_ = DB.Delete(&session).Error
		return nil, gorm.ErrRecordNotFound
	}
	return &session, nil
}

func ResolveUserToolRuntimeToken(credential string) (*UserToolRuntimeSession, *Token, error) {
	session, err := ResolveUserToolRuntimeSession(credential)
	if err != nil {
		return nil, nil, err
	}
	token, err := ValidateUserToolToken(session.UserID, session.TokenID)
	if err != nil {
		return session, token, err
	}
	return session, token, nil
}

func ValidateUserToolToken(userID, tokenID int) (*Token, error) {
	if userID <= 0 || tokenID <= 0 {
		return nil, ErrTokenInvalid
	}
	token, err := GetTokenByIds(tokenID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTokenInvalid
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return ValidateUserToolTokenRecord(userID, token)
}

func ValidateUserToolTokenRecord(userID int, token *Token) (*Token, error) {
	if userID <= 0 || token == nil || token.Id <= 0 || token.UserId != userID || token.Key == "" {
		return nil, ErrTokenInvalid
	}

	var validated *Token
	var err error
	if common.RedisEnabled {
		validated, err = ValidateUserToken(token.Key)
	} else {
		validated, err = resetDueTokenQuota(token)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrTokenInvalid
			}
			return validated, fmt.Errorf("%w: %v", ErrDatabase, err)
		}
		validated, err = validateUserTokenState(validated)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTokenInvalid
		}
		return validated, err
	}
	if validated.Id != token.Id || validated.UserId != userID {
		return nil, ErrTokenInvalid
	}
	return validated, nil
}

func GetUserToolItem(userID int, tool, kind, itemKey string) (*UserToolItem, error) {
	if userID <= 0 || !IsSupportedUserTool(tool) {
		return nil, errors.New("invalid user tool item")
	}
	var item UserToolItem
	if err := DB.Where("user_id = ? AND tool = ? AND kind = ? AND item_key = ?", userID, tool, kind, itemKey).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func ListUserToolItems(userID int, tool string, includeDeleted bool) ([]UserToolItem, error) {
	query := DB.Where("user_id = ? AND tool = ?", userID, tool).Order("updated_time ASC, id ASC")
	if !includeDeleted {
		query = query.Where("deleted = ?", false)
	}
	var items []UserToolItem
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func ListUserToolItemsPage(userID int, tool string, includeDeleted bool, afterID string, limit, payloadByteLimit int) ([]UserToolItem, bool, error) {
	if userID <= 0 || !IsSupportedUserTool(tool) || len(afterID) > 64 {
		return nil, false, errors.New("invalid user tool item page")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if payloadByteLimit <= 0 {
		payloadByteLimit = 4 * 1024 * 1024
	}

	query := DB.Model(&UserToolItem{}).
		Where("user_id = ? AND tool = ?", userID, tool).
		Order("id ASC").
		Limit(limit + 1)
	if !includeDeleted {
		query = query.Where("deleted = ?", false)
	}
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}

	rows, err := query.Rows()
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	items := make([]UserToolItem, 0, limit)
	payloadBytes := 0
	hasMore := false
	for rows.Next() {
		var item UserToolItem
		if err := DB.ScanRows(rows, &item); err != nil {
			return nil, false, err
		}
		itemBytes := len(item.Payload) + len(item.ID) + len(item.Kind) + len(item.ItemKey) + len(item.Status) + 256
		if len(items) >= limit || (len(items) > 0 && payloadBytes+itemBytes > payloadByteLimit) {
			hasMore = true
			break
		}
		items = append(items, item)
		payloadBytes += itemBytes
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return items, hasMore, nil
}

func ListUserToolChanges(userID int, tool string, cursor int64, limit int) ([]UserToolChange, error) {
	if limit <= 0 || limit > MaxUserToolChangePageSize+1 {
		limit = 500
	}
	var changes []UserToolChange
	err := DB.Where("user_id = ? AND tool = ? AND id > ?", userID, tool, cursor).
		Order("id ASC").Limit(limit).Find(&changes).Error
	return changes, err
}

func MaxUserToolChangeID(userID int, tool string) (int64, error) {
	var result struct{ ID int64 }
	err := DB.Model(&UserToolChange{}).Where("user_id = ? AND tool = ?", userID, tool).Select("COALESCE(MAX(id), 0) AS id").Scan(&result).Error
	return result.ID, err
}

func DeleteExpiredUserToolRuntimeSessions() {
	_ = DB.Where("expires_at <= ?", time.Now().UnixMilli()).Delete(&UserToolRuntimeSession{}).Error
}

func ValidateUserToolItemInput(tool, kind, itemKey string) error {
	tool = strings.TrimSpace(tool)
	kind = strings.TrimSpace(kind)
	itemKey = strings.TrimSpace(itemKey)
	if !IsSupportedUserTool(tool) {
		return fmt.Errorf("unsupported user tool: %s", tool)
	}
	if kind == "" || len(kind) > MaxUserToolKindLength || itemKey == "" || len(itemKey) > MaxUserToolItemKeyLength {
		return errors.New("invalid user tool item key")
	}
	return nil
}
