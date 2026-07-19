package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditChannelByTagUpdatesAutoBanForMatchingChannels(t *testing.T) {
	truncateTables(t)

	tag := "shared-tag"
	otherTag := "other-tag"
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []Channel{
		{Name: "first", Tag: &tag, AutoBan: &autoBanDisabled},
		{Name: "second", Tag: &tag, AutoBan: &autoBanDisabled},
		{Name: "other", Tag: &otherTag, AutoBan: &autoBanDisabled},
	}
	require.NoError(t, DB.Create(&channels).Error)

	require.NoError(t, EditChannelByTag(
		tag,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&autoBanEnabled,
	))

	var updated []Channel
	require.NoError(t, DB.Order("id ASC").Find(&updated).Error)
	require.Len(t, updated, 3)
	require.True(t, updated[0].GetAutoBan())
	require.True(t, updated[1].GetAutoBan())
	require.False(t, updated[2].GetAutoBan())
}

func TestGetPaginatedChannelTagsSortsByTagPriority(t *testing.T) {
	truncateTables(t)

	alpha := "alpha"
	beta := "beta"
	gamma := "gamma"
	mixed := "mixed"
	negative := "negative"
	empty := ""
	priority10 := int64(10)
	priority30 := int64(30)
	priority5 := int64(5)
	priority8 := int64(8)
	priorityNegative := int64(-1)
	channels := []Channel{
		{Name: "alpha-1", Tag: &alpha, Priority: &priority10},
		{Name: "alpha-2", Tag: &alpha, Priority: &priority10},
		{Name: "beta-1", Tag: &beta, Priority: &priority30},
		{Name: "gamma-1", Tag: &gamma, Priority: &priority10},
		{Name: "mixed-1", Tag: &mixed, Priority: &priority5},
		{Name: "mixed-2", Tag: &mixed, Priority: &priority8},
		{Name: "negative-1", Tag: &negative, Priority: &priorityNegative},
		{Name: "empty-tag", Tag: &empty, Priority: &priority30},
		{Name: "no-tag", Priority: &priority30},
	}
	require.NoError(t, DB.Create(&channels).Error)

	ascending, err := GetPaginatedChannelTags(
		DB.Model(&Channel{}),
		0,
		10,
		NewChannelSortOptions("priority", "asc", false),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"negative", "mixed", "alpha", "gamma", "beta"}, channelTagNames(ascending))

	descending, err := GetPaginatedChannelTags(
		DB.Model(&Channel{}),
		0,
		10,
		NewChannelSortOptions("priority", "desc", false),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"beta", "alpha", "gamma", "mixed", "negative"}, channelTagNames(descending))

	searchAscending, err := SearchTags(
		"",
		"",
		"",
		false,
		NewChannelSortOptions("priority", "asc", false),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"negative", "mixed", "alpha", "gamma", "beta"}, channelTagNames(searchAscending))

	searchDescending, err := SearchTags(
		"",
		"",
		"",
		false,
		NewChannelSortOptions("priority", "desc", false),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"beta", "alpha", "gamma", "mixed", "negative"}, channelTagNames(searchDescending))
}

func TestGetChannelsByTagSortsChildrenByPriority(t *testing.T) {
	truncateTables(t)

	tag := "shared-tag"
	priority5 := int64(5)
	priority10 := int64(10)
	channels := []Channel{
		{Name: "priority-10-first", Tag: &tag, Priority: &priority10},
		{Name: "priority-5", Tag: &tag, Priority: &priority5},
		{Name: "priority-10-second", Tag: &tag, Priority: &priority10},
	}
	require.NoError(t, DB.Create(&channels).Error)

	ascending, err := GetChannelsByTag(
		tag,
		false,
		false,
		NewChannelSortOptions("priority", "asc", false),
	)
	require.NoError(t, err)
	require.Len(t, ascending, 3)
	assert.Equal(t, []string{"priority-5", "priority-10-first", "priority-10-second"}, []string{
		ascending[0].Name,
		ascending[1].Name,
		ascending[2].Name,
	})

	descending, err := GetChannelsByTag(
		tag,
		false,
		false,
		NewChannelSortOptions("priority", "desc", false),
	)
	require.NoError(t, err)
	require.Len(t, descending, 3)
	assert.Equal(t, []string{"priority-10-first", "priority-10-second", "priority-5"}, []string{
		descending[0].Name,
		descending[1].Name,
		descending[2].Name,
	})
}

func channelTagNames(tags []*string) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != nil {
			names = append(names, *tag)
		}
	}
	return names
}
