package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-new-api-full-key"}}`, nil), nil
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
	assert.NotEmpty(t, snapshot.Keys[0].KeyFingerprint)
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
