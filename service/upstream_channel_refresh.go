package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	upstreamChannelRefreshTickInterval = 30 * time.Second
	upstreamChannelRefreshTimeout      = 45 * time.Second
	upstreamChannelRefreshBatchSize    = 100
)

var (
	upstreamChannelRefreshOnce    sync.Once
	upstreamChannelRefreshRunning atomic.Bool
	upstreamChannelRefreshLocks   sync.Map
)

type UpstreamChannelSourceStats struct {
	Total  int
	Active int
}

func DiscoverUpstreamChannels() ([]*model.UpstreamChannel, map[string]UpstreamChannelSourceStats, error) {
	sources, err := model.ListExplicitChannelSources()
	if err != nil {
		return nil, nil, err
	}
	rawURLs := make([]string, 0, len(sources))
	for _, source := range sources {
		rawURLs = append(rawURLs, source.BaseURL)
	}
	baseURLs := CollectExplicitUpstreamBaseURLs(rawURLs)
	if _, err = model.EnsureUpstreamChannels(baseURLs); err != nil {
		return nil, nil, err
	}
	rows, err := model.ListUpstreamChannels()
	if err != nil {
		return nil, nil, err
	}
	stats := make(map[string]UpstreamChannelSourceStats, len(baseURLs))
	for _, source := range sources {
		normalized, normalizeErr := NormalizeUpstreamBaseURL(source.BaseURL)
		if normalizeErr == nil {
			channelStats := stats[normalized]
			channelStats.Total++
			stats[normalized] = channelStats
		}
	}
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.SnapshotJSON) == "" {
			continue
		}
		var snapshot UpstreamSnapshot
		if unmarshalErr := common.UnmarshalJsonStr(row.SnapshotJSON, &snapshot); unmarshalErr != nil {
			continue
		}
		if err = markImportedUpstreamKeys(row.BaseURL, &snapshot); err != nil {
			return nil, nil, err
		}
		active := 0
		for _, key := range snapshot.Keys {
			if key.Active {
				active++
			}
		}
		channelStats := stats[row.BaseURL]
		channelStats.Active = active
		stats[row.BaseURL] = channelStats
		snapshotJSON, marshalErr := common.Marshal(snapshot)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		row.SnapshotJSON = string(snapshotJSON)
	}
	return rows, stats, nil
}

func markImportedUpstreamKeys(baseURL string, snapshot *UpstreamSnapshot) error {
	if snapshot == nil {
		return nil
	}
	states, err := model.GetChannelKeyStatesByBaseURL(baseURL)
	if err != nil {
		return err
	}
	matchedFingerprints := make(map[string]struct{}, len(snapshot.Keys))
	legacyImportedIndexes := make([]int, 0)
	for i := range snapshot.Keys {
		snapshot.Keys[i].Active = false
		fingerprint := snapshot.Keys[i].KeyFingerprint
		if fingerprint == "" {
			if snapshot.Keys[i].Imported {
				legacyImportedIndexes = append(legacyImportedIndexes, i)
			}
			continue
		}
		enabled, imported := states[fingerprint]
		snapshot.Keys[i].Imported = imported
		snapshot.Keys[i].Active = imported && enabled
		if imported {
			matchedFingerprints[fingerprint] = struct{}{}
		}
	}
	legacyActiveCount := 0
	for fingerprint, enabled := range states {
		if !enabled {
			continue
		}
		if _, matched := matchedFingerprints[fingerprint]; matched {
			continue
		}
		legacyActiveCount++
	}
	for _, index := range legacyImportedIndexes {
		if legacyActiveCount == 0 {
			break
		}
		snapshot.Keys[index].Active = true
		legacyActiveCount--
	}
	return nil
}

func RefreshUpstreamChannel(ctx context.Context, id int) (*model.UpstreamChannel, UpstreamSnapshot, error) {
	lock := upstreamRefreshLock(id)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		return nil, UpstreamSnapshot{}, err
	}
	if strings.TrimSpace(row.Username) == "" {
		return row, UpstreamSnapshot{}, errorsWithRefreshState(row.Id, "upstream username is not configured")
	}
	password, err := row.DecryptPassword()
	if err != nil {
		return row, UpstreamSnapshot{}, errorsWithRefreshState(row.Id, err.Error())
	}

	refreshCtx, cancel := context.WithTimeout(ctx, upstreamChannelRefreshTimeout)
	defer cancel()
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	snapshot, err := FetchUpstreamSnapshot(refreshCtx, client, row.BaseURL, row.Provider, UpstreamCredential{AuthType: row.EffectiveAuthType(), Username: row.Username, Password: password})
	attemptedAt := time.Now().Unix()
	if err != nil {
		message := truncateUpstreamError(err.Error())
		_ = model.SaveUpstreamChannelRefreshError(row.Id, message, attemptedAt)
		if refreshedRow, loadErr := model.GetUpstreamChannelByID(row.Id); loadErr == nil {
			row = refreshedRow
		}
		return row, UpstreamSnapshot{}, err
	}
	if err = markImportedUpstreamKeys(row.BaseURL, &snapshot); err != nil {
		return row, UpstreamSnapshot{}, errorsWithRefreshState(row.Id, err.Error())
	}

	snapshotJSON, err := common.Marshal(snapshot)
	if err != nil {
		return row, UpstreamSnapshot{}, errorsWithRefreshState(row.Id, err.Error())
	}
	effectiveBalance := snapshot.Balance * row.EffectiveMultiplier()
	sendNotification, notificationState := BalanceNotificationTransition(row.BalanceThreshold, effectiveBalance, row.LowBalanceNotified)
	if err = model.SaveUpstreamChannelRefresh(row.Id, snapshot.Provider, string(snapshotJSON), snapshot.Balance, attemptedAt, notificationState); err != nil {
		return row, UpstreamSnapshot{}, err
	}
	row, err = model.GetUpstreamChannelByID(id)
	if err != nil {
		return nil, UpstreamSnapshot{}, err
	}
	if sendNotification {
		subject := "Upstream channel balance is low"
		content := fmt.Sprintf("Upstream %s (%s) balance %.4f is below the configured threshold %.4f.", row.BaseURL, snapshot.Provider, effectiveBalance, row.BalanceThreshold)
		NotifyRootUser(dto.NotifyTypeUpstreamBalanceLow, subject, content)
	}
	return row, snapshot, nil
}

func RefreshAllUpstreamChannels(ctx context.Context) (int, []string) {
	rows, _, err := DiscoverUpstreamChannels()
	if err != nil {
		return 0, []string{err.Error()}
	}
	refreshed := 0
	errorsFound := make([]string, 0)
	for _, row := range rows {
		if row == nil || !row.HasPassword() || strings.TrimSpace(row.Username) == "" {
			continue
		}
		if _, _, refreshErr := RefreshUpstreamChannel(ctx, row.Id); refreshErr != nil {
			errorsFound = append(errorsFound, fmt.Sprintf("%s: %s", row.BaseURL, refreshErr.Error()))
			continue
		}
		refreshed++
	}
	return refreshed, errorsFound
}

func RevealUpstreamChannelKey(ctx context.Context, id int, keyID int64) (string, error) {
	lock := upstreamRefreshLock(id)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(row.Username) == "" {
		return "", fmt.Errorf("upstream username is not configured")
	}
	password, err := row.DecryptPassword()
	if err != nil {
		return "", err
	}
	revealCtx, cancel := context.WithTimeout(ctx, upstreamChannelRefreshTimeout)
	defer cancel()
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	return FetchUpstreamFullKey(revealCtx, client, row.BaseURL, row.Provider, UpstreamCredential{AuthType: row.EffectiveAuthType(), Username: row.Username, Password: password}, keyID)
}

func StartUpstreamChannelAutoRefreshTask() {
	upstreamChannelRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("upstream channel refresh task started: tick=%s", upstreamChannelRefreshTickInterval))
			ticker := time.NewTicker(upstreamChannelRefreshTickInterval)
			defer ticker.Stop()
			runUpstreamChannelAutoRefreshOnce()
			for range ticker.C {
				runUpstreamChannelAutoRefreshOnce()
			}
		})
	})
}

func runUpstreamChannelAutoRefreshOnce() {
	if !upstreamChannelRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer upstreamChannelRefreshRunning.Store(false)

	if _, _, err := DiscoverUpstreamChannels(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("upstream channel discovery failed: %v", err))
		return
	}
	rows, err := model.ListDueUpstreamChannels(time.Now().Unix(), upstreamChannelRefreshBatchSize)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("query due upstream channels failed: %v", err))
		return
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, _, refreshErr := RefreshUpstreamChannel(context.Background(), row.Id); refreshErr != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("refresh upstream channel %d failed: %v", row.Id, refreshErr))
		}
	}
}

func upstreamRefreshLock(id int) *sync.Mutex {
	lock, _ := upstreamChannelRefreshLocks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func errorsWithRefreshState(id int, message string) error {
	message = truncateUpstreamError(message)
	_ = model.SaveUpstreamChannelRefreshError(id, message, time.Now().Unix())
	return fmt.Errorf("%s", message)
}

func truncateUpstreamError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 2000 {
		return message[:2000]
	}
	return message
}
