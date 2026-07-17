package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCacheGetRandomSatisfiedChannelExcludesCapacityFailedChannel(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		model.InitChannelCache()
	})

	modelName := "capacity-fallback-memory-cache"
	highPriorityChannelID := 9201
	mediumPriorityChannelID := 9202
	lowPriorityChannelID := 9203
	require.NoError(t, model.DB.Where("channel_id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&model.Ability{}).Error)
	require.NoError(t, model.DB.Where("id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&model.Channel{}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id IN ?", []int{highPriorityChannelID, mediumPriorityChannelID, lowPriorityChannelID}).Delete(&model.Channel{}).Error)
	})

	highPriority := int64(100)
	mediumPriority := int64(50)
	lowPriority := int64(0)
	for _, channel := range []*model.Channel{
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
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyModelCapacityExcludedChannelIds, map[int]struct{}{
		highPriorityChannelID: {},
	})
	channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         ctx,
		TokenGroup:  "default",
		ModelName:   modelName,
		RequestPath: "/v1/responses",
		Retry:       common.GetPointer(1),
	})

	require.NoError(t, err)
	require.Equal(t, "default", selectedGroup)
	require.NotNil(t, channel)
	require.Equal(t, mediumPriorityChannelID, channel.Id)
}
