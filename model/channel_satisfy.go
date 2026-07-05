package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func CountEnabledChannelsForGroupModel(group string, modelName string, requestPath string) int {
	if group == "" || modelName == "" {
		return 0
	}
	if !common.MemoryCacheEnabled {
		return countEnabledChannelsForGroupModelDB(group, modelName, requestPath)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return 0
	}

	channels := filterChannelsByRequestPath(group2model2channels[group][modelName], requestPath)
	if len(channels) == 0 {
		normalized := ratio_setting.FormatMatchingModelName(modelName)
		if normalized != "" && normalized != modelName {
			channels = filterChannelsByRequestPath(group2model2channels[group][normalized], requestPath)
		}
	}
	return countUniqueChannelIDs(channels)
}

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID)
	}
	return false
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	var count int64
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, modelName, channelID, true).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, normalized, channelID, true).
		Count(&count).Error
	return err == nil && count > 0
}

func countEnabledChannelsForGroupModelDB(group string, modelName string, requestPath string) int {
	var abilities []Ability
	err := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, modelName, true).
		Find(&abilities).Error
	if err != nil || len(abilities) == 0 {
		normalized := ratio_setting.FormatMatchingModelName(modelName)
		if normalized == "" || normalized == modelName {
			return 0
		}
		err = DB.Model(&Ability{}).
			Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, normalized, true).
			Find(&abilities).Error
		if err != nil {
			return 0
		}
	}
	abilities = filterAbilitiesByRequestPath(abilities, requestPath)
	ids := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		ids = append(ids, ability.ChannelId)
	}
	return countUniqueChannelIDs(ids)
}

func isChannelIDInList(list []int, channelID int) bool {
	for _, id := range list {
		if id == channelID {
			return true
		}
	}
	return false
}

func countUniqueChannelIDs(channelIDs []int) int {
	if len(channelIDs) == 0 {
		return 0
	}
	seen := make(map[int]struct{}, len(channelIDs))
	for _, id := range channelIDs {
		seen[id] = struct{}{}
	}
	return len(seen)
}
