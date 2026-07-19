package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTagModePrioritySorting(t *testing.T) {
	setupModelListControllerTestDB(t)

	alpha := "alpha"
	beta := "beta"
	gamma := "gamma"
	mixed := "mixed"
	negative := "negative"
	priority10 := int64(10)
	priority30 := int64(30)
	priority5 := int64(5)
	priority8 := int64(8)
	priorityNegative := int64(-1)
	channels := []model.Channel{
		{Name: "channel-alpha-1", Tag: &alpha, Priority: &priority10},
		{Name: "channel-alpha-2", Tag: &alpha, Priority: &priority10},
		{Name: "channel-beta-1", Tag: &beta, Priority: &priority30},
		{Name: "channel-gamma-1", Tag: &gamma, Priority: &priority10},
		{Name: "channel-mixed-1", Tag: &mixed, Priority: &priority5},
		{Name: "channel-mixed-2", Tag: &mixed, Priority: &priority8},
		{Name: "channel-negative-1", Tag: &negative, Priority: &priorityNegative},
	}
	require.NoError(t, model.DB.Create(&channels).Error)

	tests := []struct {
		name     string
		path     string
		handler  func(*gin.Context)
		expected []channelTagPriority
	}{
		{
			name:    "list ascending",
			path:    "/api/channel?p=1&page_size=20&tag_mode=true&sort_by=priority&sort_order=asc",
			handler: GetAllChannels,
			expected: []channelTagPriority{
				{Tag: negative, Priority: -1},
				{Tag: mixed, Priority: 5},
				{Tag: mixed, Priority: 8},
				{Tag: alpha, Priority: 10},
				{Tag: alpha, Priority: 10},
				{Tag: gamma, Priority: 10},
				{Tag: beta, Priority: 30},
			},
		},
		{
			name:    "list descending",
			path:    "/api/channel?p=1&page_size=20&tag_mode=true&sort_by=priority&sort_order=desc",
			handler: GetAllChannels,
			expected: []channelTagPriority{
				{Tag: beta, Priority: 30},
				{Tag: alpha, Priority: 10},
				{Tag: alpha, Priority: 10},
				{Tag: gamma, Priority: 10},
				{Tag: mixed, Priority: 8},
				{Tag: mixed, Priority: 5},
				{Tag: negative, Priority: -1},
			},
		},
		{
			name:    "search ascending",
			path:    "/api/channel/search?keyword=channel&model=&p=1&page_size=20&tag_mode=true&sort_by=priority&sort_order=asc",
			handler: SearchChannels,
			expected: []channelTagPriority{
				{Tag: negative, Priority: -1},
				{Tag: mixed, Priority: 5},
				{Tag: mixed, Priority: 8},
				{Tag: alpha, Priority: 10},
				{Tag: alpha, Priority: 10},
				{Tag: gamma, Priority: 10},
				{Tag: beta, Priority: 30},
			},
		},
		{
			name:    "search descending",
			path:    "/api/channel/search?keyword=channel&model=&p=1&page_size=20&tag_mode=true&sort_by=priority&sort_order=desc",
			handler: SearchChannels,
			expected: []channelTagPriority{
				{Tag: beta, Priority: 30},
				{Tag: alpha, Priority: 10},
				{Tag: alpha, Priority: 10},
				{Tag: gamma, Priority: 10},
				{Tag: mixed, Priority: 8},
				{Tag: mixed, Priority: 5},
				{Tag: negative, Priority: -1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := requestChannelTagPriorityOrder(t, tt.path, tt.handler)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

type channelTagPriority struct {
	Tag      string
	Priority int64
}

func requestChannelTagPriorityOrder(t *testing.T, path string, handler func(*gin.Context)) []channelTagPriority {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	handler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []model.Channel `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	result := make([]channelTagPriority, 0, len(response.Data.Items))
	for _, channel := range response.Data.Items {
		result = append(result, channelTagPriority{
			Tag:      channel.GetTag(),
			Priority: *channel.Priority,
		})
	}
	return result
}
