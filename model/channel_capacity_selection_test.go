package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannelExcludesCapacityFailedChannelWithoutMemoryCache(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	modelName := "capacity-fallback-database"
	highPriorityChannelID := 9301
	mediumPriorityChannelID := 9302
	lowPriorityChannelID := 9303
	require.NoError(t, DB.Where("channel_id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&Ability{}).Error)
	require.NoError(t, DB.Where("id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&Channel{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&Channel{}).Error)
	})

	highPriority := int64(100)
	mediumPriority := int64(50)
	lowPriority := int64(0)
	for _, channel := range []*Channel{
		{
			Id:       highPriorityChannelID,
			Type:     1,
			Key:      "high-priority-key",
			Status:   common.ChannelStatusEnabled,
			Name:     "high-priority-channel",
			Models:   modelName,
			Group:    "default",
			Priority: &highPriority,
		},
		{
			Id:       mediumPriorityChannelID,
			Type:     1,
			Key:      "medium-priority-key",
			Status:   common.ChannelStatusEnabled,
			Name:     "medium-priority-channel",
			Models:   modelName,
			Group:    "default",
			Priority: &mediumPriority,
		},
		{
			Id:       lowPriorityChannelID,
			Type:     1,
			Key:      "low-priority-key",
			Status:   common.ChannelStatusEnabled,
			Name:     "low-priority-channel",
			Models:   modelName,
			Group:    "default",
			Priority: &lowPriority,
		},
	} {
		require.NoError(t, channel.Insert())
	}

	channel, err := GetRandomSatisfiedChannel("default", modelName, 0, "/v1/responses", map[int]struct{}{
		highPriorityChannelID: {},
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, mediumPriorityChannelID, channel.Id)
}
