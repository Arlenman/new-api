package model

import (
	"context"

	"gorm.io/gorm"
)

func SetUserHidden(userID int, hidden bool) error {
	return DB.Model(&User{}).
		Where("id = ?", userID).
		Update("hidden", hidden).Error
}

func getHiddenUserIDs(ctx context.Context) ([]int, error) {
	var ids []int
	err := DB.WithContext(ctx).Model(&User{}).
		Where("hidden = ?", true).
		Pluck("id", &ids).Error
	return ids, err
}

func applyHiddenUserFilter(tx *gorm.DB, userIDColumn string, excludeHidden bool) (*gorm.DB, error) {
	if !excludeHidden {
		return tx, nil
	}
	hiddenUserIDs, err := getHiddenUserIDs(tx.Statement.Context)
	if err != nil {
		return nil, err
	}
	if len(hiddenUserIDs) == 0 {
		return tx, nil
	}
	return tx.Where(userIDColumn+" NOT IN ?", hiddenUserIDs), nil
}
