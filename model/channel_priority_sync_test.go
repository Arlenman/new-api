package model

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateChannelPrioritiesByIDUpdatesChannelsAndAbilities(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-priority.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	DB = db
	t.Cleanup(func() { DB = originalDB })

	baseURL := "https://upstream.example"
	priorityOne := int64(1)
	priorityTwo := int64(2)
	channels := []Channel{
		{Id: 1, Name: "target", Key: "sk-target", BaseURL: &baseURL, Priority: &priorityOne},
		{Id: 2, Name: "other", Key: "sk-other", BaseURL: &baseURL, Priority: &priorityTwo},
	}
	require.NoError(t, db.Create(&channels).Error)
	abilities := []Ability{
		{Group: "default", Model: "gpt-4o-mini", ChannelId: 1, Enabled: true, Priority: &priorityOne},
		{Group: "premium", Model: "gpt-4o", ChannelId: 1, Enabled: true, Priority: &priorityOne},
		{Group: "default", Model: "claude-3-5-sonnet", ChannelId: 2, Enabled: true, Priority: &priorityTwo},
	}
	require.NoError(t, db.Create(&abilities).Error)

	require.NoError(t, UpdateChannelPrioritiesByID(map[int]int64{1: 15}))

	var updatedTarget Channel
	require.NoError(t, db.First(&updatedTarget, 1).Error)
	require.NotNil(t, updatedTarget.Priority)
	assert.Equal(t, int64(15), *updatedTarget.Priority)
	var untouched Channel
	require.NoError(t, db.First(&untouched, 2).Error)
	require.NotNil(t, untouched.Priority)
	assert.Equal(t, int64(2), *untouched.Priority)

	var targetAbilities []Ability
	require.NoError(t, db.Where("channel_id = ?", 1).Order("model ASC").Find(&targetAbilities).Error)
	require.Len(t, targetAbilities, 2)
	for _, ability := range targetAbilities {
		require.NotNil(t, ability.Priority)
		assert.Equal(t, int64(15), *ability.Priority)
	}
	var untouchedAbility Ability
	require.NoError(t, db.Where("channel_id = ?", 2).First(&untouchedAbility).Error)
	require.NotNil(t, untouchedAbility.Priority)
	assert.Equal(t, int64(2), *untouchedAbility.Priority)

	require.NoError(t, UpdateChannelPrioritiesByID(nil))
}
