package model

import (
	"testing"

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
