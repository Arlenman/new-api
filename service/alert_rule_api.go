package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

func TestApiNoticeConnection(ctx context.Context) (dto.ApiNoticeConnectionStatus, error) {
	status := dto.ApiNoticeConnectionStatus{APIKeyConfigured: ApiNoticeAPIKeyConfigured()}
	client, err := NewApiNoticeClientFromEnv()
	if err != nil {
		return status, errors.New("api-notice configuration is invalid")
	}

	status.Health.Status, status.Health.HTTPStatus, err = client.GetStatus(ctx, apiNoticeHealthPath)
	if err != nil {
		return status, errors.New("api-notice health check failed")
	}

	var readyErr error
	status.Ready.Status, status.Ready.HTTPStatus, readyErr = client.GetStatus(ctx, apiNoticeReadyPath)
	status.Providers, err = client.GetProviders(ctx)
	if err != nil {
		return status, errors.New("api-notice provider catalog is unavailable")
	}
	if readyErr != nil {
		return status, errors.New("api-notice is not ready")
	}
	return status, nil
}

func SendAlertRuleTestNotice(ctx context.Context, requestedProviders []string, message dto.ApiNoticeMessage) (dto.ApiNoticeTestSendResult, error) {
	if len(requestedProviders) == 0 {
		return dto.ApiNoticeTestSendResult{}, errors.New("at least one test provider is required")
	}
	if err := validateRenderedApiNoticeMessage(message); err != nil {
		return dto.ApiNoticeTestSendResult{}, err
	}
	client, err := NewApiNoticeClientFromEnv()
	if err != nil {
		return dto.ApiNoticeTestSendResult{}, errors.New("api-notice configuration is invalid")
	}
	catalog, err := client.GetProviders(ctx)
	if err != nil {
		return dto.ApiNoticeTestSendResult{}, errors.New("api-notice provider catalog is unavailable")
	}
	catalogByName := make(map[string]dto.ApiNoticeProvider, len(catalog))
	for _, provider := range catalog {
		catalogByName[provider.Name] = provider
	}

	providers := make([]string, 0, len(requestedProviders))
	seen := make(map[string]struct{}, len(requestedProviders))
	for _, providerName := range requestedProviders {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return dto.ApiNoticeTestSendResult{}, errors.New("test provider name is required")
		}
		if _, exists := seen[providerName]; exists {
			continue
		}
		provider, exists := catalogByName[providerName]
		if !exists {
			return dto.ApiNoticeTestSendResult{}, fmt.Errorf("unknown api-notice provider %q", providerName)
		}
		if !provider.Ready {
			return dto.ApiNoticeTestSendResult{}, fmt.Errorf("api-notice provider %q is not ready", providerName)
		}
		if !containsString(provider.Capabilities, message.Format) {
			return dto.ApiNoticeTestSendResult{}, fmt.Errorf("api-notice provider %q does not support %s", providerName, message.Format)
		}
		seen[providerName] = struct{}{}
		providers = append(providers, providerName)
	}

	identity := make([]byte, 12)
	if _, err = rand.Read(identity); err != nil {
		return dto.ApiNoticeTestSendResult{}, errors.New("generate test request identity failed")
	}
	providerSlug := sanitizeApiNoticeError(strings.Join(providers, "-"))
	request := dto.ApiNoticeRequest{
		IdempotencyKey: fmt.Sprintf("new-api-%s-%d-%s", providerSlug, time.Now().Unix(), hex.EncodeToString(identity)),
		Providers:      providers,
		Message:        message,
	}
	delivery, deliveryErr := client.SendNotice(ctx, request)
	result := safeApiNoticeTestSendResult(delivery, deliveryErr)
	if deliveryErr != nil {
		return result, errors.New(result.Error)
	}
	return result, nil
}

func safeApiNoticeTestSendResult(delivery dto.ApiNoticeDeliveryResult, deliveryErr error) dto.ApiNoticeTestSendResult {
	result := dto.ApiNoticeTestSendResult{
		HTTPStatus: delivery.HTTPStatus,
		Results:    make([]dto.ApiNoticeTestProviderResult, 0, len(delivery.Response.Result.Results)),
	}
	for _, providerResult := range delivery.Response.Result.Results {
		providerName := providerResult.Provider
		if providerName == "" {
			providerName = providerResult.Receipt.Provider
		}
		result.Results = append(result.Results, dto.ApiNoticeTestProviderResult{
			Provider: providerName,
			Accepted: providerResult.Receipt.Accepted,
			Attempts: providerResult.Receipt.Attempts,
			Error:    sanitizeApiNoticeError(providerResult.Error),
		})
	}
	if deliveryErr != nil {
		result.Error = publicApiNoticeErrorSummary(deliveryErr)
	}
	return result
}

func publicApiNoticeErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrApiNoticeAPIKeyNotConfigured) {
		return "api_key_not_configured"
	}
	var httpErr *ApiNoticeHTTPError
	if errors.As(err, &httpErr) {
		if summary := sanitizeApiNoticeError(httpErr.Summary); summary != "" {
			return summary
		}
		return fmt.Sprintf("http_%d", httpErr.StatusCode)
	}
	return "api_notice_connection_failed"
}
