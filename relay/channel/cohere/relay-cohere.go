package cohere

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func requestOpenAI2Cohere(textRequest dto.GeneralOpenAIRequest) *CohereRequest {
	cohereReq := CohereRequest{
		Model:       textRequest.Model,
		ChatHistory: []ChatHistory{},
		Message:     "",
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
		MaxTokens:   textRequest.GetMaxTokens(),
	}
	if common.CohereSafetySetting != "NONE" {
		cohereReq.SafetyMode = common.CohereSafetySetting
	}
	if cohereReq.MaxTokens == 0 {
		cohereReq.MaxTokens = 4000
	}
	for _, msg := range textRequest.Messages {
		if msg.Role == "user" {
			cohereReq.Message = msg.StringContent()
		} else {
			var role string
			if msg.Role == "assistant" {
				role = "CHATBOT"
			} else if msg.Role == "system" {
				role = "SYSTEM"
			} else {
				role = "USER"
			}
			cohereReq.ChatHistory = append(cohereReq.ChatHistory, ChatHistory{
				Role:    role,
				Message: msg.StringContent(),
			})
		}
	}

	return &cohereReq
}

func requestConvertRerank2Cohere(rerankRequest dto.RerankRequest) *CohereRerankRequest {
	topN := lo.FromPtrOr(rerankRequest.TopN, 1)
	if topN <= 0 {
		topN = 1
	}
	cohereReq := CohereRerankRequest{
		Query:           rerankRequest.Query,
		Documents:       rerankRequest.Documents,
		Model:           rerankRequest.Model,
		TopN:            topN,
		ReturnDocuments: true,
	}
	return &cohereReq
}

func stopReasonCohere2OpenAI(reason string) string {
	switch reason {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "max_tokens"
	default:
		return reason
	}
}

func cohereStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	info.ReceivedResponseCount = 0

	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	usage := &dto.Usage{}
	responseText := ""
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.Index(string(data), "\n"); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	dataChan := make(chan string)
	stopChan := make(chan error)
	streamDone := make(chan struct{})
	var requestDone <-chan struct{}
	if c.Request != nil {
		requestDone = c.Request.Context().Done()
	}
	go func() {
		for scanner.Scan() {
			select {
			case dataChan <- scanner.Text():
			case <-streamDone:
				return
			case <-requestDone:
				return
			}
		}
		scanErr := scanner.Err()
		requestCanceled := false
		select {
		case <-requestDone:
			requestCanceled = true
		default:
		}
		if scanErr != nil && !requestCanceled {
			common.SysLog("error reading stream: " + scanErr.Error())
		}
		select {
		case stopChan <- scanErr:
		case <-streamDone:
		case <-requestDone:
		}
	}()
	helper.SetEventStreamHeaders(c)
	var streamErr error
	clientGone := c.Stream(func(w io.Writer) bool {
		select {
		case data := <-dataChan:
			data = strings.TrimSpace(strings.TrimSuffix(data, "\r"))
			if data == "" || strings.HasPrefix(data, ":") || data == "[DONE]" || data == "data: [DONE]" {
				return true
			}
			var cohereResp CohereResponse
			if err := common.Unmarshal([]byte(data), &cohereResp); err != nil {
				common.SysLog("error unmarshalling stream response: " + err.Error())
				return true
			}

			var openaiResp dto.ChatCompletionsStreamResponse
			openaiResp.Id = responseId
			openaiResp.Created = createdTime
			openaiResp.Object = "chat.completion.chunk"
			openaiResp.Model = info.UpstreamModelName
			if cohereResp.IsFinished {
				if info.ReceivedResponseCount == 0 {
					return true
				}
				finishReason := stopReasonCohere2OpenAI(cohereResp.FinishReason)
				openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{
					{
						Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
						Index:        0,
						FinishReason: &finishReason,
					},
				}
				if cohereResp.Response != nil {
					usage.PromptTokens = cohereResp.Response.Meta.BilledUnits.InputTokens
					usage.CompletionTokens = cohereResp.Response.Meta.BilledUnits.OutputTokens
				}
			} else {
				if cohereResp.Text == "" {
					return true
				}
				openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{
					{
						Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
							Role:    "assistant",
							Content: &cohereResp.Text,
						},
						Index: 0,
					},
				}
				responseText += cohereResp.Text
				if strings.TrimSpace(cohereResp.Text) != "" {
					info.ReceivedResponseCount++
					if info.ReceivedResponseCount == 1 {
						info.FirstResponseTime = time.Now()
					}
				}
			}
			jsonStr, err := common.Marshal(openaiResp)
			if err != nil {
				common.SysLog("error marshalling stream response: " + err.Error())
				return true
			}
			c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonStr)})
			return true
		case streamErr = <-stopChan:
			return false
		case <-requestDone:
			return false
		}
	})
	close(streamDone)
	if clientGone || (c.Request != nil && c.Request.Context().Err() != nil) {
		return usage, nil
	}
	if streamErr != nil {
		return nil, types.NewOpenAIError(streamErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if emptyErr := helper.ValidateStreamResponse(info); emptyErr != nil {
		return nil, emptyErr
	}
	if usage.PromptTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	helper.Done(c)
	return usage, nil
}

func cohereHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	createdTime := common.GetTimestamp()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	var cohereResp CohereResponseResult
	err = json.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
	usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
	usage.TotalTokens = cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens

	var openaiResp dto.TextResponse
	openaiResp.Id = cohereResp.ResponseId
	openaiResp.Created = createdTime
	openaiResp.Object = "chat.completion"
	openaiResp.Model = info.UpstreamModelName
	openaiResp.Usage = usage

	openaiResp.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Content: cohereResp.Text, Role: "assistant"},
			FinishReason: stopReasonCohere2OpenAI(cohereResp.FinishReason),
		},
	}

	jsonResponse, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return &usage, nil
}

func cohereRerankHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	var cohereResp CohereRerankResponseResult
	err = json.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	if cohereResp.Meta.BilledUnits.InputTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
		usage.CompletionTokens = 0
		usage.TotalTokens = info.GetEstimatePromptTokens()
	} else {
		usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
		usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
		usage.TotalTokens = cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens
	}

	var rerankResp dto.RerankResponse
	rerankResp.Results = cohereResp.Results
	rerankResp.Usage = usage

	jsonResponse, err := json.Marshal(rerankResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, err = c.Writer.Write(jsonResponse)
	return &usage, nil
}
