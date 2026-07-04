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
	TagID      int    `json:"tag_id" gorm:"column:tag_id"`
	TagName    string `json:"tag_name" gorm:"column:tag_name"`
	UserID     int    `json:"user_id,omitempty" gorm:"column:user_id"`
	Username   string `json:"username,omitempty" gorm:"column:username"`
	TokenID    int    `json:"token_id" gorm:"column:token_id"`
	TokenName  string `json:"token_name" gorm:"column:token_name"`
	TokenUsed  int    `json:"token_used" gorm:"column:token_used"`
	Count      int    `json:"count" gorm:"column:count"`
	Quota      int    `json:"quota" gorm:"column:quota"`
	LastUsedAt int64  `json:"last_used_at" gorm:"-"`
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
	rows := make([]*TokenTagQuotaData, 0)
	selectFields := "token_tags.id as tag_id, token_tags.name as tag_name, quota_data.user_id, quota_data.username, quota_data.token_id, tokens.name as token_name, sum(quota_data.count) as count, sum(quota_data.quota) as quota, sum(quota_data.token_used) as token_used"
	groupFields := "token_tags.id, token_tags.name, quota_data.user_id, quota_data.username, quota_data.token_id, tokens.name"
	query := DB.Table("quota_data").
		Joins("join token_tag_bindings on token_tag_bindings.token_id = quota_data.token_id").
		Joins("join token_tags on token_tags.id = token_tag_bindings.tag_id").
		Joins("left join tokens on tokens.id = quota_data.token_id").
		Where("quota_data.created_at >= ? and quota_data.created_at <= ?", startTime, endTime)

	if role < common.RoleAdminUser {
		selectFields = "token_tags.id as tag_id, token_tags.name as tag_name, quota_data.token_id, tokens.name as token_name, sum(quota_data.count) as count, sum(quota_data.quota) as quota, sum(quota_data.token_used) as token_used"
		groupFields = "token_tags.id, token_tags.name, quota_data.token_id, tokens.name"
		query = query.Where("quota_data.user_id = ? and token_tags.user_id = ?", userID, userID)
	} else {
		var err error
		if query, err = applyHiddenUserFilter(query, "quota_data.user_id", true); err != nil {
			return rows, err
		}
		if username != "" {
			query = query.Where("quota_data.username = ?", username)
		}
	}
	if tokenTag != "" {
		_, nameKeys, err := normalizeTokenTagNames([]string{tokenTag})
		if err != nil {
			return nil, err
		}
		if len(nameKeys) == 0 {
			return rows, nil
		}
		query = query.Where("token_tags.name_key = ?", nameKeys[0])
	}

	err := query.Select(selectFields).
		Group(groupFields).
		Order("quota DESC").
		Find(&rows).Error
	if err != nil {
		return rows, err
	}
	return rows, fillTokenTagLastUsedAt(rows, startTime, endTime)
}

func fillTokenTagLastUsedAt(rows []*TokenTagQuotaData, startTime int64, endTime int64) error {
	if len(rows) == 0 || LOG_DB == nil {
		return nil
	}
	tokenSet := make(map[int]struct{}, len(rows))
	tokenIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.TokenID == 0 {
			continue
		}
		if _, ok := tokenSet[row.TokenID]; ok {
			continue
		}
		tokenSet[row.TokenID] = struct{}{}
		tokenIDs = append(tokenIDs, row.TokenID)
	}
	if len(tokenIDs) == 0 {
		return nil
	}

	var lastUsedRows []struct {
		TokenID    int   `gorm:"column:token_id"`
		LastUsedAt int64 `gorm:"column:last_used_at"`
	}
	if err := LOG_DB.Table("logs").
		Select("token_id, max(created_at) as last_used_at").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Where("token_id IN ?", tokenIDs).
		Group("token_id").
		Scan(&lastUsedRows).Error; err != nil {
		return err
	}

	lastUsedByToken := make(map[int]int64, len(lastUsedRows))
	for _, row := range lastUsedRows {
		lastUsedByToken[row.TokenID] = row.LastUsedAt
	}
	for _, row := range rows {
		row.LastUsedAt = lastUsedByToken[row.TokenID]
	}
	return nil
}
