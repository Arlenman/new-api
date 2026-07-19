package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	UserToolMutationResultApplied  = "applied"
	UserToolMutationResultConflict = "conflict"
)

var (
	ErrUserToolMutationIDReused = errors.New("client mutation id was already used for different content")
	ErrUserToolAssetUnavailable = errors.New("one or more assets are unavailable")
)

type UserToolMutation struct {
	ClientMutationID string
	Kind             string
	ItemKey          string
	SchemaVersion    int
	BaseRevision     int64
	Status           string
	Payload          JSONValue
	AssetIDs         []string
	CreatedTime      int64
	Deleted          bool
}

type UserToolMutationResult struct {
	Item     *UserToolItem
	AssetIDs []string
	Result   string
	Message  string
	Replayed bool
}

type userToolMutationReceiptSnapshot struct {
	Item     *UserToolItem `json:"item,omitempty"`
	AssetIDs []string      `json:"asset_ids"`
	Result   string        `json:"result"`
	Message  string        `json:"message,omitempty"`
}

type userToolMutationHashInput struct {
	Kind          string    `json:"kind"`
	ItemKey       string    `json:"key"`
	SchemaVersion int       `json:"schema_version"`
	BaseRevision  int64     `json:"base_revision"`
	Status        string    `json:"status"`
	Payload       JSONValue `json:"payload"`
	AssetIDs      []string  `json:"asset_ids"`
	CreatedTime   int64     `json:"created_at"`
	Deleted       bool      `json:"deleted"`
}

func ValidateUserToolMutation(tool string, mutation UserToolMutation) error {
	if err := ValidateUserToolItemInput(tool, mutation.Kind, mutation.ItemKey); err != nil {
		return err
	}
	if strings.TrimSpace(mutation.ClientMutationID) == "" || len(strings.TrimSpace(mutation.ClientMutationID)) > MaxUserToolClientMutationIDLength {
		return errors.New("invalid client mutation id")
	}
	if mutation.SchemaVersion < 0 {
		return errors.New("schema version must not be negative")
	}
	if mutation.BaseRevision < 0 {
		return errors.New("base revision must not be negative")
	}
	if mutation.CreatedTime < 0 {
		return errors.New("created time must not be negative")
	}
	if len(strings.TrimSpace(mutation.Status)) > MaxUserToolStatusLength {
		return errors.New("user tool item status is too long")
	}
	if len(mutation.Payload) > MaxUserToolMutationPayloadBytes {
		return errors.New("user tool item payload is too large")
	}
	if len(mutation.Payload) > 0 {
		var decoded any
		if err := common.Unmarshal([]byte(mutation.Payload), &decoded); err != nil {
			return errors.New("user tool item payload must be valid JSON")
		}
	}
	if len(mutation.AssetIDs) > MaxUserToolMutationAssetIDs {
		return errors.New("too many assets on user tool item")
	}
	for _, assetID := range mutation.AssetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" || len(assetID) > 64 {
			return errors.New("invalid user tool asset id")
		}
	}
	return nil
}

func ApplyUserToolMutation(userID int, tool string, mutation UserToolMutation) (*UserToolMutationResult, error) {
	mutation.ClientMutationID = strings.TrimSpace(mutation.ClientMutationID)
	mutation.Kind = strings.TrimSpace(mutation.Kind)
	mutation.ItemKey = strings.TrimSpace(mutation.ItemKey)
	mutation.Status = strings.TrimSpace(mutation.Status)
	if len(mutation.Payload) == 0 {
		mutation.Payload = JSONValue("null")
	}
	if mutation.Deleted {
		mutation.AssetIDs = nil
	} else {
		mutation.AssetIDs = uniqueUserToolAssetIDs(mutation.AssetIDs)
	}
	if err := ValidateUserToolMutation(tool, mutation); err != nil {
		return nil, err
	}
	requestHash, err := hashUserToolMutation(mutation)
	if err != nil {
		return nil, err
	}

	result := &UserToolMutationResult{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		replayed, err := loadUserToolMutationReceipt(tx, userID, tool, mutation.ClientMutationID, requestHash)
		if err != nil {
			return err
		}
		if replayed != nil {
			*result = *replayed
			result.Replayed = true
			return nil
		}

		if err := validateUserToolAssetOwnership(tx, userID, mutation.AssetIDs); err != nil {
			return err
		}

		var existing UserToolItem
		err = lockForUpdate(tx).Where(
			"user_id = ? AND tool = ? AND kind = ? AND item_key = ?",
			userID,
			tool,
			mutation.Kind,
			mutation.ItemKey,
		).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now().UnixMilli()
		operation := "upsert"
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if mutation.BaseRevision != 0 {
				result.Result = UserToolMutationResultConflict
				result.Message = "base revision does not match server state"
				return saveUserToolMutationReceipt(tx, userID, tool, mutation.ClientMutationID, requestHash, result, now)
			}
			createdTime := mutation.CreatedTime
			if createdTime <= 0 {
				createdTime = now
			}
			item := &UserToolItem{
				ID:            "uti_" + common.GetUUID(),
				UserID:        userID,
				Tool:          tool,
				Kind:          mutation.Kind,
				ItemKey:       mutation.ItemKey,
				SchemaVersion: mutation.SchemaVersion,
				Revision:      1,
				Status:        mutation.Status,
				Payload:       mutation.Payload,
				CreatedTime:   createdTime,
				UpdatedTime:   now,
				Deleted:       mutation.Deleted,
			}
			if mutation.Deleted {
				operation = "delete"
			}
			if err := tx.Create(item).Error; err != nil {
				return err
			}
			if err := replaceUserToolItemAssets(tx, userID, item.ID, mutation.AssetIDs, mutation.Deleted); err != nil {
				return err
			}
			if err := createUserToolChange(tx, item, operation, now); err != nil {
				return err
			}
			result.Item = item
			result.AssetIDs = mutation.AssetIDs
			result.Result = UserToolMutationResultApplied
			return saveUserToolMutationReceipt(tx, userID, tool, mutation.ClientMutationID, requestHash, result, now)
		}

		if mutation.BaseRevision != existing.Revision || userToolStatusWouldRegress(existing.Kind, existing.Status, mutation.Status) {
			assetIDs, err := listUserToolItemAssetIDs(tx, userID, existing.ID)
			if err != nil {
				return err
			}
			result.Item = &existing
			result.AssetIDs = assetIDs
			result.Result = UserToolMutationResultConflict
			if mutation.BaseRevision != existing.Revision {
				result.Message = "base revision does not match server state"
			} else {
				result.Message = "terminal task status cannot regress"
			}
			return saveUserToolMutationReceipt(tx, userID, tool, mutation.ClientMutationID, requestHash, result, now)
		}

		revision := existing.Revision + 1
		updates := map[string]any{
			"schema_version": mutation.SchemaVersion,
			"revision":       revision,
			"status":         mutation.Status,
			"payload":        mutation.Payload,
			"updated_time":   now,
			"deleted":        mutation.Deleted,
		}
		if mutation.Deleted {
			operation = "delete"
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		existing.SchemaVersion = mutation.SchemaVersion
		existing.Revision = revision
		existing.Status = mutation.Status
		existing.Payload = mutation.Payload
		existing.UpdatedTime = now
		existing.Deleted = mutation.Deleted
		if err := replaceUserToolItemAssets(tx, userID, existing.ID, mutation.AssetIDs, mutation.Deleted); err != nil {
			return err
		}
		if err := createUserToolChange(tx, &existing, operation, now); err != nil {
			return err
		}
		result.Item = &existing
		result.AssetIDs = mutation.AssetIDs
		result.Result = UserToolMutationResultApplied
		return saveUserToolMutationReceipt(tx, userID, tool, mutation.ClientMutationID, requestHash, result, now)
	})
	if err == nil {
		return result, nil
	}

	// A concurrent retry can win the receipt unique key while this transaction rolls back.
	// Reading the committed receipt here preserves idempotency across SQLite, MySQL, and PostgreSQL.
	replayed, replayErr := loadUserToolMutationReceipt(DB, userID, tool, mutation.ClientMutationID, requestHash)
	if replayErr == nil && replayed != nil {
		replayed.Replayed = true
		return replayed, nil
	}
	return nil, err
}

func hashUserToolMutation(mutation UserToolMutation) (string, error) {
	encoded, err := common.Marshal(userToolMutationHashInput{
		Kind:          mutation.Kind,
		ItemKey:       mutation.ItemKey,
		SchemaVersion: mutation.SchemaVersion,
		BaseRevision:  mutation.BaseRevision,
		Status:        mutation.Status,
		Payload:       mutation.Payload,
		AssetIDs:      mutation.AssetIDs,
		CreatedTime:   mutation.CreatedTime,
		Deleted:       mutation.Deleted,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func loadUserToolMutationReceipt(tx *gorm.DB, userID int, tool, clientMutationID, requestHash string) (*UserToolMutationResult, error) {
	var receipt UserToolMutationReceipt
	err := tx.Where(
		"user_id = ? AND tool = ? AND client_mutation_id = ?",
		userID,
		tool,
		clientMutationID,
	).First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.RequestHash != requestHash {
		return nil, ErrUserToolMutationIDReused
	}
	var snapshot userToolMutationReceiptSnapshot
	if err := common.Unmarshal([]byte(receipt.Response), &snapshot); err != nil {
		return nil, err
	}
	return &UserToolMutationResult{
		Item:     snapshot.Item,
		AssetIDs: snapshot.AssetIDs,
		Result:   snapshot.Result,
		Message:  snapshot.Message,
	}, nil
}

func saveUserToolMutationReceipt(tx *gorm.DB, userID int, tool, clientMutationID, requestHash string, result *UserToolMutationResult, now int64) error {
	assetIDs := result.AssetIDs
	if assetIDs == nil {
		assetIDs = []string{}
	}
	response, err := common.Marshal(userToolMutationReceiptSnapshot{
		Item:     result.Item,
		AssetIDs: assetIDs,
		Result:   result.Result,
		Message:  result.Message,
	})
	if err != nil {
		return err
	}
	return tx.Create(&UserToolMutationReceipt{
		UserID:           userID,
		Tool:             tool,
		ClientMutationID: clientMutationID,
		RequestHash:      requestHash,
		Response:         JSONValue(response),
		CreatedTime:      now,
	}).Error
}

func GetUserToolItemAssetIDs(userID int, itemID string) ([]string, error) {
	return listUserToolItemAssetIDs(DB, userID, itemID)
}

func GetUserToolItemsByIDs(userID int, tool string, itemIDs []string) ([]UserToolItem, error) {
	if len(itemIDs) == 0 {
		return []UserToolItem{}, nil
	}
	var items []UserToolItem
	err := DB.Where("user_id = ? AND tool = ? AND id IN ?", userID, tool, itemIDs).Find(&items).Error
	return items, err
}

func GetUserToolPreference(userID int, tool string) (*UserToolPreference, error) {
	var preference UserToolPreference
	err := DB.Where("user_id = ? AND tool = ?", userID, tool).First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &UserToolPreference{UserID: userID, Tool: tool}, nil
	}
	return &preference, err
}

func SaveUserToolPreference(userID int, tool string, tokenID int) (*UserToolPreference, error) {
	if userID <= 0 || !IsSupportedUserTool(tool) || tokenID < 0 {
		return nil, errors.New("invalid user tool preference")
	}
	if tokenID > 0 {
		if _, err := ValidateUserToolToken(userID, tokenID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UnixMilli()
	preference := &UserToolPreference{
		UserID:          userID,
		Tool:            tool,
		SelectedTokenID: tokenID,
		UpdatedTime:     now,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing UserToolPreference
		err := lockForUpdate(tx).Where("user_id = ? AND tool = ?", userID, tool).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(preference).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&existing).Updates(map[string]any{
			"selected_token_id": tokenID,
			"updated_time":      now,
		}).Error
	})
	return preference, err
}

func uniqueUserToolAssetIDs(assetIDs []string) []string {
	seen := make(map[string]struct{}, len(assetIDs))
	unique := make([]string, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" {
			continue
		}
		if _, ok := seen[assetID]; ok {
			continue
		}
		seen[assetID] = struct{}{}
		unique = append(unique, assetID)
	}
	sort.Strings(unique)
	return unique
}

func validateUserToolAssetOwnership(tx *gorm.DB, userID int, assetIDs []string) error {
	if len(assetIDs) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&UserToolAsset{}).Where("user_id = ? AND id IN ? AND deleted = ?", userID, assetIDs, false).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(assetIDs)) {
		return ErrUserToolAssetUnavailable
	}
	return nil
}

func replaceUserToolItemAssets(tx *gorm.DB, userID int, itemID string, assetIDs []string, deleted bool) error {
	if err := tx.Where("user_id = ? AND item_id = ?", userID, itemID).Delete(&UserToolItemAsset{}).Error; err != nil {
		return err
	}
	if deleted || len(assetIDs) == 0 {
		return nil
	}
	links := make([]UserToolItemAsset, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		links = append(links, UserToolItemAsset{ItemID: itemID, AssetID: assetID, UserID: userID})
	}
	return tx.Create(&links).Error
}

func listUserToolItemAssetIDs(tx *gorm.DB, userID int, itemID string) ([]string, error) {
	var links []UserToolItemAsset
	if err := tx.Where("user_id = ? AND item_id = ?", userID, itemID).Order("asset_id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	assetIDs := make([]string, 0, len(links))
	for _, link := range links {
		assetIDs = append(assetIDs, link.AssetID)
	}
	return assetIDs, nil
}

func createUserToolChange(tx *gorm.DB, item *UserToolItem, operation string, now int64) error {
	return tx.Create(&UserToolChange{
		UserID:    item.UserID,
		Tool:      item.Tool,
		ItemID:    item.ID,
		Operation: operation,
		Revision:  item.Revision,
		CreatedAt: now,
	}).Error
}

func userToolStatusWouldRegress(kind, current, next string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !strings.Contains(kind, "task") && !strings.Contains(kind, "generation") {
		return false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	next = strings.ToLower(strings.TrimSpace(next))
	terminal := map[string]bool{
		"done": true, "complete": true, "completed": true,
		"failed": true, "error": true, "canceled": true, "cancelled": true,
	}
	return terminal[current] && !terminal[next]
}
