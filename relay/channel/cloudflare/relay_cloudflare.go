package cloudflare

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func convertCf2CompletionsRequest(textRequest dto.GeneralOpenAIRequest) *CfRequest {
	p, _ := textRequest.Prompt.(string)
	return &CfRequest{
		Prompt:      p,
		MaxTokens:   textRequest.GetMaxTokens(),
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
		Temperature: textRequest.Temperature,
	}
}

func cfStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	defer service.CloseResponseBodyGracefully(resp)
	info.ReceivedResponseCount = 0

	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)

	helper.SetEventStreamHeaders(c)
	id := helper.GetResponseID(c)
	var responseText string

	for scanner.Scan() {
		data := strings.TrimSpace(scanner.Text())
		if data == "" || strings.HasPrefix(data, ":") || !strings.HasPrefix(data, "data:") {
			continue
		}
		data = strings.TrimSpace(strings.TrimPrefix(data, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var response dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal([]byte(data), &response); err != nil {
			logger.LogError(c, "error_unmarshalling_stream_response: "+err.Error())
			continue
		}

		hasBusinessEvent := false
		hasResponseEvent := false
		for index := range response.Choices {
			choice := &response.Choices[index]
			choice.Delta.Role = "assistant"
			content := choice.Delta.GetContentString()
			responseText += content
			if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || choice.Delta.Reasoning != nil || len(choice.Delta.ToolCalls) > 0 {
				hasResponseEvent = true
			}
			if strings.TrimSpace(content) != "" || strings.TrimSpace(choice.Delta.GetReasoningContent()) != "" || len(choice.Delta.ToolCalls) > 0 {
				hasBusinessEvent = true
			}
		}
		if !hasResponseEvent && info.ReceivedResponseCount == 0 {
			continue
		}

		response.Id = id
		response.Model = info.UpstreamModelName
		if hasBusinessEvent {
			info.ReceivedResponseCount++
			if info.ReceivedResponseCount == 1 {
				info.FirstResponseTime = time.Now()
			}
		}
		if err := helper.ObjectData(c, response); err != nil {
			logger.LogError(c, "error_rendering_stream_response: "+err.Error())
		}
	}

	usage := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	if c.Request != nil && c.Request.Context().Err() != nil {
		return nil, usage
	}
	if err := scanner.Err(); err != nil {
		logger.LogError(c, "error_scanning_stream_response: "+err.Error())
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError), nil
	}
	if emptyErr := helper.ValidateStreamResponse(info); emptyErr != nil {
		return emptyErr, nil
	}
	if info.ShouldIncludeUsage {
		response := helper.GenerateFinalUsageResponse(id, info.StartTime.Unix(), info.UpstreamModelName, *usage)
		if err := helper.ObjectData(c, response); err != nil {
			logger.LogError(c, "error_rendering_final_usage_response: "+err.Error())
		}
	}
	helper.Done(c)

	return nil, usage
}

func cfHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	service.CloseResponseBodyGracefully(resp)
	var response dto.TextResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	response.Model = info.UpstreamModelName
	var responseText string
	for _, choice := range response.Choices {
		responseText += choice.Message.StringContent()
	}
	usage := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	response.Usage = *usage
	response.Id = helper.GetResponseID(c)
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return nil, usage
}

func cfSTTHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	var cfResp CfAudioResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	service.CloseResponseBodyGracefully(resp)
	err = json.Unmarshal(responseBody, &cfResp)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}

	audioResp := &dto.AudioResponse{
		Text: cfResp.Result.Text,
	}

	jsonResponse, err := json.Marshal(audioResp)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	usage := service.ResponseText2Usage(c, cfResp.Result.Text, info.UpstreamModelName, info.GetEstimatePromptTokens())
	return nil, usage
}
