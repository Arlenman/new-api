package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedTokenTagQuotaHourLogs(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "password123", AffCode: "quota-hour-alice"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "bob", Password: "password123", AffCode: "quota-hour-bob"}).Error)
	seedTokenForTags(t, Token{Id: 401, UserId: 1, Key: "quota-hour-alice-client", Name: "alice-client"})
	seedTokenForTags(t, Token{Id: 402, UserId: 1, Key: "quota-hour-alice-untagged", Name: "alice-untagged"})
	seedTokenForTags(t, Token{Id: 404, UserId: 2, Key: "quota-hour-bob-client", Name: "bob-client"})
	require.NoError(t, ReplaceTokenTags(1, 401, []string{"Client A"}))
	require.NoError(t, ReplaceTokenTags(2, 404, []string{"Client A"}))

	logs := []Log{
		{UserId: 1, Username: "alice", TokenId: 401, TokenName: "alice-client", ModelName: "gpt-a", Type: LogTypeConsume, Quota: 10, PromptTokens: 2, CompletionTokens: 1, CreatedAt: 3610},
		{UserId: 1, Username: "alice", TokenId: 401, TokenName: "alice-client", ModelName: "gpt-b", Type: LogTypeConsume, Quota: 15, PromptTokens: 4, CompletionTokens: 1, CreatedAt: 3700},
		{UserId: 1, Username: "alice", TokenId: 402, TokenName: "alice-untagged", ModelName: "gpt-a", Type: LogTypeConsume, Quota: 20, PromptTokens: 5, CompletionTokens: 2, CreatedAt: 7250},
		{UserId: 2, Username: "bob", TokenId: 404, TokenName: "bob-client", ModelName: "gpt-c", Type: LogTypeConsume, Quota: 30, PromptTokens: 6, CompletionTokens: 3, CreatedAt: 10900},
		{UserId: 2, Username: "bob", TokenId: 404, TokenName: "bob-client", ModelName: "gpt-c", Type: LogTypeConsume, Quota: 40, PromptTokens: 7, CompletionTokens: 4, CreatedAt: 10910},
	}
	for index := range logs {
		require.NoError(t, LOG_DB.Create(&logs[index]).Error)
	}
}

type tokenTagQuotaHourQueryCase struct {
	name     string
	start    int64
	end      int64
	username string
	userID   int
	role     int
	filters  TokenTagQuotaFilters
}

func runTokenTagQuotaHourQueryCases(t *testing.T, cases []tokenTagQuotaHourQueryCase) []TokenTagQuotaAnalyticsResult {
	t.Helper()
	results := make([]TokenTagQuotaAnalyticsResult, 0, len(cases))
	for _, testCase := range cases {
		result, err := queryTokenTagQuotaAnalytics(context.Background(), testCase.start, testCase.end, testCase.username, testCase.userID, testCase.role, testCase.filters, true)
		require.NoError(t, err)
		results = append(results, result)
	}
	return results
}

func tokenTagQuotaHourQueryCases() []tokenTagQuotaHourQueryCase {
	return []tokenTagQuotaHourQueryCase{
		{name: "整点范围同时命中细分与趋势", start: 3600, end: 12000, role: common.RoleAdminUser},
		{name: "起始时间不在整点", start: 3620, end: 12000, role: common.RoleAdminUser},
		{name: "整段落在汇总覆盖内", start: 3600, end: 10799, role: common.RoleAdminUser},
		{name: "按标签过滤", start: 3600, end: 12000, role: common.RoleAdminUser, filters: TokenTagQuotaFilters{IncludedTags: []string{"Client A"}}},
		{name: "排除未打标签的密钥", start: 3600, end: 12000, role: common.RoleAdminUser, filters: TokenTagQuotaFilters{ExcludeUntagged: true}},
		{name: "普通用户只看自己", start: 3600, end: 12000, userID: 1, role: common.RoleCommonUser},
		{name: "管理员按用户名过滤", start: 3600, end: 12000, username: "bob", role: common.RoleAdminUser},
	}
}

func TestTokenTagQuotaHourRollupMatchesLogsAnalytics(t *testing.T) {
	truncateTables(t)
	seedTokenTagQuotaHourLogs(t)

	cases := tokenTagQuotaHourQueryCases()
	expected := runTokenTagQuotaHourQueryCases(t, cases)

	require.NoError(t, syncTokenTagQuotaHours(4*3600))
	covered, err := getTokenTagQuotaHourCoverage(context.Background())
	require.NoError(t, err)
	require.Equal(t, tokenTagQuotaHourCoverage{From: 3600, To: 4 * 3600}, covered)

	actual := runTokenTagQuotaHourQueryCases(t, cases)
	require.Len(t, actual, len(expected))
	for index := range cases {
		require.Equal(t, expected[index], actual[index], cases[index].name)
	}
}

func TestTokenTagQuotaHourRollupFallsBackToLogsBeyondCoverage(t *testing.T) {
	truncateTables(t)
	seedTokenTagQuotaHourLogs(t)

	cases := []tokenTagQuotaHourQueryCase{
		{name: "覆盖一半的区间", start: 3600, end: 12000, role: common.RoleAdminUser},
		{name: "整段超出覆盖范围", start: 7200, end: 12000, role: common.RoleAdminUser},
	}
	expected := runTokenTagQuotaHourQueryCases(t, cases)

	require.NoError(t, syncTokenTagQuotaHours(2*3600))
	covered, err := getTokenTagQuotaHourCoverage(context.Background())
	require.NoError(t, err)
	require.Equal(t, tokenTagQuotaHourCoverage{From: 3600, To: 2 * 3600}, covered)

	actual := runTokenTagQuotaHourQueryCases(t, cases)
	require.Len(t, actual, len(expected))
	for index := range cases {
		require.Equal(t, expected[index], actual[index], cases[index].name)
	}
}

func TestTokenTagQuotaHourSyncReplayDoesNotDoubleCount(t *testing.T) {
	truncateTables(t)
	seedTokenTagQuotaHourLogs(t)

	cases := []tokenTagQuotaHourQueryCase{{start: 3600, end: 12000, role: common.RoleAdminUser}}
	expected := runTokenTagQuotaHourQueryCases(t, cases)

	require.NoError(t, syncTokenTagQuotaHours(4*3600))
	// 模拟进程重启后覆盖区间回退，同一段时间被重新汇总
	require.NoError(t, saveTokenTagQuotaHourCoverage(DB, tokenTagQuotaHourCoverage{From: 3600, To: 3600}))
	require.NoError(t, syncTokenTagQuotaHours(4*3600))

	actual := runTokenTagQuotaHourQueryCases(t, cases)
	require.Equal(t, expected[0], actual[0])

	var count int64
	require.NoError(t, DB.Model(&TokenTagQuotaHour{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
}

func TestTokenTagQuotaHourRollupFallsBackWhenSchemaIsMissing(t *testing.T) {
	truncateTables(t)
	seedTokenTagQuotaHourLogs(t)

	cases := []tokenTagQuotaHourQueryCase{{start: 3600, end: 12000, role: common.RoleAdminUser}}
	expected := runTokenTagQuotaHourQueryCases(t, cases)

	// 从库可能还没等到 master 建表，此时统计必须继续走 logs 而不是报错
	require.NoError(t, DB.Migrator().DropTable(&TokenTagQuotaHourCursor{}))
	t.Cleanup(func() {
		require.NoError(t, DB.AutoMigrate(&TokenTagQuotaHourCursor{}))
	})

	actual := runTokenTagQuotaHourQueryCases(t, cases)
	require.Equal(t, expected[0], actual[0])
}

func TestTokenTagQuotaHourRollupAggregatesLogsByHour(t *testing.T) {
	truncateTables(t)
	seedTokenTagQuotaHourLogs(t)

	require.NoError(t, syncTokenTagQuotaHours(4*3600))

	var rows []TokenTagQuotaHour
	require.NoError(t, DB.Order("hour asc, token_id asc, model_name asc").Find(&rows).Error)
	require.Len(t, rows, 4)

	require.EqualValues(t, 3600, rows[0].Hour)
	require.Equal(t, 401, rows[0].TokenID)
	require.Equal(t, "gpt-a", rows[0].ModelName)
	require.Equal(t, 10, rows[0].Quota)
	require.Equal(t, 3, rows[0].TokenUsed)
	require.Equal(t, 1, rows[0].Count)
	require.EqualValues(t, 3610, rows[0].LastUsedAt)

	require.Equal(t, "gpt-b", rows[1].ModelName)
	require.Equal(t, 15, rows[1].Quota)

	require.EqualValues(t, 7200, rows[2].Hour)
	require.Equal(t, 402, rows[2].TokenID)

	require.EqualValues(t, 10800, rows[3].Hour)
	require.Equal(t, 404, rows[3].TokenID)
	require.Equal(t, 70, rows[3].Quota)
	require.Equal(t, 20, rows[3].TokenUsed)
	require.Equal(t, 2, rows[3].Count)
	require.EqualValues(t, 10910, rows[3].LastUsedAt)
}
