package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup  string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID   int    `json:"token_id" gorm:"index;default:0"`
	ChannelID int    `json:"channel_id" gorm:"index;default:0"`
	NodeName  string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	UseGroup  string
	TokenID   int
	ChannelID int
	NodeName  string
}

type TokenTagQuotaData struct {
	TagID      int           `json:"tag_id" gorm:"column:tag_id"`
	TagName    string        `json:"tag_name" gorm:"column:tag_name"`
	UserID     int           `json:"user_id,omitempty" gorm:"column:user_id"`
	Username   string        `json:"username,omitempty" gorm:"column:username"`
	TokenID    int           `json:"token_id" gorm:"column:token_id"`
	TokenName  string        `json:"token_name" gorm:"column:token_name"`
	ModelName  string        `json:"model_name" gorm:"column:model_name"`
	TokenUsed  int           `json:"token_used" gorm:"column:token_used"`
	Count      int           `json:"count" gorm:"column:count"`
	Quota      int           `json:"quota" gorm:"column:quota"`
	LastUsedAt int64         `json:"last_used_at" gorm:"column:last_used_at"`
	IPs        []TokenIPView `json:"ips,omitempty" gorm:"-"`
}

type TokenTagQuotaFilters struct {
	IncludedTags    []string
	ExcludedTags    []string
	IncludeUntagged bool
	ExcludeUntagged bool
}

type TokenTagQuotaSummary struct {
	Quota     int `json:"quota" gorm:"column:quota"`
	TokenUsed int `json:"token_used" gorm:"column:token_used"`
	Count     int `json:"count" gorm:"column:count"`
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		DB.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
			First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			//quotaDataDB.Count += quotaData.Count
			//quotaDataDB.Quota += quotaData.Quota
			//DB.Table("quota_data").Save(quotaDataDB)
			increaseQuotaData(quotaData)
		} else {
			DB.Table("quota_data").Create(quotaData)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(quotaData *QuotaData) {
	err := DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

func applyQuotaTokenTagFilter(tx *gorm.DB, userID int, tokenTag string) (*gorm.DB, error) {
	if tokenTag == "" {
		return tx, nil
	}
	tokenIDs, err := GetTokenIDsByTagName(userID, tokenTag)
	if err != nil {
		return nil, err
	}
	if len(tokenIDs) == 0 {
		return tx.Where("1 = 0"), nil
	}
	return tx.Where("token_id IN ?", tokenIDs), nil
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64, tokenTag string) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	query := DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime)
	if query, err = applyHiddenUserFilter(query, "user_id", true); err != nil {
		return nil, err
	}
	if query, err = applyQuotaTokenTagFilter(query, 0, tokenTag); err != nil {
		return nil, err
	}
	err = query.Group("user_id, username, model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64, tokenTag string) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	query := DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime)
	if query, err = applyQuotaTokenTagFilter(query, userId, tokenTag); err != nil {
		return nil, err
	}
	err = query.Group("user_id, username, model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	query := DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime)
	if query, err = applyHiddenUserFilter(query, "user_id", true); err != nil {
		return nil, err
	}
	err = query.Group("username, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string, tokenTag string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime, tokenTag)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	query := DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime)
	if query, err = applyHiddenUserFilter(query, "user_id", true); err != nil {
		return nil, err
	}
	if query, err = applyQuotaTokenTagFilter(query, 0, tokenTag); err != nil {
		return nil, err
	}
	err = query.Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetTokenTagQuotaData(startTime int64, endTime int64, username string, userID int, role int, tokenTag string) ([]*TokenTagQuotaData, error) {
	includedTags := make([]string, 0, 1)
	if tokenTag != "" {
		includedTags = append(includedTags, tokenTag)
	}
	rows, _, err := GetTokenTagQuotaAnalytics(startTime, endTime, username, userID, role, TokenTagQuotaFilters{
		IncludedTags: includedTags,
	})
	return rows, err
}

type tokenTagLogAggregate struct {
	UserID     int    `gorm:"column:user_id"`
	Username   string `gorm:"column:username"`
	TokenID    int    `gorm:"column:token_id"`
	TokenName  string `gorm:"column:token_name"`
	ModelName  string `gorm:"column:model_name"`
	TokenUsed  int    `gorm:"column:token_used"`
	Count      int    `gorm:"column:count"`
	Quota      int    `gorm:"column:quota"`
	LastUsedAt int64  `gorm:"column:last_used_at"`
}

type tokenTagAnalyticsTag struct {
	TokenID int    `gorm:"column:token_id"`
	UserID  int    `gorm:"column:user_id"`
	TagID   int    `gorm:"column:tag_id"`
	TagName string `gorm:"column:tag_name"`
	NameKey string `gorm:"column:name_key"`
}

func getTokenIDsByTagKeys(nameKeys []string, userID int) ([]int, error) {
	if len(nameKeys) == 0 {
		return []int{}, nil
	}
	query := DB.Table("token_tag_bindings").
		Distinct("token_tag_bindings.token_id").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id").
		Where("token_tags.name_key IN ?", nameKeys)
	if userID > 0 {
		query = query.Where("token_tags.user_id = ?", userID)
	}
	var tokenIDs []int
	err := query.Pluck("token_tag_bindings.token_id", &tokenIDs).Error
	return tokenIDs, err
}

func getTaggedTokenIDs(userID int) ([]int, error) {
	query := DB.Table("token_tag_bindings").
		Distinct("token_tag_bindings.token_id").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id")
	if userID > 0 {
		query = query.Where("token_tags.user_id = ?", userID)
	}
	var tokenIDs []int
	err := query.Pluck("token_tag_bindings.token_id", &tokenIDs).Error
	return tokenIDs, err
}

func buildTokenTagLogQuery(logDB *gorm.DB, startTime int64, endTime int64, username string, userID int, role int, includedTokenIDs []int, excludedTokenIDs []int, taggedTokenIDs []int, hasIncludedTags bool, includeUntagged bool, excludeUntagged bool) (*gorm.DB, error) {
	query := logDB.Table("logs").
		Where("logs.type = ?", LogTypeConsume).
		Where("logs.created_at >= ? and logs.created_at <= ?", startTime, endTime).
		Where("logs.token_id > 0")
	if role < common.RoleAdminUser {
		query = query.Where("logs.user_id = ?", userID)
	} else {
		var err error
		query, err = applyHiddenUserFilter(query, "logs.user_id", true)
		if err != nil {
			return nil, err
		}
		if username != "" {
			query = query.Where("logs.username = ?", username)
		}
	}
	if hasIncludedTags || includeUntagged {
		switch {
		case hasIncludedTags && includeUntagged:
			if len(includedTokenIDs) == 0 {
				if len(taggedTokenIDs) > 0 {
					query = query.Where("logs.token_id NOT IN ?", taggedTokenIDs)
				}
			} else if len(taggedTokenIDs) == 0 {
				query = query.Where("logs.token_id IN ?", includedTokenIDs)
			} else {
				query = query.Where("(logs.token_id IN ? OR logs.token_id NOT IN ?)", includedTokenIDs, taggedTokenIDs)
			}
		case hasIncludedTags:
			query = query.Where("logs.token_id IN ?", includedTokenIDs)
		case len(taggedTokenIDs) > 0:
			query = query.Where("logs.token_id NOT IN ?", taggedTokenIDs)
		}
	}
	if len(excludedTokenIDs) > 0 {
		query = query.Where("logs.token_id NOT IN ?", excludedTokenIDs)
	}
	if excludeUntagged {
		if len(taggedTokenIDs) == 0 {
			query = query.Where("1 = 0")
		} else {
			query = query.Where("logs.token_id IN ?", taggedTokenIDs)
		}
	}
	return query, nil
}

func GetTokenTagQuotaAnalytics(startTime int64, endTime int64, username string, userID int, role int, filters TokenTagQuotaFilters) ([]*TokenTagQuotaData, TokenTagQuotaSummary, error) {
	rows := make([]*TokenTagQuotaData, 0)
	summary := TokenTagQuotaSummary{}
	_, includedNameKeys, err := normalizeTokenTagNames(filters.IncludedTags)
	if err != nil {
		return rows, summary, err
	}
	_, excludedNameKeys, err := normalizeTokenTagNames(filters.ExcludedTags)
	if err != nil {
		return rows, summary, err
	}

	tagUserID := 0
	if role < common.RoleAdminUser {
		tagUserID = userID
	}
	var includedTokenIDs []int
	if len(includedNameKeys) > 0 {
		includedTokenIDs, err = getTokenIDsByTagKeys(includedNameKeys, tagUserID)
		if err != nil {
			return rows, summary, err
		}
		if len(includedTokenIDs) == 0 && !filters.IncludeUntagged {
			return rows, summary, nil
		}
	}
	excludedTokenIDs, err := getTokenIDsByTagKeys(excludedNameKeys, tagUserID)
	if err != nil {
		return rows, summary, err
	}
	var taggedTokenIDs []int
	if filters.IncludeUntagged || filters.ExcludeUntagged {
		taggedTokenIDs, err = getTaggedTokenIDs(tagUserID)
		if err != nil {
			return rows, summary, err
		}
	}

	logDB := LOG_DB
	if logDB == nil {
		logDB = DB
	}
	detailQuery, err := buildTokenTagLogQuery(logDB, startTime, endTime, username, userID, role, includedTokenIDs, excludedTokenIDs, taggedTokenIDs, len(includedNameKeys) > 0, filters.IncludeUntagged, filters.ExcludeUntagged)
	if err != nil {
		return rows, summary, err
	}
	var aggregates []tokenTagLogAggregate
	err = detailQuery.
		Select("logs.user_id, logs.username, logs.token_id, max(logs.token_name) as token_name, logs.model_name, count(logs.id) as count, coalesce(sum(logs.quota), 0) as quota, coalesce(sum(logs.prompt_tokens + logs.completion_tokens), 0) as token_used, max(logs.created_at) as last_used_at").
		Group("logs.user_id, logs.username, logs.token_id, logs.model_name").
		Order("quota DESC").
		Scan(&aggregates).Error
	if err != nil {
		return rows, summary, err
	}

	summaryQuery, err := buildTokenTagLogQuery(logDB, startTime, endTime, username, userID, role, includedTokenIDs, excludedTokenIDs, taggedTokenIDs, len(includedNameKeys) > 0, filters.IncludeUntagged, filters.ExcludeUntagged)
	if err != nil {
		return rows, summary, err
	}
	err = summaryQuery.
		Select("coalesce(sum(logs.quota), 0) as quota, coalesce(sum(logs.prompt_tokens + logs.completion_tokens), 0) as token_used, count(logs.id) as count").
		Scan(&summary).Error
	if err != nil {
		return rows, summary, err
	}
	if len(aggregates) == 0 {
		return rows, summary, nil
	}

	tokenIDs := make([]int, 0, len(aggregates))
	seenTokenIDs := make(map[int]struct{}, len(aggregates))
	for _, aggregate := range aggregates {
		if _, ok := seenTokenIDs[aggregate.TokenID]; ok {
			continue
		}
		seenTokenIDs[aggregate.TokenID] = struct{}{}
		tokenIDs = append(tokenIDs, aggregate.TokenID)
	}
	var tokens []Token
	if err = DB.Select("id", "name").Where("id IN ?", tokenIDs).Find(&tokens).Error; err != nil {
		return rows, summary, err
	}
	tokenNames := make(map[int]string, len(tokens))
	for _, token := range tokens {
		if token.Name != "" {
			tokenNames[token.Id] = token.Name
		}
	}

	var tags []tokenTagAnalyticsTag
	err = DB.Table("token_tag_bindings").
		Select("token_tag_bindings.token_id, token_tags.user_id, token_tags.id as tag_id, token_tags.name as tag_name, token_tags.name_key").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id").
		Where("token_tag_bindings.token_id IN ?", tokenIDs).
		Order("token_tags.name_key ASC").
		Scan(&tags).Error
	if err != nil {
		return rows, summary, err
	}
	tagsByToken := make(map[[2]int][]tokenTagAnalyticsTag)
	for _, tag := range tags {
		key := [2]int{tag.UserID, tag.TokenID}
		tagsByToken[key] = append(tagsByToken[key], tag)
	}
	includedKeySet := make(map[string]struct{}, len(includedNameKeys))
	for _, nameKey := range includedNameKeys {
		includedKeySet[nameKey] = struct{}{}
	}

	for _, aggregate := range aggregates {
		tokenName := aggregate.TokenName
		if storedName := tokenNames[aggregate.TokenID]; storedName != "" {
			tokenName = storedName
		}
		matchedTags := tagsByToken[[2]int{aggregate.UserID, aggregate.TokenID}]
		isUntagged := len(matchedTags) == 0
		if len(includedKeySet) > 0 {
			if isUntagged {
				if !filters.IncludeUntagged || filters.ExcludeUntagged {
					continue
				}
				matchedTags = []tokenTagAnalyticsTag{{}}
			} else {
				selectedTags := make([]tokenTagAnalyticsTag, 0, len(matchedTags))
				for _, tag := range matchedTags {
					if _, ok := includedKeySet[tag.NameKey]; ok {
						selectedTags = append(selectedTags, tag)
					}
				}
				matchedTags = selectedTags
			}
		} else if filters.IncludeUntagged {
			if !isUntagged || filters.ExcludeUntagged {
				continue
			}
			matchedTags = []tokenTagAnalyticsTag{{}}
		}
		if len(matchedTags) == 0 {
			if len(includedKeySet) > 0 || filters.IncludeUntagged || filters.ExcludeUntagged {
				continue
			}
			matchedTags = []tokenTagAnalyticsTag{{}}
		}
		for _, tag := range matchedTags {
			row := &TokenTagQuotaData{
				TagID:      tag.TagID,
				TagName:    tag.TagName,
				UserID:     aggregate.UserID,
				Username:   aggregate.Username,
				TokenID:    aggregate.TokenID,
				TokenName:  tokenName,
				ModelName:  aggregate.ModelName,
				TokenUsed:  aggregate.TokenUsed,
				Count:      aggregate.Count,
				Quota:      aggregate.Quota,
				LastUsedAt: aggregate.LastUsedAt,
			}
			if role < common.RoleAdminUser {
				row.UserID = 0
				row.Username = ""
			}
			rows = append(rows, row)
		}
	}
	return rows, summary, nil
}
