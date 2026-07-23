package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type upstreamGroupRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn upstreamGroupRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func upstreamGroupJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func setupUpstreamKeyGroupTest(t *testing.T, provider string, username string, snapshot UpstreamSnapshot, transport http.RoundTripper) (*model.UpstreamChannel, *gorm.DB) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalHTTPClient := httpClient
	originalCryptoSecret := common.CryptoSecret

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upstream-key-group.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UpstreamChannel{}))
	model.DB = db
	model.LOG_DB = db
	common.CryptoSecret = "upstream-key-group-test-secret"
	httpClient = &http.Client{Transport: transport}
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		httpClient = originalHTTPClient
		common.CryptoSecret = originalCryptoSecret
	})

	passwordCiphertext, err := common.EncryptSecret("upstream-channel-password", "management-secret")
	require.NoError(t, err)
	snapshotJSON, err := common.Marshal(snapshot)
	require.NoError(t, err)
	row := &model.UpstreamChannel{
		Name:                "group-test",
		BaseURL:             "https://upstream.test",
		BaseURLHash:         model.UpstreamBaseURLHash("https://upstream.test"),
		Provider:            provider,
		AuthType:            model.UpstreamAuthTypeAccessToken,
		Username:            username,
		PasswordCiphertext:  passwordCiphertext,
		SnapshotJSON:        string(snapshotJSON),
		Multiplier:          1,
		AutoRefreshInterval: 300,
		Status:              model.UpstreamChannelStatusReady,
	}
	require.NoError(t, db.Create(row).Error)
	return row, db
}

func TestUpdateUpstreamChannelKeyGroupNewAPIPreservesFieldsAndRefreshesAuthoritativeSnapshot(t *testing.T) {
	oldModelRatio := 1.25
	previous := UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Keys:     []UpstreamKey{{ID: 7, Name: "before", Group: "default"}},
		Groups:   []UpstreamGroup{{Name: "default", Ratio: 1}, {Name: "premium", Ratio: 0.45}},
		Ratios:   map[string]float64{"default": 1},
		Models: []UpstreamModel{{
			ID: "gpt-4o",
			Pricing: []UpstreamModelPricing{{
				Source:     UpstreamProviderNewAPI,
				ModelRatio: &oldModelRatio,
			}},
		}},
	}
	const detailData = `{"id":7,"user_id":42,"key":"sk-masked-value","status":1,"name":"primary","created_time":101,"accessed_time":202,"expired_time":-1,"remain_quota":998877,"unlimited_quota":false,"model_limits_enabled":true,"model_limits":"gpt-4o,gpt-4.1","allow_ips":"203.0.113.1","group":"default","cross_group_retry":true,"tags":["prod"],"quota_reset":{"period":"monthly","amount":99}}`
	var expectedToken map[string]json.RawMessage
	require.NoError(t, common.Unmarshal([]byte(detailData), &expectedToken))

	requests := make([]string, 0)
	transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		if request.URL.Path != "/api/status" {
			require.Equal(t, "Bearer management-secret", request.Header.Get("Authorization"))
			require.Equal(t, "42", request.Header.Get("New-Api-User"))
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/token/7":
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"data":`+detailData+`}`), nil
		case request.Method == http.MethodPut && request.URL.Path == "/api/token/":
			var actualToken map[string]json.RawMessage
			require.NoError(t, common.DecodeJson(request.Body, &actualToken))
			require.Len(t, actualToken, len(expectedToken))
			for name, expected := range expectedToken {
				actual, exists := actualToken[name]
				require.Truef(t, exists, "missing preserved token field %s", name)
				if name == "group" {
					assert.JSONEq(t, `"premium"`, string(actual))
					continue
				}
				assert.JSONEq(t, string(expected), string(actual), "field %s changed", name)
			}
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"message":""}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/status":
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":100}}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/user/self/groups":
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"data":{"default":{"ratio":1},"premium":{"ratio":0.45,"desc":"Premium"}}}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/token":
			require.Equal(t, "1", request.URL.Query().Get("p"))
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":100,"total":1,"items":[{"id":7,"name":"authoritative","key":"sk-authoritative-secret","group":"premium","status":1,"remain_quota":900,"used_quota":100,"unlimited_quota":false}]}}`), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return upstreamGroupJSONResponse(http.StatusNotFound, `{}`), nil
		}
	})
	row, db := setupUpstreamKeyGroupTest(t, UpstreamProviderNewAPI, "42", previous, transport)

	priority := int64(73)
	localChannel := &model.Channel{
		Name:     "ordinary-local-channel",
		Key:      "sk-authoritative-secret",
		Status:   common.ChannelStatusEnabled,
		BaseURL:  &row.BaseURL,
		Models:   "gpt-4o",
		Group:    "local-user-group",
		Priority: &priority,
	}
	require.NoError(t, db.Create(localChannel).Error)

	updatedRow, snapshot, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, 7, UpstreamKeyGroupUpdate{Group: " premium "})
	require.NoError(t, err)
	require.NotNil(t, updatedRow)
	require.Len(t, snapshot.Keys, 1)
	assert.Equal(t, "premium", snapshot.Keys[0].Group)
	assert.Equal(t, "authoritative", snapshot.Keys[0].Name)
	assert.True(t, snapshot.Keys[0].Imported)
	assert.True(t, snapshot.Keys[0].Active)
	assert.Equal(t, 0.45, snapshot.Ratios["premium"])
	require.Len(t, snapshot.Models, 1)
	assert.Equal(t, "gpt-4o", snapshot.Models[0].ID)
	require.NotNil(t, snapshot.Models[0].Pricing[0].ModelRatio)
	assert.Equal(t, oldModelRatio, *snapshot.Models[0].Pricing[0].ModelRatio)
	assert.Equal(t, []string{
		"GET /api/token/7",
		"PUT /api/token/",
		"GET /api/status",
		"GET /api/token?p=1&page_size=100",
		"GET /api/user/self/groups",
	}, requests)

	stored, err := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, err)
	var storedSnapshot UpstreamSnapshot
	require.NoError(t, common.UnmarshalJsonStr(stored.SnapshotJSON, &storedSnapshot))
	require.Len(t, storedSnapshot.Keys, 1)
	assert.Equal(t, "premium", storedSnapshot.Keys[0].Group)
	assert.Equal(t, "authoritative", storedSnapshot.Keys[0].Name)

	var unchanged model.Channel
	require.NoError(t, db.First(&unchanged, localChannel.Id).Error)
	assert.Equal(t, "ordinary-local-channel", unchanged.Name)
	assert.Equal(t, "local-user-group", unchanged.Group)
	assert.Equal(t, "gpt-4o", unchanged.Models)
	require.NotNil(t, unchanged.Priority)
	assert.Equal(t, priority, *unchanged.Priority)
	assert.Equal(t, common.ChannelStatusEnabled, unchanged.Status)
}

func TestUpdateUpstreamChannelKeyGroupNewAPIRejectsBusinessFailureAtHTTP200(t *testing.T) {
	previous := UpstreamSnapshot{
		Provider: UpstreamProviderNewAPI,
		Keys:     []UpstreamKey{{ID: 7, Group: "default"}},
		Groups:   []UpstreamGroup{{Name: "premium", Ratio: 0.45}},
		Ratios:   map[string]float64{"premium": 0.45},
		Models:   []UpstreamModel{},
	}
	requestCount := 0
	transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		switch request.Method + " " + request.URL.Path {
		case "GET /api/token/7":
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":true,"data":{"id":7,"name":"primary","group":"default","remain_quota":100,"unlimited_quota":false,"expired_time":-1}}`), nil
		case "PUT /api/token/":
			return upstreamGroupJSONResponse(http.StatusOK, `{"success":false,"message":"group update denied"}`), nil
		default:
			require.Failf(t, "unexpected request after business failure", "%s %s", request.Method, request.URL.String())
			return upstreamGroupJSONResponse(http.StatusNotFound, `{}`), nil
		}
	})
	row, _ := setupUpstreamKeyGroupTest(t, UpstreamProviderNewAPI, "42", previous, transport)
	originalSnapshotJSON := row.SnapshotJSON

	_, _, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, 7, UpstreamKeyGroupUpdate{Group: "premium"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group update denied")
	assert.Equal(t, 2, requestCount)
	stored, loadErr := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, loadErr)
	assert.Equal(t, originalSnapshotJSON, stored.SnapshotJSON)
}

func TestUpdateUpstreamChannelKeyGroupSub2APIUsesGroupIDAndAutoSnapshotProvider(t *testing.T) {
	oldGroupID := int64(3)
	newGroupID := int64(9)
	previous := UpstreamSnapshot{
		Provider: UpstreamProviderSub2API,
		Keys:     []UpstreamKey{{ID: 11, Name: "before", GroupID: &oldGroupID}},
		Groups:   []UpstreamGroup{{ID: oldGroupID, Name: "Default", Ratio: 1}, {ID: newGroupID, Name: "Premium", Ratio: 0.8}},
		Ratios:   map[string]float64{"3": 1},
		Models:   []UpstreamModel{},
	}
	requests := make([]string, 0)
	transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		require.Equal(t, "Bearer management-secret", request.Header.Get("Authorization"))
		switch {
		case request.Method == http.MethodPut && request.URL.Path == "/api/v1/keys/11":
			var payload map[string]json.RawMessage
			require.NoError(t, common.DecodeJson(request.Body, &payload))
			require.Len(t, payload, 1)
			var actualGroupID int64
			require.NoError(t, common.Unmarshal(payload["group_id"], &actualGroupID))
			assert.Equal(t, newGroupID, actualGroupID)
			return upstreamGroupJSONResponse(http.StatusOK, `{"code":0,"message":"success","data":{}}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/keys":
			return upstreamGroupJSONResponse(http.StatusOK, `{"code":0,"message":"success","data":{"page":1,"page_size":100,"total":1,"pages":1,"items":[{"id":11,"name":"authoritative-sub2","key":"sk-sub2-authoritative","group_id":9,"status":"active","quota":100,"quota_used":4}]}}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/groups/available":
			return upstreamGroupJSONResponse(http.StatusOK, `{"code":0,"message":"success","data":[{"id":3,"name":"Default","rate_multiplier":1},{"id":9,"name":"Premium","description":"Fast","platform":"claude","rate_multiplier":0.8}]}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/groups/rates":
			return upstreamGroupJSONResponse(http.StatusOK, `{"code":0,"message":"success","data":{"3":1,"9":0.75}}`), nil
		default:
			require.Failf(t, "unexpected upstream request", "%s %s", request.Method, request.URL.String())
			return upstreamGroupJSONResponse(http.StatusNotFound, `{}`), nil
		}
	})
	row, _ := setupUpstreamKeyGroupTest(t, UpstreamProviderAuto, "", previous, transport)

	_, snapshot, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, 11, UpstreamKeyGroupUpdate{Group: "ignored-name", GroupID: &newGroupID})
	require.NoError(t, err)
	require.Len(t, snapshot.Keys, 1)
	require.NotNil(t, snapshot.Keys[0].GroupID)
	assert.Equal(t, newGroupID, *snapshot.Keys[0].GroupID)
	assert.Equal(t, "Premium", snapshot.Keys[0].Group)
	assert.Equal(t, 0.75, snapshot.Ratios["9"])
	assert.Equal(t, []string{
		"PUT /api/v1/keys/11",
		"GET /api/v1/keys?page=1&page_size=100",
		"GET /api/v1/groups/available",
		"GET /api/v1/groups/rates",
	}, requests)

	stored, loadErr := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, loadErr)
	assert.Equal(t, UpstreamProviderAuto, stored.Provider)
	var storedSnapshot UpstreamSnapshot
	require.NoError(t, common.UnmarshalJsonStr(stored.SnapshotJSON, &storedSnapshot))
	assert.Equal(t, UpstreamProviderSub2API, storedSnapshot.Provider)
	require.Len(t, storedSnapshot.Keys, 1)
	assert.Equal(t, "Premium", storedSnapshot.Keys[0].Group)
}

func TestUpdateUpstreamChannelKeyGroupSub2APIRejectsBusinessFailureAtHTTP200(t *testing.T) {
	groupID := int64(9)
	previous := UpstreamSnapshot{
		Provider: UpstreamProviderSub2API,
		Keys:     []UpstreamKey{{ID: 11, GroupID: &groupID}},
		Groups:   []UpstreamGroup{{ID: groupID, Name: "Premium", Ratio: 0.8}},
		Ratios:   map[string]float64{"9": 0.8},
		Models:   []UpstreamModel{},
	}
	requestCount := 0
	transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/api/v1/keys/11", request.URL.Path)
		return upstreamGroupJSONResponse(http.StatusOK, `{"code":409,"message":"group is unavailable","data":{}}`), nil
	})
	row, _ := setupUpstreamKeyGroupTest(t, UpstreamProviderSub2API, "", previous, transport)
	originalSnapshotJSON := row.SnapshotJSON

	_, _, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, 11, UpstreamKeyGroupUpdate{GroupID: &groupID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group is unavailable")
	assert.Equal(t, 1, requestCount)
	stored, loadErr := model.GetUpstreamChannelByID(row.Id)
	require.NoError(t, loadErr)
	assert.Equal(t, originalSnapshotJSON, stored.SnapshotJSON)
}

func TestUpdateUpstreamChannelKeyGroupRejectsOtherProvider(t *testing.T) {
	previous := UpstreamSnapshot{
		Provider: UpstreamProviderOther,
		Keys:     []UpstreamKey{},
		Groups:   []UpstreamGroup{},
		Ratios:   map[string]float64{},
		Models:   []UpstreamModel{},
	}
	transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Failf(t, "unsupported provider must not be called", "%s %s", request.Method, request.URL.String())
		return upstreamGroupJSONResponse(http.StatusInternalServerError, `{}`), nil
	})
	row, _ := setupUpstreamKeyGroupTest(t, UpstreamProviderOther, "", previous, transport)

	_, _, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, 1, UpstreamKeyGroupUpdate{Group: "default"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider other")
	assert.Contains(t, err.Error(), "does not support")
}

func TestResolveUpstreamKeyGroupProviderRejectsUnresolvedAndUnknownProviders(t *testing.T) {
	provider, err := resolveUpstreamKeyGroupProvider(UpstreamProviderAuto, UpstreamProviderNewAPI)
	require.NoError(t, err)
	assert.Equal(t, UpstreamProviderNewAPI, provider)

	provider, err = resolveUpstreamKeyGroupProvider(" AUTO ", " SUB2API ")
	require.NoError(t, err)
	assert.Equal(t, UpstreamProviderSub2API, provider)

	_, err = resolveUpstreamKeyGroupProvider(UpstreamProviderAuto, UpstreamProviderOther)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not resolved")

	_, err = resolveUpstreamKeyGroupProvider("mystery", UpstreamProviderNewAPI)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown upstream provider")
	assert.Contains(t, err.Error(), "mystery")
}

func TestUpdateUpstreamChannelKeyGroupRejectsStaleSnapshotBeforeRemoteRequest(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		snapshot UpstreamSnapshot
		keyID    int64
		update   UpstreamKeyGroupUpdate
		message  string
	}{
		{
			name:     "missing key",
			provider: UpstreamProviderNewAPI,
			snapshot: UpstreamSnapshot{Provider: UpstreamProviderNewAPI, Keys: []UpstreamKey{{ID: 7}}, Groups: []UpstreamGroup{{Name: "premium"}}},
			keyID:    8,
			update:   UpstreamKeyGroupUpdate{Group: "premium"},
			message:  "refresh the key list first",
		},
		{
			name:     "missing new-api group",
			provider: UpstreamProviderNewAPI,
			snapshot: UpstreamSnapshot{Provider: UpstreamProviderNewAPI, Keys: []UpstreamKey{{ID: 7}}, Groups: []UpstreamGroup{{Name: "default"}}},
			keyID:    7,
			update:   UpstreamKeyGroupUpdate{Group: "premium"},
			message:  "refresh the group list first",
		},
		{
			name:     "missing sub2api group",
			provider: UpstreamProviderSub2API,
			snapshot: UpstreamSnapshot{Provider: UpstreamProviderSub2API, Keys: []UpstreamKey{{ID: 11}}, Groups: []UpstreamGroup{{ID: 3, Name: "Default"}}},
			keyID:    11,
			update:   UpstreamKeyGroupUpdate{GroupID: func() *int64 { value := int64(9); return &value }()},
			message:  "refresh the group list first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			transport := upstreamGroupRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestCount++
				require.Failf(t, "stale snapshot must block remote requests", "%s %s", request.Method, request.URL.String())
				return upstreamGroupJSONResponse(http.StatusInternalServerError, `{}`), nil
			})
			row, _ := setupUpstreamKeyGroupTest(t, tt.provider, "42", tt.snapshot, transport)

			_, _, err := UpdateUpstreamChannelKeyGroup(context.Background(), row.Id, tt.keyID, tt.update)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.message)
			assert.Zero(t, requestCount)
		})
	}
}
