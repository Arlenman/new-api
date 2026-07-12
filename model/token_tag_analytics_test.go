package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedTaggedQuotaData(t *testing.T) {
	t.Helper()
	seedTokenForTags(t, Token{Id: 101, UserId: 1, Key: "quota-user-1-client", Name: "client"})
	seedTokenForTags(t, Token{Id: 102, UserId: 1, Key: "quota-user-1-internal", Name: "internal"})
	seedTokenForTags(t, Token{Id: 201, UserId: 2, Key: "quota-user-2-client", Name: "client"})
	require.NoError(t, ReplaceTokenTags(1, 101, []string{"Client A", "Shared"}))
	require.NoError(t, ReplaceTokenTags(1, 102, []string{"Internal"}))
	require.NoError(t, ReplaceTokenTags(2, 201, []string{"Client A"}))

	require.NoError(t, DB.Create(&QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   101,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   102,
		ModelName: "gpt-a",
		CreatedAt: 1200,
		Count:     1,
		Quota:     200,
		TokenUsed: 80,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    2,
		Username:  "bob",
		TokenID:   201,
		ModelName: "gpt-b",
		CreatedAt: 1300,
		Count:     3,
		Quota:     300,
		TokenUsed: 120,
	}).Error)

	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          101,
		TokenName:        "client",
		Type:             LogTypeConsume,
		Quota:            40,
		PromptTokens:     10,
		CompletionTokens: 10,
		CreatedAt:        1111,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          101,
		TokenName:        "client",
		Type:             LogTypeConsume,
		Quota:            60,
		PromptTokens:     10,
		CompletionTokens: 10,
		CreatedAt:        1188,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          102,
		TokenName:        "internal",
		Type:             LogTypeConsume,
		Quota:            200,
		PromptTokens:     40,
		CompletionTokens: 40,
		CreatedAt:        1255,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           2,
		Username:         "bob",
		TokenId:          201,
		TokenName:        "client",
		Type:             LogTypeConsume,
		Quota:            300,
		PromptTokens:     60,
		CompletionTokens: 60,
		CreatedAt:        1399,
	}).Error)
}

func TestQuotaDataCanFilterByTokenTag(t *testing.T) {
	truncateTables(t)
	seedTaggedQuotaData(t)

	selfRows, err := GetQuotaDataByUserId(1, 1000, 2000, "Client A")
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Equal(t, "gpt-a", selfRows[0].ModelName)
	require.Equal(t, 100, selfRows[0].Quota)
	require.Equal(t, 40, selfRows[0].TokenUsed)

	adminRows, err := GetAllQuotaDates(1000, 2000, "", "Client A")
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
}

func TestGetTokenTagQuotaDataScopesByRoleAndUsername(t *testing.T) {
	truncateTables(t)
	seedTaggedQuotaData(t)

	selfRows, err := GetTokenTagQuotaData(1000, 2000, "", 1, common.RoleCommonUser, "")
	require.NoError(t, err)
	require.Len(t, selfRows, 3)
	require.Equal(t, "Internal", selfRows[0].TagName)
	require.Equal(t, "internal", selfRows[0].TokenName)
	require.Equal(t, 200, selfRows[0].Quota)
	require.EqualValues(t, 1255, selfRows[0].LastUsedAt)
	require.Empty(t, selfRows[0].Username)

	adminRows, err := GetTokenTagQuotaData(1000, 2000, "", 0, common.RoleAdminUser, "")
	require.NoError(t, err)
	require.Len(t, adminRows, 4)
	require.Equal(t, "Client A", adminRows[0].TagName)
	require.Equal(t, "bob", adminRows[0].Username)
	require.Equal(t, 300, adminRows[0].Quota)
	require.EqualValues(t, 1399, adminRows[0].LastUsedAt)

	aliceRows, err := GetTokenTagQuotaData(1000, 2000, "alice", 0, common.RoleAdminUser, "")
	require.NoError(t, err)
	require.Len(t, aliceRows, 3)
	for _, row := range aliceRows {
		require.Equal(t, "alice", row.Username)
	}
}

func TestGetTokenTagQuotaDataFiltersBySelectedTag(t *testing.T) {
	truncateTables(t)
	seedTaggedQuotaData(t)

	rows, err := GetTokenTagQuotaData(1000, 2000, "", 0, common.RoleAdminUser, "Client A")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "Client A", row.TagName)
	}
	require.Equal(t, "bob", rows[0].Username)
	require.EqualValues(t, 1399, rows[0].LastUsedAt)
	require.Equal(t, "alice", rows[1].Username)
	require.EqualValues(t, 1188, rows[1].LastUsedAt)

	selfRows, err := GetTokenTagQuotaData(1000, 2000, "", 1, common.RoleCommonUser, "Client A")
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Equal(t, "Client A", selfRows[0].TagName)
	require.Empty(t, selfRows[0].Username)
	require.EqualValues(t, 1188, selfRows[0].LastUsedAt)
}

func TestGetTokenTagQuotaDataIncludesUntaggedLogKeys(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "password123", AffCode: "alice-aff"}).Error)
	seedTokenForTags(t, Token{Id: 301, UserId: 1, Key: "quota-user-1-untagged", Name: "untagged"})
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          301,
		TokenName:        "untagged-log-name",
		Type:             LogTypeConsume,
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 5,
		CreatedAt:        1100,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          301,
		TokenName:        "untagged-log-name",
		Type:             LogTypeConsume,
		Quota:            150,
		PromptTokens:     20,
		CompletionTokens: 15,
		CreatedAt:        1200,
	}).Error)

	adminRows, err := GetTokenTagQuotaData(1000, 2000, "", 0, common.RoleAdminUser, "")
	require.NoError(t, err)
	require.Len(t, adminRows, 1)
	require.Equal(t, 0, adminRows[0].TagID)
	require.Empty(t, adminRows[0].TagName)
	require.Equal(t, "alice", adminRows[0].Username)
	require.Equal(t, 301, adminRows[0].TokenID)
	require.Equal(t, "untagged", adminRows[0].TokenName)
	require.Equal(t, 2, adminRows[0].Count)
	require.Equal(t, 250, adminRows[0].Quota)
	require.Equal(t, 50, adminRows[0].TokenUsed)
	require.EqualValues(t, 1200, adminRows[0].LastUsedAt)

	selfRows, err := GetTokenTagQuotaData(1000, 2000, "", 1, common.RoleCommonUser, "")
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Empty(t, selfRows[0].Username)
	require.Empty(t, selfRows[0].TagName)
}

func TestListTokenTagOptionsScopesForDashboard(t *testing.T) {
	truncateTables(t)
	seedTaggedQuotaData(t)

	selfOptions, err := ListTokenTagOptions(1, "", common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, selfOptions, 3)
	require.Equal(t, []string{"Client A", "Internal", "Shared"}, []string{
		selfOptions[0].Name,
		selfOptions[1].Name,
		selfOptions[2].Name,
	})

	adminOptions, err := ListTokenTagOptions(0, "", common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminOptions, 3)

	aliceOptions, err := ListTokenTagOptions(0, "alice", common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, aliceOptions, 3)
	for _, option := range aliceOptions {
		require.Equal(t, 1, option.UserID)
	}
}

func seedTokenTagAnalyticsModels(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "password123", AffCode: "alice-analytics-aff"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "bob", Password: "password123", AffCode: "bob-analytics-aff"}).Error)
	seedTokenForTags(t, Token{Id: 401, UserId: 1, Key: "analytics-alice-client", Name: "alice-client"})
	seedTokenForTags(t, Token{Id: 402, UserId: 1, Key: "analytics-alice-internal", Name: "alice-internal"})
	seedTokenForTags(t, Token{Id: 403, UserId: 1, Key: "analytics-alice-untagged", Name: "alice-untagged"})
	seedTokenForTags(t, Token{Id: 404, UserId: 2, Key: "analytics-bob-client", Name: "bob-client"})
	require.NoError(t, ReplaceTokenTags(1, 401, []string{"Client A", "Shared"}))
	require.NoError(t, ReplaceTokenTags(1, 402, []string{"Internal"}))
	require.NoError(t, ReplaceTokenTags(2, 404, []string{"Client A"}))

	logs := []Log{
		{UserId: 1, Username: "alice", TokenId: 401, TokenName: "alice-client", ModelName: "gpt-a", Type: LogTypeConsume, Quota: 40, PromptTokens: 10, CompletionTokens: 5, CreatedAt: 1100},
		{UserId: 1, Username: "alice", TokenId: 401, TokenName: "alice-client", ModelName: "gpt-b", Type: LogTypeConsume, Quota: 60, PromptTokens: 20, CompletionTokens: 5, CreatedAt: 1200},
		{UserId: 1, Username: "alice", TokenId: 402, TokenName: "alice-internal", ModelName: "gpt-a", Type: LogTypeConsume, Quota: 200, PromptTokens: 30, CompletionTokens: 10, CreatedAt: 1300},
		{UserId: 1, Username: "alice", TokenId: 403, TokenName: "alice-untagged", ModelName: "", Type: LogTypeConsume, Quota: 80, PromptTokens: 8, CompletionTokens: 2, CreatedAt: 1400},
		{UserId: 2, Username: "bob", TokenId: 404, TokenName: "bob-client", ModelName: "gpt-c", Type: LogTypeConsume, Quota: 300, PromptTokens: 50, CompletionTokens: 20, CreatedAt: 1500},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}
}

func TestGetTokenTagQuotaAnalyticsSupportsMultipleIncludedTagsAndModelDetails(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludedTags: []string{" client a ", "Internal", "CLIENT A", ""},
	})
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Equal(t, TokenTagQuotaSummary{Quota: 600, TokenUsed: 150, Count: 4}, summary)

	byTagModel := make(map[string]*TokenTagQuotaData, len(rows))
	for _, row := range rows {
		byTagModel[row.Username+"/"+row.TagName+"/"+row.ModelName] = row
		require.NotEqual(t, "Shared", row.TagName)
	}
	require.Equal(t, 40, byTagModel["alice/Client A/gpt-a"].Quota)
	require.Equal(t, 60, byTagModel["alice/Client A/gpt-b"].Quota)
	require.Equal(t, 200, byTagModel["alice/Internal/gpt-a"].Quota)
	require.Equal(t, 300, byTagModel["bob/Client A/gpt-c"].Quota)
}

func TestGetTokenTagQuotaAnalyticsExcludesWholeKeyBeforeIncludedTags(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludedTags: []string{"Client A", "Internal"},
		ExcludedTags: []string{" shared ", "SHARED"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, TokenTagQuotaSummary{Quota: 500, TokenUsed: 110, Count: 2}, summary)
	for _, row := range rows {
		require.NotEqual(t, 401, row.TokenID)
	}
}

func TestGetTokenTagQuotaAnalyticsSummaryCountsMultiTagKeyOnceAndKeepsUntagged(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{})
	require.NoError(t, err)
	require.Len(t, rows, 7)
	require.Equal(t, TokenTagQuotaSummary{Quota: 680, TokenUsed: 160, Count: 5}, summary)

	detailQuota := 0
	untaggedFound := false
	for _, row := range rows {
		detailQuota += row.Quota
		if row.TokenID == 403 {
			untaggedFound = true
			require.Empty(t, row.TagName)
			require.Empty(t, row.ModelName)
		}
	}
	require.Equal(t, 780, detailQuota)
	require.True(t, untaggedFound)
}

func TestGetTokenTagQuotaAnalyticsCanIncludeOnlyUntaggedKeys(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludeUntagged: true,
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 80, TokenUsed: 10, Count: 1}, summary)
	require.Len(t, rows, 1)
	require.Equal(t, 403, rows[0].TokenID)
	require.Empty(t, rows[0].TagName)
}

func TestGetTokenTagQuotaAnalyticsIncludesTagsOrUntaggedKeys(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludedTags:    []string{"Client A"},
		IncludeUntagged: true,
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 480, TokenUsed: 120, Count: 4}, summary)
	require.Len(t, rows, 4)

	seenTokenIDs := make(map[int]bool)
	for _, row := range rows {
		seenTokenIDs[row.TokenID] = true
		if row.TokenID == 403 {
			require.Empty(t, row.TagName)
		} else {
			require.Equal(t, "Client A", row.TagName)
		}
	}
	require.Equal(t, map[int]bool{401: true, 403: true, 404: true}, seenTokenIDs)
}

func TestGetTokenTagQuotaAnalyticsCanExcludeUntaggedKeys(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		ExcludeUntagged: true,
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 600, TokenUsed: 150, Count: 4}, summary)
	require.Len(t, rows, 6)
	for _, row := range rows {
		require.NotEqual(t, 403, row.TokenID)
		require.NotEmpty(t, row.TagName)
	}
}

func TestGetTokenTagQuotaAnalyticsUntaggedExclusionTakesPriority(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludedTags:    []string{"Client A"},
		IncludeUntagged: true,
		ExcludeUntagged: true,
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 400, TokenUsed: 110, Count: 3}, summary)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.NotEqual(t, 403, row.TokenID)
		require.Equal(t, "Client A", row.TagName)
	}
}

func TestGetTokenTagQuotaAnalyticsUntaggedFilterKeepsUserScope(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "bob", 1, common.RoleCommonUser, TokenTagQuotaFilters{
		IncludeUntagged: true,
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 80, TokenUsed: 10, Count: 1}, summary)
	require.Len(t, rows, 1)
	require.Equal(t, 403, rows[0].TokenID)
	require.Zero(t, rows[0].UserID)
	require.Empty(t, rows[0].Username)
}

func TestGetTokenTagQuotaAnalyticsExcludingTagDoesNotRemoveUntaggedKey(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "alice", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		ExcludedTags: []string{"Internal"},
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 180, TokenUsed: 50, Count: 3}, summary)

	foundUntagged := false
	for _, row := range rows {
		require.NotEqual(t, 402, row.TokenID)
		if row.TokenID == 403 {
			foundUntagged = true
		}
	}
	require.True(t, foundUntagged)
}

func TestGetTokenTagQuotaAnalyticsExcludesKeysMatchingAnyExcludedTag(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		ExcludedTags: []string{"Shared", "Internal"},
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 380, TokenUsed: 80, Count: 2}, summary)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.NotEqual(t, 401, row.TokenID)
		require.NotEqual(t, 402, row.TokenID)
	}
}

func TestGetTokenTagQuotaAnalyticsScopesCommonUserDetailsAndSummary(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "bob", 1, common.RoleCommonUser, TokenTagQuotaFilters{})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 380, TokenUsed: 90, Count: 4}, summary)
	require.Len(t, rows, 6)
	for _, row := range rows {
		require.Zero(t, row.UserID)
		require.Empty(t, row.Username)
		require.NotEqual(t, 404, row.TokenID)
	}
}

func TestGetTokenTagQuotaAnalyticsExcludesHiddenUsersFromSummary(t *testing.T) {
	truncateTables(t)
	seedHiddenUserAnalyticsData(t)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 100, TokenUsed: 30, Count: 1}, summary)
	require.Len(t, rows, 1)
	require.Equal(t, "visible", rows[0].Username)
	require.Equal(t, "Visible Tag", rows[0].TagName)
}

func TestGetTokenTagQuotaAnalyticsAggregatesRepeatedModelLogs(t *testing.T) {
	truncateTables(t)
	seedTokenTagAnalyticsModels(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          401,
		TokenName:        "alice-client",
		ModelName:        "gpt-a",
		Type:             LogTypeConsume,
		Quota:            20,
		PromptTokens:     3,
		CompletionTokens: 2,
		CreatedAt:        1600,
	}).Error)

	rows, summary, err := GetTokenTagQuotaAnalytics(1000, 2000, "", 0, common.RoleAdminUser, TokenTagQuotaFilters{
		IncludedTags: []string{"Client A"},
	})
	require.NoError(t, err)
	require.Equal(t, TokenTagQuotaSummary{Quota: 420, TokenUsed: 115, Count: 4}, summary)

	var aggregated *TokenTagQuotaData
	for _, row := range rows {
		if row.TokenID == 401 && row.ModelName == "gpt-a" {
			aggregated = row
			break
		}
	}
	require.NotNil(t, aggregated)
	require.Equal(t, 60, aggregated.Quota)
	require.Equal(t, 20, aggregated.TokenUsed)
	require.Equal(t, 2, aggregated.Count)
	require.EqualValues(t, 1600, aggregated.LastUsedAt)
}
