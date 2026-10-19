package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureReasoningContentReplaySerializesForUpstream(t *testing.T) {
	reasoning := "先列出目录"
	messages := []dto.Message{
		{Role: "assistant", Content: "", Reasoning: &reasoning},
		{Role: "assistant", Content: "", ToolCalls: []byte(`[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]`)},
	}

	ensureReasoningContentReplay(messages)

	payload, err := common.Marshal(dto.GeneralOpenAIRequest{
		Model:    "deepseek-v4.1-flash",
		Messages: messages,
	})
	require.NoError(t, err)
	body := string(payload)
	assert.Contains(t, body, `"reasoning_content":"先列出目录"`)
	assert.Contains(t, body, `"reasoning_content":""`)
}

func TestEnsureReasoningContentReplayCopiesReasoningField(t *testing.T) {
	reasoning := "先列出目录，再读文件"
	messages := []dto.Message{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "查一下目录"},
		{Role: "assistant", Content: "", Reasoning: &reasoning},
	}

	ensureReasoningContentReplay(messages)

	require.NotNil(t, messages[2].ReasoningContent)
	assert.Equal(t, reasoning, *messages[2].ReasoningContent)
}

func TestEnsureReasoningContentReplayKeepsExistingValue(t *testing.T) {
	reasoning := "上游给的 reasoning"
	existing := "上游给的 reasoning_content"
	messages := []dto.Message{
		{Role: "assistant", Content: "answer", Reasoning: &reasoning, ReasoningContent: &existing},
	}

	ensureReasoningContentReplay(messages)

	require.NotNil(t, messages[0].ReasoningContent)
	assert.Equal(t, existing, *messages[0].ReasoningContent)
}

func TestEnsureReasoningContentReplayFillsToolCallTurns(t *testing.T) {
	messages := []dto.Message{
		{Role: "assistant", Content: "", ToolCalls: []byte(`[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]`)},
	}

	ensureReasoningContentReplay(messages)

	require.NotNil(t, messages[0].ReasoningContent)
	assert.Equal(t, "", *messages[0].ReasoningContent)
}

func TestEnsureReasoningContentReplaySkipsPlainAssistantTurns(t *testing.T) {
	messages := []dto.Message{
		{Role: "assistant", Content: "plain answer"},
		{Role: "assistant", Content: "", ToolCalls: []byte(`[]`)},
		{Role: "user", Content: "next"},
		{Role: "tool", ToolCallId: "call_1", Content: "result"},
	}

	ensureReasoningContentReplay(messages)

	assert.Nil(t, messages[0].ReasoningContent)
	assert.Nil(t, messages[1].ReasoningContent)
	assert.Nil(t, messages[2].ReasoningContent)
	assert.Nil(t, messages[3].ReasoningContent)
}

func TestConvertOpenAIRequestAppliesReasoningContentCompatOnlyWhenEnabled(t *testing.T) {
	reasoning := "客户端回放的思维链"

	newInfo := func(compat bool) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:       constant.ChannelTypeOpenAI,
				UpstreamModelName: "deepseek-v4.1-flash",
				ChannelSetting:    dto.ChannelSettings{ReasoningContentCompat: compat},
			},
		}
	}
	newRequest := func() *dto.GeneralOpenAIRequest {
		return &dto.GeneralOpenAIRequest{
			Model: "deepseek-v4.1-flash",
			Messages: []dto.Message{
				{Role: "user", Content: "继续"},
				{Role: "assistant", Content: "", Reasoning: &reasoning},
			},
		}
	}

	enabled := newRequest()
	_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, newInfo(true), enabled)
	require.NoError(t, err)
	require.NotNil(t, enabled.Messages[1].ReasoningContent)
	assert.Equal(t, reasoning, *enabled.Messages[1].ReasoningContent)

	disabled := newRequest()
	_, err = (&Adaptor{}).ConvertOpenAIRequest(nil, newInfo(false), disabled)
	require.NoError(t, err)
	assert.Nil(t, disabled.Messages[1].ReasoningContent)
}
