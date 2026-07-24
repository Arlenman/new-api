package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestFetchNewAPIUpstreamSnapshot(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	singleRevealRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":500000}}`, nil), nil
		case "/api/user/login":
			headers := make(http.Header)
			headers.Add("Set-Cookie", "session=ok; Path=/")
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root","role":100,"group":"default"}}`, headers), nil
		case "/api/user/self":
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			cookie, err := r.Cookie("session")
			require.NoError(t, err)
			require.Equal(t, "ok", cookie.Value)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root","role":100,"group":"default","quota":750000,"used_quota":250000}}`, nil), nil
		case "/api/user/self/groups":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"default":{"ratio":1,"desc":"Default"},"vip":{"ratio":0.8,"desc":"VIP"}}}`, nil), nil
		case "/api/token":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"items":[{"id":7,"name":"primary","key":"sk-abcd...wxyz","group":"default","status":1,"remain_quota":1000,"used_quota":50}]}}`, nil), nil
		case "/api/token/7/key":
			singleRevealRequested = true
			require.Fail(t, "snapshot refresh must not reveal masked keys one by one")
			return nil, nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := fetchNewAPIUpstreamSnapshot(context.Background(), client, "https://upstream.test", UpstreamCredential{Username: "root", Password: "secret"})
	require.NoError(t, err)
	assert.Equal(t, UpstreamProviderNewAPI, snapshot.Provider)
	assert.InDelta(t, 1.5, snapshot.Balance, 0.000001)
	assert.Equal(t, "root", snapshot.Account.Username)
	require.Len(t, snapshot.Keys, 1)
	assert.Equal(t, "sk-abcd...wxyz", snapshot.Keys[0].MaskedKey)
	assert.False(t, singleRevealRequested)
	assert.Empty(t, snapshot.Keys[0].KeyFingerprint)
	require.Len(t, snapshot.Groups, 2)
	assert.Equal(t, 0.8, snapshot.Ratios["vip"])
}

func TestFetchNewAPIUpstreamSnapshotUsesManagementTokenWhenTurnstileIsRequired(t *testing.T) {
	const managementToken = "management-secret"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/user/login":
			return jsonResponse(http.StatusOK, `{"success":false,"message":"Turnstile token 为空"}`, nil), nil
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":100}}`, nil), nil
		case "/api/user/self":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root","quota":1250,"used_quota":250}}`, nil), nil
		case "/api/user/self/groups":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{}}`, nil), nil
		case "/api/token":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":0,"items":[]}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := fetchNewAPIUpstreamSnapshot(context.Background(), client, "https://upstream.test", UpstreamCredential{Username: "42", Password: managementToken})
	require.NoError(t, err)
	assert.Equal(t, UpstreamProviderNewAPI, snapshot.Provider)
	assert.InDelta(t, 12.5, snapshot.Balance, 0.000001)
}

func TestFetchNewAPIUpstreamSnapshotUsesExplicitManagementAccessToken(t *testing.T) {
	const managementToken = "management-secret"
	loginRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/user/login" {
			loginRequested = true
			return jsonResponse(http.StatusInternalServerError, `{}`, nil), nil
		}
		switch r.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":100}}`, nil), nil
		case "/api/user/self":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root","quota":1250}}`, nil), nil
		case "/api/user/self/groups":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{}}`, nil), nil
		case "/api/token":
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":0,"items":[]}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := fetchNewAPIUpstreamSnapshot(context.Background(), client, "https://upstream.test", UpstreamCredential{
		AuthType: "access_token",
		Username: "42",
		Password: managementToken,
	})
	require.NoError(t, err)
	assert.False(t, loginRequested)
	assert.InDelta(t, 12.5, snapshot.Balance, 0.000001)
}

func TestFetchNewAPIUpstreamSnapshotExplainsTurnstileManagementTokenSetup(t *testing.T) {
	const managementToken = "management-secret"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/user/login", r.URL.Path)
		return jsonResponse(http.StatusOK, `{"success":false,"message":"Turnstile token 为空"}`, nil), nil
	})}

	_, err := fetchNewAPIUpstreamSnapshot(context.Background(), client, "https://upstream.test", UpstreamCredential{Username: "root", Password: managementToken})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNewAPITurnstileRequiresAccessToken))
	assert.Equal(t, UpstreamErrorCodeTurnstileRequiresAccessToken, UpstreamErrorCode(err))
	assert.Equal(t, UpstreamErrorCodeTurnstileRequiresAccessToken, UpstreamErrorCodeFromMessage(err.Error()))
	assert.Contains(t, err.Error(), "user ID")
	assert.Contains(t, err.Error(), "management access token")
	assert.NotContains(t, err.Error(), managementToken)
}

func TestFetchSub2APIUpstreamSnapshot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/auth/login" {
			require.Equal(t, "Bearer token-123", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/auth/login":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"access_token":"token-123","token_type":"Bearer","user":{"id":9,"email":"owner@example.com","username":"owner","role":"user","balance":12.5}}}`, nil), nil
		case "/api/v1/user/profile":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"id":9,"email":"owner@example.com","username":"owner","role":"user","balance":12.5}}`, nil), nil
		case "/api/v1/keys":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"items":[{"id":3,"name":"main","key":"sk-sub2-secret","group_id":5,"status":"active","quota":100,"quota_used":4}]}}`, nil), nil
		case "/api/v1/groups/available":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":[{"id":5,"name":" Claude ","description":"Main group","platform":"claude","rate_multiplier":1.2}]}`, nil), nil
		case "/api/v1/groups/rates":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"5":0.9}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := fetchSub2APIUpstreamSnapshot(context.Background(), client, "https://sub2.test", UpstreamCredential{Username: "owner@example.com", Password: "secret"})
	require.NoError(t, err)
	assert.Equal(t, UpstreamProviderSub2API, snapshot.Provider)
	assert.InDelta(t, 12.5, snapshot.Balance, 0.000001)
	assert.Equal(t, "owner@example.com", snapshot.Account.Email)
	require.Len(t, snapshot.Keys, 1)
	assert.NotEqual(t, "sk-sub2-secret", snapshot.Keys[0].MaskedKey)
	assert.Contains(t, snapshot.Keys[0].MaskedKey, "...")
	assert.Equal(t, "Claude", snapshot.Keys[0].Group)
	require.Len(t, snapshot.Groups, 1)
	assert.Equal(t, 0.9, snapshot.Ratios["5"])
}

func TestFetchSub2APIUpstreamSnapshotUsesExplicitAccessToken(t *testing.T) {
	const accessToken = "sub2-access-secret"
	loginRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/login" {
			loginRequested = true
			return jsonResponse(http.StatusInternalServerError, `{}`, nil), nil
		}
		require.Equal(t, "Bearer "+accessToken, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"id":9,"email":"owner@example.com","username":"owner","role":"user","balance":12.5}}`, nil), nil
		case "/api/v1/keys":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"items":[{"id":3,"name":"main","key":"sk-sub2...cret","group_id":5,"status":"active","quota":100,"quota_used":4}]}}`, nil), nil
		case "/api/v1/keys/3":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"id":3,"name":"main","key":"sk-sub2-secret","group_id":5,"status":"active"}}`, nil), nil
		case "/api/v1/groups/available":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":[{"id":5,"name":"Claude","rate_multiplier":1.2}]}`, nil), nil
		case "/api/v1/groups/rates":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"5":0.9}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := FetchUpstreamSnapshot(context.Background(), client, "https://sub2.test", UpstreamProviderSub2API, UpstreamCredential{
		AuthType: model.UpstreamAuthTypeAccessToken,
		Username: "owner@example.com",
		Password: accessToken,
	})
	require.NoError(t, err)
	assert.False(t, loginRequested)
	assert.Equal(t, UpstreamProviderSub2API, snapshot.Provider)
	assert.InDelta(t, 12.5, snapshot.Balance, 0.000001)
}

func TestFetchSub2APIUpstreamSnapshotExplainsTurnstileAccessTokenSetup(t *testing.T) {
	const password = "account-password"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v1/auth/login", r.URL.Path)
		return jsonResponse(http.StatusBadRequest, `{"code":400,"message":"turnstile verification failed","reason":"TURNSTILE_VERIFICATION_FAILED"}`, nil), nil
	})}

	_, err := FetchUpstreamSnapshot(context.Background(), client, "https://sub2.test", UpstreamProviderSub2API, UpstreamCredential{
		AuthType: model.UpstreamAuthTypePassword,
		Username: "owner@example.com",
		Password: password,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSub2APITurnstileRequiresAccessToken)
	assert.Equal(t, UpstreamErrorCodeTurnstileRequiresAccessToken, UpstreamErrorCode(err))
	assert.Equal(t, UpstreamErrorCodeTurnstileRequiresAccessToken, UpstreamErrorCodeFromMessage(err.Error()))
	assert.Contains(t, err.Error(), "Sub2API")
	assert.Contains(t, strings.ToLower(err.Error()), "access token")
	assert.NotContains(t, err.Error(), password)
}

func TestApplyUpstreamGroupNamesUsesReadableName(t *testing.T) {
	groupID := int64(27)
	unknownGroupID := int64(99)
	keys := []UpstreamKey{
		{ID: 1, Group: "27", GroupID: &groupID},
		{ID: 2, Group: "99", GroupID: &unknownGroupID},
	}
	groups := []UpstreamGroup{{ID: groupID, Name: " 云起 "}}

	applyUpstreamGroupNames(keys, groups)

	assert.Equal(t, "云起", keys[0].Group)
	require.NotNil(t, keys[0].GroupID)
	assert.Equal(t, groupID, *keys[0].GroupID)
	assert.Equal(t, "99", keys[1].Group)
}

func TestDetectUpstreamProviderRequiresExplicitSub2APIEnvelope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v1/settings/public", r.URL.Path)
		return jsonResponse(http.StatusOK, `{"data":{"site_name":"not-sub2api"}}`, nil), nil
	})}

	provider := DetectUpstreamProvider(context.Background(), client, "https://upstream.test")
	assert.Equal(t, UpstreamProviderNewAPI, provider)
}

func TestFetchNewAPIKeysLoadsEveryPage(t *testing.T) {
	requestedPages := make([]string, 0, 2)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/token", r.URL.Path)
		page := r.URL.Query().Get("p")
		requestedPages = append(requestedPages, page)
		switch page {
		case "1":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":1,"total":2,"items":[{"id":1,"name":"first","key":"sk-first-secret","group":"default","status":1}]}}`, nil), nil
		case "2":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":2,"page_size":1,"total":2,"items":[{"id":2,"name":"second","key":"sk-second-secret","group":"vip","status":1}]}}`, nil), nil
		default:
			return jsonResponse(http.StatusBadRequest, `{}`, nil), nil
		}
	})}

	keys, err := fetchNewAPIKeys(context.Background(), client, "https://upstream.test", nil, common.QuotaPerUnit)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, []string{"1", "2"}, requestedPages)
	assert.Equal(t, int64(2), keys[1].ID)
}

func TestFetchUpstreamKeysDoesNotRevealAsteriskMaskedNewAPIKey(t *testing.T) {
	singleRevealRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":1000000}}`, nil), nil
		case "/api/token":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":1,"items":[{"id":7,"name":"masked","key":"new-**********efix","group":"default","status":1}]}}`, nil), nil
		case "/api/token/7/key":
			singleRevealRequested = true
			require.Fail(t, "ordinary key refresh must not reveal masked keys one by one")
			return nil, nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	snapshot, err := FetchUpstreamKeys(context.Background(), client, "https://upstream.test", UpstreamProviderNewAPI, UpstreamCredential{
		AuthType: model.UpstreamAuthTypeAccessToken,
		Username: "42",
		Password: "management-token",
	})
	require.NoError(t, err)
	require.Len(t, snapshot.Keys, 1)
	assert.False(t, singleRevealRequested)
	assert.Equal(t, "new-****...efix", snapshot.Keys[0].MaskedKey)
	assert.Empty(t, snapshot.Keys[0].KeyFingerprint)
}

func TestMaskedUpstreamKeyCouldMatchAnyUsesProviderSemantics(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		maskedKey  string
		localKeys  []string
		couldMatch bool
	}{
		{
			name:       "new-api local sk prefix is optional",
			provider:   UpstreamProviderNewAPI,
			maskedKey:  "abcd****...wxyz",
			localKeys:  []string{"sk-abcd-middle-wxyz"},
			couldMatch: true,
		},
		{
			name:       "new-api masked sk prefix is optional",
			provider:   UpstreamProviderNewAPI,
			maskedKey:  "sk-abcd...wxyz",
			localKeys:  []string{"abcd-middle-wxyz"},
			couldMatch: true,
		},
		{
			name:       "sub2api keeps sk prefix exact",
			provider:   UpstreamProviderSub2API,
			maskedKey:  "abcd****...wxyz",
			localKeys:  []string{"sk-abcd-middle-wxyz"},
			couldMatch: false,
		},
		{
			name:       "visible suffix excludes candidate",
			provider:   UpstreamProviderNewAPI,
			maskedKey:  "abcd****...wxyz",
			localKeys:  []string{"sk-abcd-middle-nope"},
			couldMatch: false,
		},
		{
			name:       "fully masked key cannot be safely excluded",
			provider:   UpstreamProviderNewAPI,
			maskedKey:  "****",
			localKeys:  []string{"sk-any-local-key"},
			couldMatch: true,
		},
		{
			name:       "no local candidates need no reveal",
			provider:   UpstreamProviderNewAPI,
			maskedKey:  "****",
			localKeys:  nil,
			couldMatch: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.couldMatch, maskedUpstreamKeyCouldMatchAny(test.provider, test.maskedKey, test.localKeys))
		})
	}
}

func TestFetchUpstreamKeysForLinkUsesSub2APIRevealRouteForMatchingCandidateOnly(t *testing.T) {
	const accessToken = "sub2-link-access-token"
	revealedKeyIDs := make([]string, 0, 1)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/api/v1/keys":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"page":1,"page_size":100,"total":2,"pages":1,"items":[{"id":3,"name":"matching","key":"sk-m****...cret","group_id":5,"status":"active"},{"id":4,"name":"prefix differs","key":"stri****...cret","group_id":5,"status":"active"}]}}`, nil), nil
		case "/api/v1/keys/3":
			revealedKeyIDs = append(revealedKeyIDs, "3")
			require.Equal(t, http.MethodGet, request.Method)
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-match-secret"}}`, nil), nil
		case "/api/v1/keys/4":
			revealedKeyIDs = append(revealedKeyIDs, "4")
			return jsonResponse(http.StatusTooManyRequests, `{"message":"too many requests"}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	snapshot, err := FetchUpstreamKeysForLink(
		context.Background(),
		client,
		"https://sub2.test",
		UpstreamProviderSub2API,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Password: accessToken},
		[]string{"sk-match-secret"},
	)
	require.NoError(t, err)
	require.Len(t, snapshot.Keys, 2)
	assert.Equal(t, []string{"3"}, revealedKeyIDs)
	assert.Equal(t, UpstreamKeyFingerprintForProvider(UpstreamProviderSub2API, "https://sub2.test", "sk-match-secret"), snapshot.Keys[0].KeyFingerprint)
	assert.Empty(t, snapshot.Keys[1].KeyFingerprint)
}

func TestFetchSub2APIKeysLoadsEveryPage(t *testing.T) {
	requestedPages := make([]string, 0, 2)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v1/keys", r.URL.Path)
		page := r.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		switch page {
		case "1":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"page":1,"page_size":1,"total":2,"pages":2,"items":[{"id":1,"name":"first","key":"sk-first-secret","group_id":5,"status":"active"}]}}`, nil), nil
		case "2":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"page":2,"page_size":1,"total":2,"pages":2,"items":[{"id":2,"name":"second","key":"sk-second-secret","group_id":6,"status":"active"}]}}`, nil), nil
		default:
			return jsonResponse(http.StatusBadRequest, `{}`, nil), nil
		}
	})}

	keys, err := fetchSub2APIKeys(context.Background(), client, "https://sub2.test", nil)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, []string{"1", "2"}, requestedPages)
	assert.Equal(t, int64(2), keys[1].ID)
}

func TestFetchNewAPIUpstreamSnapshotUsesUpstreamQuotaPerUnit(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/status":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":100}}`, nil), nil
		case "/api/user/login":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root"}}`, nil), nil
		case "/api/user/self":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42,"username":"root","quota":1000}}`, nil), nil
		case "/api/user/self/groups":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{}}`, nil), nil
		case "/api/token":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":0,"items":[]}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	snapshot, err := fetchNewAPIUpstreamSnapshot(context.Background(), client, "https://upstream.test", UpstreamCredential{Username: "root", Password: "secret"})
	require.NoError(t, err)
	assert.InDelta(t, 10, snapshot.Balance, 0.000001)
}

func TestFetchNewAPIFullKeysWithSessionBatchesMultipleIDs(t *testing.T) {
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/token/batch/keys", r.URL.Path)
		require.Equal(t, "Bearer management-token", r.Header.Get("Authorization"))

		var payload struct {
			IDs []int64 `json:"ids"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		assert.Equal(t, []int64{7, 8, 9}, payload.IDs)
		return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"7":"sk-seven","8":"sk-eight","9":"sk-nine"}}}`, nil), nil
	})}

	keys, err := fetchNewAPIFullKeysWithSession(
		context.Background(),
		client,
		"https://upstream.test",
		map[string]string{"Authorization": "Bearer management-token"},
		[]int64{7, 8, 9},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, map[int64]string{7: "sk-seven", 8: "sk-eight", 9: "sk-nine"}, keys)
}

func TestFetchNewAPIFullKeysWithSessionSplitsRequestsAtBatchLimit(t *testing.T) {
	keyIDs := make([]int64, 205)
	for i := range keyIDs {
		keyIDs[i] = int64(i + 1)
	}

	requestedBatches := make([][]int64, 0, 3)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/token/batch/keys", r.URL.Path)
		var payload struct {
			IDs []int64 `json:"ids"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		requestedBatches = append(requestedBatches, append([]int64(nil), payload.IDs...))

		responseKeys := make(map[int64]string, len(payload.IDs))
		for _, keyID := range payload.IDs {
			responseKeys[keyID] = fmt.Sprintf("sk-%d", keyID)
		}
		body, err := common.Marshal(map[string]any{
			"success": true,
			"data":    map[string]any{"keys": responseKeys},
		})
		require.NoError(t, err)
		return jsonResponse(http.StatusOK, string(body), nil), nil
	})}

	keys, err := fetchNewAPIFullKeysWithSession(context.Background(), client, "https://upstream.test", nil, keyIDs)
	require.NoError(t, err)
	require.Len(t, requestedBatches, 3)
	assert.Equal(t, keyIDs[:100], requestedBatches[0])
	assert.Equal(t, keyIDs[100:200], requestedBatches[1])
	assert.Equal(t, keyIDs[200:], requestedBatches[2])
	require.Len(t, keys, len(keyIDs))
	assert.Equal(t, "sk-1", keys[1])
	assert.Equal(t, "sk-100", keys[100])
	assert.Equal(t, "sk-101", keys[101])
	assert.Equal(t, "sk-205", keys[205])
}

func TestFetchNewAPIFullKeysWithSessionFallsBackWhenBatchEndpointIsUnsupported(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			batchRequestCount := 0
			singleKeyRequests := make([]int64, 0, 2)
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/token/batch/keys":
					batchRequestCount++
					var payload struct {
						IDs []int64 `json:"ids"`
					}
					require.NoError(t, common.DecodeJson(r.Body, &payload))
					assert.Equal(t, []int64{11, 12}, payload.IDs)
					return jsonResponse(status, `{}`, nil), nil
				case "/api/token/11/key":
					singleKeyRequests = append(singleKeyRequests, 11)
					return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-eleven"}}`, nil), nil
				case "/api/token/12/key":
					singleKeyRequests = append(singleKeyRequests, 12)
					return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-twelve"}}`, nil), nil
				default:
					require.Failf(t, "unexpected upstream request", "%s %s", r.Method, r.URL.String())
					return nil, nil
				}
			})}

			keys, err := fetchNewAPIFullKeysWithSession(context.Background(), client, "https://upstream.test", nil, []int64{11, 12})
			require.NoError(t, err)
			assert.Equal(t, 1, batchRequestCount)
			assert.Equal(t, []int64{11, 12}, singleKeyRequests)
			assert.Equal(t, map[int64]string{11: "sk-eleven", 12: "sk-twelve"}, keys)
		})
	}
}

func TestFetchNewAPIFullKeysWithSessionDoesNotFallBackOnRateLimit(t *testing.T) {
	batchRequestCount := 0
	singleRevealRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/token/batch/keys":
			batchRequestCount++
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(r.Body, &payload))
			assert.Equal(t, []int64{21, 22}, payload.IDs)
			return jsonResponse(http.StatusTooManyRequests, `{"success":false,"message":"rate limited"}`, nil), nil
		case "/api/token/21/key", "/api/token/22/key":
			singleRevealRequested = true
			require.Fail(t, "rate limits must not trigger single-key fallback requests")
			return nil, nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	keys, err := fetchNewAPIFullKeysWithSession(context.Background(), client, "https://upstream.test", nil, []int64{21, 22})
	require.Error(t, err)
	assert.Nil(t, keys)
	assert.Equal(t, http.StatusTooManyRequests, UpstreamHTTPStatus(err))
	assert.Equal(t, 1, batchRequestCount)
	assert.False(t, singleRevealRequested)
}

func TestFetchNewAPIFullKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/user/login":
			headers := make(http.Header)
			headers.Add("Set-Cookie", "session=ok; Path=/")
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"id":42}}`, headers), nil
		case "/api/token/7/key":
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			cookie, err := r.Cookie("session")
			require.NoError(t, err)
			assert.Equal(t, "ok", cookie.Value)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-new-api-full-key"}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	key, err := FetchUpstreamFullKey(context.Background(), client, "https://upstream.test", UpstreamProviderNewAPI, UpstreamCredential{Username: "root", Password: "secret"}, 7)
	require.NoError(t, err)
	assert.Equal(t, "sk-new-api-full-key", key)
}

func TestFetchNewAPIFullKeyUsesManagementTokenWhenTurnstileIsRequired(t *testing.T) {
	const managementToken = "management-secret"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/user/login":
			return jsonResponse(http.StatusOK, `{"success":false,"message":"Turnstile token is empty"}`, nil), nil
		case "/api/token/7/key":
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "Bearer "+managementToken, r.Header.Get("Authorization"))
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-new-api-full-key"}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	key, err := FetchUpstreamFullKey(context.Background(), client, "https://upstream.test", UpstreamProviderNewAPI, UpstreamCredential{Username: "42", Password: managementToken}, 7)
	require.NoError(t, err)
	assert.Equal(t, "sk-new-api-full-key", key)
}

func TestFetchSub2APIFullKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"access_token":"token-123"}}`, nil), nil
		case "/api/v1/keys/9":
			require.Equal(t, "Bearer token-123", r.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-sub2-full-key"}}`, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		}
	})}

	key, err := FetchUpstreamFullKey(context.Background(), client, "https://sub2.test", UpstreamProviderSub2API, UpstreamCredential{Username: "owner@example.com", Password: "secret"}, 9)
	require.NoError(t, err)
	assert.Equal(t, "sk-sub2-full-key", key)
}

func TestFetchSub2APIFullKeyUsesExplicitAccessToken(t *testing.T) {
	const accessToken = "sub2-access-secret"
	loginRequested := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/login" {
			loginRequested = true
			return jsonResponse(http.StatusInternalServerError, `{}`, nil), nil
		}
		require.Equal(t, "/api/v1/keys/9", r.URL.Path)
		require.Equal(t, "Bearer "+accessToken, r.Header.Get("Authorization"))
		return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-sub2-full-key"}}`, nil), nil
	})}

	key, err := FetchUpstreamFullKey(context.Background(), client, "https://sub2.test", UpstreamProviderSub2API, UpstreamCredential{
		AuthType: model.UpstreamAuthTypeAccessToken,
		Username: "owner@example.com",
		Password: accessToken,
	}, 9)
	require.NoError(t, err)
	assert.False(t, loginRequested)
	assert.Equal(t, "sk-sub2-full-key", key)
}

func TestFetchUpstreamModelsFromNewAPI(t *testing.T) {
	requestedKeyIDs := make([]int64, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/token/batch/keys":
			require.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			requestedKeyIDs = append(requestedKeyIDs, payload.IDs...)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"2":"sk-default","3":"sk-vip"}}}`, nil), nil
		case "/v1/models":
			switch request.Header.Get("Authorization") {
			case "Bearer sk-vip":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o"},{"id":"vip-only"}]}`, nil), nil
			case "Bearer sk-default":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`, nil), nil
			default:
				require.Failf(t, "unexpected model credential", "%q", request.Header.Get("Authorization"))
				return nil, nil
			}
		case "/api/ratio_config":
			require.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"model_ratio":{"gpt-4o":0,"not-exposed":9},"completion_ratio":{"gpt-4o":2},"cache_ratio":{"gpt-4o-mini":0.5},"create_cache_ratio":{"gpt-4o-mini":1.25},"model_price":{"vip-only":0.02}}}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	groupDefault := int64(1)
	groupVIP := int64(2)
	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://upstream.test",
		UpstreamProviderNewAPI,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Username: "42", Password: "management-token"},
		[]UpstreamKey{
			{ID: 1, Group: "default", GroupID: &groupDefault, Status: "disabled"},
			{ID: 2, Group: "default", GroupID: &groupDefault, Status: "enabled"},
			{ID: 3, Group: "vip", GroupID: &groupVIP, Status: "enabled"},
		},
		"vip",
	)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, requestedKeyIDs)
	require.Len(t, models, 3)
	assert.Equal(t, []string{"gpt-4o", "gpt-4o-mini", "vip-only"}, []string{models[0].ID, models[1].ID, models[2].ID})

	require.Len(t, models[0].Pricing, 1)
	require.NotNil(t, models[0].Pricing[0].ModelRatio)
	assert.Zero(t, *models[0].Pricing[0].ModelRatio)
	require.NotNil(t, models[0].Pricing[0].CompletionRatio)
	assert.Equal(t, 2.0, *models[0].Pricing[0].CompletionRatio)
	require.Len(t, models[1].Pricing, 1)
	require.NotNil(t, models[1].Pricing[0].CacheRatio)
	assert.Equal(t, 0.5, *models[1].Pricing[0].CacheRatio)
	require.Len(t, models[2].Pricing, 1)
	require.NotNil(t, models[2].Pricing[0].ModelPrice)
	assert.Equal(t, 0.02, *models[2].Pricing[0].ModelPrice)
	for _, item := range models {
		assert.NotEqual(t, "not-exposed", item.ID)
	}

	encoded, err := common.Marshal(models)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"model_ratio":0`)
	assert.NotContains(t, string(encoded), "management-token")
	assert.NotContains(t, string(encoded), "sk-vip")
}

func TestFetchUpstreamModelsProbesAllEligibleKeysAndSkipsFailures(t *testing.T) {
	requestedKeyIDs := make([]int64, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/token/batch/keys":
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			requestedKeyIDs = append(requestedKeyIDs, payload.IDs...)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"1":"sk-first","2":"sk-second"}}}`, nil), nil
		case "/v1/models":
			switch request.Header.Get("Authorization") {
			case "Bearer sk-first":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o"}]}`, nil), nil
			case "Bearer sk-second":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"claude-sonnet-4-5"}]}`, nil), nil
			default:
				require.Failf(t, "unexpected model credential", "%q", request.Header.Get("Authorization"))
				return nil, nil
			}
		case "/api/ratio_config":
			return jsonResponse(http.StatusForbidden, `{"success":false,"message":"not exposed"}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://upstream.test",
		UpstreamProviderNewAPI,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Username: "42", Password: "management-token"},
		[]UpstreamKey{
			{ID: 1, Group: "default", Status: "enabled"},
			{ID: 2, Group: "default", Status: "enabled"},
			{ID: 3, Group: "default", Status: "disabled"},
		},
		"default",
	)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, requestedKeyIDs)
	assert.Equal(t, []string{"claude-sonnet-4-5", "gpt-4o"}, []string{models[0].ID, models[1].ID})
}

func TestFetchUpstreamModelsSupportsSub2APIPricingShapeVariants(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/v1/keys/9":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-sub2-model"}}`, nil), nil
		case "/v1/models":
			return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o"},{"id":"claude-sonnet-4-5"}]}`, nil), nil
		case "/api/v1/channels/available":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"channels":[{"name":"Variant Channel","platforms":[{"platform":"openai","supported_models":["gpt-4o",{"model":"claude-sonnet-4-5","billing_mode":"token","input_price":0.000002,"output_price":0.000008}]}]}]}}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://sub2.test",
		UpstreamProviderSub2API,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Password: "user-access-token"},
		[]UpstreamKey{{ID: 9, Status: "active"}},
		"",
	)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "claude-sonnet-4-5", models[0].ID)
	require.Len(t, models[0].Pricing, 1)
	assert.Equal(t, "token", models[0].Pricing[0].BillingMode)
	require.NotNil(t, models[0].Pricing[0].InputPrice)
	assert.Equal(t, 0.000002, *models[0].Pricing[0].InputPrice)
	assert.Equal(t, "gpt-4o", models[1].ID)
	require.Len(t, models[1].Pricing, 1)
	assert.Equal(t, "openai", models[1].Pricing[0].Platform)
	assert.Empty(t, models[1].Pricing[0].BillingMode)
}

func TestFetchUpstreamModelsKeepsModelsWhenNewAPIPricingIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/token/batch/keys":
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			assert.Equal(t, []int64{1}, payload.IDs)
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"keys":{"1":"sk-model"}}}`, nil), nil
		case "/v1/models":
			return jsonResponse(http.StatusOK, `{"data":[{"id":"gpt-4o"}]}`, nil), nil
		case "/api/ratio_config":
			return jsonResponse(http.StatusForbidden, `{"success":false,"message":"ratio configuration is not exposed"}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://upstream.test",
		UpstreamProviderNewAPI,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Username: "42", Password: "management-token"},
		[]UpstreamKey{{ID: 1, Group: "default", Status: "enabled"}},
		"",
	)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "gpt-4o", models[0].ID)
	assert.Empty(t, models[0].Pricing)
}

func TestFetchUpstreamModelsFromSub2API(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/v1/keys/9":
			require.Equal(t, "Bearer user-access-token", request.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-sub2-model"}}`, nil), nil
		case "/v1/models":
			require.Equal(t, "Bearer sk-sub2-model", request.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"data":[{"id":"claude-sonnet-4-5"},{"id":"gpt-4o"}]}`, nil), nil
		case "/api/v1/channels/available":
			require.Equal(t, "Bearer user-access-token", request.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":[{"name":"Primary Claude","platforms":[{"platform":"anthropic","supported_models":[{"name":"claude-sonnet-4-5","pricing":{"billing_mode":"token","input_price":0.000003,"output_price":0.000015,"cache_write_price":0,"cache_read_price":0.0000003,"intervals":[{"min_tokens":0,"max_tokens":200000,"tier_label":"standard","input_price":0.000003,"output_price":0.000015}]}}]}]},{"name":"Backup Claude","platforms":[{"platform":"anthropic","supported_models":[{"name":"claude-sonnet-4-5","platform":"anthropic-compatible","pricing":{"billing_mode":"request","per_request_price":0.02}}]}]},{"name":"Unexposed","platforms":[{"platform":"openai","supported_models":[{"name":"not-exposed","pricing":{"billing_mode":"token","input_price":1}}]}]}]}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://sub2.test",
		UpstreamProviderSub2API,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Password: "user-access-token"},
		[]UpstreamKey{{ID: 9, Group: "default", Status: "active"}},
		"",
	)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "claude-sonnet-4-5", models[0].ID)
	require.Len(t, models[0].Pricing, 2)
	assert.Equal(t, "Backup Claude", models[0].Pricing[0].ChannelName)
	assert.Equal(t, "request", models[0].Pricing[0].BillingMode)
	require.NotNil(t, models[0].Pricing[0].PerRequestPrice)
	assert.Equal(t, 0.02, *models[0].Pricing[0].PerRequestPrice)
	assert.Equal(t, "Primary Claude", models[0].Pricing[1].ChannelName)
	assert.Equal(t, "anthropic", models[0].Pricing[1].Platform)
	require.NotNil(t, models[0].Pricing[1].CacheWritePrice)
	assert.Zero(t, *models[0].Pricing[1].CacheWritePrice)
	require.Len(t, models[0].Pricing[1].Intervals, 1)
	require.NotNil(t, models[0].Pricing[1].Intervals[0].MaxTokens)
	assert.Equal(t, 200000, *models[0].Pricing[1].Intervals[0].MaxTokens)
	assert.Equal(t, "gpt-4o", models[1].ID)
	assert.Empty(t, models[1].Pricing)
}

func TestFetchUpstreamModelsAllowsEmptyModelListAndMissingSub2APIPricingEndpoint(t *testing.T) {
	pricingRequested := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/v1/keys/9":
			return jsonResponse(http.StatusOK, `{"code":0,"message":"success","data":{"key":"sk-sub2-model"}}`, nil), nil
		case "/v1/models":
			return jsonResponse(http.StatusOK, `{"data":[]}`, nil), nil
		case "/api/v1/channels/available":
			pricingRequested = true
			return jsonResponse(http.StatusNotFound, `{}`, nil), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}

	models, err := FetchUpstreamModels(
		context.Background(),
		client,
		"https://sub2.test",
		UpstreamProviderSub2API,
		UpstreamCredential{AuthType: model.UpstreamAuthTypeAccessToken, Password: "user-access-token"},
		[]UpstreamKey{{ID: 9, Status: "active"}},
		"",
	)
	require.NoError(t, err)
	assert.Empty(t, models)
	assert.False(t, pricingRequested)
}

func TestDoUpstreamJSONCollapsesHTMLHTTPError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader(
				`<html><head><title>403 Forbidden</title></head><body>` + strings.Repeat("blocked", 500) + `</body></html>`,
			)),
		}, nil
	})}

	err := doUpstreamJSON(context.Background(), client, http.MethodGet, "https://example.com/api/v1/groups/available", nil, nil, nil)

	require.Error(t, err)
	assert.Equal(t, "upstream returned HTTP 403: Forbidden", err.Error())
	assert.Equal(t, http.StatusForbidden, UpstreamHTTPStatus(err))
}

func TestDoUpstreamJSONUsesJSONMessageForHTTPError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnauthorized, `{"success":false,"message":"invalid access token"}`, nil), nil
	})}
	var target newAPIEnvelope

	err := doUpstreamJSON(context.Background(), client, http.MethodGet, "https://example.com/api/user/self", nil, nil, &target)

	require.Error(t, err)
	assert.Equal(t, "invalid access token", target.Message)
	assert.Equal(t, "upstream returned HTTP 401: invalid access token", err.Error())
	assert.Equal(t, http.StatusUnauthorized, UpstreamHTTPStatus(err))
}

func TestUpstreamHTTPStatusFindsWrappedError(t *testing.T) {
	err := fmt.Errorf("fetch groups: %w", &UpstreamHTTPError{StatusCode: http.StatusTooManyRequests, Message: "rate limited"})

	assert.Equal(t, http.StatusTooManyRequests, UpstreamHTTPStatus(err))
}
