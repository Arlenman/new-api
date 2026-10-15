package relay

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func shouldPassThroughRequestBody(info *relaycommon.RelayInfo, passThrough bool) bool {
	if !passThrough || info == nil {
		return passThrough
	}

	upstreamFormat := info.GetUpstreamRelayFormat()
	if info.RelayFormat == types.RelayFormatOpenAIResponses && upstreamFormat == types.RelayFormatOpenAI {
		return false
	}
	if info.RelayFormat == types.RelayFormatOpenAI && upstreamFormat == types.RelayFormatOpenAIResponses {
		return false
	}
	return true
}

func ConvertOpenAIResponsesRequestForUpstream(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, request dto.OpenAIResponsesRequest) (any, error) {
	if info.GetUpstreamRelayFormat() != types.RelayFormatOpenAI {
		return adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	}

	result, err := service.ConvertRequestVia(c, info, &request, types.RelayFormatOpenAI)
	if err != nil {
		return nil, err
	}
	chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat request, got %T", result.Value)
	}
	info.FinalRequestRelayFormat = types.RelayFormatOpenAI
	return adaptor.ConvertOpenAIRequest(c, info, chatRequest)
}

func ConvertOpenAIRequestForUpstream(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, request *dto.GeneralOpenAIRequest) (any, error) {
	if info.GetUpstreamRelayFormat() != types.RelayFormatOpenAIResponses {
		return adaptor.ConvertOpenAIRequest(c, info, request)
	}

	result, err := service.ConvertRequestVia(c, info, request, types.RelayFormatOpenAIResponses)
	if err != nil {
		return nil, err
	}
	responsesRequest, ok := result.Value.(*dto.OpenAIResponsesRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI responses request, got %T", result.Value)
	}
	info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses
	return adaptor.ConvertOpenAIResponsesRequest(c, info, *responsesRequest)
}

func DoOpenAIResponseForMappedEndpoint(c *gin.Context, adaptor channel.Adaptor, resp *http.Response, info *relaycommon.RelayInfo, clientStream bool) (any, *types.NewAPIError) {
	upstreamFormat := info.GetUpstreamRelayFormat()
	if info.RelayFormat == types.RelayFormatOpenAIResponses && upstreamFormat == types.RelayFormatOpenAI {
		info.IsStream = clientStream || (resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream"))
		if clientStream {
			return openai.OaiChatToResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiChatToResponsesHandler(c, info, resp)
	}
	if info.RelayFormat == types.RelayFormatOpenAI && upstreamFormat == types.RelayFormatOpenAIResponses {
		upstreamStream := resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
		info.IsStream = clientStream || upstreamStream
		if upstreamStream && !clientStream {
			info.IsStream = false
			return openai.OaiResponsesToChatBufferedStreamHandler(c, info, resp)
		}
		if upstreamStream {
			return openai.OaiResponsesToChatStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesToChatHandler(c, info, resp)
	}
	return adaptor.DoResponse(c, resp, info)
}
