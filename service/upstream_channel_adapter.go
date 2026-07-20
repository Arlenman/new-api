package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	upstreamResponseLimit                         = 8 << 20
	upstreamKeyPageSize                           = 100
	upstreamKeyMaxPages                           = 10000
	UpstreamErrorCodeTurnstileRequiresAccessToken = "upstream_turnstile_requires_access_token"
)

var (
	ErrNewAPITurnstileRequiresAccessToken = errors.New(
		"new-api has Turnstile enabled; use the numeric upstream user ID and a management access token",
	)
	ErrSub2APITurnstileRequiresAccessToken = errors.New(
		"Sub2API has Turnstile enabled; use a browser-issued access token instead of background account login",
	)
)

func UpstreamErrorCode(err error) string {
	if errors.Is(err, ErrNewAPITurnstileRequiresAccessToken) || errors.Is(err, ErrSub2APITurnstileRequiresAccessToken) {
		return UpstreamErrorCodeTurnstileRequiresAccessToken
	}
	return ""
}

func UpstreamErrorCodeFromMessage(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == strings.ToLower(ErrNewAPITurnstileRequiresAccessToken.Error()) ||
		normalized == strings.ToLower(ErrSub2APITurnstileRequiresAccessToken.Error()) ||
		(strings.Contains(normalized, "turnstile") && strings.Contains(normalized, "access token")) {
		return UpstreamErrorCodeTurnstileRequiresAccessToken
	}
	return ""
}

type UpstreamCredential struct {
	AuthType string
	Username string
	Password string
}

type UpstreamAccount struct {
	ID       int64   `json:"id"`
	Username string  `json:"username"`
	Email    string  `json:"email,omitempty"`
	Role     string  `json:"role,omitempty"`
	Group    string  `json:"group,omitempty"`
	Balance  float64 `json:"balance"`
}

type UpstreamKey struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	MaskedKey      string  `json:"masked_key"`
	Group          string  `json:"group,omitempty"`
	GroupID        *int64  `json:"group_id,omitempty"`
	Status         string  `json:"status"`
	Quota          float64 `json:"quota,omitempty"`
	QuotaUsed      float64 `json:"quota_used,omitempty"`
	RemainQuota    float64 `json:"remain_quota,omitempty"`
	Imported       bool    `json:"imported"`
	Active         bool    `json:"active"`
	KeyFingerprint string  `json:"key_fingerprint,omitempty"`
}

type UpstreamGroup struct {
	ID          int64   `json:"id,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Platform    string  `json:"platform,omitempty"`
	Ratio       float64 `json:"ratio"`
}

type UpstreamModelPricingInterval struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price,omitempty"`
	OutputPrice     *float64 `json:"output_price,omitempty"`
	CacheWritePrice *float64 `json:"cache_write_price,omitempty"`
	CacheReadPrice  *float64 `json:"cache_read_price,omitempty"`
	PerRequestPrice *float64 `json:"per_request_price,omitempty"`
}

type UpstreamModelPricing struct {
	Source           string                         `json:"source"`
	ChannelName      string                         `json:"channel_name,omitempty"`
	Platform         string                         `json:"platform,omitempty"`
	BillingMode      string                         `json:"billing_mode,omitempty"`
	ModelRatio       *float64                       `json:"model_ratio,omitempty"`
	CompletionRatio  *float64                       `json:"completion_ratio,omitempty"`
	CacheRatio       *float64                       `json:"cache_ratio,omitempty"`
	CreateCacheRatio *float64                       `json:"create_cache_ratio,omitempty"`
	ModelPrice       *float64                       `json:"model_price,omitempty"`
	InputPrice       *float64                       `json:"input_price,omitempty"`
	OutputPrice      *float64                       `json:"output_price,omitempty"`
	CacheWritePrice  *float64                       `json:"cache_write_price,omitempty"`
	CacheReadPrice   *float64                       `json:"cache_read_price,omitempty"`
	ImageInputPrice  *float64                       `json:"image_input_price,omitempty"`
	ImageOutputPrice *float64                       `json:"image_output_price,omitempty"`
	PerRequestPrice  *float64                       `json:"per_request_price,omitempty"`
	Intervals        []UpstreamModelPricingInterval `json:"intervals,omitempty"`
}

type UpstreamModel struct {
	ID      string                 `json:"id"`
	Pricing []UpstreamModelPricing `json:"pricing"`
}

type UpstreamSnapshot struct {
	Provider    string             `json:"provider"`
	Balance     float64            `json:"balance"`
	Account     UpstreamAccount    `json:"account"`
	Keys        []UpstreamKey      `json:"keys"`
	Groups      []UpstreamGroup    `json:"groups"`
	Ratios      map[string]float64 `json:"ratios"`
	Models      []UpstreamModel    `json:"models"`
	RetrievedAt int64              `json:"retrieved_at"`
}

func applyUpstreamGroupNames(keys []UpstreamKey, groups []UpstreamGroup) {
	groupNames := make(map[int64]string, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if group.ID != 0 && name != "" {
			groupNames[group.ID] = name
		}
	}
	for i := range keys {
		if keys[i].GroupID == nil {
			continue
		}
		if name, ok := groupNames[*keys[i].GroupID]; ok {
			keys[i].Group = name
		}
	}
}

type newAPIEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type sub2APIEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

func supportsUpstreamAccessToken(provider string) bool {
	return provider == UpstreamProviderNewAPI || provider == UpstreamProviderSub2API
}

func FetchUpstreamSnapshot(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	normalized, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" || provider == UpstreamProviderAuto {
		provider = DetectUpstreamProvider(ctx, client, normalized)
	}
	authType := model.NormalizeUpstreamAuthType(credential.AuthType)
	if authType != model.UpstreamAuthTypePassword && authType != model.UpstreamAuthTypeAccessToken {
		return UpstreamSnapshot{}, fmt.Errorf("unsupported upstream authentication method %q", authType)
	}
	if authType == model.UpstreamAuthTypeAccessToken && !supportsUpstreamAccessToken(provider) {
		return UpstreamSnapshot{}, errors.New("access token authentication is only supported for new-api and sub2api upstream channels")
	}
	switch provider {
	case UpstreamProviderNewAPI:
		return fetchNewAPIUpstreamSnapshot(ctx, client, normalized, credential)
	case UpstreamProviderSub2API:
		return fetchSub2APIUpstreamSnapshot(ctx, client, normalized, credential)
	default:
		return UpstreamSnapshot{}, fmt.Errorf("unsupported upstream provider %q", provider)
	}
}

func DetectUpstreamProvider(ctx context.Context, client *http.Client, baseURL string) string {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL(baseURL, "/api/v1/settings/public"), nil)
	if err == nil {
		resp, requestErr := client.Do(req)
		if requestErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var envelope struct {
					Code *int            `json:"code"`
					Data json.RawMessage `json:"data"`
				}
				if common.DecodeJson(io.LimitReader(resp.Body, upstreamResponseLimit), &envelope) == nil &&
					envelope.Code != nil && *envelope.Code == 0 && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
					return UpstreamProviderSub2API
				}
			}
		}
	}
	return UpstreamProviderNewAPI
}

// FetchUpstreamBalance authenticates with an upstream and retrieves only its account balance.
func FetchUpstreamBalance(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	normalized, resolvedProvider, err := prepareUpstreamProvider(ctx, client, baseURL, provider, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	if resolvedProvider == UpstreamProviderNewAPI {
		sessionClient, headers, err := authenticateNewAPI(ctx, client, normalized, credential)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		quotaPerUnit, err := fetchNewAPIQuotaPerUnit(ctx, sessionClient, normalized)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		account, err := fetchNewAPIAccount(ctx, sessionClient, normalized, headers, quotaPerUnit)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		return UpstreamSnapshot{Provider: resolvedProvider, Balance: account.Balance, Account: account, RetrievedAt: time.Now().Unix()}, nil
	}

	sessionClient, headers, err := authenticateSub2API(ctx, client, normalized, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	account, err := fetchSub2APIAccount(ctx, sessionClient, normalized, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	return UpstreamSnapshot{Provider: resolvedProvider, Balance: account.Balance, Account: account, RetrievedAt: time.Now().Unix()}, nil
}

// FetchUpstreamKeys authenticates with an upstream and retrieves only its key list.
func FetchUpstreamKeys(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	normalized, resolvedProvider, err := prepareUpstreamProvider(ctx, client, baseURL, provider, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	if resolvedProvider == UpstreamProviderNewAPI {
		sessionClient, headers, err := authenticateNewAPI(ctx, client, normalized, credential)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		quotaPerUnit, err := fetchNewAPIQuotaPerUnit(ctx, sessionClient, normalized)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		keys, err := fetchNewAPIKeys(ctx, sessionClient, normalized, headers, quotaPerUnit)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		for i := range keys {
			if keys[i].KeyFingerprint != "" {
				continue
			}
			fullKey, fetchErr := fetchNewAPIFullKeyWithSession(ctx, sessionClient, normalized, headers, keys[i].ID)
			if fetchErr != nil {
				return UpstreamSnapshot{}, fmt.Errorf("fetch new-api key %d for import status: %w", keys[i].ID, fetchErr)
			}
			keys[i].KeyFingerprint = upstreamKeyFingerprint(fullKey)
		}
		return UpstreamSnapshot{Provider: resolvedProvider, Keys: keys, RetrievedAt: time.Now().Unix()}, nil
	}

	sessionClient, headers, err := authenticateSub2API(ctx, client, normalized, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	keys, err := fetchSub2APIKeys(ctx, sessionClient, normalized, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range keys {
		if keys[i].KeyFingerprint != "" {
			continue
		}
		fullKey, fetchErr := fetchSub2APIFullKeyWithToken(ctx, sessionClient, normalized, headers, keys[i].ID)
		if fetchErr != nil {
			return UpstreamSnapshot{}, fmt.Errorf("fetch sub2api key %d for import status: %w", keys[i].ID, fetchErr)
		}
		keys[i].KeyFingerprint = upstreamKeyFingerprint(fullKey)
	}
	return UpstreamSnapshot{Provider: resolvedProvider, Keys: keys, RetrievedAt: time.Now().Unix()}, nil
}

// FetchUpstreamGroups authenticates with an upstream and retrieves only its groups and ratios.
func FetchUpstreamGroups(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	normalized, resolvedProvider, err := prepareUpstreamProvider(ctx, client, baseURL, provider, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	if resolvedProvider == UpstreamProviderNewAPI {
		sessionClient, headers, err := authenticateNewAPI(ctx, client, normalized, credential)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		groups, ratios, err := fetchNewAPIGroups(ctx, sessionClient, normalized, headers)
		if err != nil {
			return UpstreamSnapshot{}, err
		}
		return UpstreamSnapshot{Provider: resolvedProvider, Groups: groups, Ratios: ratios, RetrievedAt: time.Now().Unix()}, nil
	}

	sessionClient, headers, err := authenticateSub2API(ctx, client, normalized, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	groups, err := fetchSub2APIGroups(ctx, sessionClient, normalized, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	ratios, err := fetchSub2APIRatios(ctx, sessionClient, normalized, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range groups {
		if ratio, ok := ratios[strconv.FormatInt(groups[i].ID, 10)]; ok {
			groups[i].Ratio = ratio
		}
	}
	return UpstreamSnapshot{Provider: resolvedProvider, Groups: groups, Ratios: ratios, RetrievedAt: time.Now().Unix()}, nil
}

// upstreamModelDiscoveryResult keeps pricing discovery failures separate from model discovery failures.
// A pricing endpoint can be unavailable while the upstream model list is still usable.
type upstreamModelDiscoveryResult struct {
	Models       []UpstreamModel
	PricingError error
}

// FetchUpstreamModels retrieves the union exposed by every eligible upstream key.
// Pricing discovery is best-effort: a provider without a pricing endpoint still returns its model list.
func FetchUpstreamModels(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential, keys []UpstreamKey, selectedGroup string) ([]UpstreamModel, error) {
	result, err := fetchUpstreamModelsWithPricingStatus(ctx, client, baseURL, provider, credential, keys, selectedGroup)
	if err != nil {
		return nil, err
	}
	return result.Models, nil
}

func fetchUpstreamModelsWithPricingStatus(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential, keys []UpstreamKey, selectedGroup string) (upstreamModelDiscoveryResult, error) {
	normalized, resolvedProvider, err := prepareUpstreamProvider(ctx, client, baseURL, provider, credential)
	if err != nil {
		return upstreamModelDiscoveryResult{}, err
	}
	probeKeys := selectUpstreamModelProbeKeys(keys, selectedGroup)
	if len(probeKeys) == 0 {
		return upstreamModelDiscoveryResult{}, errors.New("no upstream keys are available for model discovery")
	}

	var sessionClient *http.Client
	var headers map[string]string
	if resolvedProvider == UpstreamProviderNewAPI {
		sessionClient, headers, err = authenticateNewAPI(ctx, client, normalized, credential)
	} else {
		sessionClient, headers, err = authenticateSub2API(ctx, client, normalized, credential)
	}
	if err != nil {
		return upstreamModelDiscoveryResult{}, err
	}

	modelSet := make(map[string]struct{})
	probeErrors := make([]string, 0)
	for _, key := range probeKeys {
		var fullKey string
		if resolvedProvider == UpstreamProviderNewAPI {
			fullKey, err = fetchNewAPIFullKeyWithSession(ctx, sessionClient, normalized, headers, key.ID)
		} else {
			fullKey, err = fetchSub2APIFullKeyWithToken(ctx, sessionClient, normalized, headers, key.ID)
		}
		if err != nil {
			probeErrors = append(probeErrors, fmt.Sprintf("key %d: fetch key: %v", key.ID, err))
			continue
		}
		keyModels, fetchErr := fetchUpstreamKeyModelsAllowEmpty(ctx, sessionClient, normalized, fullKey)
		if fetchErr != nil {
			probeErrors = append(probeErrors, fmt.Sprintf("key %d: fetch models: %v", key.ID, fetchErr))
			continue
		}
		for _, modelID := range keyModels {
			modelSet[modelID] = struct{}{}
		}
	}

	modelIDs := make([]string, 0, len(modelSet))
	for modelID := range modelSet {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	models := make([]UpstreamModel, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, UpstreamModel{ID: modelID, Pricing: []UpstreamModelPricing{}})
	}
	if len(models) == 0 {
		if len(probeErrors) > 0 {
			return upstreamModelDiscoveryResult{}, fmt.Errorf("upstream model discovery failed: %s", strings.Join(probeErrors, "; "))
		}
		return upstreamModelDiscoveryResult{Models: models}, nil
	}

	var pricingByModel map[string][]UpstreamModelPricing
	if resolvedProvider == UpstreamProviderNewAPI {
		pricingByModel, err = fetchNewAPIModelPricing(ctx, sessionClient, normalized, headers, modelSet)
	} else {
		pricingByModel, err = fetchSub2APIModelPricing(ctx, sessionClient, normalized, headers, modelSet)
	}
	result := upstreamModelDiscoveryResult{Models: models, PricingError: err}
	if err != nil {
		return result, nil
	}
	for i := range result.Models {
		if pricing, ok := pricingByModel[result.Models[i].ID]; ok {
			result.Models[i].Pricing = pricing
		}
	}
	return result, nil
}

func selectUpstreamModelProbeKeys(keys []UpstreamKey, selectedGroup string) []UpstreamKey {
	selectedGroup = strings.TrimSpace(selectedGroup)
	candidates := make([]UpstreamKey, 0, len(keys))
	seenIDs := make(map[int64]struct{}, len(keys))
	for _, key := range keys {
		if key.ID <= 0 || !isUpstreamModelProbeEligible(key) {
			continue
		}
		if _, exists := seenIDs[key.ID]; exists {
			continue
		}
		seenIDs[key.ID] = struct{}{}
		candidates = append(candidates, key)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		leftSelected := selectedGroup != "" && strings.TrimSpace(candidates[i].Group) == selectedGroup
		rightSelected := selectedGroup != "" && strings.TrimSpace(candidates[j].Group) == selectedGroup
		if leftSelected != rightSelected {
			return leftSelected
		}
		leftStatus := upstreamModelProbeStatusPriority(candidates[i].Status)
		rightStatus := upstreamModelProbeStatusPriority(candidates[j].Status)
		if leftStatus != rightStatus {
			return leftStatus < rightStatus
		}
		leftGroup := upstreamModelProbeGroup(candidates[i])
		rightGroup := upstreamModelProbeGroup(candidates[j])
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

func isUpstreamModelProbeEligible(key UpstreamKey) bool {
	switch strings.ToLower(strings.TrimSpace(key.Status)) {
	case "0", "disabled", "inactive", "revoked", "banned", "expired", "deactivated":
		return false
	default:
		return true
	}
}

func IsUpstreamKeyEligible(key UpstreamKey) bool {
	return isUpstreamModelProbeEligible(key)
}

func upstreamModelProbeGroup(key UpstreamKey) string {
	if key.GroupID != nil {
		return "id:" + strconv.FormatInt(*key.GroupID, 10)
	}
	if group := strings.TrimSpace(key.Group); group != "" {
		return "name:" + group
	}
	return "ungrouped"
}

func upstreamModelProbeStatusPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "1", "active", "enabled":
		return 0
	case "":
		return 1
	default:
		return 2
	}
}

func fetchNewAPIModelPricing(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, modelSet map[string]struct{}) (map[string][]UpstreamModelPricing, error) {
	var envelope newAPIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/ratio_config"), nil, headers, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, upstreamEnvelopeError("fetch new-api model pricing", envelope.Message)
	}
	var data struct {
		ModelRatio       map[string]float64 `json:"model_ratio"`
		CompletionRatio  map[string]float64 `json:"completion_ratio"`
		CacheRatio       map[string]float64 `json:"cache_ratio"`
		CreateCacheRatio map[string]float64 `json:"create_cache_ratio"`
		ModelPrice       map[string]float64 `json:"model_price"`
	}
	if err := common.Unmarshal(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("decode new-api model pricing response: %w", err)
	}

	pricingByModel := make(map[string][]UpstreamModelPricing, len(modelSet))
	for modelID := range modelSet {
		pricing := UpstreamModelPricing{Source: UpstreamProviderNewAPI}
		hasPricing := false
		if value, ok := data.ModelRatio[modelID]; ok {
			pricing.ModelRatio = &value
			hasPricing = true
		}
		if value, ok := data.CompletionRatio[modelID]; ok {
			pricing.CompletionRatio = &value
			hasPricing = true
		}
		if value, ok := data.CacheRatio[modelID]; ok {
			pricing.CacheRatio = &value
			hasPricing = true
		}
		if value, ok := data.CreateCacheRatio[modelID]; ok {
			pricing.CreateCacheRatio = &value
			hasPricing = true
		}
		if value, ok := data.ModelPrice[modelID]; ok {
			pricing.ModelPrice = &value
			hasPricing = true
		}
		if hasPricing {
			pricingByModel[modelID] = []UpstreamModelPricing{pricing}
		}
	}
	return pricingByModel, nil
}

type sub2APIChannelPricing struct {
	Name      string                   `json:"name"`
	Platforms []sub2APIPlatformPricing `json:"platforms"`
}

type sub2APIPlatformPricing struct {
	Platform        string            `json:"platform"`
	SupportedModels []json.RawMessage `json:"supported_models"`
}

type sub2APIModelPricingPayload struct {
	BillingMode      string                         `json:"billing_mode"`
	InputPrice       *float64                       `json:"input_price"`
	OutputPrice      *float64                       `json:"output_price"`
	CacheWritePrice  *float64                       `json:"cache_write_price"`
	CacheReadPrice   *float64                       `json:"cache_read_price"`
	ImageInputPrice  *float64                       `json:"image_input_price"`
	ImageOutputPrice *float64                       `json:"image_output_price"`
	PerRequestPrice  *float64                       `json:"per_request_price"`
	Intervals        []UpstreamModelPricingInterval `json:"intervals"`
}

type sub2APISupportedModelPayload struct {
	Name             string                         `json:"name"`
	Model            string                         `json:"model"`
	Platform         string                         `json:"platform"`
	Pricing          json.RawMessage                `json:"pricing"`
	BillingMode      string                         `json:"billing_mode"`
	InputPrice       *float64                       `json:"input_price"`
	OutputPrice      *float64                       `json:"output_price"`
	CacheWritePrice  *float64                       `json:"cache_write_price"`
	CacheReadPrice   *float64                       `json:"cache_read_price"`
	ImageInputPrice  *float64                       `json:"image_input_price"`
	ImageOutputPrice *float64                       `json:"image_output_price"`
	PerRequestPrice  *float64                       `json:"per_request_price"`
	Intervals        []UpstreamModelPricingInterval `json:"intervals"`
}

func fetchSub2APIModelPricing(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, modelSet map[string]struct{}) (map[string][]UpstreamModelPricing, error) {
	var envelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/v1/channels/available"), nil, headers, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, upstreamEnvelopeError("fetch sub2api model pricing", envelope.Message)
	}
	channels, err := decodeSub2APIChannels(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("decode sub2api model pricing response: %w", err)
	}

	pricingByModel := make(map[string][]UpstreamModelPricing, len(modelSet))
	for _, channel := range channels {
		for _, platformSection := range channel.Platforms {
			for _, rawModel := range platformSection.SupportedModels {
				modelID, supportedModel, ok := decodeSub2APISupportedModel(rawModel)
				if !ok {
					continue
				}
				if _, ok := modelSet[modelID]; !ok {
					continue
				}
				platform := strings.TrimSpace(supportedModel.Platform)
				if platform == "" {
					platform = strings.TrimSpace(platformSection.Platform)
				}
				pricing := UpstreamModelPricing{
					Source:      UpstreamProviderSub2API,
					ChannelName: strings.TrimSpace(channel.Name),
					Platform:    platform,
				}
				applySub2APIPricing(&pricing, supportedModel)
				if len(supportedModel.Pricing) > 0 && string(supportedModel.Pricing) != "null" {
					var nested sub2APIModelPricingPayload
					if err := common.Unmarshal(supportedModel.Pricing, &nested); err == nil {
						applySub2APINestedPricing(&pricing, nested)
					}
				}
				pricingByModel[modelID] = append(pricingByModel[modelID], pricing)
			}
		}
	}
	for modelID := range pricingByModel {
		sort.SliceStable(pricingByModel[modelID], func(i, j int) bool {
			left := pricingByModel[modelID][i]
			right := pricingByModel[modelID][j]
			if left.ChannelName != right.ChannelName {
				return left.ChannelName < right.ChannelName
			}
			if left.Platform != right.Platform {
				return left.Platform < right.Platform
			}
			return left.BillingMode < right.BillingMode
		})
	}
	return pricingByModel, nil
}

func decodeSub2APIChannels(data json.RawMessage) ([]sub2APIChannelPricing, error) {
	var channels []sub2APIChannelPricing
	if err := common.Unmarshal(data, &channels); err == nil {
		return channels, nil
	}
	var wrapper struct {
		Channels []sub2APIChannelPricing `json:"channels"`
		Items    []sub2APIChannelPricing `json:"items"`
		Data     []sub2APIChannelPricing `json:"data"`
	}
	if err := common.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.Channels != nil {
		return wrapper.Channels, nil
	}
	if wrapper.Items != nil {
		return wrapper.Items, nil
	}
	return wrapper.Data, nil
}

func decodeSub2APISupportedModel(raw json.RawMessage) (string, sub2APISupportedModelPayload, bool) {
	var modelName string
	if err := common.Unmarshal(raw, &modelName); err == nil {
		modelName = strings.TrimSpace(modelName)
		return modelName, sub2APISupportedModelPayload{Name: modelName}, modelName != ""
	}
	var supportedModel sub2APISupportedModelPayload
	if err := common.Unmarshal(raw, &supportedModel); err != nil {
		return "", sub2APISupportedModelPayload{}, false
	}
	modelID := strings.TrimSpace(supportedModel.Name)
	if modelID == "" {
		modelID = strings.TrimSpace(supportedModel.Model)
	}
	return modelID, supportedModel, modelID != ""
}

func applySub2APIPricing(pricing *UpstreamModelPricing, payload sub2APISupportedModelPayload) {
	pricing.BillingMode = strings.TrimSpace(payload.BillingMode)
	pricing.InputPrice = payload.InputPrice
	pricing.OutputPrice = payload.OutputPrice
	pricing.CacheWritePrice = payload.CacheWritePrice
	pricing.CacheReadPrice = payload.CacheReadPrice
	pricing.ImageInputPrice = payload.ImageInputPrice
	pricing.ImageOutputPrice = payload.ImageOutputPrice
	pricing.PerRequestPrice = payload.PerRequestPrice
	pricing.Intervals = payload.Intervals
}

func applySub2APINestedPricing(pricing *UpstreamModelPricing, payload sub2APIModelPricingPayload) {
	if strings.TrimSpace(payload.BillingMode) != "" {
		pricing.BillingMode = strings.TrimSpace(payload.BillingMode)
	}
	if payload.InputPrice != nil {
		pricing.InputPrice = payload.InputPrice
	}
	if payload.OutputPrice != nil {
		pricing.OutputPrice = payload.OutputPrice
	}
	if payload.CacheWritePrice != nil {
		pricing.CacheWritePrice = payload.CacheWritePrice
	}
	if payload.CacheReadPrice != nil {
		pricing.CacheReadPrice = payload.CacheReadPrice
	}
	if payload.ImageInputPrice != nil {
		pricing.ImageInputPrice = payload.ImageInputPrice
	}
	if payload.ImageOutputPrice != nil {
		pricing.ImageOutputPrice = payload.ImageOutputPrice
	}
	if payload.PerRequestPrice != nil {
		pricing.PerRequestPrice = payload.PerRequestPrice
	}
	if payload.Intervals != nil {
		pricing.Intervals = payload.Intervals
	}
}

func prepareUpstreamProvider(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential) (string, string, error) {
	normalized, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return "", "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	resolvedProvider := strings.TrimSpace(strings.ToLower(provider))
	if resolvedProvider == "" || resolvedProvider == UpstreamProviderAuto {
		resolvedProvider = DetectUpstreamProvider(ctx, client, normalized)
	}
	authType := model.NormalizeUpstreamAuthType(credential.AuthType)
	if authType != model.UpstreamAuthTypePassword && authType != model.UpstreamAuthTypeAccessToken {
		return "", "", fmt.Errorf("unsupported upstream authentication method %q", authType)
	}
	if authType == model.UpstreamAuthTypeAccessToken && !supportsUpstreamAccessToken(resolvedProvider) {
		return "", "", errors.New("access token authentication is only supported for new-api and sub2api upstream channels")
	}
	if resolvedProvider != UpstreamProviderNewAPI && resolvedProvider != UpstreamProviderSub2API {
		return "", "", fmt.Errorf("unsupported upstream provider %q", resolvedProvider)
	}
	return normalized, resolvedProvider, nil
}

func fetchNewAPIQuotaPerUnit(ctx context.Context, client *http.Client, baseURL string) (float64, error) {
	quotaPerUnit := common.QuotaPerUnit
	var statusEnvelope newAPIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/status"), nil, nil, &statusEnvelope); err != nil {
		return 0, fmt.Errorf("fetch new-api status failed: %w", err)
	}
	if !statusEnvelope.Success {
		return 0, upstreamEnvelopeError("fetch new-api status", statusEnvelope.Message)
	}
	var statusData struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
	}
	if err := common.Unmarshal(statusEnvelope.Data, &statusData); err != nil {
		return 0, fmt.Errorf("decode new-api status response: %w", err)
	}
	if statusData.QuotaPerUnit > 0 {
		quotaPerUnit = statusData.QuotaPerUnit
	}
	return quotaPerUnit, nil
}

func fetchNewAPIAccount(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, quotaPerUnit float64) (UpstreamAccount, error) {
	var selfEnvelope newAPIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/user/self"), nil, headers, &selfEnvelope); err != nil {
		return UpstreamAccount{}, fmt.Errorf("fetch new-api account failed: %w", err)
	}
	if !selfEnvelope.Success {
		return UpstreamAccount{}, upstreamEnvelopeError("fetch new-api account", selfEnvelope.Message)
	}
	var accountData struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     int    `json:"role"`
		Group    string `json:"group"`
		Quota    int64  `json:"quota"`
	}
	if err := common.Unmarshal(selfEnvelope.Data, &accountData); err != nil {
		return UpstreamAccount{}, fmt.Errorf("decode new-api account response: %w", err)
	}
	balance := 0.0
	if quotaPerUnit > 0 {
		balance = float64(accountData.Quota) / quotaPerUnit
	}
	return UpstreamAccount{ID: accountData.ID, Username: accountData.Username, Role: strconv.Itoa(accountData.Role), Group: accountData.Group, Balance: balance}, nil
}

func authenticateSub2API(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential) (*http.Client, map[string]string, error) {
	authType := model.NormalizeUpstreamAuthType(credential.AuthType)
	if authType == model.UpstreamAuthTypeAccessToken {
		accessToken := strings.TrimSpace(credential.Password)
		if accessToken == "" {
			return nil, nil, errors.New("sub2api access token is required")
		}
		return client, map[string]string{"Authorization": "Bearer " + accessToken}, nil
	}
	if authType != model.UpstreamAuthTypePassword {
		return nil, nil, fmt.Errorf("unsupported sub2api authentication method %q", authType)
	}

	loginPayload := map[string]string{"email": credential.Username, "password": credential.Password}
	var loginEnvelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodPost, upstreamURL(baseURL, "/api/v1/auth/login"), loginPayload, nil, &loginEnvelope); err != nil {
		if strings.EqualFold(strings.TrimSpace(loginEnvelope.Reason), "TURNSTILE_VERIFICATION_FAILED") || strings.Contains(strings.ToLower(loginEnvelope.Message), "turnstile") {
			return nil, nil, ErrSub2APITurnstileRequiresAccessToken
		}
		return nil, nil, fmt.Errorf("sub2api login failed: %w", err)
	}
	if loginEnvelope.Code != 0 {
		if strings.EqualFold(strings.TrimSpace(loginEnvelope.Reason), "TURNSTILE_VERIFICATION_FAILED") || strings.Contains(strings.ToLower(loginEnvelope.Message), "turnstile") {
			return nil, nil, ErrSub2APITurnstileRequiresAccessToken
		}
		return nil, nil, upstreamEnvelopeError("sub2api login", loginEnvelope.Message)
	}
	var loginData struct {
		AccessToken string `json:"access_token"`
		Requires2FA bool   `json:"requires_2fa"`
	}
	if err := common.Unmarshal(loginEnvelope.Data, &loginData); err != nil {
		return nil, nil, fmt.Errorf("decode sub2api login response: %w", err)
	}
	if loginData.Requires2FA {
		return nil, nil, errors.New("sub2api account requires two-factor authentication")
	}
	if loginData.AccessToken == "" {
		return nil, nil, errors.New("sub2api login response did not include an access token")
	}
	return client, map[string]string{"Authorization": "Bearer " + loginData.AccessToken}, nil
}

func fetchSub2APIAccount(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) (UpstreamAccount, error) {
	var profileEnvelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/v1/user/profile"), nil, headers, &profileEnvelope); err != nil {
		return UpstreamAccount{}, fmt.Errorf("fetch sub2api account failed: %w", err)
	}
	if profileEnvelope.Code != 0 {
		return UpstreamAccount{}, upstreamEnvelopeError("fetch sub2api account", profileEnvelope.Message)
	}
	var accountData struct {
		ID       int64   `json:"id"`
		Email    string  `json:"email"`
		Username string  `json:"username"`
		Role     string  `json:"role"`
		Balance  float64 `json:"balance"`
	}
	if err := common.Unmarshal(profileEnvelope.Data, &accountData); err != nil {
		return UpstreamAccount{}, fmt.Errorf("decode sub2api account response: %w", err)
	}
	return UpstreamAccount{ID: accountData.ID, Username: accountData.Username, Email: accountData.Email, Role: accountData.Role, Balance: accountData.Balance}, nil
}

func fetchNewAPIUpstreamSnapshot(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	sessionClient, headers, err := authenticateNewAPI(ctx, client, baseURL, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	quotaPerUnit := common.QuotaPerUnit
	var statusEnvelope newAPIEnvelope
	if err = doUpstreamJSON(ctx, sessionClient, http.MethodGet, upstreamURL(baseURL, "/api/status"), nil, nil, &statusEnvelope); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("fetch new-api status failed: %w", err)
	}
	if !statusEnvelope.Success {
		return UpstreamSnapshot{}, upstreamEnvelopeError("fetch new-api status", statusEnvelope.Message)
	}
	var statusData struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
	}
	if err = common.Unmarshal(statusEnvelope.Data, &statusData); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("decode new-api status response: %w", err)
	}
	if statusData.QuotaPerUnit > 0 {
		quotaPerUnit = statusData.QuotaPerUnit
	}

	var selfEnvelope newAPIEnvelope
	if err = doUpstreamJSON(ctx, sessionClient, http.MethodGet, upstreamURL(baseURL, "/api/user/self"), nil, headers, &selfEnvelope); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("fetch new-api account failed: %w", err)
	}
	if !selfEnvelope.Success {
		return UpstreamSnapshot{}, upstreamEnvelopeError("fetch new-api account", selfEnvelope.Message)
	}
	var accountData struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     int    `json:"role"`
		Group    string `json:"group"`
		Quota    int64  `json:"quota"`
	}
	if err = common.Unmarshal(selfEnvelope.Data, &accountData); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("decode new-api account response: %w", err)
	}
	balance := 0.0
	if quotaPerUnit > 0 {
		balance = float64(accountData.Quota) / quotaPerUnit
	}

	groups, ratios, err := fetchNewAPIGroups(ctx, sessionClient, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	keys, err := fetchNewAPIKeys(ctx, sessionClient, baseURL, headers, quotaPerUnit)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range keys {
		if keys[i].KeyFingerprint != "" {
			continue
		}
		fullKey, fetchErr := fetchNewAPIFullKeyWithSession(ctx, sessionClient, baseURL, headers, keys[i].ID)
		if fetchErr != nil {
			return UpstreamSnapshot{}, fmt.Errorf("fetch new-api key %d for import status: %w", keys[i].ID, fetchErr)
		}
		keys[i].KeyFingerprint = upstreamKeyFingerprint(fullKey)
	}

	return UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Balance:  balance,
		Account: UpstreamAccount{
			ID:       accountData.ID,
			Username: accountData.Username,
			Role:     strconv.Itoa(accountData.Role),
			Group:    accountData.Group,
			Balance:  balance,
		},
		Keys:        keys,
		Groups:      groups,
		Ratios:      ratios,
		RetrievedAt: time.Now().Unix(),
	}, nil
}

func fetchNewAPIGroups(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) ([]UpstreamGroup, map[string]float64, error) {
	var envelope newAPIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/user/self/groups"), nil, headers, &envelope); err != nil {
		return nil, nil, fmt.Errorf("fetch new-api groups failed: %w", err)
	}
	if !envelope.Success {
		return nil, nil, upstreamEnvelopeError("fetch new-api groups", envelope.Message)
	}
	var rawGroups map[string]struct {
		Ratio float64 `json:"ratio"`
		Desc  string  `json:"desc"`
	}
	if err := common.Unmarshal(envelope.Data, &rawGroups); err != nil {
		return nil, nil, fmt.Errorf("decode new-api groups response: %w", err)
	}
	names := make([]string, 0, len(rawGroups))
	for name := range rawGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]UpstreamGroup, 0, len(names))
	ratios := make(map[string]float64, len(names))
	for _, name := range names {
		group := rawGroups[name]
		groups = append(groups, UpstreamGroup{Name: name, Description: group.Desc, Ratio: group.Ratio})
		ratios[name] = group.Ratio
	}
	return groups, ratios, nil
}

func fetchNewAPIKeys(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, quotaPerUnit float64) ([]UpstreamKey, error) {
	unit := quotaPerUnit
	if unit <= 0 {
		unit = common.QuotaPerUnit
	}
	keys := make([]UpstreamKey, 0)
	for pageNumber := 1; pageNumber <= upstreamKeyMaxPages; pageNumber++ {
		var envelope newAPIEnvelope
		path := fmt.Sprintf("/api/token?p=%d&page_size=%d", pageNumber, upstreamKeyPageSize)
		if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, path), nil, headers, &envelope); err != nil {
			return nil, fmt.Errorf("fetch new-api keys failed: %w", err)
		}
		if !envelope.Success {
			return nil, upstreamEnvelopeError("fetch new-api keys", envelope.Message)
		}
		var page struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
			Items    []struct {
				ID          int64  `json:"id"`
				Name        string `json:"name"`
				Key         string `json:"key"`
				Group       string `json:"group"`
				Status      int    `json:"status"`
				RemainQuota int64  `json:"remain_quota"`
				UsedQuota   int64  `json:"used_quota"`
				Unlimited   bool   `json:"unlimited_quota"`
			} `json:"items"`
		}
		if err := common.Unmarshal(envelope.Data, &page); err != nil {
			return nil, fmt.Errorf("decode new-api keys response: %w", err)
		}
		for _, item := range page.Items {
			remainQuota := float64(item.RemainQuota)
			quotaUsed := float64(item.UsedQuota)
			if unit > 0 {
				remainQuota /= unit
				quotaUsed /= unit
			}
			fingerprint := ""
			if !strings.Contains(item.Key, "...") {
				fingerprint = upstreamKeyFingerprint(item.Key)
			}
			keys = append(keys, UpstreamKey{
				ID:             item.ID,
				Name:           item.Name,
				MaskedKey:      maskUpstreamKey(item.Key),
				Group:          item.Group,
				Status:         strconv.Itoa(item.Status),
				QuotaUsed:      quotaUsed,
				RemainQuota:    remainQuota,
				KeyFingerprint: fingerprint,
			})
		}
		if len(page.Items) == 0 || (page.Total > 0 && len(keys) >= page.Total) || (page.Total == 0 && len(page.Items) < upstreamKeyPageSize) {
			return keys, nil
		}
	}
	return nil, errors.New("fetch new-api keys exceeded the pagination limit")
}

func fetchSub2APIUpstreamSnapshot(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential) (UpstreamSnapshot, error) {
	sessionClient, headers, err := authenticateSub2API(ctx, client, baseURL, credential)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	account, err := fetchSub2APIAccount(ctx, sessionClient, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}

	keys, err := fetchSub2APIKeys(ctx, sessionClient, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range keys {
		if keys[i].KeyFingerprint != "" {
			continue
		}
		fullKey, fetchErr := fetchSub2APIFullKeyWithToken(ctx, sessionClient, baseURL, headers, keys[i].ID)
		if fetchErr != nil {
			return UpstreamSnapshot{}, fmt.Errorf("fetch sub2api key %d for import status: %w", keys[i].ID, fetchErr)
		}
		keys[i].KeyFingerprint = upstreamKeyFingerprint(fullKey)
	}
	groups, err := fetchSub2APIGroups(ctx, sessionClient, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	ratios, err := fetchSub2APIRatios(ctx, sessionClient, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range groups {
		if ratio, ok := ratios[strconv.FormatInt(groups[i].ID, 10)]; ok {
			groups[i].Ratio = ratio
		}
	}
	applyUpstreamGroupNames(keys, groups)

	return UpstreamSnapshot{
		Provider:    UpstreamProviderSub2API,
		Balance:     account.Balance,
		Account:     account,
		Keys:        keys,
		Groups:      groups,
		Ratios:      ratios,
		RetrievedAt: time.Now().Unix(),
	}, nil
}

func fetchSub2APIKeys(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) ([]UpstreamKey, error) {
	keys := make([]UpstreamKey, 0)
	for pageNumber := 1; pageNumber <= upstreamKeyMaxPages; pageNumber++ {
		var envelope sub2APIEnvelope
		path := fmt.Sprintf("/api/v1/keys?page=%d&page_size=%d", pageNumber, upstreamKeyPageSize)
		if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, path), nil, headers, &envelope); err != nil {
			return nil, fmt.Errorf("fetch sub2api keys failed: %w", err)
		}
		if envelope.Code != 0 {
			return nil, upstreamEnvelopeError("fetch sub2api keys", envelope.Message)
		}
		var page struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
			Pages    int `json:"pages"`
			Items    []struct {
				ID        int64   `json:"id"`
				Name      string  `json:"name"`
				Key       string  `json:"key"`
				GroupID   *int64  `json:"group_id"`
				Status    string  `json:"status"`
				Quota     float64 `json:"quota"`
				QuotaUsed float64 `json:"quota_used"`
			} `json:"items"`
		}
		if err := common.Unmarshal(envelope.Data, &page); err != nil {
			return nil, fmt.Errorf("decode sub2api keys response: %w", err)
		}
		for _, item := range page.Items {
			fingerprint := ""
			if !strings.Contains(item.Key, "...") {
				fingerprint = upstreamKeyFingerprint(item.Key)
			}
			keys = append(keys, UpstreamKey{
				ID:             item.ID,
				Name:           item.Name,
				MaskedKey:      maskUpstreamKey(item.Key),
				GroupID:        item.GroupID,
				Status:         item.Status,
				Quota:          item.Quota,
				QuotaUsed:      item.QuotaUsed,
				RemainQuota:    item.Quota - item.QuotaUsed,
				KeyFingerprint: fingerprint,
			})
		}
		if len(page.Items) == 0 || (page.Pages > 0 && pageNumber >= page.Pages) || (page.Total > 0 && len(keys) >= page.Total) || (page.Pages == 0 && page.Total == 0 && len(page.Items) < upstreamKeyPageSize) {
			return keys, nil
		}
	}
	return nil, errors.New("fetch sub2api keys exceeded the pagination limit")
}

func fetchSub2APIGroups(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) ([]UpstreamGroup, error) {
	var envelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/v1/groups/available"), nil, headers, &envelope); err != nil {
		return nil, fmt.Errorf("fetch sub2api groups failed: %w", err)
	}
	if envelope.Code != 0 {
		return nil, upstreamEnvelopeError("fetch sub2api groups", envelope.Message)
	}
	var raw []struct {
		ID             int64   `json:"id"`
		Name           string  `json:"name"`
		Description    string  `json:"description"`
		Platform       string  `json:"platform"`
		RateMultiplier float64 `json:"rate_multiplier"`
	}
	if err := common.Unmarshal(envelope.Data, &raw); err != nil {
		return nil, fmt.Errorf("decode sub2api groups response: %w", err)
	}
	groups := make([]UpstreamGroup, 0, len(raw))
	for _, item := range raw {
		groups = append(groups, UpstreamGroup{ID: item.ID, Name: strings.TrimSpace(item.Name), Description: item.Description, Platform: item.Platform, Ratio: item.RateMultiplier})
	}
	return groups, nil
}

func fetchSub2APIRatios(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) (map[string]float64, error) {
	var envelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/v1/groups/rates"), nil, headers, &envelope); err != nil {
		return nil, fmt.Errorf("fetch sub2api ratios failed: %w", err)
	}
	if envelope.Code != 0 {
		return nil, upstreamEnvelopeError("fetch sub2api ratios", envelope.Message)
	}
	ratios := map[string]float64{}
	if err := common.Unmarshal(envelope.Data, &ratios); err != nil {
		return nil, fmt.Errorf("decode sub2api ratios response: %w", err)
	}
	return ratios, nil
}

func FetchUpstreamFullKey(ctx context.Context, client *http.Client, baseURL string, provider string, credential UpstreamCredential, keyID int64) (string, error) {
	normalized, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" || provider == UpstreamProviderAuto {
		provider = DetectUpstreamProvider(ctx, client, normalized)
	}
	authType := model.NormalizeUpstreamAuthType(credential.AuthType)
	if authType != model.UpstreamAuthTypePassword && authType != model.UpstreamAuthTypeAccessToken {
		return "", fmt.Errorf("unsupported upstream authentication method %q", authType)
	}
	if authType == model.UpstreamAuthTypeAccessToken && !supportsUpstreamAccessToken(provider) {
		return "", errors.New("access token authentication is only supported for new-api and sub2api upstream channels")
	}
	switch provider {
	case UpstreamProviderNewAPI:
		return fetchNewAPIFullKey(ctx, client, normalized, credential, keyID)
	case UpstreamProviderSub2API:
		return fetchSub2APIFullKey(ctx, client, normalized, credential, keyID)
	default:
		return "", fmt.Errorf("unsupported upstream provider %q", provider)
	}
}

func fetchNewAPIFullKey(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential, keyID int64) (string, error) {
	sessionClient, headers, err := authenticateNewAPI(ctx, client, baseURL, credential)
	if err != nil {
		return "", err
	}
	return fetchNewAPIFullKeyWithSession(ctx, sessionClient, baseURL, headers, keyID)
}

func authenticateNewAPI(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential) (*http.Client, map[string]string, error) {
	authType := model.NormalizeUpstreamAuthType(credential.AuthType)
	if authType == model.UpstreamAuthTypeAccessToken {
		headers, err := newAPIManagementAccessHeaders(credential)
		if err != nil {
			return nil, nil, err
		}
		sessionClient := *client
		return &sessionClient, headers, nil
	}
	if authType != model.UpstreamAuthTypePassword {
		return nil, nil, fmt.Errorf("unsupported new-api authentication method %q", authType)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}
	sessionClient := *client
	sessionClient.Jar = jar
	var loginEnvelope newAPIEnvelope
	if err = doUpstreamJSON(ctx, &sessionClient, http.MethodPost, upstreamURL(baseURL, "/api/user/login"), map[string]string{"username": credential.Username, "password": credential.Password}, nil, &loginEnvelope); err != nil {
		return nil, nil, fmt.Errorf("new-api login failed: %w", err)
	}
	if !loginEnvelope.Success {
		if !strings.Contains(strings.ToLower(loginEnvelope.Message), "turnstile") {
			return nil, nil, upstreamEnvelopeError("new-api login", loginEnvelope.Message)
		}
		headers, managementErr := newAPIManagementAccessHeaders(credential)
		if managementErr != nil {
			return nil, nil, ErrNewAPITurnstileRequiresAccessToken
		}
		return &sessionClient, headers, nil
	}
	var loginData struct {
		ID         int64 `json:"id"`
		Require2FA bool  `json:"require_2fa"`
	}
	if err = common.Unmarshal(loginEnvelope.Data, &loginData); err != nil {
		return nil, nil, fmt.Errorf("decode new-api login response: %w", err)
	}
	if loginData.Require2FA {
		return nil, nil, errors.New("new-api account requires two-factor authentication")
	}
	if loginData.ID <= 0 {
		return nil, nil, errors.New("new-api login response did not include a user id")
	}
	return &sessionClient, map[string]string{"New-Api-User": strconv.FormatInt(loginData.ID, 10)}, nil
}

func newAPIManagementAccessHeaders(credential UpstreamCredential) (map[string]string, error) {
	userID, err := strconv.ParseInt(strings.TrimSpace(credential.Username), 10, 64)
	if err != nil || userID <= 0 {
		return nil, errors.New("new-api management access token authentication requires a positive numeric user ID")
	}
	managementToken := strings.TrimSpace(credential.Password)
	if managementToken == "" {
		return nil, errors.New("new-api management access token is required")
	}
	return map[string]string{
		"Authorization": "Bearer " + managementToken,
		"New-Api-User":  strconv.FormatInt(userID, 10),
	}, nil
}

func fetchNewAPIFullKeyWithSession(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, keyID int64) (string, error) {
	var envelope newAPIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodPost, upstreamURL(baseURL, fmt.Sprintf("/api/token/%d/key", keyID)), nil, headers, &envelope); err != nil {
		return "", err
	}
	if !envelope.Success {
		return "", upstreamEnvelopeError("fetch new-api key", envelope.Message)
	}
	var data struct {
		Key string `json:"key"`
	}
	if err := common.Unmarshal(envelope.Data, &data); err != nil {
		return "", err
	}
	if strings.TrimSpace(data.Key) == "" {
		return "", errors.New("new-api key response was empty")
	}
	return data.Key, nil
}

func fetchSub2APIFullKey(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential, keyID int64) (string, error) {
	sessionClient, headers, err := authenticateSub2API(ctx, client, baseURL, credential)
	if err != nil {
		return "", err
	}
	return fetchSub2APIFullKeyWithToken(ctx, sessionClient, baseURL, headers, keyID)
}

func fetchSub2APIFullKeyWithToken(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, keyID int64) (string, error) {
	var envelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, fmt.Sprintf("/api/v1/keys/%d", keyID)), nil, headers, &envelope); err != nil {
		return "", err
	}
	if envelope.Code != 0 {
		return "", upstreamEnvelopeError("fetch sub2api key", envelope.Message)
	}
	var data struct {
		Key string `json:"key"`
	}
	if err := common.Unmarshal(envelope.Data, &data); err != nil {
		return "", err
	}
	if strings.TrimSpace(data.Key) == "" {
		return "", errors.New("sub2api key response was empty")
	}
	return data.Key, nil
}

func upstreamKeyFingerprint(key string) string {
	return model.UpstreamKeyFingerprint(key)
}

func doUpstreamJSON(ctx context.Context, client *http.Client, method string, url string, payload any, headers map[string]string, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if target != nil && len(message) > 0 {
			_ = common.Unmarshal(message, target)
		}
		messageText := strings.TrimSpace(string(message))
		if messageText == "" {
			messageText = resp.Status
		}
		return fmt.Errorf("upstream returned HTTP %d: %s", resp.StatusCode, messageText)
	}
	if target == nil {
		return nil
	}
	if err := common.DecodeJson(io.LimitReader(resp.Body, upstreamResponseLimit), target); err != nil {
		return fmt.Errorf("decode upstream response: %w", err)
	}
	return nil
}

func upstreamURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func upstreamEnvelopeError(action string, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "upstream rejected the request"
	}
	return fmt.Errorf("%s: %s", action, message)
}

func maskUpstreamKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, "...") {
		return key
	}
	if len(key) <= 12 {
		return "****"
	}
	return key[:8] + "..." + key[len(key)-4:]
}
