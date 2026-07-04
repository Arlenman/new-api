package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func seedTaggedLogData(t *testing.T) {
	t.Helper()
	seedTokenForTags(t, Token{Id: 101, UserId: 1, Key: "key-user-1-client", Name: "client"})
	seedTokenForTags(t, Token{Id: 102, UserId: 1, Key: "key-user-1-internal", Name: "internal"})
	seedTokenForTags(t, Token{Id: 201, UserId: 2, Key: "key-user-2-client", Name: "client"})
	require.NoError(t, ReplaceTokenTags(1, 101, []string{"Client A"}))
	require.NoError(t, ReplaceTokenTags(1, 102, []string{"Internal"}))
	require.NoError(t, ReplaceTokenTags(2, 201, []string{"Client A"}))

	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          101,
		TokenName:        "client",
		Type:             LogTypeConsume,
		CreatedAt:        1100,
		ModelName:        "gpt-a",
		Quota:            100,
		PromptTokens:     30,
		CompletionTokens: 20,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          102,
		TokenName:        "internal",
		Type:             LogTypeConsume,
		CreatedAt:        1200,
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     60,
		CompletionTokens: 40,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           2,
		Username:         "bob",
		TokenId:          201,
		TokenName:        "client",
		Type:             LogTypeConsume,
		CreatedAt:        1300,
		ModelName:        "gpt-b",
		Quota:            300,
		PromptTokens:     90,
		CompletionTokens: 60,
	}).Error)
}

func TestGetUserLogsFiltersByTokenTag(t *testing.T) {
	truncateTables(t)
	seedTaggedLogData(t)

	logs, total, err := GetUserLogs(1, LogTypeConsume, 1000, 2000, "", "", 0, 10, "", "", "", "Client A")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, 101, logs[0].TokenId)
}

func TestGetAllLogsFiltersByTokenTagAndUsername(t *testing.T) {
	truncateTables(t)
	seedTaggedLogData(t)

	logs, total, err := GetAllLogs(LogTypeConsume, 1000, 2000, "", "", "", 0, 10, 0, "", "", "", "Client A")
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	require.Equal(t, 201, logs[0].TokenId)
	require.Equal(t, 101, logs[1].TokenId)

	aliceLogs, aliceTotal, err := GetAllLogs(LogTypeConsume, 1000, 2000, "", "alice", "", 0, 10, 0, "", "", "", "Client A")
	require.NoError(t, err)
	require.EqualValues(t, 1, aliceTotal)
	require.Len(t, aliceLogs, 1)
	require.Equal(t, 101, aliceLogs[0].TokenId)
}

func TestSumUsedQuotaFiltersByTokenTag(t *testing.T) {
	truncateTables(t)
	seedTaggedLogData(t)

	stat, err := SumUsedQuota(LogTypeConsume, 1000, 2000, "", "", "", 0, "", "Client A", true)
	require.NoError(t, err)
	require.Equal(t, 400, stat.Quota)

	aliceStat, err := SumUsedQuota(LogTypeConsume, 1000, 2000, "", "alice", "", 0, "", "Client A", true)
	require.NoError(t, err)
	require.Equal(t, 100, aliceStat.Quota)
}
