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
		UserId:    1,
		Username:  "alice",
		TokenId:   101,
		TokenName: "client",
		Type:      LogTypeConsume,
		CreatedAt: 1111,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		TokenId:   101,
		TokenName: "client",
		Type:      LogTypeConsume,
		CreatedAt: 1188,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		TokenId:   102,
		TokenName: "internal",
		Type:      LogTypeConsume,
		CreatedAt: 1255,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    2,
		Username:  "bob",
		TokenId:   201,
		TokenName: "client",
		Type:      LogTypeConsume,
		CreatedAt: 1399,
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
