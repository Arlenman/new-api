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

type channelPriorityModeResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items      []model.Channel `json:"items"`
		Total      int64           `json:"total"`
		Page       int             `json:"page"`
		PageSize   int             `json:"page_size"`
		TypeCounts map[int64]int64 `json:"type_counts"`
	} `json:"data"`
}

func TestGetAllChannelsPriorityModePaginatesPriorityGroups(t *testing.T) {
	setupModelListControllerTestDB(t)

	priority100 := int64(100)
	priority50 := int64(50)
	priority10 := int64(10)
	tagA := "tag-a"
	tagB := "tag-b"
	channels := []model.Channel{
		{Name: "priority-100-a", Priority: &priority100, Tag: &tagA, Status: common.ChannelStatusEnabled, Type: 1, Group: "team"},
		{Name: "priority-100-b", Priority: &priority100, Tag: &tagB, Status: common.ChannelStatusEnabled, Type: 1, Group: "team"},
		{Name: "priority-50", Priority: &priority50, Tag: &tagA, Status: common.ChannelStatusEnabled, Type: 1, Group: "team"},
		{Name: "priority-10-wrong-status", Priority: &priority10, Tag: &tagA, Status: common.ChannelStatusManuallyDisabled, Type: 1, Group: "team"},
	}
	require.NoError(t, model.DB.Create(&channels).Error)

	response := requestChannelPriorityMode(t, "/api/channel?p=1&page_size=1&priority_mode=true&status=enabled&type=1&group=team", GetAllChannels)
	require.Len(t, response.Data.Items, 2)
	assert.Equal(t, []string{"priority-100-a", "priority-100-b"}, channelNames(response.Data.Items))
	assert.Equal(t, int64(2), response.Data.Total)
	assert.Equal(t, 1, response.Data.Page)
	assert.Equal(t, 1, response.Data.PageSize)

	secondPage := requestChannelPriorityMode(t, "/api/channel?p=2&page_size=1&priority_mode=true&status=enabled&type=1&group=team", GetAllChannels)
	require.Len(t, secondPage.Data.Items, 1)
	assert.Equal(t, "priority-50", secondPage.Data.Items[0].Name)
}

func TestSearchChannelsPriorityModeAppliesAllFilters(t *testing.T) {
	setupModelListControllerTestDB(t)

	priority100 := int64(100)
	priority90 := int64(90)
	priority80 := int64(80)
	priority70 := int64(70)
	priority60 := int64(60)
	priority50 := int64(50)
	priority40 := int64(40)
	channels := []model.Channel{
		{Name: "needle-100-a", Priority: &priority100, Status: common.ChannelStatusEnabled, Type: 1, Group: "team,default", Models: "gpt-4o"},
		{Name: "needle-100-b", Priority: &priority100, Status: common.ChannelStatusEnabled, Type: 1, Group: "team", Models: "gpt-4o-mini,gpt-4o"},
		{Name: "needle-90", Priority: &priority90, Status: common.ChannelStatusEnabled, Type: 1, Group: "team", Models: "gpt-4o"},
		{Name: "needle-wrong-status", Priority: &priority80, Status: common.ChannelStatusManuallyDisabled, Type: 1, Group: "team", Models: "gpt-4o"},
		{Name: "needle-wrong-type", Priority: &priority70, Status: common.ChannelStatusEnabled, Type: 2, Group: "team", Models: "gpt-4o"},
		{Name: "needle-wrong-group", Priority: &priority60, Status: common.ChannelStatusEnabled, Type: 1, Group: "other", Models: "gpt-4o"},
		{Name: "needle-wrong-model", Priority: &priority50, Status: common.ChannelStatusEnabled, Type: 1, Group: "team", Models: "claude-3"},
		{Name: "other-name", Priority: &priority40, Status: common.ChannelStatusEnabled, Type: 1, Group: "team", Models: "gpt-4o"},
	}
	require.NoError(t, model.DB.Create(&channels).Error)

	path := "/api/channel/search?keyword=needle&model=gpt-4o&group=team&status=enabled&type=1&priority_mode=true&p=1&page_size=1"
	response := requestChannelPriorityMode(t, path, SearchChannels)
	require.Len(t, response.Data.Items, 2)
	assert.Equal(t, []string{"needle-100-a", "needle-100-b"}, channelNames(response.Data.Items))
	assert.Equal(t, int64(2), response.Data.Total)
	assert.Equal(t, map[int64]int64{1: 3, 2: 1}, response.Data.TypeCounts)

	secondPagePath := "/api/channel/search?keyword=needle&model=gpt-4o&group=team&status=enabled&type=1&priority_mode=true&p=2&page_size=1"
	secondPage := requestChannelPriorityMode(t, secondPagePath, SearchChannels)
	require.Len(t, secondPage.Data.Items, 1)
	assert.Equal(t, "needle-90", secondPage.Data.Items[0].Name)
}

func TestPriorityModeTakesPrecedenceOverTagMode(t *testing.T) {
	setupModelListControllerTestDB(t)

	priority100 := int64(100)
	priority50 := int64(50)
	tagA := "tag-a"
	tagB := "tag-b"
	channels := []model.Channel{
		{Name: "same-priority-tag-a", Priority: &priority100, Tag: &tagA},
		{Name: "same-priority-tag-b", Priority: &priority100, Tag: &tagB},
		{Name: "lower-priority-tag-a", Priority: &priority50, Tag: &tagA},
	}
	require.NoError(t, model.DB.Create(&channels).Error)

	response := requestChannelPriorityMode(t, "/api/channel?p=1&page_size=1&tag_mode=true&priority_mode=true", GetAllChannels)
	assert.Equal(t, []string{"same-priority-tag-a", "same-priority-tag-b"}, channelNames(response.Data.Items))
	assert.Equal(t, int64(2), response.Data.Total)
}

func requestChannelPriorityMode(t *testing.T, path string, handler func(*gin.Context)) channelPriorityModeResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	handler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelPriorityModeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	return response
}

func channelNames(channels []model.Channel) []string {
	names := make([]string, 0, len(channels))
	for _, channel := range channels {
		names = append(names, channel.Name)
	}
	return names
}
