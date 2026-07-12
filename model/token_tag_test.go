package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func seedTokenForTags(t *testing.T, token Token) {
	t.Helper()
	require.NoError(t, DB.Create(&token).Error)
}

func TestReplaceTokenTagsNormalizesAndScopesByUser(t *testing.T) {
	truncateTables(t)
	seedTokenForTags(t, Token{Id: 101, UserId: 1, Key: "key-user-1", Name: "primary"})
	seedTokenForTags(t, Token{Id: 202, UserId: 2, Key: "key-user-2", Name: "other"})

	require.NoError(t, ReplaceTokenTags(1, 101, []string{" Alpha ", "alpha", "Beta", ""}))
	require.NoError(t, ReplaceTokenTags(2, 202, []string{"alpha"}))

	userOneTags, err := GetTokenTagNames(1, 101)
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha", "Beta"}, userOneTags)

	userTwoTags, err := GetTokenTagNames(2, 202)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha"}, userTwoTags)

	allUserOneTags, err := ListTokenTagsByUser(1)
	require.NoError(t, err)
	require.Len(t, allUserOneTags, 2)
	require.Equal(t, "Alpha", allUserOneTags[0].Name)
	require.Equal(t, "Beta", allUserOneTags[1].Name)
}

func TestReplaceTokenTagsReplacesExistingBindings(t *testing.T) {
	truncateTables(t)
	seedTokenForTags(t, Token{Id: 101, UserId: 1, Key: "key-user-1", Name: "primary"})

	require.NoError(t, ReplaceTokenTags(1, 101, []string{"Alpha", "Beta"}))
	require.NoError(t, ReplaceTokenTags(1, 101, []string{"Gamma"}))

	tags, err := GetTokenTagNames(1, 101)
	require.NoError(t, err)
	require.Equal(t, []string{"Gamma"}, tags)

	var bindingCount int64
	require.NoError(t, DB.Model(&TokenTagBinding{}).Where("token_id = ?", 101).Count(&bindingCount).Error)
	require.EqualValues(t, 1, bindingCount)
}
