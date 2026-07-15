package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMarkGeminiSensitiveResponseKeepsSafetySignal(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	safety := "SAFETY"
	stop := "STOP"

	markGeminiSensitiveResponse(c, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{FinishReason: &safety},
			{FinishReason: &stop},
		},
	})

	assert.Equal(t, "gemini_finish_reason=SAFETY", common.GetContextKeyString(c, constant.ContextKeySensitiveRequestReason))
}

func TestMarkGeminiSensitiveResponseIgnoresOrdinaryFinishReason(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	stop := "STOP"

	markGeminiSensitiveResponse(c, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{FinishReason: &stop}},
	})

	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySensitiveRequestReason))
}
