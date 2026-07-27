package service

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetUpstreamChannelStatisticsAggregatesNormalizedSourcesAndKeepsCompleteManagedList(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-statistics.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	managed := []*model.UpstreamChannel{
		{Name: "Primary", BaseURL: "https://api.example.com/v1/", BaseURLHash: model.UpstreamBaseURLHash("https://api.example.com/v1/"), Provider: UpstreamProviderNewAPI, Priority: 30},
		{Name: "Secondary", BaseURL: "https://other.example/api", BaseURLHash: model.UpstreamBaseURLHash("https://other.example/api"), Provider: UpstreamProviderSub2API, Priority: 20},
		{Name: "No usage", BaseURL: "https://zero.example", BaseURLHash: model.UpstreamBaseURLHash("https://zero.example"), Provider: UpstreamProviderOther, Priority: 10},
	}
	for _, channel := range managed {
		require.NoError(t, db.Create(channel).Error)
	}
	suppressedAt := int64(90)
	suppressed := &model.UpstreamChannel{
		Name:         "Suppressed",
		BaseURL:      "https://suppressed.example",
		BaseURLHash:  model.UpstreamBaseURLHash("https://suppressed.example"),
		Provider:     UpstreamProviderOther,
		SuppressedAt: &suppressedAt,
	}
	require.NoError(t, db.Create(suppressed).Error)

	primaryBaseURL := "https://api.example.com"
	primaryAliasBaseURL := "https://API.EXAMPLE.COM:443/api/v1/"
	secondaryBaseURL := "https://other.example/v1"
	unmanagedBaseURL := "https://unmanaged.example"
	suppressedBaseURL := "https://suppressed.example/v1"
	sources := []*model.Channel{
		{Name: "primary-1", Key: "key-1", BaseURL: &primaryBaseURL},
		{Name: "primary-2", Key: "key-2", BaseURL: &primaryAliasBaseURL},
		{Name: "secondary", Key: "key-3", BaseURL: &secondaryBaseURL},
		{Name: "unmanaged", Key: "key-4", BaseURL: &unmanagedBaseURL},
		{Name: "suppressed", Key: "key-5", BaseURL: &suppressedBaseURL},
	}
	for _, source := range sources {
		require.NoError(t, db.Create(source).Error)
	}

	logs := []model.Log{
		{ChannelId: sources[0].Id, Type: model.LogTypeConsume, CreatedAt: 100, Quota: 10, PromptTokens: 2, CompletionTokens: 3},
		{ChannelId: sources[1].Id, Type: model.LogTypeConsume, CreatedAt: 150, Quota: 20, PromptTokens: 4, CompletionTokens: 6},
		{ChannelId: sources[0].Id, Type: model.LogTypeConsume, CreatedAt: 150, Quota: 5, PromptTokens: 1, CompletionTokens: 1},
		{ChannelId: sources[2].Id, Type: model.LogTypeConsume, CreatedAt: 200, Quota: 40, PromptTokens: 8, CompletionTokens: 12},
		{ChannelId: sources[0].Id, Type: model.LogTypeConsume, CreatedAt: 99, Quota: 100, PromptTokens: 100, CompletionTokens: 100},
		{ChannelId: sources[2].Id, Type: model.LogTypeConsume, CreatedAt: 201, Quota: 100, PromptTokens: 100, CompletionTokens: 100},
		{ChannelId: sources[0].Id, Type: model.LogTypeError, CreatedAt: 150, Quota: 100, PromptTokens: 100, CompletionTokens: 100},
		{ChannelId: sources[3].Id, Type: model.LogTypeConsume, CreatedAt: 150, Quota: 100, PromptTokens: 100, CompletionTokens: 100},
		{ChannelId: sources[4].Id, Type: model.LogTypeConsume, CreatedAt: 150, Quota: 100, PromptTokens: 100, CompletionTokens: 100},
	}
	require.NoError(t, db.Create(&logs).Error)

	result, err := GetUpstreamChannelStatistics(100, 200)
	require.NoError(t, err)
	require.Len(t, result.Data, 3)

	byChannelID := make(map[int]*UpstreamChannelStatisticsItem, len(result.Data))
	for _, item := range result.Data {
		byChannelID[item.ChannelID] = item
	}
	require.Contains(t, byChannelID, managed[0].Id)
	assert.Equal(t, int64(35), byChannelID[managed[0].Id].Quota)
	assert.Equal(t, int64(17), byChannelID[managed[0].Id].TokenUsed)
	assert.Equal(t, int64(3), byChannelID[managed[0].Id].Count)
	assert.Equal(t, int64(150), byChannelID[managed[0].Id].LastUsedAt)
	require.Contains(t, byChannelID, managed[1].Id)
	assert.Equal(t, int64(40), byChannelID[managed[1].Id].Quota)
	assert.Equal(t, int64(20), byChannelID[managed[1].Id].TokenUsed)
	assert.Equal(t, int64(1), byChannelID[managed[1].Id].Count)
	assert.Equal(t, int64(200), byChannelID[managed[1].Id].LastUsedAt)
	require.Contains(t, byChannelID, managed[2].Id)
	assert.Zero(t, byChannelID[managed[2].Id].Quota)
	assert.Zero(t, byChannelID[managed[2].Id].TokenUsed)
	assert.Zero(t, byChannelID[managed[2].Id].Count)
	assert.Zero(t, byChannelID[managed[2].Id].LastUsedAt)
	assert.NotContains(t, byChannelID, suppressed.Id)
	assert.Equal(t, UpstreamChannelStatisticsSummary{Quota: 75, TokenUsed: 37, Count: 4}, result.Summary)

	require.Len(t, result.Trend, 2)
	assert.Equal(t, &UpstreamChannelStatisticsTrendItem{
		ChannelID: managed[0].Id, ChannelName: "Primary", BaseURL: "https://api.example.com/v1/", Provider: UpstreamProviderNewAPI,
		CreatedAt: 0, Quota: 35, TokenUsed: 17, Count: 3,
	}, result.Trend[0])
	assert.Equal(t, &UpstreamChannelStatisticsTrendItem{
		ChannelID: managed[1].Id, ChannelName: "Secondary", BaseURL: "https://other.example/api", Provider: UpstreamProviderSub2API,
		CreatedAt: 0, Quota: 40, TokenUsed: 20, Count: 1,
	}, result.Trend[1])
}

func TestGetUpstreamChannelStatisticsUsesStableOwnerForDuplicateBaseURLs(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-statistics-duplicate.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})

	first := &model.UpstreamChannel{
		Name:        "First",
		BaseURL:     "https://duplicate.example/v1",
		BaseURLHash: model.UpstreamBaseURLHash("https://duplicate.example/v1"),
		Priority:    100,
	}
	second := &model.UpstreamChannel{
		Name:        "Second",
		BaseURL:     "https://duplicate.example/api",
		BaseURLHash: model.UpstreamBaseURLHash("https://duplicate.example/api"),
		Priority:    1,
	}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	baseURL := "https://duplicate.example"
	source := &model.Channel{Name: "source", Key: "key", BaseURL: &baseURL}
	require.NoError(t, db.Create(source).Error)
	require.NoError(t, db.Create(&model.Log{
		ChannelId:        source.Id,
		Type:             model.LogTypeConsume,
		CreatedAt:        100,
		Quota:            10,
		PromptTokens:     2,
		CompletionTokens: 3,
	}).Error)

	result, err := GetUpstreamChannelStatistics(100, 100)
	require.NoError(t, err)
	require.Len(t, result.Data, 2)

	byChannelID := make(map[int]*UpstreamChannelStatisticsItem, len(result.Data))
	for _, item := range result.Data {
		byChannelID[item.ChannelID] = item
	}
	assert.Equal(t, int64(10), byChannelID[first.Id].Quota)
	assert.Equal(t, int64(5), byChannelID[first.Id].TokenUsed)
	assert.Equal(t, int64(1), byChannelID[first.Id].Count)
	assert.Zero(t, byChannelID[second.Id].Quota)
	assert.Zero(t, byChannelID[second.Id].TokenUsed)
	assert.Zero(t, byChannelID[second.Id].Count)
}
