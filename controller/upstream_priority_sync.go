package controller

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

const upstreamPriorityBase int64 = 10

type upstreamPriorityCandidate struct {
	Channel        *model.UpstreamChannel
	Snapshot       service.UpstreamSnapshot
	EffectiveRatio decimal.Decimal
	Priority       int64
	SelectedKeys   []service.UpstreamKey
}

type upstreamPrioritySyncSummary struct {
	Refreshed       int      `json:"refreshed"`
	Ranked          int      `json:"ranked"`
	Tested          int      `json:"tested"`
	Passed          int      `json:"passed"`
	PriorityUpdated int      `json:"priority_updated"`
	Skipped         int      `json:"skipped"`
	Errors          []string `json:"errors"`
}

type upstreamPrioritySyncHandler struct{}

func (upstreamPrioritySyncHandler) Type() string {
	return model.SystemTaskTypeUpstreamPrioritySync
}

func (upstreamPrioritySyncHandler) Enabled() bool {
	return operation_setting.GetUpstreamPrioritySetting().Enabled
}

func (upstreamPrioritySyncHandler) Interval() time.Duration {
	seconds := operation_setting.GetUpstreamPrioritySetting().IntervalSeconds
	if seconds < operation_setting.UpstreamPriorityMinIntervalSeconds || seconds > operation_setting.UpstreamPriorityMaxIntervalSeconds {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func (upstreamPrioritySyncHandler) NewPayload() any { return nil }

func (upstreamPrioritySyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if !operation_setting.GetUpstreamPrioritySetting().Enabled {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, upstreamPrioritySyncSummary{Errors: []string{}}, nil)
		return
	}

	summary, err := runUpstreamPrioritySyncTask(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func runUpstreamPrioritySyncTask(ctx context.Context) (upstreamPrioritySyncSummary, error) {
	summary := upstreamPrioritySyncSummary{Errors: []string{}}
	rows, err := model.ListUpstreamChannels()
	if err != nil {
		return summary, err
	}

	candidates := make([]upstreamPriorityCandidate, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if row == nil || !row.HasPassword() || (service.UpstreamCredentialRequiresUsername(row.Provider, row.EffectiveAuthType()) && strings.TrimSpace(row.Username) == "") {
			summary.Skipped++
			continue
		}

		refreshedRow, snapshot, refreshErr := service.RefreshUpstreamChannelGroups(ctx, row.Id)
		if refreshErr != nil {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			summary.Skipped++
			summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d refresh failed: %v", row.Id, refreshErr))
			continue
		}
		summary.Refreshed++

		candidate, candidateErr := buildUpstreamPriorityCandidate(refreshedRow, snapshot)
		if candidateErr != nil {
			summary.Skipped++
			summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d skipped: %v", row.Id, candidateErr))
			continue
		}
		candidates = append(candidates, candidate)
	}

	assignUpstreamPriorityRanks(candidates)
	upstreamPriorities := make(map[int]int64, len(candidates))
	for i := range candidates {
		upstreamPriorities[candidates[i].Channel.Id] = candidates[i].Priority
	}
	if err := model.UpdateUpstreamChannelPriorities(upstreamPriorities); err != nil {
		return summary, err
	}
	summary.Ranked = len(candidates)
	if len(candidates) == 0 {
		return summary, nil
	}

	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return summary, err
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return summary, err
	}
	channelsByBaseURL := indexChannelsByNormalizedBaseURL(channels)

	maxLatencySeconds := operation_setting.GetUpstreamPrioritySetting().MaxTestLatencySeconds
	if maxLatencySeconds < operation_setting.UpstreamPriorityMinLatencySeconds || maxLatencySeconds > operation_setting.UpstreamPriorityMaxLatencySeconds {
		maxLatencySeconds = 5
	}
	maxLatency := time.Duration(maxLatencySeconds) * time.Second
	channelPriorities := make(map[int]int64)

	for i := range candidates {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		candidate := &candidates[i]
		matchingChannels := matchChannelsForUpstreamCandidate(*candidate, channelsByBaseURL)
		if len(matchingChannels) == 0 {
			summary.Skipped++
			continue
		}

		fullKey, revealErr := service.RevealUpstreamChannelKey(ctx, candidate.Channel.Id, candidate.SelectedKeys[0].ID)
		if revealErr != nil {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			summary.Skipped++
			summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d key reveal failed: %v", candidate.Channel.Id, revealErr))
			continue
		}
		if strings.TrimSpace(fullKey) == "" {
			summary.Skipped++
			summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d key reveal returned an empty key", candidate.Channel.Id))
			continue
		}

		testTemplate := selectUpstreamPriorityTestChannel(matchingChannels, candidate.Channel.DefaultTestModel)
		testChannelCopy := *testTemplate
		testChannelCopy.Key = fullKey
		testChannelCopy.Keys = nil
		testChannelCopy.ChannelInfo.IsMultiKey = false
		testChannelCopy.ChannelInfo.MultiKeySize = 0
		testChannelCopy.ChannelInfo.MultiKeyStatusList = nil
		testChannelCopy.ChannelInfo.MultiKeyDisabledReason = nil
		testChannelCopy.ChannelInfo.MultiKeyDisabledTime = nil

		testCtx, cancel := context.WithTimeout(ctx, maxLatency)
		startedAt := time.Now()
		result := testChannel(testCtx, &testChannelCopy, testUserID, candidate.Channel.DefaultTestModel, "", false)
		elapsed := time.Since(startedAt)
		cancel()
		summary.Tested++
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if result.localErr != nil || result.newAPIError != nil || elapsed > maxLatency {
			summary.Skipped++
			if result.localErr != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d test failed: %v", candidate.Channel.Id, result.localErr))
			} else if result.newAPIError != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d test failed: %v", candidate.Channel.Id, result.newAPIError))
			} else {
				summary.Errors = append(summary.Errors, fmt.Sprintf("upstream channel %d test exceeded %d seconds", candidate.Channel.Id, maxLatencySeconds))
			}
			continue
		}

		summary.Passed++
		for _, channel := range matchingChannels {
			if current, exists := channelPriorities[channel.Id]; !exists || candidate.Priority > current {
				channelPriorities[channel.Id] = candidate.Priority
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if err := model.UpdateChannelPrioritiesByID(channelPriorities); err != nil {
		return summary, err
	}
	if len(channelPriorities) > 0 {
		model.InitChannelCache()
		service.ResetProxyClientCache()
	}
	summary.PriorityUpdated = len(channelPriorities)
	return summary, nil
}

func buildUpstreamPriorityCandidate(channel *model.UpstreamChannel, snapshot service.UpstreamSnapshot) (upstreamPriorityCandidate, error) {
	if channel == nil {
		return upstreamPriorityCandidate{}, fmt.Errorf("channel is unavailable")
	}
	selectedGroupName := strings.TrimSpace(channel.SelectedGroup)
	if selectedGroupName == "" {
		return upstreamPriorityCandidate{}, fmt.Errorf("selected group is required")
	}
	defaultTestModel := strings.TrimSpace(channel.DefaultTestModel)
	if defaultTestModel == "" {
		return upstreamPriorityCandidate{}, fmt.Errorf("default test model is required")
	}

	var selectedGroup *service.UpstreamGroup
	for i := range snapshot.Groups {
		if strings.TrimSpace(snapshot.Groups[i].Name) == selectedGroupName {
			selectedGroup = &snapshot.Groups[i]
			break
		}
	}
	if selectedGroup == nil {
		return upstreamPriorityCandidate{}, fmt.Errorf("selected group is unavailable")
	}

	modelAvailable := false
	for _, upstreamModel := range snapshot.Models {
		if strings.TrimSpace(upstreamModel.ID) == defaultTestModel {
			modelAvailable = true
			break
		}
	}
	if !modelAvailable {
		return upstreamPriorityCandidate{}, fmt.Errorf("default test model is unavailable")
	}

	ratio := selectedGroup.Ratio
	if selectedGroup.ID != 0 {
		if snapshotRatio, ok := snapshot.Ratios[strconv.FormatInt(selectedGroup.ID, 10)]; ok {
			ratio = snapshotRatio
		} else if snapshotRatio, ok := snapshot.Ratios[selectedGroupName]; ok {
			ratio = snapshotRatio
		}
	} else if snapshotRatio, ok := snapshot.Ratios[selectedGroupName]; ok {
		ratio = snapshotRatio
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
		return upstreamPriorityCandidate{}, fmt.Errorf("selected group ratio is invalid")
	}
	multiplier := channel.EffectiveMultiplier()
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier <= 0 {
		return upstreamPriorityCandidate{}, fmt.Errorf("channel multiplier is invalid")
	}

	selectedKeys := make([]service.UpstreamKey, 0)
	for _, key := range snapshot.Keys {
		belongsToSelectedGroup := strings.TrimSpace(key.Group) == selectedGroupName
		if !belongsToSelectedGroup && selectedGroup.ID != 0 && key.GroupID != nil {
			belongsToSelectedGroup = *key.GroupID == selectedGroup.ID
		}
		if !belongsToSelectedGroup || !service.IsUpstreamKeyEligible(key) || strings.TrimSpace(key.KeyFingerprint) == "" {
			continue
		}
		selectedKeys = append(selectedKeys, key)
	}
	if len(selectedKeys) == 0 {
		return upstreamPriorityCandidate{}, fmt.Errorf("selected group has no eligible key")
	}

	return upstreamPriorityCandidate{
		Channel:        channel,
		Snapshot:       snapshot,
		EffectiveRatio: decimal.NewFromFloat(ratio).Mul(decimal.NewFromFloat(multiplier)),
		SelectedKeys:   selectedKeys,
	}, nil
}

func assignUpstreamPriorityRanks(candidates []upstreamPriorityCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		comparison := candidates[i].EffectiveRatio.Cmp(candidates[j].EffectiveRatio)
		if comparison != 0 {
			return comparison > 0
		}
		return candidates[i].Channel.Id < candidates[j].Channel.Id
	})

	priority := upstreamPriorityBase
	for i := range candidates {
		if i > 0 && !candidates[i].EffectiveRatio.Equal(candidates[i-1].EffectiveRatio) {
			priority++
		}
		candidates[i].Priority = priority
	}
}

func indexChannelsByNormalizedBaseURL(channels []*model.Channel) map[string][]*model.Channel {
	result := make(map[string][]*model.Channel)
	for _, channel := range channels {
		if channel == nil || channel.Id <= 0 {
			continue
		}
		normalized, err := service.NormalizeUpstreamBaseURL(channel.GetBaseURL())
		if err != nil {
			continue
		}
		result[normalized] = append(result[normalized], channel)
	}
	for baseURL := range result {
		sort.SliceStable(result[baseURL], func(i, j int) bool {
			return result[baseURL][i].Id < result[baseURL][j].Id
		})
	}
	return result
}

func matchChannelsForUpstreamCandidate(candidate upstreamPriorityCandidate, channelsByBaseURL map[string][]*model.Channel) []*model.Channel {
	normalized, err := service.NormalizeUpstreamBaseURL(candidate.Channel.BaseURL)
	if err != nil {
		return nil
	}
	selectedFingerprints := make(map[string]struct{}, len(candidate.SelectedKeys))
	for _, key := range candidate.SelectedKeys {
		fingerprint := strings.TrimSpace(key.KeyFingerprint)
		if fingerprint != "" {
			selectedFingerprints[fingerprint] = struct{}{}
		}
	}

	matched := make([]*model.Channel, 0)
	for _, channel := range channelsByBaseURL[normalized] {
		found := false
		for _, key := range channel.GetKeys() {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := selectedFingerprints[model.UpstreamKeyFingerprint(key)]; ok {
				found = true
				break
			}
		}
		if found {
			matched = append(matched, channel)
		}
	}
	return matched
}

func selectUpstreamPriorityTestChannel(channels []*model.Channel, defaultTestModel string) *model.Channel {
	if len(channels) == 0 {
		return nil
	}
	for _, channel := range channels {
		for _, modelName := range channel.GetModels() {
			if strings.TrimSpace(modelName) == defaultTestModel {
				return channel
			}
		}
	}
	return channels[0]
}
