package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedHiddenUserAnalyticsData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "visible", Password: "password123", AffCode: "visible-aff"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "hidden", Password: "password123", AffCode: "hidden-aff", Hidden: true}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 101, UserId: 1, Key: "sk-visible", Name: "visible-key"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 202, UserId: 2, Key: "sk-hidden", Name: "hidden-key"}).Error)
	require.NoError(t, ReplaceTokenTags(1, 101, []string{"Visible Tag"}))
	require.NoError(t, ReplaceTokenTags(2, 202, []string{"Hidden Tag"}))
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    1,
		Username:  "visible",
		NodeName:  "node-a",
		TokenID:   101,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-visible",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    2,
		Username:  "hidden",
		NodeName:  "node-b",
		TokenID:   202,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-hidden",
		CreatedAt: 1200,
		Count:     4,
		Quota:     300,
		TokenUsed: 80,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "visible",
		TokenId:          101,
		TokenName:        "visible-key",
		ModelName:        "gpt-visible",
		Type:             LogTypeConsume,
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		CreatedAt:        1199,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           2,
		Username:         "hidden",
		TokenId:          202,
		TokenName:        "hidden-key",
		ModelName:        "gpt-hidden",
		Type:             LogTypeConsume,
		Quota:            300,
		PromptTokens:     30,
		CompletionTokens: 40,
		CreatedAt:        1299,
	}).Error)
	require.NoError(t, DB.Create(&Midjourney{Id: 1, UserId: 1, MjId: "mj-visible", SubmitTime: 1100}).Error)
	require.NoError(t, DB.Create(&Midjourney{Id: 2, UserId: 2, MjId: "mj-hidden", SubmitTime: 1200}).Error)
	require.NoError(t, DB.Create(&Task{TaskID: "task-visible", UserId: 1, SubmitTime: 1100}).Error)
	require.NoError(t, DB.Create(&Task{TaskID: "task-hidden", UserId: 2, SubmitTime: 1200}).Error)
}

func TestUserListRespectsShowHiddenFlag(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "visible", Password: "password123", AffCode: "visible-aff"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "hidden", Password: "password123", AffCode: "hidden-aff", Hidden: true}).Error)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 20}
	visibleOnly, total, err := GetAllUsers(pageInfo, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, visibleOnly, 1)
	require.Equal(t, "visible", visibleOnly[0].Username)

	allUsers, total, err := GetAllUsers(pageInfo, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, allUsers, 2)

	searchVisibleOnly, total, err := SearchUsers("", "", nil, nil, 0, 20, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, searchVisibleOnly, 1)
	require.Equal(t, "visible", searchVisibleOnly[0].Username)

	searchAll, total, err := SearchUsers("", "", nil, nil, 0, 20, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, searchAll, 2)
}

func TestAdminAnalyticsExcludeHiddenUsers(t *testing.T) {
	truncateTables(t)
	seedHiddenUserAnalyticsData(t)

	quotaRows, err := GetAllQuotaDates(1000, 2000, "", "")
	require.NoError(t, err)
	require.Len(t, quotaRows, 1)
	require.Equal(t, "gpt-visible", quotaRows[0].ModelName)

	userRows, err := GetQuotaDataGroupByUser(1000, 2000)
	require.NoError(t, err)
	require.Len(t, userRows, 1)
	require.Equal(t, "visible", userRows[0].Username)

	flowRows, err := GetFlowQuotaData(1000, 2000, "", 0, common.RoleAdminUser, "")
	require.NoError(t, err)
	require.Len(t, flowRows, 1)
	require.Equal(t, "visible", flowRows[0].Username)

	tagRows, err := GetTokenTagQuotaData(1000, 2000, "", 0, common.RoleAdminUser, "")
	require.NoError(t, err)
	require.Len(t, tagRows, 1)
	require.Equal(t, "Visible Tag", tagRows[0].TagName)

	logRows, total, err := GetAllLogs(LogTypeConsume, 1000, 2000, "", "", "", 0, 20, 0, "", "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logRows, 1)
	require.Equal(t, "visible", logRows[0].Username)

	adminStat, err := SumUsedQuota(LogTypeConsume, 1000, 2000, "", "", "", 0, "", "", true)
	require.NoError(t, err)
	require.Equal(t, 100, adminStat.Quota)

	selfStat, err := SumUsedQuota(LogTypeConsume, 1000, 2000, "", "hidden", "", 0, "", "", false)
	require.NoError(t, err)
	require.Equal(t, 300, selfStat.Quota)

	mjRows := GetAllTasks(0, 20, TaskQueryParams{})
	require.Len(t, mjRows, 1)
	require.Equal(t, "mj-visible", mjRows[0].MjId)
	require.EqualValues(t, 1, CountAllTasks(TaskQueryParams{}))

	taskRows := TaskGetAllTasks(0, 20, SyncTaskQueryParams{})
	require.Len(t, taskRows, 1)
	require.Equal(t, "task-visible", taskRows[0].TaskID)
	require.EqualValues(t, 1, TaskCountAllTasks(SyncTaskQueryParams{}))
}

func TestTokenTagOptionsExcludeHiddenUsers(t *testing.T) {
	truncateTables(t)
	seedHiddenUserAnalyticsData(t)

	options, err := ListTokenTagOptions(0, "", common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.Equal(t, "Visible Tag", options[0].Name)

	hiddenUserOptions, err := ListTokenTagOptions(0, "hidden", common.RoleAdminUser)
	require.NoError(t, err)
	require.Empty(t, hiddenUserOptions)
}
