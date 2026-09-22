package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenTagAnalyticsCancelsMetadataQueries(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		table   string
		filters TokenTagQuotaFilters
	}{
		{name: "included tags", table: "token_tag_bindings", filters: TokenTagQuotaFilters{IncludedTags: []string{"missing"}}},
		{name: "excluded tags", table: "token_tag_bindings", filters: TokenTagQuotaFilters{ExcludedTags: []string{"Client A"}}},
		{name: "untagged keys", table: "token_tag_bindings", filters: TokenTagQuotaFilters{IncludeUntagged: true}},
		{name: "hidden users", table: "users"},
		{name: "token names", table: "tokens"},
		{name: "tag names", table: "token_tag_bindings"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			seedTaggedQuotaData(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queryAttempted := false
			cancelQuery := func(tx *gorm.DB) {
				if tx.Statement.Table == testCase.table {
					queryAttempted = true
					cancel()
				}
			}
			checkCancellation := func(tx *gorm.DB) {
				if tx.Statement.Table == testCase.table {
					assert.ErrorIs(t, tx.Error, context.Canceled)
				}
			}
			require.NoError(t, DB.Callback().Query().Before("gorm:query").Register("token_tag_cancel", cancelQuery))
			require.NoError(t, DB.Callback().Query().After("gorm:query").Register("token_tag_check", checkCancellation))
			require.NoError(t, DB.Callback().Row().Before("gorm:row").Register("token_tag_cancel", cancelQuery))
			require.NoError(t, DB.Callback().Row().After("gorm:row").Register("token_tag_check", checkCancellation))
			t.Cleanup(func() {
				require.NoError(t, DB.Callback().Query().Remove("token_tag_cancel"))
				require.NoError(t, DB.Callback().Query().Remove("token_tag_check"))
				require.NoError(t, DB.Callback().Row().Remove("token_tag_cancel"))
				require.NoError(t, DB.Callback().Row().Remove("token_tag_check"))
			})

			_, _, err := getTokenTagQuotaAnalytics(ctx, 1000, 2000, "", 0, common.RoleAdminUser, testCase.filters)
			assert.True(t, queryAttempted)
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestTokenTagIPLookupHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GetTokenIPsByTokenIDsWithContext(ctx, []int{101})
	assert.ErrorIs(t, err, context.Canceled)
}
