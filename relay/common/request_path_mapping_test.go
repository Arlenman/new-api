package common

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func TestResolveRequestPathPrefersModelMapping(t *testing.T) {
	mapping := `{"models":{"gpt-4o":{"/v1/responses":"/v1/chat/completions"}},"channel":{"/v1/responses":"/v1/legacy-responses"}}`

	got := ResolveRequestPath("gpt-4o", "/v1/responses?stream=true", mapping)
	assert.Equal(t, "/v1/chat/completions?stream=true", got)
}

func TestResolveRequestPathFallsBackToChannelMapping(t *testing.T) {
	mapping := `{"channel":{"/v1/responses":"/v1/chat/completions"}}`

	got := ResolveRequestPath("gpt-4o", "/v1/responses?stream=true", mapping)
	assert.Equal(t, "/v1/chat/completions?stream=true", got)
}

func TestResolveRequestPathFallsBackToOriginalPath(t *testing.T) {
	original := "/v1/responses?stream=true"

	got := ResolveRequestPath("gpt-4o", original, "")
	assert.Equal(t, original, got)
}

func TestResolveRequestPathSupportsRegexModelMapping(t *testing.T) {
	mapping := `{"models":{"re:^gpt-4": {"/v1/responses":"/v1/chat/completions"}}}`

	assert.Equal(t, "/v1/chat/completions", ResolveRequestPath("gpt-4o", "/v1/responses", mapping))
	assert.Equal(t, "/v1/responses", ResolveRequestPath("claude-3", "/v1/responses", mapping))
}

func TestResolveRequestPathPrefersMoreSpecificRegexModelMapping(t *testing.T) {
	mapping := `{"models":{"re:^gpt-":{"/v1/responses":"/v1/general"},"re:^gpt-4":{"/v1/responses":"/v1/specific"}}}`

	assert.Equal(t, "/v1/specific", ResolveRequestPath("gpt-4o", "/v1/responses", mapping))
}

func TestResolveRequestPathForModelsPrefersUpstreamModel(t *testing.T) {
	mapping := `{"models":{"upstream-model":{"/v1/responses":"/v1/upstream"},"origin-model":{"/v1/responses":"/v1/origin"}}}`

	assert.Equal(t, "/v1/upstream", ResolveRequestPathForModels([]string{"upstream-model", "origin-model"}, "/v1/responses", mapping))
}

func TestRelayInfoGetUpstreamRelayFormatUsesMappedEndpoint(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "glm-5.3",
		ChannelMeta: &ChannelMeta{
			UpstreamModelName:         "glm-5.3",
			ChannelRequestPathMapping: `{"models":{"glm-5.3":{"/v1/responses":"/v1/chat/completions"}}}`,
		},
	}

	assert.Equal(t, types.RelayFormatOpenAI, info.GetUpstreamRelayFormat())
}

func TestRelayInfoGetUpstreamRelayFormatKeepsInboundFormatWithoutMapping(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "glm-5.3",
		ChannelMeta:     &ChannelMeta{UpstreamModelName: "glm-5.3"},
	}

	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetUpstreamRelayFormat())
}

func TestResolveRequestPathTargetQueryOverridesOriginalQuery(t *testing.T) {
	mapping := `{"channel":{"/v1/responses":"/v1/chat/completions?version=2"}}`

	assert.Equal(t, "/v1/chat/completions?version=2", ResolveRequestPath("gpt-4o", "/v1/responses?stream=true", mapping))
}

func TestResolveRequestPathPreservesOriginalPathOnInvalidMapping(t *testing.T) {
	original := "/v1/responses?stream=true"

	assert.Equal(t, original, ResolveRequestPath("gpt-4o", original, `{"channel":{"v1/responses":"/v1/chat/completions"}}`))
	assert.Equal(t, original, ResolveRequestPath("gpt-4o", original, "{"))
	assert.Equal(t, original, ResolveRequestPath("gpt-4o", original, `{"channel":{"/v1/responses":"//evil.example/v1/chat/completions#fragment"}}`))
}
