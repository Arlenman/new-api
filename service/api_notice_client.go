package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	apiNoticeDefaultBaseURL = "http://127.0.0.1:18080"
	apiNoticeNoticesPath    = "/v1/notices"
	apiNoticeProvidersPath  = "/v1/providers"
	apiNoticeHealthPath     = "/healthz"
	apiNoticeReadyPath      = "/readyz"

	apiNoticeAuthorizationHeader = "Authorization"
	apiNoticeSendTimeout         = 35 * time.Second
	apiNoticeProbeTimeout        = 3 * time.Second
)

var ErrApiNoticeAPIKeyNotConfigured = errors.New("API_NOTICE_API_KEY is not configured")

type ApiNoticeHTTPError struct {
	StatusCode int
	Retryable  bool
	RetryAfter time.Duration
	Summary    string
}

func (e *ApiNoticeHTTPError) Error() string {
	if e == nil {
		return "api-notice request failed"
	}
	return fmt.Sprintf("api-notice request failed with HTTP %d: %s", e.StatusCode, e.Summary)
}

type ApiNoticeClient struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	probeClient *http.Client
	now         func() time.Time
}

func NewApiNoticeClientFromEnv() (*ApiNoticeClient, error) {
	runtime, err := loadApiNoticeRuntimeConfig()
	if err != nil {
		return nil, err
	}
	return &ApiNoticeClient{
		baseURL: runtime.BaseURL,
		apiKey:  runtime.APIKey,
		httpClient: &http.Client{
			Timeout: apiNoticeSendTimeout,
		},
		probeClient: &http.Client{
			Timeout: apiNoticeProbeTimeout,
		},
		now: time.Now,
	}, nil
}

func ApiNoticeAPIKeyConfigured() bool {
	runtime, err := loadApiNoticeRuntimeConfig()
	return err == nil && runtime.APIKey != ""
}

func ApiNoticeConfiguredBaseURL() (string, error) {
	runtime, err := loadApiNoticeRuntimeConfig()
	if err != nil {
		return "", err
	}
	return runtime.BaseURL, nil
}

func loadApiNoticeAPIKeyFromEnvironmentOrFile() (string, string, error) {
	if value := os.Getenv("API_NOTICE_API_KEY"); strings.TrimSpace(value) != "" {
		return value, "environment", nil
	}
	// Legacy environment names remain read-only compatibility inputs. Their
	// values are used as Bearer API keys and are never used for HMAC signing.
	if value := os.Getenv("API_NOTICE_HMAC_SECRET"); strings.TrimSpace(value) != "" {
		return value, "environment", nil
	}

	fileCandidates := make([]string, 0, 4)
	if value := strings.TrimSpace(os.Getenv("API_NOTICE_API_KEY_FILE")); value != "" {
		fileCandidates = append(fileCandidates, value)
	}
	if value := strings.TrimSpace(os.Getenv("API_NOTICE_HMAC_SECRET_FILE")); value != "" {
		fileCandidates = append(fileCandidates, value)
	}
	if len(fileCandidates) == 0 {
		home, err := os.UserHomeDir()
		if err == nil {
			fileCandidates = append(fileCandidates,
				filepath.Join(home, ".config", "api-notice", "api-key"),
				filepath.Join(home, ".config", "api-notice", "hmac-secret"),
			)
		}
	}

	for _, candidate := range fileCandidates {
		path, err := expandApiNoticeConfigPath(candidate)
		if err != nil {
			return "", "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", "", errors.New("read api-notice API key file failed")
		}
		apiKey := strings.TrimRight(string(data), "\r\n")
		if strings.TrimSpace(apiKey) != "" {
			return apiKey, "file", nil
		}
	}
	return "", "", ErrApiNoticeAPIKeyNotConfigured
}

func expandApiNoticeConfigPath(value string) (string, error) {
	value = os.ExpandEnv(strings.TrimSpace(value))
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("resolve api-notice API key file failed")
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	if value == "" {
		return "", errors.New("api-notice API key file path is empty")
	}
	return value, nil
}

func normalizeApiNoticeBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = apiNoticeDefaultBaseURL
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("API_NOTICE_BASE_URL must be a valid HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("API_NOTICE_BASE_URL must not contain credentials, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (client *ApiNoticeClient) SendNotice(ctx context.Context, request dto.ApiNoticeRequest) (dto.ApiNoticeDeliveryResult, error) {
	body, err := common.Marshal(request)
	if err != nil {
		return dto.ApiNoticeDeliveryResult{}, fmt.Errorf("encode api-notice request: %w", err)
	}
	return client.SendNoticeRaw(ctx, body)
}

func (client *ApiNoticeClient) SendNoticeRaw(ctx context.Context, body []byte) (dto.ApiNoticeDeliveryResult, error) {
	if client == nil || client.apiKey == "" {
		return dto.ApiNoticeDeliveryResult{}, ErrApiNoticeAPIKeyNotConfigured
	}
	if client.httpClient == nil {
		client.httpClient = &http.Client{Timeout: apiNoticeSendTimeout}
	}
	if client.now == nil {
		client.now = time.Now
	}

	var noticeRequest dto.ApiNoticeRequest
	if err := common.Unmarshal(body, &noticeRequest); err != nil {
		return dto.ApiNoticeDeliveryResult{}, errors.New("api-notice request body is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+apiNoticeNoticesPath, bytes.NewReader(body))
	if err != nil {
		return dto.ApiNoticeDeliveryResult{}, fmt.Errorf("build api-notice request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiNoticeAuthorizationHeader, "Bearer "+client.apiKey)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return dto.ApiNoticeDeliveryResult{}, fmt.Errorf("call api-notice: %w", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return dto.ApiNoticeDeliveryResult{HTTPStatus: resp.StatusCode}, fmt.Errorf("read api-notice response: %w", readErr)
	}
	result := dto.ApiNoticeDeliveryResult{HTTPStatus: resp.StatusCode}
	if len(bytes.TrimSpace(responseBody)) > 0 {
		if decodeErr := common.DecodeJson(bytes.NewReader(responseBody), &result.Response); decodeErr != nil {
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				return result, errors.New("api-notice returned an invalid response")
			}
		}
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		if summary := validateApiNoticeDelivery(noticeRequest, result.Response, client.apiKey); summary == "" {
			return result, nil
		} else {
			return result, &ApiNoticeHTTPError{StatusCode: resp.StatusCode, Summary: summary}
		}
	}

	summary := sanitizeApiNoticeError(redactApiNoticeCredential(result.Response.Error, client.apiKey))
	if summary == "" {
		summary = strings.ToLower(strings.ReplaceAll(http.StatusText(resp.StatusCode), " ", "_"))
	}
	retryAfter := time.Duration(0)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter = parseApiNoticeRetryAfter(resp.Header.Get("Retry-After"), client.now())
	}
	return result, &ApiNoticeHTTPError{
		StatusCode: resp.StatusCode,
		Retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout,
		RetryAfter: retryAfter,
		Summary:    summary,
	}
}

func validateApiNoticeDelivery(request dto.ApiNoticeRequest, response dto.ApiNoticeResponse, apiKey string) string {
	if !response.Success {
		if summary := sanitizeApiNoticeError(redactApiNoticeCredential(response.Error, apiKey)); summary != "" {
			return summary
		}
		return "unsuccessful_response"
	}
	if len(response.Result.Results) == 0 {
		return "missing_provider_results"
	}

	accepted := make(map[string]bool, len(response.Result.Results))
	for _, providerResult := range response.Result.Results {
		provider := strings.TrimSpace(providerResult.Provider)
		if provider == "" {
			provider = strings.TrimSpace(providerResult.Receipt.Provider)
		}
		if provider == "" || !providerResult.Receipt.Accepted {
			return "provider_not_accepted"
		}
		accepted[provider] = true
	}
	for _, provider := range request.Providers {
		if !accepted[provider] {
			return "missing_provider_result"
		}
	}
	return ""
}

func (client *ApiNoticeClient) GetProviders(ctx context.Context) ([]dto.ApiNoticeProvider, error) {
	var response struct {
		Providers []dto.ApiNoticeProvider `json:"providers"`
	}
	if err := client.getJSON(ctx, apiNoticeProvidersPath, &response); err != nil {
		return nil, err
	}
	return response.Providers, nil
}

func (client *ApiNoticeClient) GetStatus(ctx context.Context, path string) (string, int, error) {
	var response struct {
		Status string `json:"status"`
	}
	statusCode, err := client.getJSONWithStatus(ctx, path, &response)
	return response.Status, statusCode, err
}

func (client *ApiNoticeClient) getJSON(ctx context.Context, path string, target any) error {
	_, err := client.getJSONWithStatus(ctx, path, target)
	return err
}

func (client *ApiNoticeClient) getJSONWithStatus(ctx context.Context, path string, target any) (int, error) {
	if client == nil {
		return 0, errors.New("api-notice client is required")
	}
	if client.probeClient == nil {
		client.probeClient = &http.Client{Timeout: apiNoticeProbeTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.probeClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call api-notice: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return resp.StatusCode, errors.New("read api-notice response failed")
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err = common.DecodeJson(bytes.NewReader(body), target); err != nil {
			return resp.StatusCode, errors.New("api-notice returned an invalid response")
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, &ApiNoticeHTTPError{StatusCode: resp.StatusCode, Summary: strings.ToLower(strings.ReplaceAll(http.StatusText(resp.StatusCode), " ", "_"))}
	}
	return resp.StatusCode, nil
}

func parseApiNoticeRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func redactApiNoticeCredential(value, apiKey string) string {
	if apiKey == "" {
		return value
	}
	return strings.ReplaceAll(value, apiKey, "redacted")
}

func sanitizeApiNoticeError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
		if builder.Len() >= 200 {
			break
		}
	}
	return builder.String()
}
