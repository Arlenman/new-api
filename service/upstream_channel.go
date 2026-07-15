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
