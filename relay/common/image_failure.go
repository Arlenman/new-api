package common

import (
	"regexp"
	"strings"
	"unicode/utf8"

	basecommon "github.com/QuantumNous/new-api/common"
)

const imageErrorSummaryLimit = 512

var (
	imageErrorBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	imageErrorSecretPattern = regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|token|secret|authorization|b64[_-]?json)\b\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	imageErrorKeyPattern    = regexp.MustCompile(`(?i)\b(?:sk|rk|pk|ak)-[a-z0-9_-]{8,}\b`)
	imageErrorOpaquePattern = regexp.MustCompile(`\b[A-Za-z0-9+/_=-]{64,}\b`)
)

// ImageFailureMetadata contains non-sensitive diagnostics for an image relay
// failure. It intentionally stores only response metadata, never image URLs or
// base64 payloads, so it can be attached to persistent error logs safely.
type ImageFailureMetadata struct {
	Stage        string
	StatusCode   int
	ContentType  string
	ImageCount   int64
	HasURL       bool
	HasB64JSON   bool
	ErrorSummary string
}

// SanitizeImageErrorSummary keeps a bounded diagnostic summary while removing
// credentials, full upstream locations and image payloads. Callers may safely
// place the result in request logs or structured Playground errors, but must
// never attach the original upstream payload alongside it.
func SanitizeImageErrorSummary(message string) string {
	summary := strings.Join(strings.Fields(message), " ")
	if summary == "" {
		return "upstream image request failed"
	}

	summary = basecommon.MaskSensitiveInfo(summary)
	summary = imageErrorBearerPattern.ReplaceAllString(summary, "Bearer ***")
	summary = imageErrorSecretPattern.ReplaceAllString(summary, "credential=***")
	summary = imageErrorKeyPattern.ReplaceAllString(summary, "key-***")
	summary = imageErrorOpaquePattern.ReplaceAllString(summary, "[redacted]")
	summary = strings.Join(strings.Fields(summary), " ")

	if len(summary) <= imageErrorSummaryLimit {
		return summary
	}
	cut := imageErrorSummaryLimit - len("...")
	for cut > 0 && !utf8.RuneStart(summary[cut]) {
		cut--
	}
	return strings.TrimSpace(summary[:cut]) + "..."
}
