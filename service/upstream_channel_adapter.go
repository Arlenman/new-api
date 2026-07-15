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

var ErrNewAPITurnstileRequiresAccessToken = errors.New(
	"new-api has Turnstile enabled; use the numeric upstream user ID and a management access token",
)

func UpstreamErrorCode(err error) string {
	if errors.Is(err, ErrNewAPITurnstileRequiresAccessToken) {
		return UpstreamErrorCodeTurnstileRequiresAccessToken
	}
	return ""
}

func UpstreamErrorCodeFromMessage(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == strings.ToLower(ErrNewAPITurnstileRequiresAccessToken.Error()) ||
		(strings.Contains(normalized, "turnstile") && strings.Contains(normalized, "management access token")) {
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

type UpstreamSnapshot struct {
	Provider    string             `json:"provider"`
	Balance     float64            `json:"balance"`
	Account     UpstreamAccount    `json:"account"`
	Keys        []UpstreamKey      `json:"keys"`
	Groups      []UpstreamGroup    `json:"groups"`
	Ratios      map[string]float64 `json:"ratios"`
	RetrievedAt int64              `json:"retrieved_at"`
}

type newAPIEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type sub2APIEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
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
	if authType == model.UpstreamAuthTypeAccessToken && provider != UpstreamProviderNewAPI {
		return UpstreamSnapshot{}, errors.New("management access token authentication is only supported for new-api upstream channels")
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
	loginPayload := map[string]string{"email": credential.Username, "password": credential.Password}
	var loginEnvelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodPost, upstreamURL(baseURL, "/api/v1/auth/login"), loginPayload, nil, &loginEnvelope); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("sub2api login failed: %w", err)
	}
	if loginEnvelope.Code != 0 {
		return UpstreamSnapshot{}, upstreamEnvelopeError("sub2api login", loginEnvelope.Message)
	}
	var loginData struct {
		AccessToken string `json:"access_token"`
		Requires2FA bool   `json:"requires_2fa"`
	}
	if err := common.Unmarshal(loginEnvelope.Data, &loginData); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("decode sub2api login response: %w", err)
	}
	if loginData.Requires2FA {
		return UpstreamSnapshot{}, errors.New("sub2api account requires two-factor authentication")
	}
	if loginData.AccessToken == "" {
		return UpstreamSnapshot{}, errors.New("sub2api login response did not include an access token")
	}
	headers := map[string]string{"Authorization": "Bearer " + loginData.AccessToken}

	var profileEnvelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/api/v1/user/profile"), nil, headers, &profileEnvelope); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("fetch sub2api account failed: %w", err)
	}
	if profileEnvelope.Code != 0 {
		return UpstreamSnapshot{}, upstreamEnvelopeError("fetch sub2api account", profileEnvelope.Message)
	}
	var accountData struct {
		ID       int64   `json:"id"`
		Email    string  `json:"email"`
		Username string  `json:"username"`
		Role     string  `json:"role"`
		Balance  float64 `json:"balance"`
	}
	if err := common.Unmarshal(profileEnvelope.Data, &accountData); err != nil {
		return UpstreamSnapshot{}, fmt.Errorf("decode sub2api account response: %w", err)
	}

	keys, err := fetchSub2APIKeys(ctx, client, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range keys {
		if keys[i].KeyFingerprint != "" {
			continue
		}
		fullKey, fetchErr := fetchSub2APIFullKeyWithToken(ctx, client, baseURL, headers, keys[i].ID)
		if fetchErr != nil {
			return UpstreamSnapshot{}, fmt.Errorf("fetch sub2api key %d for import status: %w", keys[i].ID, fetchErr)
		}
		keys[i].KeyFingerprint = upstreamKeyFingerprint(fullKey)
	}
	groups, err := fetchSub2APIGroups(ctx, client, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	ratios, err := fetchSub2APIRatios(ctx, client, baseURL, headers)
	if err != nil {
		return UpstreamSnapshot{}, err
	}
	for i := range groups {
		if ratio, ok := ratios[strconv.FormatInt(groups[i].ID, 10)]; ok {
			groups[i].Ratio = ratio
		}
	}
	groupNames := make(map[int64]string, len(groups))
	for _, group := range groups {
		groupNames[group.ID] = group.Name
	}
	for i := range keys {
		if keys[i].GroupID != nil {
			keys[i].Group = groupNames[*keys[i].GroupID]
		}
	}

	return UpstreamSnapshot{
		Provider: UpstreamProviderSub2API,
		Balance:  accountData.Balance,
		Account: UpstreamAccount{
			ID:       accountData.ID,
			Username: accountData.Username,
			Email:    accountData.Email,
			Role:     accountData.Role,
			Balance:  accountData.Balance,
		},
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
	if authType == model.UpstreamAuthTypeAccessToken && provider != UpstreamProviderNewAPI {
		return "", errors.New("management access token authentication is only supported for new-api upstream channels")
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
	var loginEnvelope sub2APIEnvelope
	if err := doUpstreamJSON(ctx, client, http.MethodPost, upstreamURL(baseURL, "/api/v1/auth/login"), map[string]string{"email": credential.Username, "password": credential.Password}, nil, &loginEnvelope); err != nil {
		return "", err
	}
	if loginEnvelope.Code != 0 {
		return "", upstreamEnvelopeError("sub2api login", loginEnvelope.Message)
	}
	var loginData struct {
		AccessToken string `json:"access_token"`
	}
	if err := common.Unmarshal(loginEnvelope.Data, &loginData); err != nil || loginData.AccessToken == "" {
		return "", errors.New("sub2api login response did not include an access token")
	}
	headers := map[string]string{"Authorization": "Bearer " + loginData.AccessToken}
	return fetchSub2APIFullKeyWithToken(ctx, client, baseURL, headers, keyID)
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
