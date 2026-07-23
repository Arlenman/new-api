package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPriorityGroupingPaginatesDistinctPriorities(t *testing.T) {
	truncateTables(t)

	priority30 := int64(30)
	priority10 := int64(10)
	priorityNegative := int64(-5)
	channels := []Channel{
		{Name: "priority-30", Priority: &priority30},
		{Name: "priority-10-first", Priority: &priority10},
		{Name: "priority-10-second", Priority: &priority10},
		{Name: "priority-zero"},
		{Name: "priority-negative", Priority: &priorityNegative},
	}
	require.NoError(t, DB.Create(&channels).Error)

	firstPage, err := GetPaginatedChannelPriorities(DB.Model(&Channel{}), 0, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{30, 10}, firstPage)

	secondPage, err := GetPaginatedChannelPriorities(DB.Model(&Channel{}), 2, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{0, -5}, secondPage)

	total, err := CountChannelPriorities(DB.Model(&Channel{}))
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)

	priorityChannels, err := GetChannelsByPriority(DB.Model(&Channel{}), 10)
	require.NoError(t, err)
	require.Len(t, priorityChannels, 2)
	assert.Equal(t, []string{"priority-10-first", "priority-10-second"}, []string{
		priorityChannels[0].Name,
		priorityChannels[1].Name,
	})
}

func TestChannelPriorityGroupingUsesSearchAndGroupFilters(t *testing.T) {
	truncateTables(t)

	priority30 := int64(30)
	priority20 := int64(20)
	priority10 := int64(10)
	baseURL := "https://needle.example.com"
	channels := []Channel{
		{Name: "matching", Priority: &priority30, Group: "team,default", Models: "gpt-4o", BaseURL: &baseURL},
		{Name: "wrong-model", Priority: &priority20, Group: "team", Models: "claude-3", BaseURL: &baseURL},
		{Name: "wrong-group", Priority: &priority10, Group: "other", Models: "gpt-4o", BaseURL: &baseURL},
	}
	require.NoError(t, DB.Create(&channels).Error)

	query := ApplyChannelGroupFilter(DB.Model(&Channel{}), "team")
	query = ApplyChannelSearchFilter(query, "needle", "gpt-4o")
	priorities, err := GetPaginatedChannelPriorities(query, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, []int64{30}, priorities)

	matchedChannels, err := GetChannelsByPriority(
		ApplyChannelSearchFilter(ApplyChannelGroupFilter(DB.Model(&Channel{}), "team"), "needle", "gpt-4o"),
		30,
	)
	require.NoError(t, err)
	require.Len(t, matchedChannels, 1)
	assert.Equal(t, "matching", matchedChannels[0].Name)
	assert.Empty(t, matchedChannels[0].Key)
	assert.Equal(t, common.ChannelStatusEnabled, matchedChannels[0].Status)
}
