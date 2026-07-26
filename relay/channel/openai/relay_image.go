package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func updateOpenAIImageCount(info *relaycommon.RelayInfo, count int64) {
	if info == nil || !info.PriceData.UsePrice || count <= 0 || count > int64(dto.MaxImageN) {
		return
	}
	info.PriceData.AddOtherRatio("n", float64(count))
}

type openAIImageDataMetadata struct {
	ImageCount int64
	HasURL     bool
	HasB64JSON bool
	Indexes    []int
}

func inspectOpenAIImageData(responseBody []byte) openAIImageDataMetadata {
	metadata := openAIImageDataMetadata{}
	data := gjson.GetBytes(responseBody, "data")
	if !data.IsArray() {
		return metadata
	}
	for index, item := range data.Array() {
		hasURL := item.Get("url").Type == gjson.String && strings.TrimSpace(item.Get("url").Str) != ""
		hasB64JSON := item.Get("b64_json").Type == gjson.String && strings.TrimSpace(item.Get("b64_json").Str) != ""
		metadata.HasURL = metadata.HasURL || hasURL
		metadata.HasB64JSON = metadata.HasB64JSON || hasB64JSON
		if !hasURL && !hasB64JSON {
			continue
		}
		metadata.ImageCount++
		metadata.Indexes = append(metadata.Indexes, index)
	}
	return metadata
}

func countValidOpenAIImageData(items []dto.ImageData) (int64, bool, bool) {
	var count int64
	var hasURL bool
	var hasB64JSON bool
	for _, item := range items {
		itemHasURL := strings.TrimSpace(item.Url) != ""
		itemHasB64JSON := strings.TrimSpace(item.B64Json) != ""
		hasURL = hasURL || itemHasURL
		hasB64JSON = hasB64JSON || itemHasB64JSON
		if itemHasURL || itemHasB64JSON {
			count++
		}
	}
	return count, hasURL, hasB64JSON
}

func invalidOpenAIImageDataError(message string) *types.NewAPIError {
	return types.NewOpenAIError(
		errors.New(relaycommon.SanitizeImageErrorSummary(message)),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
}

func logOpenAIImageFailure(c *gin.Context, stage string, statusCode int, contentType string, imageCount int64, hasURL bool, hasB64JSON bool, errorSummary string) {
	requestID := ""
	channelID := 0
	modelName := ""
	errorSummary = relaycommon.SanitizeImageErrorSummary(errorSummary)
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
		channelID = c.GetInt("channel_id")
		modelName = c.GetString("original_model")
		common.SetContextKey(c, constant.ContextKeyImageFailureMetadata, relaycommon.ImageFailureMetadata{
			Stage:        stage,
			StatusCode:   statusCode,
			ContentType:  contentType,
			ImageCount:   imageCount,
			HasURL:       hasURL,
			HasB64JSON:   hasB64JSON,
			ErrorSummary: errorSummary,
		})
	}
	logger.LogError(c, fmt.Sprintf(
		"image response failure request_id=%s channel_id=%d model=%s stage=%s status=%d content_type=%q image_count=%d has_url=%t has_b64_json=%t error_summary=%q",
		requestID,
		channelID,
		modelName,
		stage,
		statusCode,
		contentType,
		imageCount,
		hasURL,
		hasB64JSON,
		errorSummary,
	))
}

// OpenaiImageHandler handles non-streaming OpenAI image responses
// (generations/edits), returning the parsed usage for billing.
func OpenaiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		logOpenAIImageFailure(c, "decode_response", resp.StatusCode, resp.Header.Get("Content-Type"), 0, false, false, "failed to decode upstream image response")
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	imageMetadata := inspectOpenAIImageData(responseBody)
	if imageMetadata.ImageCount == 0 {
		message := "upstream image response did not include a valid url or b64_json"
		logOpenAIImageFailure(c, "validate_image_data", resp.StatusCode, resp.Header.Get("Content-Type"), 0, imageMetadata.HasURL, imageMetadata.HasB64JSON, message)
		return nil, invalidOpenAIImageDataError(message)
	}
	updateOpenAIImageCount(info, imageMetadata.ImageCount)

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

// normalizeOpenAIUsage maps the OpenAI Images usage shape (input_tokens /
// output_tokens / input_tokens_details) onto the canonical prompt/completion
// fields. It is used only on the OpenAI image relay paths (generations/edits,
// streaming and non-streaming): the image API never returns prompt_tokens /
// completion_tokens, so the overwrite (=) semantics here are equivalent to the
// previous additive (+=) behavior while avoiding any future double-counting if
// both field sets are ever populated. Do not reuse this on chat/embedding paths
// without revisiting the overwrite semantics.
func normalizeOpenAIUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.CacheWriteTokens = usage.InputTokensDetails.CacheWriteTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return openaiImageJSONAsStreamHandler(c, info, resp)
	}
	// Reuse the shared streaming engine (helper.StreamScannerHandler) so the
	// image streaming path gets the same ping keepalive, streaming-timeout
	// watchdog, client-disconnect detection, panic recovery and goroutine
	// cleanup as every other relay stream. The scanner delivers only the
	// "data:" payload, so the SSE "event:" line is rebuilt from the JSON "type"
	// field (real OpenAI image events keep event == type).
	usage := &dto.Usage{}
	var lastStreamData []byte
	var streamError string
	var completedImages int64
	var sawCompletedEvent bool
	var hasCompletedURL bool
	var hasCompletedB64JSON bool

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		raw := common.StringToByteSlice(data)
		lastStreamData = raw
		if isOpenAIImageStreamErrorEvent(raw) {
			// Record the error as a soft error; the scanner drives the final
			// EndReason. HasErrors() flags the failure for logging/handling.
			streamError = extractOpenAIImageStreamErrorMessage(raw)
			sr.Error(fmt.Errorf("%s", streamError))
		}
		var chunk struct {
			Type    string          `json:"type"`
			Usage   dto.Usage       `json:"usage"`
			Data    []dto.ImageData `json:"data"`
			URL     string          `json:"url"`
			B64JSON string          `json:"b64_json"`
		}
		if err := common.Unmarshal(raw, &chunk); err == nil {
			normalizeOpenAIUsage(&chunk.Usage)
			if service.ValidUsage(&chunk.Usage) {
				usage = &chunk.Usage
			}
			completedEvent := chunk.Type == "image_generation.completed" || chunk.Type == "image_edit.completed"
			implicitCompletedData := strings.TrimSpace(chunk.Type) == "" && len(chunk.Data) > 0
			if completedEvent || implicitCompletedData {
				sawCompletedEvent = true
				dataCount, dataHasURL, dataHasB64JSON := countValidOpenAIImageData(chunk.Data)
				completedCount := dataCount
				if completedCount == 0 {
					topLevelHasURL := strings.TrimSpace(chunk.URL) != ""
					topLevelHasB64JSON := strings.TrimSpace(chunk.B64JSON) != ""
					if topLevelHasURL || topLevelHasB64JSON {
						completedCount = 1
					}
					dataHasURL = dataHasURL || topLevelHasURL
					dataHasB64JSON = dataHasB64JSON || topLevelHasB64JSON
				}
				completedImages += completedCount
				hasCompletedURL = hasCompletedURL || dataHasURL
				hasCompletedB64JSON = hasCompletedB64JSON || dataHasB64JSON
			}
		}
		if err := writeOpenaiImageStreamChunk(c, raw); err != nil {
			sr.Stop(err)
		}
	})

	endReason := relaycommon.StreamEndReasonNone
	if info.StreamStatus != nil {
		endReason = info.StreamStatus.EndReason
	}
	upstreamFinished := endReason == relaycommon.StreamEndReasonDone || endReason == relaycommon.StreamEndReasonEOF
	clientAborted := endReason == relaycommon.StreamEndReasonClientGone || endReason == relaycommon.StreamEndReasonHandlerStop
	if !clientAborted && streamError != "" {
		logOpenAIImageFailure(c, "parse_stream", resp.StatusCode, resp.Header.Get("Content-Type"), completedImages, hasCompletedURL, hasCompletedB64JSON, streamError)
		return nil, invalidOpenAIImageDataError(streamError)
	}
	if !clientAborted && completedImages == 0 {
		stage := "parse_stream"
		message := "image stream ended before a completed image was received"
		if upstreamFinished {
			message = "empty image stream response"
		}
		if sawCompletedEvent {
			stage = "validate_image_data"
			message = "completed image stream event did not include a valid url or b64_json"
		}
		logOpenAIImageFailure(c, stage, resp.StatusCode, resp.Header.Get("Content-Type"), 0, hasCompletedURL, hasCompletedB64JSON, message)
		return nil, invalidOpenAIImageDataError(message)
	}

	// StreamScannerHandler consumes the upstream [DONE]; re-emit it so the
	// client still receives a terminal data: [DONE].
	if info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone {
		helper.Done(c)
	}

	applyUsagePostProcessing(info, usage, lastStreamData)
	// Trust completedImages for every non-client termination. On client-side
	// aborts (client_gone, or handler_stop from a failed client
	// write) the counter undercounts what upstream actually generated and
	// charged, so keep the requested n — otherwise a client could pay for one
	// image by disconnecting right after the first completed event. The abort
	// guard only blocks lowering the charge: if completed events already
	// exceed the recorded n, bill the higher actual count regardless.
	if info.StreamStatus != nil {
		requestedN := 1.0
		if n, ok := info.PriceData.OtherRatios()["n"]; ok {
			requestedN = n
		}
		if !clientAborted || float64(completedImages) > requestedN {
			updateOpenAIImageCount(info, completedImages)
		}
	}
	return usage, nil
}

// writeOpenaiImageStreamChunk rebuilds the SSE frame for an image stream chunk:
// it emits an "event:" line derived from the JSON "type" field (when present)
// followed by the verbatim "data:" payload, mirroring helper.ResponseChunkData.
func writeOpenaiImageStreamChunk(c *gin.Context, data []byte) error {
	var payload struct {
		Type string `json:"type"`
	}
	_ = common.Unmarshal(data, &payload)
	if eventName := strings.TrimSpace(payload.Type); eventName != "" {
		return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventName}, string(data))
	}
	return helper.StringData(c, string(data))
}

// isOpenAIImageStreamErrorEvent detects upstream error chunks by JSON content
// only ("type" of error/upstream_error, or a non-empty "error" field). The SSE
// "event:" line is not available here: StreamScannerHandler delivers only the
// "data:" payload. A payload carrying just a "message" key is deliberately NOT
// treated as an error to avoid false positives.
func isOpenAIImageStreamErrorEvent(data []byte) bool {
	if !json.Valid(data) {
		return false
	}
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	return payloadType == "error" || payloadType == "upstream_error" || len(payload.Error) > 0
}

func extractOpenAIImageStreamErrorMessage(data []byte) string {
	if len(data) == 0 || !json.Valid(data) {
		return "upstream image stream returned error event"
	}
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return "upstream image stream returned error event"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return relaycommon.SanitizeImageErrorSummary(msg)
	}
	if len(payload.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return relaycommon.SanitizeImageErrorSummary(msg)
			}
		}

	}
	return "upstream image stream returned error event"
}

func openaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	// Only decode usage/error. Do not Unmarshal data[] into dto.ImageResponse —
	// b64_json values are large and would be copied into Go strings then
	// re-marshaled for each SSE event.
	var usageResp dto.SimpleResponse
	if err := common.Unmarshal(responseBody, &usageResp); err != nil {
		logOpenAIImageFailure(c, "decode_response", resp.StatusCode, resp.Header.Get("Content-Type"), 0, false, false, "failed to decode upstream image response")
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	imageMetadata := inspectOpenAIImageData(responseBody)
	if imageMetadata.ImageCount == 0 {
		message := "upstream image response did not include a valid url or b64_json"
		logOpenAIImageFailure(c, "validate_image_data", resp.StatusCode, resp.Header.Get("Content-Type"), 0, imageMetadata.HasURL, imageMetadata.HasB64JSON, message)
		return nil, invalidOpenAIImageDataError(message)
	}
	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)

	imageCount := imageMetadata.ImageCount
	updateOpenAIImageCount(info, imageCount)

	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)

	created := gjson.GetBytes(responseBody, "created").Int()
	if created == 0 {
		created = time.Now().Unix()
	}
	if info != nil {
		info.SetFirstResponseTime()
	}

	validUsage := service.ValidUsage(&usageResp.Usage)
	var usageJSON []byte
	if validUsage {
		usageJSON, err = common.Marshal(usageResp.Usage)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}

	for _, imageIndex := range imageMetadata.Indexes {
		image := gjson.GetBytes(responseBody, "data."+strconv.Itoa(imageIndex))
		payload := []byte(`{"type":"image_generation.completed"}`)
		payload, err = sjson.SetBytes(payload, "created_at", created)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if validUsage {
			payload, err = sjson.SetRawBytes(payload, "usage", usageJSON)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			}
		}
		// b64_json goes last: every sjson.Set* reallocates the whole payload,
		// so inserting the large blob after all small fields avoids re-copying
		// multi-MB buffers.
		for _, field := range []string{"url", "revised_prompt", "b64_json"} {
			value := image.Get(field)
			if value.Type != gjson.String || value.Raw == `""` {
				continue
			}
			raw := []byte(value.Raw)
			if value.Index > 0 {
				raw = responseBody[value.Index : value.Index+len(value.Raw)]
			}
			payload, err = sjson.SetRawBytes(payload, field, raw)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			}
		}
		if writeErr := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "image_generation.completed"}, string(payload)); writeErr != nil {
			if info != nil && info.StreamStatus != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, writeErr)
			}
			return &usageResp.Usage, nil
		}
	}
	if err := writeOpenaiImageStreamDone(c); err != nil {
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		}
		return &usageResp.Usage, nil
	}
	if info != nil {
		info.ReceivedResponseCount += int(imageCount)
		if info.StreamStatus == nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	}
	return &usageResp.Usage, nil
}

func writeOpenaiImageStreamDone(c *gin.Context) error {
	return helper.StringData(c, "[DONE]")
}
