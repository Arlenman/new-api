package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// UpstreamKeyGroupUpdate identifies the remote group selected for an upstream key.
// New API uses Group, while Sub2API uses GroupID.
type UpstreamKeyGroupUpdate struct {
	Group   string
	GroupID *int64
}

// UpdateUpstreamChannelKeyGroup changes one key's group on the remote upstream and
// persists a freshly fetched authoritative snapshot. It never updates ordinary
// local channel configuration.
func UpdateUpstreamChannelKeyGroup(ctx context.Context, upstreamChannelID int, keyID int64, update UpstreamKeyGroupUpdate) (*model.UpstreamChannel, UpstreamSnapshot, error) {
	if upstreamChannelID <= 0 {
		return nil, UpstreamSnapshot{}, errors.New("invalid upstream channel id")
	}
	if keyID <= 0 {
		return nil, UpstreamSnapshot{}, errors.New("invalid upstream key id")
	}

	lock := upstreamRefreshLock(upstreamChannelID)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(upstreamChannelID)
	if err != nil {
		return nil, UpstreamSnapshot{}, err
	}
	previous, err := loadUpstreamSnapshot(row)
	if err != nil {
		return row, UpstreamSnapshot{}, fmt.Errorf("load upstream snapshot: %w", err)
	}
	provider, err := resolveUpstreamKeyGroupProvider(row.Provider, previous.Provider)
	if err != nil {
		return row, previous, err
	}

	keyExists := false
	for _, key := range previous.Keys {
		if key.ID == keyID {
			keyExists = true
			break
		}
	}
	if !keyExists {
		return row, previous, fmt.Errorf("upstream key %d is not present in the latest snapshot; refresh the key list first", keyID)
	}

	switch provider {
	case UpstreamProviderNewAPI:
		group := strings.TrimSpace(update.Group)
		if group == "" {
			return row, previous, errors.New("new-api key group name is required")
		}
		groupExists := false
		for _, candidate := range previous.Groups {
			if strings.TrimSpace(candidate.Name) == group {
				groupExists = true
				break
			}
		}
		if !groupExists {
			return row, previous, fmt.Errorf("new-api group %q is not present in the latest snapshot; refresh the group list first", group)
		}
	case UpstreamProviderSub2API:
		if update.GroupID == nil || *update.GroupID <= 0 {
			return row, previous, errors.New("sub2api key group id must be a positive number")
		}
		groupExists := false
		for _, candidate := range previous.Groups {
			if candidate.ID == *update.GroupID {
				groupExists = true
				break
			}
		}
		if !groupExists {
			return row, previous, fmt.Errorf("sub2api group %d is not present in the latest snapshot; refresh the group list first", *update.GroupID)
		}
	}

	authType := row.EffectiveAuthType()
	if UpstreamCredentialRequiresUsername(provider, authType) && strings.TrimSpace(row.Username) == "" {
		return row, previous, errors.New("upstream username is not configured")
	}
	password, err := row.DecryptPassword()
	if err != nil {
		return row, previous, err
	}
	credential := UpstreamCredential{
		AuthType: authType,
		Username: row.Username,
		Password: password,
	}
	baseURL, err := NormalizeUpstreamBaseURL(row.BaseURL)
	if err != nil {
		return row, previous, err
	}

	requestCtx, cancel, client := refreshUpstreamClient(ctx)
	defer cancel()

	switch provider {
	case UpstreamProviderNewAPI:
		if err = updateNewAPIUpstreamKeyGroup(requestCtx, client, baseURL, credential, keyID, strings.TrimSpace(update.Group)); err != nil {
			return row, previous, err
		}
	case UpstreamProviderSub2API:
		if err = updateSub2APIUpstreamKeyGroup(requestCtx, client, baseURL, credential, keyID, *update.GroupID); err != nil {
			return row, previous, err
		}
	}

	// The fetch helpers do not acquire upstreamRefreshLock, so keeping the
	// per-channel lock here serializes mutation and refresh without lock re-entry.
	refreshedKeys, err := FetchUpstreamKeys(requestCtx, client, baseURL, provider, credential)
	if err != nil {
		return row, previous, fmt.Errorf("remote key group was updated, but refreshing authoritative upstream keys failed: %w", err)
	}
	refreshedGroups, err := FetchUpstreamGroups(requestCtx, client, baseURL, provider, credential)
	if err != nil {
		return row, previous, fmt.Errorf("remote key group was updated, but refreshing authoritative upstream groups failed: %w", err)
	}
	refreshed := previous
	refreshed.Provider = provider
	refreshed.Keys = refreshedKeys.Keys
	refreshed.Groups = refreshedGroups.Groups
	refreshed.Ratios = refreshedGroups.Ratios
	refreshed.RetrievedAt = refreshedGroups.RetrievedAt
	applyUpstreamGroupNames(refreshed.Keys, refreshed.Groups)
	if err = markImportedUpstreamKeys(row.BaseURL, &refreshed); err != nil {
		return row, previous, fmt.Errorf("remote key group was updated, but reconciling the refreshed snapshot failed: %w", err)
	}
	refreshed.Models = previous.Models
	NormalizeUpstreamSnapshot(&refreshed)

	snapshotJSON, err := common.Marshal(refreshed)
	if err != nil {
		return row, previous, fmt.Errorf("encode refreshed upstream snapshot: %w", err)
	}
	if err = model.UpdateUpstreamChannelSnapshot(row.Id, string(snapshotJSON)); err != nil {
		return row, previous, fmt.Errorf("remote key group was updated, but saving the refreshed snapshot failed: %w", err)
	}
	row, err = model.GetUpstreamChannelByID(row.Id)
	if err != nil {
		return nil, refreshed, err
	}

	for _, key := range refreshed.Keys {
		if key.ID != keyID {
			continue
		}
		if provider == UpstreamProviderNewAPI && strings.TrimSpace(key.Group) != strings.TrimSpace(update.Group) {
			return row, refreshed, fmt.Errorf("new-api key %d still reports group %q after the update", keyID, key.Group)
		}
		if provider == UpstreamProviderSub2API && (key.GroupID == nil || *key.GroupID != *update.GroupID) {
			return row, refreshed, fmt.Errorf("sub2api key %d still reports a different group after the update", keyID)
		}
		return row, refreshed, nil
	}
	return row, refreshed, fmt.Errorf("upstream key %d was not present in the authoritative refresh", keyID)
}

func resolveUpstreamKeyGroupProvider(configuredProvider string, snapshotProvider string) (string, error) {
	configuredProvider = strings.ToLower(strings.TrimSpace(configuredProvider))
	if configuredProvider == UpstreamProviderAuto || configuredProvider == "" {
		resolved := strings.ToLower(strings.TrimSpace(snapshotProvider))
		switch resolved {
		case UpstreamProviderNewAPI, UpstreamProviderSub2API:
			return resolved, nil
		default:
			return "", errors.New("upstream provider is not resolved; refresh the upstream channel before changing a key group")
		}
	}

	switch configuredProvider {
	case UpstreamProviderNewAPI, UpstreamProviderSub2API:
		return configuredProvider, nil
	case UpstreamProviderOther:
		return "", errors.New("upstream provider other does not support remote key group updates")
	default:
		return "", fmt.Errorf("unknown upstream provider %q does not support remote key group updates", configuredProvider)
	}
}

func updateNewAPIUpstreamKeyGroup(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential, keyID int64, group string) error {
	sessionClient, headers, err := authenticateNewAPI(ctx, client, baseURL, credential)
	if err != nil {
		return err
	}

	var detailEnvelope newAPIEnvelope
	path := fmt.Sprintf("/api/token/%d", keyID)
	if err = doUpstreamJSON(ctx, sessionClient, http.MethodGet, upstreamURL(baseURL, path), nil, headers, &detailEnvelope); err != nil {
		return fmt.Errorf("fetch new-api key details failed: %w", err)
	}
	if !detailEnvelope.Success {
		return upstreamEnvelopeError("fetch new-api key details", detailEnvelope.Message)
	}

	var token map[string]json.RawMessage
	if err = common.Unmarshal(detailEnvelope.Data, &token); err != nil {
		return fmt.Errorf("decode new-api key details: %w", err)
	}
	if len(token) == 0 {
		return errors.New("new-api key details response was empty")
	}
	idJSON, ok := token["id"]
	if !ok {
		return errors.New("new-api key details response did not include an id")
	}
	var returnedID int64
	if err = common.Unmarshal(idJSON, &returnedID); err != nil {
		return fmt.Errorf("decode new-api key id: %w", err)
	}
	if returnedID != keyID {
		return fmt.Errorf("new-api key details returned id %d for requested key %d", returnedID, keyID)
	}
	groupJSON, err := common.Marshal(group)
	if err != nil {
		return err
	}
	token["group"] = groupJSON

	var updateEnvelope newAPIEnvelope
	if err = doUpstreamJSON(ctx, sessionClient, http.MethodPut, upstreamURL(baseURL, "/api/token/"), token, headers, &updateEnvelope); err != nil {
		return fmt.Errorf("update new-api key group failed: %w", err)
	}
	if !updateEnvelope.Success {
		return upstreamEnvelopeError("update new-api key group", updateEnvelope.Message)
	}
	return nil
}

func updateSub2APIUpstreamKeyGroup(ctx context.Context, client *http.Client, baseURL string, credential UpstreamCredential, keyID int64, groupID int64) error {
	sessionClient, headers, err := authenticateSub2API(ctx, client, baseURL, credential)
	if err != nil {
		return err
	}
	payload := struct {
		GroupID int64 `json:"group_id"`
	}{GroupID: groupID}

	var envelope sub2APIEnvelope
	path := fmt.Sprintf("/api/v1/keys/%d", keyID)
	if err = doUpstreamJSON(ctx, sessionClient, http.MethodPut, upstreamURL(baseURL, path), payload, headers, &envelope); err != nil {
		return fmt.Errorf("update sub2api key group failed: %w", err)
	}
	if envelope.Code != 0 {
		return upstreamEnvelopeError("update sub2api key group", envelope.Message)
	}
	return nil
}
