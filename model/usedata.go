package model

import (
	"context"
	"fmt"
	"sort"
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

type TokenTagQuotaTrendData struct {
	TagID     int    `json:"tag_id" gorm:"column:tag_id"`
	TagName   string `json:"tag_name" gorm:"column:tag_name"`
	UserID    int    `json:"user_id,omitempty" gorm:"column:user_id"`
	Username  string `json:"username,omitempty" gorm:"column:username"`
	CreatedAt int64  `json:"created_at" gorm:"column:created_at"`
	TokenUsed int    `json:"token_used" gorm:"column:token_used"`
	Count     int    `json:"count" gorm:"column:count"`
	Quota     int    `json:"quota" gorm:"column:quota"`
}

type TokenTagQuotaAnalyticsResult struct {
	Data    []*TokenTagQuotaData
	Summary TokenTagQuotaSummary
	Trend   []*TokenTagQuotaTrendData
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

// tokenTagHourBucketExpression 把日志时间对齐到整点，明细与趋势共用同一份聚合结果。
const tokenTagHourBucketExpression = "logs.created_at - (logs.created_at % 3600)"

type tokenTagHourlyAggregate struct {
	UserID     int    `gorm:"column:user_id"`
	Username   string `gorm:"column:username"`
	TokenID    int    `gorm:"column:token_id"`
	TokenName  string `gorm:"column:token_name"`
	ModelName  string `gorm:"column:model_name"`
	Hour       int64  `gorm:"column:hour"`
	TokenUsed  int    `gorm:"column:token_used"`
	Count      int    `gorm:"column:count"`
	Quota      int    `gorm:"column:quota"`
	LastUsedAt int64  `gorm:"column:last_used_at"`
}

type tokenTagDetailKey struct {
	UserID    int
	Username  string
	TokenID   int
	ModelName string
}

type tokenTagDetailAggregate struct {
	tokenTagDetailKey
	TokenName  string
	TokenUsed  int
	Count      int
	Quota      int
	LastUsedAt int64
}

type tokenTagTrendSeriesMeta struct {
	UserID   int
	Username string
}

type tokenTagTrendLogAggregate struct {
	TokenID   int   `gorm:"column:token_id"`
	CreatedAt int64 `gorm:"column:created_at"`
	TokenUsed int   `gorm:"column:token_used"`
	Count     int   `gorm:"column:count"`
	Quota     int   `gorm:"column:quota"`
}

type tokenTagAnalyticsTag struct {
	TokenID int    `gorm:"column:token_id"`
	UserID  int    `gorm:"column:user_id"`
	TagID   int    `gorm:"column:tag_id"`
	TagName string `gorm:"column:tag_name"`
	NameKey string `gorm:"column:name_key"`
}

func getTokenIDsByTagKeys(ctx context.Context, nameKeys []string, userID int) ([]int, error) {
	if len(nameKeys) == 0 {
		return []int{}, nil
	}
	query := DB.WithContext(ctx).Table("token_tag_bindings").
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

func getTaggedTokenIDs(ctx context.Context, userID int) ([]int, error) {
	query := DB.WithContext(ctx).Table("token_tag_bindings").
		Distinct("token_tag_bindings.token_id").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id")
	if userID > 0 {
		query = query.Where("token_tags.user_id = ?", userID)
	}
	var tokenIDs []int
	err := query.Pluck("token_tag_bindings.token_id", &tokenIDs).Error
	return tokenIDs, err
}

// tokenTagQuotaScope 是令牌标签统计的过滤条件，明细、趋势与小时汇总共用同一套语义。
type tokenTagQuotaScope struct {
	Username         string
	UserID           int
	Role             int
	IncludedTokenIDs []int
	ExcludedTokenIDs []int
	TaggedTokenIDs   []int
	HasIncludedTags  bool
	IncludeUntagged  bool
	ExcludeUntagged  bool
}

func buildTokenTagLogQuery(logDB *gorm.DB, startTime int64, endTime int64, scope tokenTagQuotaScope) (*gorm.DB, error) {
	query := logDB.Table("logs").
		Where("logs.type = ?", LogTypeConsume).
		Where("logs.created_at >= ? and logs.created_at <= ?", startTime, endTime).
		Where("logs.token_id > 0")
	return applyTokenTagQuotaScope(query, "logs", scope)
}

// applyTokenTagQuotaScope 把用户、用户名与标签过滤套到任意一张按密钥统计的表上。
func applyTokenTagQuotaScope(query *gorm.DB, columnPrefix string, scope tokenTagQuotaScope) (*gorm.DB, error) {
	prefix := ""
	if columnPrefix != "" {
		prefix = columnPrefix + "."
	}
	if scope.Role < common.RoleAdminUser {
		query = query.Where(prefix+"user_id = ?", scope.UserID)
	} else {
		var err error
		query, err = applyHiddenUserFilter(query, prefix+"user_id", true)
		if err != nil {
			return nil, err
		}
		if scope.Username != "" {
			query = query.Where(prefix+"username = ?", scope.Username)
		}
	}
	if scope.HasIncludedTags || scope.IncludeUntagged {
		switch {
		case scope.HasIncludedTags && scope.IncludeUntagged:
			if len(scope.IncludedTokenIDs) == 0 {
				if len(scope.TaggedTokenIDs) > 0 {
					query = query.Where(prefix+"token_id NOT IN ?", scope.TaggedTokenIDs)
				}
			} else if len(scope.TaggedTokenIDs) == 0 {
				query = query.Where(prefix+"token_id IN ?", scope.IncludedTokenIDs)
			} else {
				query = query.Where("("+prefix+"token_id IN ? OR "+prefix+"token_id NOT IN ?)", scope.IncludedTokenIDs, scope.TaggedTokenIDs)
			}
		case scope.HasIncludedTags:
			query = query.Where(prefix+"token_id IN ?", scope.IncludedTokenIDs)
		case len(scope.TaggedTokenIDs) > 0:
			query = query.Where(prefix+"token_id NOT IN ?", scope.TaggedTokenIDs)
		}
	}
	if len(scope.ExcludedTokenIDs) > 0 {
		query = query.Where(prefix+"token_id NOT IN ?", scope.ExcludedTokenIDs)
	}
	if scope.ExcludeUntagged {
		if len(scope.TaggedTokenIDs) == 0 {
			query = query.Where("1 = 0")
		} else {
			query = query.Where(prefix+"token_id IN ?", scope.TaggedTokenIDs)
		}
	}
	return query, nil
}

const tokenTagAnalyticsLogIndexName = "idx_logs_token_tag_analytics"

// ensureTokenTagAnalyticsLogIndex 为令牌标签统计补一个覆盖索引。
// PostgreSQL/SQLite 可以据此走 index-only scan，不再为每一行回表读取
// content/other 等大字段，这是整月统计从数十秒降到秒级的关键。
// MySQL 不能对 TEXT 列建全列索引，退化为只覆盖过滤与分组用的整型列。
func ensureTokenTagAnalyticsLogIndex(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var statement string
	switch db.Dialector.Name() {
	case "postgres", "sqlite":
		statement = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON logs (type, created_at, user_id, token_id, model_name, username, token_name, quota, prompt_tokens, completion_tokens)", tokenTagAnalyticsLogIndexName)
	case "mysql":
		statement = fmt.Sprintf("CREATE INDEX %s ON logs (type, created_at, user_id, token_id)", tokenTagAnalyticsLogIndexName)
	default:
		return nil
	}
	if !db.Migrator().HasTable(&Log{}) || db.Migrator().HasIndex(&Log{}, tokenTagAnalyticsLogIndexName) {
		return nil
	}
	return db.Exec(statement).Error
}

func GetTokenTagQuotaAnalytics(startTime int64, endTime int64, username string, userID int, role int, filters TokenTagQuotaFilters) ([]*TokenTagQuotaData, TokenTagQuotaSummary, error) {
	result, err := queryTokenTagQuotaAnalytics(context.Background(), startTime, endTime, username, userID, role, filters, false)
	return result.Data, result.Summary, err
}

// queryTokenTagQuotaAnalytics 用一次日志聚合同时得到明细、汇总与趋势。
// 日志先按「用户 + 密钥 + 模型 + 小时」聚合，明细与趋势都由这份结果在应用层推导：
// 明细按「用户 + 密钥 + 模型」累加，趋势按「密钥 + 小时」累加。
// 同一时间范围只扫描一遍 logs 表，而不是为明细和趋势各扫一遍。
func queryTokenTagQuotaAnalytics(ctx context.Context, startTime int64, endTime int64, username string, userID int, role int, filters TokenTagQuotaFilters, includeTrend bool) (TokenTagQuotaAnalyticsResult, error) {
	result := TokenTagQuotaAnalyticsResult{
		Data:  make([]*TokenTagQuotaData, 0),
		Trend: make([]*TokenTagQuotaTrendData, 0),
	}
	_, includedNameKeys, err := normalizeTokenTagNames(filters.IncludedTags)
	if err != nil {
		return result, err
	}
	_, excludedNameKeys, err := normalizeTokenTagNames(filters.ExcludedTags)
	if err != nil {
		return result, err
	}

	tagUserID := 0
	if role < common.RoleAdminUser {
		tagUserID = userID
	}
	var includedTokenIDs []int
	if len(includedNameKeys) > 0 {
		includedTokenIDs, err = getTokenIDsByTagKeys(ctx, includedNameKeys, tagUserID)
		if err != nil {
			return result, err
		}
		if len(includedTokenIDs) == 0 && !filters.IncludeUntagged {
			return result, nil
		}
	}
	excludedTokenIDs, err := getTokenIDsByTagKeys(ctx, excludedNameKeys, tagUserID)
	if err != nil {
		return result, err
	}
	var taggedTokenIDs []int
	if filters.IncludeUntagged || filters.ExcludeUntagged {
		taggedTokenIDs, err = getTaggedTokenIDs(ctx, tagUserID)
		if err != nil {
			return result, err
		}
	}

	scope := tokenTagQuotaScope{
		Username:         username,
		UserID:           userID,
		Role:             role,
		IncludedTokenIDs: includedTokenIDs,
		ExcludedTokenIDs: excludedTokenIDs,
		TaggedTokenIDs:   taggedTokenIDs,
		HasIncludedTags:  len(includedNameKeys) > 0,
		IncludeUntagged:  filters.IncludeUntagged,
		ExcludeUntagged:  filters.ExcludeUntagged,
	}
	hourly, err := collectTokenTagHourly(ctx, startTime, endTime, scope)
	if err != nil {
		return result, err
	}
	if len(hourly) == 0 {
		return result, nil
	}

	// 同一小时的同一把密钥可能命中多个模型，趋势需要把它们合并成一个点；
	// 明细则按「用户 + 密钥 + 模型」累加整个时间范围。
	aggregates := make([]*tokenTagDetailAggregate, 0, len(hourly))
	aggregateByKey := make(map[tokenTagDetailKey]*tokenTagDetailAggregate, len(hourly))
	var trendBuckets []tokenTagTrendLogAggregate
	for _, entry := range hourly {
		key := tokenTagDetailKey{UserID: entry.UserID, Username: entry.Username, TokenID: entry.TokenID, ModelName: entry.ModelName}
		aggregate, ok := aggregateByKey[key]
		if !ok {
			aggregate = &tokenTagDetailAggregate{tokenTagDetailKey: key}
			aggregateByKey[key] = aggregate
			aggregates = append(aggregates, aggregate)
		}
		if entry.TokenName > aggregate.TokenName {
			aggregate.TokenName = entry.TokenName
		}
		aggregate.TokenUsed += entry.TokenUsed
		aggregate.Count += entry.Count
		aggregate.Quota += entry.Quota
		if entry.LastUsedAt > aggregate.LastUsedAt {
			aggregate.LastUsedAt = entry.LastUsedAt
		}
		if !includeTrend {
			continue
		}
		if len(trendBuckets) == 0 || trendBuckets[len(trendBuckets)-1].CreatedAt != entry.Hour || trendBuckets[len(trendBuckets)-1].TokenID != entry.TokenID {
			trendBuckets = append(trendBuckets, tokenTagTrendLogAggregate{CreatedAt: entry.Hour, TokenID: entry.TokenID})
		}
		bucket := &trendBuckets[len(trendBuckets)-1]
		bucket.TokenUsed += entry.TokenUsed
		bucket.Count += entry.Count
		bucket.Quota += entry.Quota
	}
	sort.SliceStable(aggregates, func(i, j int) bool {
		if aggregates[i].Quota != aggregates[j].Quota {
			return aggregates[i].Quota > aggregates[j].Quota
		}
		if aggregates[i].TokenID != aggregates[j].TokenID {
			return aggregates[i].TokenID < aggregates[j].TokenID
		}
		return aggregates[i].ModelName < aggregates[j].ModelName
	})
	for _, aggregate := range aggregates {
		result.Summary.Quota += aggregate.Quota
		result.Summary.TokenUsed += aggregate.TokenUsed
		result.Summary.Count += aggregate.Count
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
	if err = DB.WithContext(ctx).Select("id", "name").Where("id IN ?", tokenIDs).Find(&tokens).Error; err != nil {
		return result, err
	}
	tokenNames := make(map[int]string, len(tokens))
	for _, token := range tokens {
		if token.Name != "" {
			tokenNames[token.Id] = token.Name
		}
	}

	var tags []tokenTagAnalyticsTag
	err = DB.WithContext(ctx).Table("token_tag_bindings").
		Select("token_tag_bindings.token_id, token_tags.user_id, token_tags.id as tag_id, token_tags.name as tag_name, token_tags.name_key").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id").
		Where("token_tag_bindings.token_id IN ?", tokenIDs).
		Order("token_tags.name_key ASC").
		Scan(&tags).Error
	if err != nil {
		return result, err
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

	matchedTagsByToken := make(map[int][]tokenTagAnalyticsTag, len(tokenIDs))
	trendMetaByToken := make(map[int]tokenTagTrendSeriesMeta, len(tokenIDs))
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
		rowUserID := aggregate.UserID
		rowUsername := aggregate.Username
		if role < common.RoleAdminUser {
			rowUserID = 0
			rowUsername = ""
		}
		if _, ok := matchedTagsByToken[aggregate.TokenID]; !ok {
			matchedTagsByToken[aggregate.TokenID] = matchedTags
			trendMetaByToken[aggregate.TokenID] = tokenTagTrendSeriesMeta{UserID: rowUserID, Username: rowUsername}
		}
		for _, tag := range matchedTags {
			result.Data = append(result.Data, &TokenTagQuotaData{
				TagID:      tag.TagID,
				TagName:    tag.TagName,
				UserID:     rowUserID,
				Username:   rowUsername,
				TokenID:    aggregate.TokenID,
				TokenName:  tokenName,
				ModelName:  aggregate.ModelName,
				TokenUsed:  aggregate.TokenUsed,
				Count:      aggregate.Count,
				Quota:      aggregate.Quota,
				LastUsedAt: aggregate.LastUsedAt,
			})
		}
	}
	for _, bucket := range trendBuckets {
		meta, ok := trendMetaByToken[bucket.TokenID]
		if !ok {
			continue
		}
		for _, tag := range matchedTagsByToken[bucket.TokenID] {
			result.Trend = append(result.Trend, &TokenTagQuotaTrendData{
				TagID:     tag.TagID,
				TagName:   tag.TagName,
				UserID:    meta.UserID,
				Username:  meta.Username,
				CreatedAt: bucket.CreatedAt,
				TokenUsed: bucket.TokenUsed,
				Count:     bucket.Count,
				Quota:     bucket.Quota,
			})
		}
	}
	return result, nil
}

func GetTokenTagQuotaAnalyticsWithTrend(ctx context.Context, startTime int64, endTime int64, username string, userID int, role int, filters TokenTagQuotaFilters) (TokenTagQuotaAnalyticsResult, error) {
	return queryTokenTagQuotaAnalytics(ctx, startTime, endTime, username, userID, role, filters, true)
}
