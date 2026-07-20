package service

import (
	"context"
	"errors"
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
	requestedKeyIDs := make([]string, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/token/3/key":
			requestedKeyIDs = append(requestedKeyIDs, "3")
			require.Equal(t, "Bearer management-token", request.Header.Get("Authorization"))
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-vip"}}`, nil), nil
		case "/api/token/2/key":
			requestedKeyIDs = append(requestedKeyIDs, "2")
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-default"}}`, nil), nil
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
	assert.Equal(t, []string{"3", "2"}, requestedKeyIDs)
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
	requestedKeyIDs := make([]string, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/token/1/key":
			requestedKeyIDs = append(requestedKeyIDs, "1")
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-first"}}`, nil), nil
		case "/api/token/2/key":
			requestedKeyIDs = append(requestedKeyIDs, "2")
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-second"}}`, nil), nil
		case "/api/token/3/key":
			requestedKeyIDs = append(requestedKeyIDs, "3")
			return jsonResponse(http.StatusUnauthorized, `{"success":false,"message":"key unavailable"}`, nil), nil
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
	assert.Equal(t, []string{"1", "2"}, requestedKeyIDs)
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
		case "/api/token/1/key":
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"key":"sk-model"}}`, nil), nil
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
