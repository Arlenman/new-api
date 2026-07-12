package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const maxTokenTagNameLength = 50

type TokenTag struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index;uniqueIndex:idx_token_tags_user_name_key,priority:1"`
	Name      string `json:"name" gorm:"size:50;not null"`
	NameKey   string `json:"-" gorm:"size:50;not null;uniqueIndex:idx_token_tags_user_name_key,priority:2"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

type TokenTagBinding struct {
	Id      int `json:"id"`
	TokenID int `json:"token_id" gorm:"index;uniqueIndex:idx_token_tag_bindings_token_tag,priority:1"`
	TagID   int `json:"tag_id" gorm:"index;uniqueIndex:idx_token_tag_bindings_token_tag,priority:2"`
}

func normalizeTokenTagNames(names []string) ([]string, []string, error) {
	normalizedNames := make([]string, 0, len(names))
	nameKeys := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		name := strings.Join(strings.Fields(strings.TrimSpace(rawName)), " ")
		if name == "" {
			continue
		}
		if len(name) > maxTokenTagNameLength {
			return nil, nil, errors.New("标签名称过长")
		}
		nameKey := strings.ToLower(name)
		if _, ok := seen[nameKey]; ok {
			continue
		}
		seen[nameKey] = struct{}{}
		normalizedNames = append(normalizedNames, name)
		nameKeys = append(nameKeys, nameKey)
	}
	return normalizedNames, nameKeys, nil
}

func ListTokenTagsByUser(userID int) ([]TokenTag, error) {
	if userID == 0 {
		return nil, errors.New("userID 为空")
	}
	var tags []TokenTag
	err := DB.Where("user_id = ?", userID).Order("name_key asc").Find(&tags).Error
	return tags, err
}

func ListTokenTagOptions(userID int, username string, role int) ([]TokenTag, error) {
	if role < common.RoleAdminUser {
		return ListTokenTagsByUser(userID)
	}
	if username != "" {
		var user User
		err := DB.Where("username = ?", username).First(&user).Error
		if err == nil {
			if user.Hidden {
				return []TokenTag{}, nil
			}
			return ListTokenTagsByUser(user.Id)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		var tags []TokenTag
		query := DB.Table("token_tags").
			Select("token_tags.id, token_tags.user_id, token_tags.name, token_tags.created_at, token_tags.updated_at").
			Joins("join token_tag_bindings on token_tag_bindings.tag_id = token_tags.id").
			Joins("join quota_data on quota_data.token_id = token_tag_bindings.token_id").
			Where("quota_data.username = ?", username)
		query, err = applyHiddenUserFilter(query, "quota_data.user_id", true)
		if err != nil {
			return nil, err
		}
		err = query.
			Group("token_tags.id, token_tags.user_id, token_tags.name, token_tags.created_at, token_tags.updated_at, token_tags.name_key").
			Order("token_tags.name_key asc").
			Find(&tags).Error
		return tags, err
	}

	var tags []TokenTag
	query := DB.Table("token_tags").
		Select("min(id) as id, min(user_id) as user_id, min(name) as name, min(created_at) as created_at, max(updated_at) as updated_at").
		Group("name_key").
		Order("name_key asc")
	query, err := applyHiddenUserFilter(query, "token_tags.user_id", true)
	if err != nil {
		return nil, err
	}
	err = query.Find(&tags).Error
	return tags, err
}

func GetTokenTagNames(userID int, tokenID int) ([]string, error) {
	if userID == 0 || tokenID == 0 {
		return nil, errors.New("userID 或 tokenID 为空")
	}
	var tags []TokenTag
	err := DB.Table("token_tags").
		Select("token_tags.name").
		Joins("join token_tag_bindings on token_tag_bindings.tag_id = token_tags.id").
		Joins("join tokens on tokens.id = token_tag_bindings.token_id").
		Where("tokens.id = ? and tokens.user_id = ? and token_tags.user_id = ?", tokenID, userID, userID).
		Order("token_tags.name_key asc").
		Find(&tags).Error
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	return names, nil
}

func GetTokenIDsByTagName(userID int, tagName string) ([]int, error) {
	names, nameKeys, err := normalizeTokenTagNames([]string{tagName})
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	query := DB.Table("token_tag_bindings").
		Select("token_tag_bindings.token_id").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id").
		Where("token_tags.name_key = ?", nameKeys[0])
	if userID > 0 {
		query = query.Where("token_tags.user_id = ?", userID)
	}
	var tokenIDs []int
	if err := query.Order("token_tag_bindings.token_id asc").Pluck("token_tag_bindings.token_id", &tokenIDs).Error; err != nil {
		return nil, err
	}
	return tokenIDs, nil
}

func ReplaceTokenTags(userID int, tokenID int, names []string) error {
	if userID == 0 || tokenID == 0 {
		return errors.New("userID 或 tokenID 为空")
	}
	normalizedNames, nameKeys, err := normalizeTokenTagNames(names)
	if err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		return replaceTokenTagsTx(tx, userID, tokenID, normalizedNames, nameKeys)
	})
}

func ReplaceTokenTagsTx(tx *gorm.DB, userID int, tokenID int, names []string) error {
	if userID == 0 || tokenID == 0 {
		return errors.New("userID 或 tokenID 为空")
	}
	normalizedNames, nameKeys, err := normalizeTokenTagNames(names)
	if err != nil {
		return err
	}
	return replaceTokenTagsTx(tx, userID, tokenID, normalizedNames, nameKeys)
}

func replaceTokenTagsTx(tx *gorm.DB, userID int, tokenID int, normalizedNames []string, nameKeys []string) error {
	var token Token
	if err := tx.Where("id = ? and user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		return err
	}
	if err := tx.Where("token_id = ?", tokenID).Delete(&TokenTagBinding{}).Error; err != nil {
		return err
	}
	for idx, name := range normalizedNames {
		nameKey := nameKeys[idx]
		tag := TokenTag{}
		err := tx.Where("user_id = ? and name_key = ?", userID, nameKey).First(&tag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := common.GetTimestamp()
			tag = TokenTag{
				UserID:    userID,
				Name:      name,
				NameKey:   nameKey,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := tx.Create(&tag).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.Create(&TokenTagBinding{TokenID: tokenID, TagID: tag.Id}).Error; err != nil {
			return err
		}
	}
	return nil
}
