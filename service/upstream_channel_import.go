package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

const (
	upstreamImportKeyLimit             = 100
	upstreamImportModelLimit           = 1000
	upstreamImportGroupMaxLength       = 64
	upstreamImportTextMaxLength        = 255
	upstreamImportPriorityMin          = math.MinInt32
	upstreamImportPriorityMax          = math.MaxInt32
	upstreamImportWeightMax      int64 = math.MaxInt32
)

type UpstreamKeyImportOptions struct {
	KeyIDs     []int64
	Groups     *[]string
	Tag        *string
	NamePrefix *string
	Priority   *int64
	Weight     *int64
	TestModel  *string
	Models     *[]string
	AutoBan    *int
	Remark     *string
}

type normalizedUpstreamKeyImportOptions struct {
	KeyIDs          []int64
	Group           string
	Tag             string
	NamePrefix      string
	Priority        int64
	Weight          uint
	TestModel       string
	Models          []string
	HasCustomModels bool
	AutoBan         int
	Remark          string
}

type UpstreamKeyImportResult struct {
	Imported   int   `json:"imported"`
	Updated    int   `json:"updated"`
	Skipped    int   `json:"skipped"`
	Disabled   int   `json:"disabled"`
	ChannelIDs []int `json:"channel_ids"`
}

func ImportUpstreamChannelKeys(ctx context.Context, upstreamChannelID int, options UpstreamKeyImportOptions) (UpstreamKeyImportResult, error) {
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	return importUpstreamChannelKeys(ctx, client, upstreamChannelID, options)
}

func importUpstreamChannelKeys(ctx context.Context, client *http.Client, upstreamChannelID int, options UpstreamKeyImportOptions) (UpstreamKeyImportResult, error) {
	if len(options.KeyIDs) == 0 {
		return UpstreamKeyImportResult{}, errors.New("select at least one upstream key")
	}
	if len(options.KeyIDs) > upstreamImportKeyLimit {
		return UpstreamKeyImportResult{}, fmt.Errorf("cannot import more than %d upstream keys at once", upstreamImportKeyLimit)
	}

	lock := upstreamRefreshLock(upstreamChannelID)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(upstreamChannelID)
	if err != nil {
		return UpstreamKeyImportResult{}, err
	}
	importOptions, err := normalizeUpstreamKeyImportOptions(options, row.BaseURL, row.Name, row.Priority)
	if err != nil {
		return UpstreamKeyImportResult{}, err
	}
	if strings.TrimSpace(row.Username) == "" {
		return UpstreamKeyImportResult{}, errors.New("upstream username is not configured")
	}
	password, err := row.DecryptPassword()
	if err != nil {
		return UpstreamKeyImportResult{}, err
	}
	if strings.TrimSpace(row.SnapshotJSON) == "" {
		return UpstreamKeyImportResult{}, errors.New("refresh the upstream channel before importing keys")
	}

	var snapshot UpstreamSnapshot
	if err = common.UnmarshalJsonStr(row.SnapshotJSON, &snapshot); err != nil {
		return UpstreamKeyImportResult{}, fmt.Errorf("decode upstream snapshot: %w", err)
	}
	keyIndexByID := make(map[int64]int, len(snapshot.Keys))
	for i := range snapshot.Keys {
		keyIndexByID[snapshot.Keys[i].ID] = i
	}
	for _, keyID := range importOptions.KeyIDs {
		if _, exists := keyIndexByID[keyID]; !exists {
			return UpstreamKeyImportResult{}, fmt.Errorf("upstream key %d is not present in the latest snapshot", keyID)
		}
	}

	importCtx, cancel := context.WithTimeout(ctx, upstreamChannelRefreshTimeout)
	defer cancel()
	provider := snapshot.Provider
	if provider == "" {
		provider = row.Provider
	}
	credential := UpstreamCredential{AuthType: row.EffectiveAuthType(), Username: row.Username, Password: password}
	channels := make([]model.Channel, 0, len(importOptions.KeyIDs))
	disabled := 0
	for _, keyID := range importOptions.KeyIDs {
		fullKey, fetchErr := FetchUpstreamFullKey(importCtx, client, row.BaseURL, provider, credential, keyID)
		if fetchErr != nil {
			return UpstreamKeyImportResult{}, fmt.Errorf("fetch upstream key %d: %w", keyID, fetchErr)
		}
		fullKey = strings.TrimSpace(fullKey)
		if fullKey == "" {
			return UpstreamKeyImportResult{}, fmt.Errorf("upstream key %d is empty", keyID)
		}
		keyIndex := keyIndexByID[keyID]
		snapshot.Keys[keyIndex].KeyFingerprint = model.UpstreamKeyFingerprint(fullKey)

		fetchedModels, modelsErr := fetchUpstreamKeyModels(importCtx, client, row.BaseURL, fullKey)
		models := fetchedModels
		if importOptions.HasCustomModels {
			models = append([]string(nil), importOptions.Models...)
			if modelsErr == nil {
				availableModels := make(map[string]struct{}, len(fetchedModels))
				for _, modelName := range fetchedModels {
					availableModels[modelName] = struct{}{}
				}
				models = models[:0]
				for _, modelName := range importOptions.Models {
					if _, exists := availableModels[modelName]; exists {
						models = append(models, modelName)
					}
				}
			}
		}
		status := common.ChannelStatusEnabled
		if modelsErr != nil || len(models) == 0 {
			status = common.ChannelStatusManuallyDisabled
			disabled++
		}
		keySnapshot := snapshot.Keys[keyIndex]
		keyName := strings.TrimSpace(keySnapshot.Name)
		if keyName == "" {
			keyName = strconv.FormatInt(keyID, 10)
		}

		baseURL := row.BaseURL
		priority := importOptions.Priority
		weight := importOptions.Weight
		autoBan := importOptions.AutoBan
		var tag *string
		if importOptions.Tag != "" {
			value := importOptions.Tag
			tag = &value
		}
		var testModel *string
		if importOptions.TestModel != "" {
			value := importOptions.TestModel
			testModel = &value
		}
		remark := importOptions.Remark
		channels = append(channels, model.Channel{
			Type:        constant.ChannelTypeOpenAI,
			Key:         fullKey,
			Status:      status,
			Name:        fmt.Sprintf("%s-%s", importOptions.NamePrefix, keyName),
			CreatedTime: common.GetTimestamp(),
			BaseURL:     &baseURL,
			Models:      strings.Join(models, ","),
			Group:       importOptions.Group,
			Tag:         tag,
			Priority:    &priority,
			Weight:      &weight,
			TestModel:   testModel,
			AutoBan:     &autoBan,
			Remark:      &remark,
		})
	}

	upsertResult, err := model.UpsertImportedUpstreamChannels(channels)
	if err != nil {
		return UpstreamKeyImportResult{}, err
	}
	if len(upsertResult.ChannelIDs) > 0 {
		model.InitChannelCache()
		ResetProxyClientCache()
		if evaluateErr := EvaluateEnabledChannelCountAlertRules(ctx); evaluateErr != nil {
			common.SysError("evaluate enabled channel count alert after upstream key import: " + evaluateErr.Error())
		}
	}
	if err = markImportedUpstreamKeys(row.BaseURL, &snapshot); err != nil {
		return UpstreamKeyImportResult{}, err
	}
	snapshotJSON, marshalErr := common.Marshal(snapshot)
	if marshalErr != nil {
		return UpstreamKeyImportResult{}, marshalErr
	}
	if err = model.UpdateUpstreamChannelSnapshot(row.Id, string(snapshotJSON)); err != nil {
		return UpstreamKeyImportResult{}, err
	}
	return UpstreamKeyImportResult{
		Imported:   upsertResult.Imported,
		Updated:    upsertResult.Updated,
		Skipped:    0,
		Disabled:   disabled,
		ChannelIDs: upsertResult.ChannelIDs,
	}, nil
}

func normalizeUpstreamKeyImportOptions(options UpstreamKeyImportOptions, baseURL string, channelName string, defaultPriority int64) (normalizedUpstreamKeyImportOptions, error) {
	keyIDs, err := normalizeUpstreamKeyIDs(options.KeyIDs)
	if err != nil {
		return normalizedUpstreamKeyImportOptions{}, err
	}

	defaultTag := strings.TrimSpace(channelName)
	if defaultTag == "" {
		defaultTag = model.UpstreamChannelDefaultName(baseURL)
	}
	normalized := normalizedUpstreamKeyImportOptions{
		KeyIDs:   keyIDs,
		Group:    "default",
		Tag:      defaultTag,
		Priority: defaultPriority,
		AutoBan:  1,
	}

	if options.Groups != nil {
		groups := make([]string, 0, len(*options.Groups))
		seenGroups := make(map[string]struct{}, len(*options.Groups))
		for _, rawGroup := range *options.Groups {
			group := strings.TrimSpace(rawGroup)
			if group == "" {
				continue
			}
			if strings.Contains(group, ",") {
				return normalizedUpstreamKeyImportOptions{}, errors.New("channel groups must not contain commas")
			}
			if _, exists := seenGroups[group]; exists {
				continue
			}
			seenGroups[group] = struct{}{}
			groups = append(groups, group)
		}
		if len(groups) == 0 {
			return normalizedUpstreamKeyImportOptions{}, errors.New("select at least one channel group")
		}
		normalized.Group = strings.Join(groups, ",")
	}
	if utf8.RuneCountInString(normalized.Group) > upstreamImportGroupMaxLength {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel groups must not exceed %d characters", upstreamImportGroupMaxLength)
	}

	if options.Tag != nil {
		normalized.Tag = strings.TrimSpace(*options.Tag)
	}
	if utf8.RuneCountInString(normalized.Tag) > upstreamImportTextMaxLength {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel tag must not exceed %d characters", upstreamImportTextMaxLength)
	}

	if options.NamePrefix != nil {
		normalized.NamePrefix = strings.TrimSpace(*options.NamePrefix)
	} else if normalized.Tag != "" {
		normalized.NamePrefix = normalized.Tag
	} else {
		normalized.NamePrefix = defaultTag
	}
	if normalized.NamePrefix == "" {
		return normalizedUpstreamKeyImportOptions{}, errors.New("channel name prefix is required")
	}
	if utf8.RuneCountInString(normalized.NamePrefix) > upstreamImportTextMaxLength {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel name prefix must not exceed %d characters", upstreamImportTextMaxLength)
	}

	if options.Priority != nil {
		normalized.Priority = *options.Priority
	}
	if normalized.Priority < upstreamImportPriorityMin || normalized.Priority > upstreamImportPriorityMax {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel priority must be between %d and %d", upstreamImportPriorityMin, upstreamImportPriorityMax)
	}

	var weight int64
	if options.Weight != nil {
		weight = *options.Weight
	}
	if weight < 0 || weight > upstreamImportWeightMax {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel weight must be between 0 and %d", upstreamImportWeightMax)
	}
	normalized.Weight = uint(weight)

	if options.TestModel != nil {
		normalized.TestModel = strings.TrimSpace(*options.TestModel)
	}
	if utf8.RuneCountInString(normalized.TestModel) > upstreamImportTextMaxLength {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("test model must not exceed %d characters", upstreamImportTextMaxLength)
	}

	if options.Models != nil {
		normalized.HasCustomModels = true
		if len(*options.Models) > upstreamImportModelLimit {
			return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("cannot select more than %d models", upstreamImportModelLimit)
		}
		seenModels := make(map[string]struct{}, len(*options.Models))
		for _, rawModel := range *options.Models {
			modelName := strings.TrimSpace(rawModel)
			if modelName == "" {
				continue
			}
			if strings.Contains(modelName, ",") {
				return normalizedUpstreamKeyImportOptions{}, errors.New("model names must not contain commas")
			}
			if utf8.RuneCountInString(modelName) > upstreamImportTextMaxLength {
				return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("model names must not exceed %d characters", upstreamImportTextMaxLength)
			}
			if _, exists := seenModels[modelName]; exists {
				continue
			}
			seenModels[modelName] = struct{}{}
			normalized.Models = append(normalized.Models, modelName)
		}
		sort.Strings(normalized.Models)
	}

	if options.AutoBan != nil {
		normalized.AutoBan = *options.AutoBan
	}
	if normalized.AutoBan != 0 && normalized.AutoBan != 1 {
		return normalizedUpstreamKeyImportOptions{}, errors.New("auto ban must be 0 or 1")
	}

	if options.Remark != nil {
		normalized.Remark = strings.TrimSpace(*options.Remark)
	}
	if utf8.RuneCountInString(normalized.Remark) > upstreamImportTextMaxLength {
		return normalizedUpstreamKeyImportOptions{}, fmt.Errorf("channel remark must not exceed %d characters", upstreamImportTextMaxLength)
	}
	return normalized, nil
}

func normalizeUpstreamKeyIDs(keyIDs []int64) ([]int64, error) {
	if len(keyIDs) == 0 {
		return nil, errors.New("select at least one upstream key")
	}
	if len(keyIDs) > upstreamImportKeyLimit {
		return nil, fmt.Errorf("cannot import more than %d upstream keys at once", upstreamImportKeyLimit)
	}
	normalized := make([]int64, 0, len(keyIDs))
	seen := make(map[int64]struct{}, len(keyIDs))
	for _, keyID := range keyIDs {
		if keyID <= 0 {
			return nil, errors.New("upstream key ids must be positive")
		}
		if _, exists := seen[keyID]; exists {
			continue
		}
		seen[keyID] = struct{}{}
		normalized = append(normalized, keyID)
	}
	return normalized, nil
}

func FetchUpstreamChannelKeyModels(ctx context.Context, upstreamChannelID int, keyIDs []int64) ([]string, error) {
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	return fetchUpstreamChannelKeyModels(ctx, client, upstreamChannelID, keyIDs)
}

func fetchUpstreamChannelKeyModels(ctx context.Context, client *http.Client, upstreamChannelID int, keyIDs []int64) ([]string, error) {
	normalizedKeyIDs, err := normalizeUpstreamKeyIDs(keyIDs)
	if err != nil {
		return nil, err
	}

	lock := upstreamRefreshLock(upstreamChannelID)
	lock.Lock()
	defer lock.Unlock()

	row, err := model.GetUpstreamChannelByID(upstreamChannelID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.Username) == "" {
		return nil, errors.New("upstream username is not configured")
	}
	password, err := row.DecryptPassword()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.SnapshotJSON) == "" {
		return nil, errors.New("refresh the upstream channel before fetching models")
	}

	var snapshot UpstreamSnapshot
	if err = common.UnmarshalJsonStr(row.SnapshotJSON, &snapshot); err != nil {
		return nil, fmt.Errorf("decode upstream snapshot: %w", err)
	}
	keyIDsInSnapshot := make(map[int64]struct{}, len(snapshot.Keys))
	for _, key := range snapshot.Keys {
		keyIDsInSnapshot[key.ID] = struct{}{}
	}
	for _, keyID := range normalizedKeyIDs {
		if _, exists := keyIDsInSnapshot[keyID]; !exists {
			return nil, fmt.Errorf("upstream key %d is not present in the latest snapshot", keyID)
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, upstreamChannelRefreshTimeout)
	defer cancel()
	provider := snapshot.Provider
	if provider == "" {
		provider = row.Provider
	}
	credential := UpstreamCredential{AuthType: row.EffectiveAuthType(), Username: row.Username, Password: password}
	modelSet := make(map[string]struct{})
	for _, keyID := range normalizedKeyIDs {
		fullKey, fetchErr := FetchUpstreamFullKey(fetchCtx, client, row.BaseURL, provider, credential, keyID)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch upstream key %d: %w", keyID, fetchErr)
		}
		fullKey = strings.TrimSpace(fullKey)
		if fullKey == "" {
			return nil, fmt.Errorf("upstream key %d is empty", keyID)
		}
		models, fetchErr := fetchUpstreamKeyModels(fetchCtx, client, row.BaseURL, fullKey)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch models for upstream key %d: %w", keyID, fetchErr)
		}
		for _, modelName := range models {
			modelSet[modelName] = struct{}{}
		}
	}
	models := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models, nil
}

func fetchUpstreamKeyModels(ctx context.Context, client *http.Client, baseURL string, key string) ([]string, error) {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	if err := doUpstreamJSON(ctx, client, http.MethodGet, upstreamURL(baseURL, "/v1/models"), nil, headers, &response); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(response.Data))
	models := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		modelName := strings.TrimSpace(item.ID)
		if modelName == "" {
			continue
		}
		if _, exists := seen[modelName]; exists {
			continue
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, errors.New("upstream model list is empty")
	}
	return models, nil
}
