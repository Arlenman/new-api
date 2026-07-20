package model

import "gorm.io/gorm"

func SetUserHidden(userID int, hidden bool) error {
	return DB.Model(&User{}).
		Where("id = ?", userID).
		Update("hidden", hidden).Error
}

func getHiddenUserIDs() ([]int, error) {
	var ids []int
	err := DB.Model(&User{}).
		Where("hidden = ?", true).
		Pluck("id", &ids).Error
	return ids, err
}

func applyHiddenUserFilter(tx *gorm.DB, userIDColumn string, excludeHidden bool) (*gorm.DB, error) {
	if !excludeHidden {
		return tx, nil
	}
	hiddenUserIDs, err := getHiddenUserIDs()
	if err != nil {
		return nil, err
	}
	if len(hiddenUserIDs) == 0 {
		return tx, nil
	}
	return tx.Where(userIDColumn+" NOT IN ?", hiddenUserIDs), nil
}
