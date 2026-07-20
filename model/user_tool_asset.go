package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const MaxUserToolAssetBytes int64 = 512 * 1024 * 1024

func StoreUserToolAsset(userID int, filename, contentType, claimedSha256 string, reader io.Reader) (*UserToolAsset, error) {
	if userID <= 0 || reader == nil {
		return nil, errors.New("invalid asset upload")
	}
	root := UserToolAssetDir()
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(root, ".upload-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(reader, MaxUserToolAssetBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written <= 0 || written > MaxUserToolAssetBytes {
		return nil, fmt.Errorf("asset size must be between 1 and %d bytes", MaxUserToolAssetBytes)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	claimedSha256 = strings.ToLower(strings.TrimSpace(claimedSha256))
	if claimedSha256 != "" && claimedSha256 != digest {
		return nil, errors.New("asset sha256 mismatch")
	}

	var existing UserToolAsset
	err = DB.Where("user_id = ? AND sha256 = ?", userID, digest).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	relativePath := filepath.Join(fmt.Sprintf("%d", userID), digest[:2], digest)
	absolutePath := filepath.Join(root, relativePath)
	assetFileReady := false
	if fileInfo, statErr := os.Stat(absolutePath); statErr == nil {
		if !fileInfo.Mode().IsRegular() {
			return nil, errors.New("asset storage path is not a regular file")
		}
		assetFileReady = fileInfo.Size() == written
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if err == nil && !existing.Deleted && existing.StoragePath == relativePath && assetFileReady {
		return &existing, nil
	}
	if !assetFileReady {
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
			return nil, err
		}
		if err := os.Rename(tempPath, absolutePath); err != nil {
			return nil, err
		}
		if err := os.Chmod(absolutePath, 0o640); err != nil {
			return nil, err
		}
	}

	now := time.Now().UnixMilli()
	filename = sanitizeUserToolAssetFilename(filename)
	if filename == "" {
		filename = digest
	}
	contentType = NormalizeUserToolAssetContentType(contentType)
	if err == nil && !existing.Deleted {
		updateResult := DB.Model(&UserToolAsset{}).
			Where("id = ? AND user_id = ? AND deleted = ?", existing.ID, userID, false).
			Updates(map[string]any{
				"size_bytes":   written,
				"storage_path": relativePath,
				"updated_time": now,
			})
		if updateResult.Error != nil {
			return nil, updateResult.Error
		}
		if updateResult.RowsAffected == 0 {
			var active UserToolAsset
			if lookupErr := DB.Where("user_id = ? AND sha256 = ? AND deleted = ?", userID, digest, false).First(&active).Error; lookupErr == nil {
				return &active, nil
			}
			return nil, errors.New("asset repair raced with another mutation")
		}
		existing.SizeBytes = written
		existing.StoragePath = relativePath
		existing.UpdatedTime = now
		return &existing, nil
	}
	if err == nil && existing.Deleted {
		updates := map[string]any{
			"filename":     filename,
			"content_type": contentType,
			"size_bytes":   written,
			"storage_path": relativePath,
			"updated_time": now,
			"deleted":      false,
		}
		updateResult := DB.Model(&UserToolAsset{}).
			Where("id = ? AND user_id = ? AND deleted = ?", existing.ID, userID, true).
			Updates(updates)
		if updateResult.Error != nil {
			return nil, updateResult.Error
		}
		if updateResult.RowsAffected == 0 {
			var active UserToolAsset
			if lookupErr := DB.Where("user_id = ? AND sha256 = ? AND deleted = ?", userID, digest, false).First(&active).Error; lookupErr == nil {
				return &active, nil
			}
			return nil, errors.New("asset reactivation raced with another upload")
		}
		existing.Filename = filename
		existing.ContentType = contentType
		existing.SizeBytes = written
		existing.StoragePath = relativePath
		existing.UpdatedTime = now
		existing.Deleted = false
		return &existing, nil
	}

	asset := &UserToolAsset{
		ID:          "uta_" + common.GetUUID(),
		UserID:      userID,
		Sha256:      digest,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   written,
		StoragePath: relativePath,
		CreatedTime: now,
		UpdatedTime: now,
	}
	if err := DB.Create(asset).Error; err != nil {
		var raced UserToolAsset
		if lookupErr := DB.Where("user_id = ? AND sha256 = ? AND deleted = ?", userID, digest, false).First(&raced).Error; lookupErr == nil {
			return &raced, nil
		}
		return nil, err
	}
	return asset, nil
}

func ResolveUserToolAsset(userID int, assetID string) (*UserToolAsset, string, error) {
	var asset UserToolAsset
	if err := DB.Where("id = ? AND user_id = ? AND deleted = ?", strings.TrimSpace(assetID), userID, false).First(&asset).Error; err != nil {
		return nil, "", err
	}
	absolutePath := UserToolAssetAbsolutePath(asset)
	root, err := filepath.Abs(UserToolAssetDir())
	if err != nil {
		return nil, "", err
	}
	absolutePath, err = filepath.Abs(absolutePath)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(root, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("invalid asset storage path")
	}
	return &asset, absolutePath, nil
}

func GetUserToolItemAssetIDsByItemIDs(userID int, itemIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(itemIDs))
	if userID <= 0 || len(itemIDs) == 0 {
		return result, nil
	}

	var links []UserToolItemAsset
	if err := DB.Where("user_id = ? AND item_id IN ?", userID, itemIDs).
		Order("item_id ASC, asset_id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	for _, link := range links {
		result[link.ItemID] = append(result[link.ItemID], link.AssetID)
	}
	return result, nil
}

func GetUserToolAssetsByIDs(userID int, assetIDSet map[string]struct{}) ([]UserToolAsset, error) {
	if len(assetIDSet) == 0 {
		return []UserToolAsset{}, nil
	}
	assetIDs := make([]string, 0, len(assetIDSet))
	for assetID := range assetIDSet {
		assetIDs = append(assetIDs, assetID)
	}
	sort.Strings(assetIDs)
	var assets []UserToolAsset
	err := DB.Where("user_id = ? AND id IN ? AND deleted = ?", userID, assetIDs, false).
		Order("created_time ASC, id ASC").Find(&assets).Error
	return assets, err
}

func sanitizeUserToolAssetFilename(filename string) string {
	filename = strings.TrimSpace(filepath.Base(filename))
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, filename)
	runes := []rune(filename)
	if len(runes) > 255 {
		filename = string(runes[:255])
	}
	return filename
}

func NormalizeUserToolAssetContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if err != nil || mediaType == "" || len(mediaType) > 150 {
		return "application/octet-stream"
	}
	return mediaType
}
