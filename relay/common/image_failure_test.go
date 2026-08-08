package common

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeImageErrorSummary(t *testing.T) {
	longPayload := strings.Repeat("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo", 40)
	message := "\n stream ID 77 INTERNAL_ERROR while fetching https://private.example/image?token=secret " +
		"api_key=sk-sensitive Authorization: Bearer sk-auth Bearer sk-live b64_json=" + longPayload

	summary := SanitizeImageErrorSummary(message)

	assert.Contains(t, summary, "INTERNAL_ERROR")
	assert.NotContains(t, summary, "private.example")
	assert.NotContains(t, summary, "secret")
	assert.NotContains(t, summary, "sk-sensitive")
	assert.NotContains(t, summary, "sk-auth")
	assert.NotContains(t, summary, "sk-live")
	assert.NotContains(t, summary, longPayload[:32])
	assert.LessOrEqual(t, len(summary), imageErrorSummaryLimit)
	assert.True(t, utf8.ValidString(summary))
}

func TestSanitizeImageErrorSummaryBoundsUTF8Safely(t *testing.T) {
	summary := SanitizeImageErrorSummary("INTERNAL_ERROR " + strings.Repeat("图", imageErrorSummaryLimit))

	require.True(t, utf8.ValidString(summary))
	assert.Contains(t, summary, "INTERNAL_ERROR")
	assert.LessOrEqual(t, len(summary), imageErrorSummaryLimit)
	assert.True(t, strings.HasSuffix(summary, "..."))
}

func TestSanitizeImageErrorSummaryUsesSafeFallback(t *testing.T) {
	assert.Equal(t, "upstream image request failed", SanitizeImageErrorSummary(" \n\t "))
}
