package service

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	UpstreamKeyInUseStatusUnlinked     = "unlinked"
	UpstreamKeyInUseStatusEnabled      = "enabled"
	UpstreamKeyInUseStatusAutoDisabled = "auto_disabled"
	UpstreamKeyInUseStatusDisabled     = "disabled"
)

type UpstreamKeyLinkSummary struct {
	Total        int `json:"total"`
	Linked       int `json:"linked"`
	Enabled      int `json:"enabled"`
	AutoDisabled int `json:"auto_disabled"`
	Disabled     int `json:"disabled"`
	Unlinked     int `json:"unlinked"`
}

func LinkUpstreamChannelKeys(ctx context.Context, upstreamChannelID int) (*model.UpstreamChannel, UpstreamSnapshot, UpstreamKeyLinkSummary, error) {
	lock := upstreamRefreshLock(upstreamChannelID)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(upstreamChannelID)
	if err != nil {
		return nil, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, err
	}
	credential, err := refreshUpstreamCredential(row)
	if err != nil {
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, err
	}
	linkCtx, cancel, client := refreshUpstreamClient(ctx)
	defer cancel()

	localSources, err := listMatchingChannelKeySources(row.BaseURL)
	if err != nil {
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, err
	}
	localKeys := make([]string, 0, len(localSources))
	for _, source := range localSources {
		localKeys = append(localKeys, source.Key)
	}

	fetched, err := FetchUpstreamKeysForLink(linkCtx, client, row.BaseURL, row.Provider, credential, localKeys)
	attemptedAt := time.Now().Unix()
	if err != nil {
		message := truncateUpstreamError(err.Error())
		_ = model.SaveUpstreamChannelRefreshError(row.Id, message, attemptedAt)
		if refreshedRow, loadErr := model.GetUpstreamChannelByID(row.Id); loadErr == nil {
			row = refreshedRow
		}
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, err
	}
	if err = reconcileUpstreamKeyLinks(row.BaseURL, &fetched); err != nil {
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, errorsWithRefreshState(row.Id, err.Error())
	}

	snapshot, err := loadUpstreamSnapshot(row)
	if err != nil {
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, errorsWithRefreshState(row.Id, err.Error())
	}
	snapshot.Provider = fetched.Provider
	applyUpstreamGroupNames(fetched.Keys, snapshot.Groups)
	snapshot.Keys = fetched.Keys
	snapshot.RetrievedAt = fetched.RetrievedAt
	NormalizeUpstreamSnapshot(&snapshot)

	row, err = savePartialUpstreamSnapshot(row, snapshot, attemptedAt, "")
	if err != nil {
		return row, UpstreamSnapshot{}, UpstreamKeyLinkSummary{}, err
	}
	return row, snapshot, summarizeUpstreamKeyLinks(snapshot.Keys), nil
}

func reconcileUpstreamKeyLinks(baseURL string, snapshot *UpstreamSnapshot) error {
	if snapshot == nil {
		return nil
	}
	sources, err := listMatchingChannelKeySources(baseURL)
	if err != nil {
		return err
	}
	normalizedBaseURL, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return err
	}

	statuses := make(map[string]string)
	for _, source := range sources {
		fingerprint := UpstreamKeyFingerprintForProvider(snapshot.Provider, normalizedBaseURL, source.Key)
		candidate := localChannelInUseStatus(source.Status)
		if inUseStatusRank(candidate) > inUseStatusRank(statuses[fingerprint]) {
			statuses[fingerprint] = candidate
		}
	}

	for i := range snapshot.Keys {
		key := &snapshot.Keys[i]
		key.Linked = false
		key.Imported = false
		key.Active = false
		key.InUseStatus = UpstreamKeyInUseStatusUnlinked

		fingerprint := strings.TrimSpace(key.KeyFingerprint)
		if fingerprint == "" {
			continue
		}
		status, linked := statuses[fingerprint]
		if !linked {
			continue
		}
		key.Linked = true
		key.Imported = true
		key.InUseStatus = status
		key.Active = status == UpstreamKeyInUseStatusEnabled
	}
	return nil
}

func listMatchingChannelKeySources(baseURL string) ([]model.ChannelKeySource, error) {
	normalizedBaseURL, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	sources, err := model.ListChannelKeySources()
	if err != nil {
		return nil, err
	}
	matching := make([]model.ChannelKeySource, 0)
	for _, source := range sources {
		normalizedSourceURL, normalizeErr := NormalizeUpstreamBaseURL(source.BaseURL)
		if normalizeErr != nil || normalizedSourceURL != normalizedBaseURL {
			continue
		}
		matching = append(matching, source)
	}
	return matching, nil
}

func localChannelInUseStatus(status int) string {
	switch status {
	case common.ChannelStatusEnabled:
		return UpstreamKeyInUseStatusEnabled
	case common.ChannelStatusAutoDisabled:
		return UpstreamKeyInUseStatusAutoDisabled
	default:
		return UpstreamKeyInUseStatusDisabled
	}
}

func inUseStatusRank(status string) int {
	switch status {
	case UpstreamKeyInUseStatusEnabled:
		return 3
	case UpstreamKeyInUseStatusAutoDisabled:
		return 2
	case UpstreamKeyInUseStatusDisabled:
		return 1
	default:
		return 0
	}
}

func summarizeUpstreamKeyLinks(keys []UpstreamKey) UpstreamKeyLinkSummary {
	summary := UpstreamKeyLinkSummary{}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		identity := strings.TrimSpace(key.KeyFingerprint)
		if identity == "" {
			summary.Total++
			summary.Unlinked++
			continue
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		summary.Total++
		switch key.InUseStatus {
		case UpstreamKeyInUseStatusEnabled:
			summary.Linked++
			summary.Enabled++
		case UpstreamKeyInUseStatusAutoDisabled:
			summary.Linked++
			summary.AutoDisabled++
		case UpstreamKeyInUseStatusDisabled:
			summary.Linked++
			summary.Disabled++
		case UpstreamKeyInUseStatusUnlinked, "":
			summary.Unlinked++
		default:
			summary.Unlinked++
		}
	}
	return summary
}

func SummarizeUpstreamKeyLinks(keys []UpstreamKey) UpstreamKeyLinkSummary {
	return summarizeUpstreamKeyLinks(keys)
}
