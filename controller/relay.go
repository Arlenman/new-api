package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			prepareRelayErrorResponse(c)
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			if writeCommittedRelayStreamError(c, relayFormat, newAPIError) {
				return
			}
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			if setting.LogSensitiveRequestEnabled {
				snapshot := service.BuildSensitiveRequestSnapshot(request)
				signal := service.SensitiveUpstreamSignal{
					Reason:     "sensitive_words",
					ErrorCode:  string(types.ErrorCodeSensitiveWordsDetected),
					StatusCode: http.StatusBadRequest,
				}
				recordSensitiveRequest(c, relayInfo, 0, "local", words, signal, snapshot, "Local sensitive-word rule blocked request")
			}
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	retryState := newRelayRetryState()
	if relayFormat != types.RelayFormatOpenAIRealtime {
		installRelayResponseWriter(c, relayInfo)
	} else {
		// Realtime is a WebSocket stream. Its business output bypasses the HTTP
		// ResponseWriter, so the normal Relay response boundary cannot safely
		// determine whether replaying the request is still side-effect free.
		retryState.relayServerErrorRetryEnabled = false
		retryState.upstreamAttemptLimit = 0
	}

	for ; retryState.shouldContinue(retryParam.GetRetry()); retryParam.IncreaseRetry() {
		retryState.beginIteration()
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			if shouldPreserveRelayErrorOnChannelSelectionFailure(c, relayInfo.LastError, channelErr) {
				newAPIError = relayInfo.LastError
			} else {
				newAPIError = channelErr
			}
			break
		}
		if shouldStopPlaygroundRelayGatewayTimeoutRetry(c, channel.Id, relayInfo.LastError, retryParam.GetRetry()) {
			newAPIError = relayInfo.LastError
			break
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		common.SetContextKey(c, constant.ContextKeySensitiveRequestReason, "")
		beginRelayUpstreamAttempt(c, relayInfo)
		retryState.recordUpstreamAttempt()
		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		newAPIError = finishRelayUpstreamAttempt(c, newAPIError)

		if setting.LogSensitiveRequestEnabled {
			marker := common.GetContextKeyString(c, constant.ContextKeySensitiveRequestReason)
			if signal, sensitive := service.ClassifySensitiveUpstreamBlock(marker, newAPIError); sensitive {
				snapshot := service.BuildSensitiveRequestSnapshot(request)
				recordSensitiveRequest(c, relayInfo, channel.Id, "upstream", nil, signal, snapshot, "Upstream content safety policy blocked request")
			}
		}

		if relayFormat != types.RelayFormatOpenAIRealtime && newAPIError == nil && c.Request.Context().Err() == nil && !hasRelayBusinessResponseWritten(c) {
			newAPIError = helper.NewEmptyResponseError("")
		}
		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		if isModelCapacityError(newAPIError) {
			excludedChannelIds, _ := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyModelCapacityExcludedChannelIds)
			if excludedChannelIds == nil {
				excludedChannelIds = make(map[int]struct{})
			}
			excludedChannelIds[channel.Id] = struct{}{}
			common.SetContextKey(c, constant.ContextKeyModelCapacityExcludedChannelIds, excludedChannelIds)
		}

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		willRetry := retryState.shouldRetry(c, newAPIError, retryParam.GetRetry())
		handlePlaygroundRelayChannelFailure(c, channel.Id, newAPIError, willRetry)
		handleRelayServerErrorChannelFailure(c, channel.Id, newAPIError, willRetry)
		if !willRetry {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func recordSensitiveRequest(c *gin.Context, relayInfo *relaycommon.RelayInfo, channelID int, source string, matchedWords []string, signal service.SensitiveUpstreamSignal, snapshot service.SensitiveRequestSnapshot, content string) {
	err := model.RecordSensitiveRequestLog(c, relayInfo.UserId, model.RecordSensitiveRequestLogParams{
		TokenId:          relayInfo.TokenId,
		TokenName:        c.GetString("token_name"),
		ModelName:        relayInfo.OriginModelName,
		ChannelId:        channelID,
		Group:            relayInfo.UsingGroup,
		IsStream:         relayInfo.IsStream,
		Content:          content,
		SensitiveRequest: service.SensitiveRequestFailureMetadata(source, signal, matchedWords, snapshot),
	})
	if err != nil {
		logger.LogError(c, service.SensitiveRequestLogFailureMessage(source, channelID, err))
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

const (
	playgroundRelayGatewayTimeoutSameChannelRetryLimit = 1
	relayServerErrorMaxAttempts                        = 5
	relayHeartbeatPayload                              = ": PING\n\n"
)

type relayRetryState struct {
	maxRetryTimes                int
	upstreamAttemptCount         int
	upstreamAttemptLimit         int
	relayServerErrorRetryEnabled bool
	relayServerErrorRetryPending bool
}

func newRelayRetryState() *relayRetryState {
	return &relayRetryState{
		maxRetryTimes:                common.RetryTimes,
		upstreamAttemptLimit:         0,
		relayServerErrorRetryEnabled: true,
	}
}

func (state *relayRetryState) shouldContinue(retry int) bool {
	if state.upstreamAttemptLimit > 0 && state.upstreamAttemptCount >= state.upstreamAttemptLimit {
		return false
	}
	return retry <= state.maxRetryTimes || state.relayServerErrorRetryPending
}

func (state *relayRetryState) beginIteration() {
	state.relayServerErrorRetryPending = false
}

func (state *relayRetryState) recordUpstreamAttempt() {
	state.upstreamAttemptCount++
}

func (state *relayRetryState) shouldRetry(c *gin.Context, err *types.NewAPIError, retry int) bool {
	serverError := state.relayServerErrorRetryEnabled && isRelayServerErrorRetryTarget(err)
	if serverError && state.upstreamAttemptLimit == 0 {
		state.upstreamAttemptLimit = relayServerErrorMaxAttempts
	}
	if !serverError && shouldIncreaseRelayRetryBudget(c, err) && state.maxRetryTimes < 3 {
		state.maxRetryTimes = 3
	}

	willRetry := false
	if serverError {
		willRetry = shouldRetryForRelayServerError(c, err, state.upstreamAttemptCount)
	} else {
		willRetry = shouldRetry(c, err, state.maxRetryTimes-retry)
	}
	if state.upstreamAttemptLimit > 0 && state.upstreamAttemptCount >= state.upstreamAttemptLimit {
		willRetry = false
	}
	state.relayServerErrorRetryPending = serverError && willRetry
	return willRetry
}

type relayResponseWriter struct {
	gin.ResponseWriter
	context                 *gin.Context
	mu                      sync.Mutex
	pendingSSE              bytes.Buffer
	deferredSSE             bytes.Buffer
	pendingBody             bytes.Buffer
	pendingStatusCode       int
	initialHeader           http.Header
	forcePassthrough        bool
	allowAudioTextResponse  bool
	clientStreamCommitted   atomic.Bool
	businessResponseWritten atomic.Bool
}

func (writer *relayResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *relayResponseWriter) Write(body []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writeLocked(body)
}

func (writer *relayResponseWriter) WriteString(body string) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writeLocked([]byte(body))
}

func (writer *relayResponseWriter) WriteHeader(code int) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.forcePassthrough || writer.businessResponseWritten.Load() {
		writer.writePendingHeaderLocked()
		writer.ResponseWriter.WriteHeader(code)
		return
	}
	// Match net/http's first-header-wins behavior while the attempt is still
	// buffered. A later provider write must not change the status that will be
	// committed with the first business payload.
	if writer.pendingStatusCode == 0 {
		writer.pendingStatusCode = code
	}
}

func (writer *relayResponseWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.forcePassthrough || writer.businessResponseWritten.Load() {
		writer.writePendingHeaderLocked()
		writer.ResponseWriter.Flush()
	}
}

func (writer *relayResponseWriter) WriteHeaderNow() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.forcePassthrough || writer.businessResponseWritten.Load() {
		writer.writePendingHeaderLocked()
		writer.ResponseWriter.WriteHeaderNow()
	}
}

func (writer *relayResponseWriter) writeLocked(body []byte) (int, error) {
	if len(body) == 0 {
		return 0, nil
	}
	if writer.forcePassthrough {
		writer.writePendingHeaderLocked()
		return writer.ResponseWriter.Write(body)
	}
	if writer.businessResponseWritten.Load() {
		writer.writePendingHeaderLocked()
		return writer.ResponseWriter.Write(body)
	}
	if !isRelaySSEPayload(writer.Header().Get("Content-Type"), body) {
		if len(bytes.TrimSpace(body)) == 0 {
			return len(body), nil
		}
		contentType := writer.Header().Get("Content-Type")
		if isRelayJSONPayload(contentType, body) {
			return writer.writeJSONBodyLocked(body)
		}
		if writer.allowAudioTextResponse {
			mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
			trimmed := bytes.TrimSpace(body)
			lowerBody := bytes.ToLower(trimmed)
			isHTML := mediaType == "text/html" ||
				bytes.HasPrefix(lowerBody, []byte("<!doctype html")) ||
				bytes.HasPrefix(lowerBody, []byte("<html"))
			if !isHTML && (mediaType == "" || mediaType == "text/plain" || mediaType == "text/vtt" ||
				mediaType == "application/x-subrip" || mediaType == "application/srt") {
				writer.writePendingHeaderLocked()
				n, err := writer.ResponseWriter.Write(body)
				if n > 0 {
					writer.markBusinessResponseWritten()
				}
				if err != nil {
					return n, err
				}
				return len(body), nil
			}
		}
		if !isRelayBinaryPayload(contentType) {
			// A 200 text/HTML body is frequently a proxy or provider failure page,
			// not model output. Keep it uncommitted so the controller can fail over.
			_, _ = writer.pendingBody.Write(body)
			return len(body), nil
		}
		writer.writePendingHeaderLocked()
		n, err := writer.ResponseWriter.Write(body)
		if n > 0 {
			writer.markBusinessResponseWritten()
		}
		if err != nil {
			return n, err
		}
		return len(body), nil
	}

	_, _ = writer.pendingSSE.Write(body)
	for {
		event, consumed, ok := splitNextRelaySSEEvent(writer.pendingSSE.Bytes())
		if !ok {
			break
		}
		event = append([]byte(nil), event...)
		remaining := append([]byte(nil), writer.pendingSSE.Bytes()[consumed:]...)
		writer.pendingSSE.Reset()
		_, _ = writer.pendingSSE.Write(remaining)

		if err := writer.processRelaySSEEventLocked(event); err != nil {
			return 0, err
		}
	}
	return len(body), nil
}

func (writer *relayResponseWriter) processRelaySSEEventLocked(event []byte) error {
	switch classifyRelaySSEEvent(event) {
	case relaySSEEventHeartbeat:
		writer.writePendingHeaderLocked()
		if _, err := writer.ResponseWriter.Write(event); err != nil {
			return err
		}
		writer.clientStreamCommitted.Store(true)
		writer.ResponseWriter.Flush()
	case relaySSEEventDeferred:
		_, _ = writer.deferredSSE.Write(event)
	case relaySSEEventBusiness:
		writer.writePendingHeaderLocked()
		if writer.deferredSSE.Len() > 0 {
			if _, err := writer.ResponseWriter.Write(writer.deferredSSE.Bytes()); err != nil {
				return err
			}
			writer.deferredSSE.Reset()
		}
		if _, err := writer.ResponseWriter.Write(event); err != nil {
			return err
		}
		writer.markBusinessResponseWritten()
		if writer.pendingSSE.Len() > 0 {
			if _, err := writer.ResponseWriter.Write(writer.pendingSSE.Bytes()); err != nil {
				return err
			}
			writer.pendingSSE.Reset()
		}
	case relaySSEEventDiscard:
		// Terminal-only markers, empty data events and event-only metadata must
		// not commit the client stream before another channel can be tried.
	}
	return nil
}

func (writer *relayResponseWriter) finishAttempt() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.pendingSSE.Len() == 0 {
		return
	}

	// The upstream handler has returned, so the buffered bytes can no longer be
	// a partial write. Process them as the final SSE event without adding a
	// synthetic delimiter to the client response.
	event := append([]byte(nil), writer.pendingSSE.Bytes()...)
	writer.pendingSSE.Reset()
	_ = writer.processRelaySSEEventLocked(event)
}

func (writer *relayResponseWriter) markBusinessResponseWritten() {
	writer.businessResponseWritten.Store(true)
	if writer.context != nil {
		common.SetContextKey(writer.context, constant.ContextKeyRelayBusinessResponseWritten, true)
	}
}

func (writer *relayResponseWriter) beginAttempt() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pendingSSE.Reset()
	writer.deferredSSE.Reset()
	writer.pendingBody.Reset()
	writer.pendingStatusCode = 0
	writer.forcePassthrough = false
	writer.businessResponseWritten.Store(false)
	if writer.context != nil {
		common.SetContextKey(writer.context, constant.ContextKeyRelayBusinessResponseWritten, false)
		writer.context.Set("event_stream_headers_set", false)
	}
	writer.restoreAttemptHeadersLocked()
}

func (writer *relayResponseWriter) prepareErrorResponse() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pendingSSE.Reset()
	writer.deferredSSE.Reset()
	writer.pendingBody.Reset()
	writer.pendingStatusCode = 0
	writer.forcePassthrough = true
	if !writer.businessResponseWritten.Load() && !writer.ResponseWriter.Written() {
		writer.restoreAttemptHeadersLocked()
		if writer.context != nil {
			writer.context.Set("event_stream_headers_set", false)
		}
	}
}

func (writer *relayResponseWriter) writePendingHeaderLocked() {
	if writer.pendingStatusCode == 0 {
		return
	}
	writer.ResponseWriter.WriteHeader(writer.pendingStatusCode)
	writer.pendingStatusCode = 0
}

func (writer *relayResponseWriter) restoreAttemptHeadersLocked() {
	header := writer.Header()
	for key := range header {
		header.Del(key)
	}
	for key, values := range writer.initialHeader {
		header[key] = append([]string(nil), values...)
	}
}

func (writer *relayResponseWriter) writeJSONBodyLocked(body []byte) (int, error) {
	_, _ = writer.pendingBody.Write(body)

	var value any
	if err := common.Unmarshal(writer.pendingBody.Bytes(), &value); err != nil {
		// Keep a possibly fragmented JSON response pending. If the handler ends
		// without producing a complete business payload, the controller will
		// fail over instead of committing an empty 200 response.
		return len(body), nil
	}
	if !helper.HasBusinessResponseData(writer.pendingBody.Bytes()) {
		writer.pendingBody.Reset()
		return len(body), nil
	}

	payload := append([]byte(nil), writer.pendingBody.Bytes()...)
	writer.pendingBody.Reset()
	writer.writePendingHeaderLocked()
	if _, err := writer.ResponseWriter.Write(payload); err != nil {
		return 0, err
	}
	writer.markBusinessResponseWritten()
	return len(body), nil
}

type relaySSEEventKind uint8

const (
	relaySSEEventDiscard relaySSEEventKind = iota
	relaySSEEventHeartbeat
	relaySSEEventDeferred
	relaySSEEventBusiness
)

func isRelayJSONPayload(contentType string, body []byte) bool {
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "json") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{', '[', '"':
		return true
	default:
		return bytes.Equal(trimmed, []byte("null"))
	}
}

func isRelayBinaryPayload(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "video/") {
		return true
	}
	switch mediaType {
	case "application/octet-stream", "application/pdf", "application/zip", "application/gzip":
		return true
	default:
		return false
	}
}

func isRelaySSEPayload(contentType string, body []byte) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	for _, prefix := range [][]byte{
		[]byte(":"),
		[]byte("data:"),
		[]byte("event:"),
		[]byte("id:"),
		[]byte("retry:"),
	} {
		if bytes.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func splitNextRelaySSEEvent(buffer []byte) ([]byte, int, bool) {
	lfIndex := bytes.Index(buffer, []byte("\n\n"))
	crlfIndex := bytes.Index(buffer, []byte("\r\n\r\n"))
	index := -1
	delimiterLength := 0
	if lfIndex >= 0 {
		index = lfIndex
		delimiterLength = 2
	}
	if crlfIndex >= 0 && (index < 0 || crlfIndex < index) {
		index = crlfIndex
		delimiterLength = 4
	}
	if index < 0 {
		return nil, 0, false
	}
	consumed := index + delimiterLength
	return buffer[:consumed], consumed, true
}

func classifyRelaySSEEvent(event []byte) relaySSEEventKind {
	normalized := strings.ReplaceAll(string(event), "\r\n", "\n")
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		return relaySSEEventDiscard
	}
	if strings.EqualFold(trimmed, "[DONE]") {
		return relaySSEEventDiscard
	}

	lines := strings.Split(normalized, "\n")
	commentOnly := true
	hasData := false
	dataParts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		commentOnly = false
		if strings.HasPrefix(line, "data:") {
			hasData = true
			dataParts = append(dataParts, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if commentOnly {
		return relaySSEEventHeartbeat
	}
	if !hasData {
		return relaySSEEventDeferred
	}

	payload := strings.TrimSpace(strings.Join(dataParts, "\n"))
	switch {
	case payload == "":
		return relaySSEEventDiscard
	case strings.EqualFold(payload, "[DONE]"):
		return relaySSEEventDiscard
	case strings.EqualFold(payload, "null"):
		return relaySSEEventDiscard
	case payload == "{}" || payload == "[]":
		return relaySSEEventDiscard
	}

	var imageEvent struct {
		Type    string `json:"type"`
		URL     string `json:"url"`
		B64JSON string `json:"b64_json"`
		Data    []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := common.Unmarshal([]byte(payload), &imageEvent); err == nil {
		switch strings.ToLower(strings.TrimSpace(imageEvent.Type)) {
		case "image_generation.partial_image", "image_edit.partial_image":
			return relaySSEEventDeferred
		case "image_generation.completed", "image_edit.completed":
			if strings.TrimSpace(imageEvent.URL) != "" || strings.TrimSpace(imageEvent.B64JSON) != "" {
				return relaySSEEventBusiness
			}
			for _, image := range imageEvent.Data {
				if strings.TrimSpace(image.URL) != "" || strings.TrimSpace(image.B64JSON) != "" {
					return relaySSEEventBusiness
				}
			}
			return relaySSEEventDeferred
		}
	}
	if helper.HasBusinessResponseData([]byte(payload)) {
		return relaySSEEventBusiness
	}
	return relaySSEEventDeferred
}

func installRelayResponseWriter(c *gin.Context, relayInfo *relaycommon.RelayInfo) *relayResponseWriter {
	if writer, ok := c.Writer.(*relayResponseWriter); ok {
		return writer
	}
	allowAudioTextResponse := false
	if relayInfo != nil {
		switch relayInfo.RelayMode {
		case relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation:
			if audioRequest, ok := relayInfo.Request.(*dto.AudioRequest); ok {
				switch strings.ToLower(strings.TrimSpace(audioRequest.ResponseFormat)) {
				case "text", "srt", "vtt":
					allowAudioTextResponse = true
				}
			}
		}
	}
	writer := &relayResponseWriter{
		ResponseWriter:         c.Writer,
		context:                c,
		initialHeader:          c.Writer.Header().Clone(),
		allowAudioTextResponse: allowAudioTextResponse,
	}
	c.Writer = writer
	common.SetContextKey(c, constant.ContextKeyRelayResponseBoundaryInstalled, true)
	common.SetContextKey(c, constant.ContextKeyRelayBusinessResponseWritten, false)
	return writer
}

func hasRelayBusinessResponseWritten(c *gin.Context) bool {
	writer, ok := c.Writer.(*relayResponseWriter)
	return ok && writer.businessResponseWritten.Load()
}

func beginRelayUpstreamAttempt(c *gin.Context, info *relaycommon.RelayInfo) {
	if info != nil {
		info.ReceivedResponseCount = 0
		info.SendResponseCount = 0
		info.StreamStatus = nil
	}
	if writer, ok := c.Writer.(*relayResponseWriter); ok {
		writer.beginAttempt()
	}
}

func finishRelayUpstreamAttempt(c *gin.Context, relayErr *types.NewAPIError) *types.NewAPIError {
	writer, ok := c.Writer.(*relayResponseWriter)
	if !ok {
		return relayErr
	}
	writer.finishAttempt()
	if relayErr != nil && relayErr.GetErrorCode() == types.ErrorCodeEmptyResponse && writer.businessResponseWritten.Load() {
		// Provider handlers may validate the response boundary before returning.
		// Once the EOF-terminated event is committed, that empty-response error is stale.
		return nil
	}
	return relayErr
}

func prepareRelayErrorResponse(c *gin.Context) {
	if writer, ok := c.Writer.(*relayResponseWriter); ok {
		writer.prepareErrorResponse()
	}
}

func writeCommittedRelayStreamError(c *gin.Context, relayFormat types.RelayFormat, relayErr *types.NewAPIError) bool {
	writer, ok := c.Writer.(*relayResponseWriter)
	if !ok || relayErr == nil || writer.businessResponseWritten.Load() || !writer.clientStreamCommitted.Load() {
		return false
	}

	var payload any
	if relayFormat == types.RelayFormatClaude {
		payload = gin.H{"type": "error", "error": relayErr.ToClaudeError()}
	} else {
		payload = gin.H{"error": relayErr.ToOpenAIError()}
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return false
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.forcePassthrough = true
	var event []byte
	if relayFormat == types.RelayFormatClaude {
		event = []byte("event: error\ndata: " + string(encoded) + "\n\n")
	} else {
		event = []byte("data: " + string(encoded) + "\n\ndata: [DONE]\n\n")
	}
	if _, err := writer.ResponseWriter.Write(event); err != nil {
		return false
	}
	writer.ResponseWriter.Flush()
	return true
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

type noAvailableRelayChannelError struct {
	message string
}

func (err *noAvailableRelayChannelError) Error() string {
	return err.message
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(&noAvailableRelayChannelError{
			message: fmt.Sprintf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName),
		}, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetryByAutomaticDisableStatusCode(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	code := openaiErr.StatusCode
	if code < 100 || code > 599 {
		return false
	}
	if operation_setting.IsAlwaysSkipRetryStatusCode(code) {
		return false
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldDisableByStatusCode(code)
}

func isRelayServerErrorRetryTarget(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	if openaiErr.GetErrorCode() == types.ErrorCodeEmptyResponse {
		return true
	}
	if openaiErr.OriginalStatusCode == http.StatusInternalServerError {
		return true
	}
	return openaiErr.OriginalStatusCode == 0 &&
		openaiErr.StatusCode == http.StatusInternalServerError &&
		openaiErr.GetErrorCode() == types.ErrorCodeDoRequestFailed
}

func shouldRetryForRelayServerError(c *gin.Context, openaiErr *types.NewAPIError, attempts int) bool {
	if !isRelayServerErrorRetryTarget(openaiErr) || types.IsSkipRetryError(openaiErr) {
		return false
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if attempts >= relayServerErrorMaxAttempts {
		return false
	}
	if hasRelayBusinessResponseWritten(c) {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if _, ok := c.Get(string(constant.ContextKeyTokenSpecificChannelId)); ok {
		return false
	}
	return true
}

func shouldPreserveRelayErrorOnChannelSelectionFailure(c *gin.Context, lastErr, channelErr *types.NewAPIError) bool {
	if lastErr == nil {
		return false
	}
	if isRelayServerErrorRetryTarget(lastErr) {
		var noCandidateErr *noAvailableRelayChannelError
		return errors.As(channelErr, &noCandidateErr)
	}
	return isModelCapacityError(lastErr) ||
		isPlaygroundImageQuotaExhaustedError(c, lastErr) ||
		isPlaygroundRelayTransportFailure(c, lastErr) ||
		isPlaygroundRelayGatewayTimeout(c, lastErr.StatusCode)
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if isModelCapacityError(openaiErr) {
		if retryTimes <= 0 {
			return false
		}
		if _, ok := c.Get(string(constant.ContextKeyTokenSpecificChannelId)); ok {
			return false
		}
		if c.Writer != nil && c.Writer.Written() {
			return false
		}
		return true
	}
	if isPlaygroundImageQuotaExhaustedError(c, openaiErr) {
		if retryTimes <= 0 {
			return false
		}
		if c.Writer != nil && c.Writer.Written() {
			return false
		}
		if _, ok := c.Get("specific_channel_id"); ok {
			return false
		}
		if _, ok := c.Get(string(constant.ContextKeyTokenSpecificChannelId)); ok {
			return false
		}
		return !isSinglePlaygroundRelayCandidateChannel(c)
	}
	if isPlaygroundRelayGatewayTimeout(c, openaiErr.StatusCode) {
		if retryTimes <= 0 {
			return false
		}
		if c.Writer != nil && c.Writer.Written() {
			return false
		}
		if _, ok := c.Get("specific_channel_id"); ok {
			return false
		}
		if _, ok := c.Get(string(constant.ContextKeyTokenSpecificChannelId)); ok {
			return false
		}
		return !isSinglePlaygroundRelayCandidateChannel(c)
	}
	if isPlaygroundRelayTransportFailure(c, openaiErr) {
		if retryTimes <= 0 {
			return false
		}
		if c.Writer != nil && c.Writer.Written() {
			return false
		}
		if _, ok := c.Get("specific_channel_id"); ok {
			return false
		}
		if _, ok := c.Get(string(constant.ContextKeyTokenSpecificChannelId)); ok {
			return false
		}
		return !isSinglePlaygroundRelayCandidateChannel(c)
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if isPlaygroundRelayGatewayTimeout(c, code) {
		if isSinglePlaygroundRelayCandidateChannel(c) {
			return false
		}
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code) || operation_setting.ShouldDisableByStatusCode(code)
}

func shouldIncreaseRelayRetryBudget(c *gin.Context, openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	if isModelCapacityError(openaiErr) {
		return true
	}
	if isPlaygroundImageQuotaExhaustedError(c, openaiErr) {
		return true
	}
	if isPlaygroundRelayGatewayTimeout(c, openaiErr.StatusCode) {
		return true
	}
	if isPlaygroundRelayTransportFailure(c, openaiErr) {
		return true
	}
	return shouldRetryByAutomaticDisableStatusCode(openaiErr)
}

func shouldAutoDisableChannel(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if isModelCapacityError(err) {
		return false
	}
	if isPlaygroundImageQuotaExhaustedError(c, err) {
		return false
	}
	if isPlaygroundRelayGatewayTimeout(c, err.StatusCode) {
		return false
	}
	return service.ShouldDisableChannel(err)
}

func isModelCapacityError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "selected model is at capacity. please try a different model.")
}

func isPlaygroundImageQuotaExhaustedError(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil || err.StatusCode != http.StatusTooManyRequests || c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	if !relayconstant.IsPlaygroundImagePath(c.Request.URL.Path) {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no available image quota")
}

func isPlaygroundRelayGatewayTimeout(c *gin.Context, code int) bool {
	if code != http.StatusGatewayTimeout && code != 524 {
		return false
	}
	return isPlaygroundRelayRequest(c)
}

func isPlaygroundRelayTransportFailure(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil || err.GetErrorCode() != types.ErrorCodeDoRequestFailed {
		return false
	}
	return isPlaygroundRelayRequest(c)
}

func isPlaygroundRelayRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	return relayconstant.IsPlaygroundRelayPath(c.Request.URL.Path)
}

func isSinglePlaygroundRelayCandidateChannel(c *gin.Context) bool {
	usedChannels := c.GetStringSlice("use_channel")
	if len(usedChannels) != 1 {
		return false
	}
	return common.GetContextKeyInt(c, constant.ContextKeyPlaygroundRelayCandidateChannelCount) == 1
}

func handlePlaygroundRelayChannelFailure(c *gin.Context, channelID int, err *types.NewAPIError, willRetry bool) {
	if channelID <= 0 || err == nil {
		return
	}
	if !isPlaygroundImageQuotaExhaustedError(c, err) &&
		!isPlaygroundRelayTransportFailure(c, err) &&
		!isPlaygroundRelayGatewayTimeout(c, err.StatusCode) {
		return
	}

	service.ClearCurrentChannelAffinityCache(c)
	if !willRetry {
		return
	}

	excludedChannelIDs, _ := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyPlaygroundRelayExcludedChannelIds)
	if excludedChannelIDs == nil {
		excludedChannelIDs = make(map[int]struct{})
	}
	excludedChannelIDs[channelID] = struct{}{}
	common.SetContextKey(c, constant.ContextKeyPlaygroundRelayExcludedChannelIds, excludedChannelIDs)
}

func handleRelayServerErrorChannelFailure(c *gin.Context, channelID int, err *types.NewAPIError, willRetry bool) {
	if channelID <= 0 || !willRetry || !isRelayServerErrorRetryTarget(err) {
		return
	}

	service.ClearCurrentChannelAffinityCache(c)
	excludedChannelIDs, _ := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyRelayServerErrorExcludedChannelIds)
	if excludedChannelIDs == nil {
		excludedChannelIDs = make(map[int]struct{})
	}
	excludedChannelIDs[channelID] = struct{}{}
	common.SetContextKey(c, constant.ContextKeyRelayServerErrorExcludedChannelIds, excludedChannelIDs)
}

func shouldStopPlaygroundRelayGatewayTimeoutRetry(c *gin.Context, channelID int, lastErr *types.NewAPIError, retry int) bool {
	if retry <= 0 || lastErr == nil || !isPlaygroundRelayGatewayTimeout(c, lastErr.StatusCode) {
		return false
	}
	usedCount := 0
	for _, usedChannel := range c.GetStringSlice("use_channel") {
		if usedChannel == strconv.Itoa(channelID) {
			usedCount++
		}
	}
	return usedCount > playgroundRelayGatewayTimeoutSameChannelRetryLimit
}

func buildChannelErrorLogOther(c *gin.Context, err *types.NewAPIError, autoDisableTriggered bool) map[string]interface{} {
	other := make(map[string]interface{})
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	other["error_type"] = err.GetErrorType()
	other["error_code"] = err.GetErrorCode()
	other["status_code"] = err.StatusCode
	other["channel_id"] = c.GetInt("channel_id")
	other["channel_name"] = c.GetString("channel_name")
	other["channel_type"] = c.GetInt("channel_type")
	if taskID := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyPlaygroundImageTaskID)); taskID != "" {
		other["task_id"] = taskID
	}
	if metadata, ok := common.GetContextKeyType[relaycommon.ImageFailureMetadata](c, constant.ContextKeyImageFailureMetadata); ok {
		other["stage"] = metadata.Stage
		other["upstream_status_code"] = metadata.StatusCode
		other["content_type"] = metadata.ContentType
		other["image_count"] = metadata.ImageCount
		other["has_url"] = metadata.HasURL
		other["has_b64_json"] = metadata.HasB64JSON
		if metadata.ErrorSummary != "" {
			other["error_summary"] = metadata.ErrorSummary
		}
	}

	adminInfo := make(map[string]interface{})
	adminInfo["use_channel"] = c.GetStringSlice("use_channel")
	if autoDisableTriggered {
		adminInfo["channel_auto_disable_triggered"] = true
	}
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	service.AppendChannelAffinityAdminInfo(c, adminInfo)
	other["admin_info"] = adminInfo
	return other
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	autoDisableTriggered := shouldAutoDisableChannel(c, err) && channelError.AutoBan
	if autoDisableTriggered {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := buildChannelErrorLogOther(c, err, autoDisableTriggered)
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
		common.SetContextKey(c, constant.ContextKeyChannelErrorLogRecorded, true)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		chargedQuota, settleErr := service.SettleBillingQuota(c, relayInfo, result.Quota)
		if settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		relayInfo.PriceData.Quota = chargedQuota
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.TokenQuotaCharges = service.SnapshotTokenQuotaCharges(relayInfo)
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = chargedQuota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
