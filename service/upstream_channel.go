package service

import (
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	UpstreamProviderAuto    = "auto"
	UpstreamProviderNewAPI  = "new-api"
	UpstreamProviderSub2API = "sub2api"
	UpstreamProviderOther   = "other"
)

func UpstreamCredentialRequiresUsername(provider string, authType string) bool {
	return strings.TrimSpace(provider) != UpstreamProviderSub2API ||
		model.NormalizeUpstreamAuthType(authType) != model.UpstreamAuthTypeAccessToken
}

type UpstreamChannelLogMetrics struct {
	Availability24h            *float64
	AverageFirstTokenLatencyMs *float64
}

func NormalizeUpstreamBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("base URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("base URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", errors.New("base URL must include a host and no user info")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	if (parsed.Scheme == "https" && parsed.Port() == "443") || (parsed.Scheme == "http" && parsed.Port() == "80") {
		parsed.Host = parsed.Hostname()
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	path := strings.TrimRight(parsed.EscapedPath(), "/")
	for _, suffix := range []string{"/api/v1", "/api", "/v1"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			path = strings.TrimSuffix(path, path[len(path)-len(suffix):])
			break
		}
	}
	parsed.Path = strings.TrimRight(path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func CollectExplicitUpstreamBaseURLs(rawURLs []string) []string {
	unique := make(map[string]struct{}, len(rawURLs))
	for _, raw := range rawURLs {
		normalized, err := NormalizeUpstreamBaseURL(raw)
		if err != nil || normalized == "" {
			continue
		}
		unique[normalized] = struct{}{}
	}
	urls := make([]string, 0, len(unique))
	for normalized := range unique {
		urls = append(urls, normalized)
	}
	sort.Strings(urls)
	return urls
}

func GetUpstreamChannelLogMetricsSince(startTimestamp int64) (map[string]UpstreamChannelLogMetrics, error) {
	sources, err := model.ListExplicitChannelSources()
	if err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0, len(sources))
	baseURLByChannelID := make(map[int]string, len(sources))
	for _, source := range sources {
		if source.ID <= 0 {
			continue
		}
		normalized, normalizeErr := NormalizeUpstreamBaseURL(source.BaseURL)
		if normalizeErr != nil {
			continue
		}
		channelIDs = append(channelIDs, source.ID)
		baseURLByChannelID[source.ID] = normalized
	}

	channelMetrics, err := model.GetChannelLogMetricsSince(channelIDs, startTimestamp)
	if err != nil {
		return nil, err
	}
	type aggregate struct {
		requestCount                 int64
		successCount                 int64
		firstTokenLatencyTotalMs     float64
		firstTokenLatencySampleCount int64
	}
	aggregates := make(map[string]aggregate)
	for channelID, metric := range channelMetrics {
		baseURL, ok := baseURLByChannelID[channelID]
		if !ok {
			continue
		}
		current := aggregates[baseURL]
		current.requestCount += metric.RequestCount
		current.successCount += metric.SuccessCount
		current.firstTokenLatencyTotalMs += metric.FirstTokenLatencyTotalMs
		current.firstTokenLatencySampleCount += metric.FirstTokenLatencySampleCount
		aggregates[baseURL] = current
	}

	result := make(map[string]UpstreamChannelLogMetrics, len(aggregates))
	for baseURL, current := range aggregates {
		metrics := UpstreamChannelLogMetrics{}
		if current.requestCount > 0 {
			availability := float64(current.successCount) / float64(current.requestCount) * 100
			metrics.Availability24h = &availability
		}
		if current.firstTokenLatencySampleCount > 0 {
			average := current.firstTokenLatencyTotalMs / float64(current.firstTokenLatencySampleCount)
			metrics.AverageFirstTokenLatencyMs = &average
		}
		result[baseURL] = metrics
	}
	return result, nil
}

func DeleteUpstreamChannel(id int) error {
	return model.DeleteUpstreamChannel(id)
}

func BalanceNotificationTransition(threshold float64, balance float64, notified bool) (bool, bool) {
	if threshold <= 0 {
		return false, false
	}
	if balance < threshold {
		return !notified, true
	}
	return false, false
}

func UpdateUpstreamChannelSelectedGroup(id int, selectedGroup string) (*model.UpstreamChannel, error) {
	selectedGroup = strings.TrimSpace(selectedGroup)
	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		return nil, err
	}
	if selectedGroup != "" {
		var snapshot UpstreamSnapshot
		if row.SnapshotJSON == "" || common.Unmarshal([]byte(row.SnapshotJSON), &snapshot) != nil {
			return nil, errors.New("selected upstream group is unavailable")
		}
		available := false
		for _, group := range snapshot.Groups {
			if strings.TrimSpace(group.Name) == selectedGroup {
				available = true
				break
			}
		}
		if !available {
			return nil, errors.New("selected upstream group is unavailable")
		}
	}
	if err = model.UpdateUpstreamChannelSelectedGroup(id, selectedGroup); err != nil {
		return nil, err
	}
	return model.GetUpstreamChannelByID(id)
}

func UpdateUpstreamChannelDefaultTestModel(id int, defaultTestModel string) (*model.UpstreamChannel, error) {
	defaultTestModel = strings.TrimSpace(defaultTestModel)
	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		return nil, err
	}
	if defaultTestModel != "" {
		var snapshot UpstreamSnapshot
		if strings.TrimSpace(row.SnapshotJSON) == "" || common.UnmarshalJsonStr(row.SnapshotJSON, &snapshot) != nil {
			return nil, errors.New("default test model is unavailable")
		}
		available := false
		for _, upstreamModel := range snapshot.Models {
			if strings.TrimSpace(upstreamModel.ID) == defaultTestModel {
				available = true
				break
			}
		}
		if !available {
			return nil, errors.New("default test model is unavailable")
		}
	}
	if err = model.UpdateUpstreamChannelDefaultTestModel(id, defaultTestModel); err != nil {
		return nil, err
	}
	return model.GetUpstreamChannelByID(id)
}
