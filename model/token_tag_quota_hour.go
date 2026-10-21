package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// tokenTagQuotaHourSyncInterval 是增量汇总的调度间隔。
	tokenTagQuotaHourSyncInterval = time.Minute
	// tokenTagQuotaHourChunkSize 是单次汇总覆盖的秒数；历史回填按此切片，避免长事务与长查询。
	tokenTagQuotaHourChunkSize = int64(24 * 3600)
)

// TokenTagQuotaHour 是令牌标签统计的小时级汇总。
// 粒度与统计明细一致（用户 + 密钥 + 模型），标签在查询时按当前绑定关系关联，
// 因此绑定关系变化不会让历史统计失真。
type TokenTagQuotaHour struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	Hour       int64  `json:"hour" gorm:"uniqueIndex:idx_token_tag_quota_hour,priority:1"`
	UserID     int    `json:"user_id" gorm:"uniqueIndex:idx_token_tag_quota_hour,priority:2"`
	TokenID    int    `json:"token_id" gorm:"uniqueIndex:idx_token_tag_quota_hour,priority:3"`
	ModelName  string `json:"model_name" gorm:"size:191;default:'';uniqueIndex:idx_token_tag_quota_hour,priority:4"`
	Username   string `json:"username" gorm:"size:191;default:'';uniqueIndex:idx_token_tag_quota_hour,priority:5"`
	TokenName  string `json:"token_name" gorm:"size:191;default:''"`
	TokenUsed  int    `json:"token_used" gorm:"default:0"`
	Count      int    `json:"count" gorm:"default:0"`
	Quota      int    `json:"quota" gorm:"default:0"`
	LastUsedAt int64  `json:"last_used_at" gorm:"default:0"`
}

// TokenTagQuotaHourCursor 记录小时汇总已经完整覆盖的区间 [covered_from, covered_to)，只保留 id=1 一行。
type TokenTagQuotaHourCursor struct {
	Id          int   `json:"id" gorm:"primaryKey"`
	CoveredFrom int64 `json:"covered_from"`
	CoveredTo   int64 `json:"covered_to"`
	SyncedAt    int64 `json:"synced_at"`
}

type tokenTagQuotaHourCoverage struct {
	From int64
	To   int64
}

var (
	tokenTagQuotaHourSyncMutex    sync.Mutex
	tokenTagQuotaHourFallbackOnce sync.Once
)

func StartTokenTagQuotaHourSync() {
	for {
		if err := syncTokenTagQuotaHours(common.GetTimestamp()); err != nil {
			common.SysError("failed to sync token tag quota hours: " + err.Error())
		}
		time.Sleep(tokenTagQuotaHourSyncInterval)
	}
}

// syncTokenTagQuotaHours 把已闭合的整点汇总进 token_tag_quota_hours。
// 先向前补上停机期间新闭合的小时，再从最新的小时往历史回填，
// 这样刚上线或停机很久之后，最近的时间范围最先变快，历史范围逐步补齐。
// 读取侧只信任已覆盖的区间，边界与未覆盖部分回退到 logs，因此任何时刻统计都是完整的。
func syncTokenTagQuotaHours(now int64) error {
	tokenTagQuotaHourSyncMutex.Lock()
	defer tokenTagQuotaHourSyncMutex.Unlock()

	logDB := LOG_DB
	if logDB == nil {
		logDB = DB
	}
	target := now - now%3600
	coverage, err := getTokenTagQuotaHourCoverage(context.Background())
	if err != nil {
		return err
	}
	if coverage.To <= 0 {
		coverage = tokenTagQuotaHourCoverage{From: target, To: target}
		if err := saveTokenTagQuotaHourCoverage(DB, coverage); err != nil {
			return err
		}
	}
	for coverage.To < target {
		next := coverage.To + tokenTagQuotaHourChunkSize
		if next > target {
			next = target
		}
		if err := aggregateTokenTagQuotaHourChunk(logDB, coverage.To, next, tokenTagQuotaHourCoverage{From: coverage.From, To: next}); err != nil {
			return err
		}
		coverage.To = next
	}

	earliest, err := getEarliestConsumeLogHour(logDB)
	if err != nil {
		return err
	}
	if earliest <= 0 {
		return nil
	}
	if coverage.From-earliest > tokenTagQuotaHourChunkSize {
		common.SysLog(fmt.Sprintf("token tag quota hour backfill started: %d -> %d", earliest, coverage.From))
	}
	for coverage.From > earliest {
		start := coverage.From - tokenTagQuotaHourChunkSize
		if start < earliest {
			start = earliest
		}
		if err := aggregateTokenTagQuotaHourChunk(logDB, start, coverage.From, tokenTagQuotaHourCoverage{From: start, To: coverage.To}); err != nil {
			return err
		}
		coverage.From = start
	}
	return nil
}

func aggregateTokenTagQuotaHourChunk(logDB *gorm.DB, start int64, end int64, covered tokenTagQuotaHourCoverage) error {
	const hourExpression = "created_at - (created_at % 3600)"
	rows := make([]TokenTagQuotaHour, 0)
	if err := logDB.Table("logs").
		Select("user_id, username, token_id, model_name, max(token_name) as token_name, "+hourExpression+" as hour, count(*) as count, coalesce(sum(quota), 0) as quota, coalesce(sum(prompt_tokens + completion_tokens), 0) as token_used, max(created_at) as last_used_at").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? and created_at < ?", start, end).
		Group("user_id, username, token_id, model_name, " + hourExpression).
		Scan(&rows).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("hour >= ? and hour < ?", start, end).Delete(&TokenTagQuotaHour{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.CreateInBatches(&rows, 200).Error; err != nil {
				return err
			}
		}
		return saveTokenTagQuotaHourCoverage(tx, covered)
	})
}

func getEarliestConsumeLogHour(logDB *gorm.DB) (int64, error) {
	var earliest int64
	row := logDB.Table("logs").
		Where("type = ?", LogTypeConsume).
		Select("coalesce(min(created_at), 0)").
		Row()
	if err := row.Scan(&earliest); err != nil {
		return 0, err
	}
	if earliest <= 0 {
		return 0, nil
	}
	return earliest - earliest%3600, nil
}

func getTokenTagQuotaHourCoverage(ctx context.Context) (tokenTagQuotaHourCoverage, error) {
	var record TokenTagQuotaHourCursor
	err := DB.WithContext(ctx).Where("id = ?", 1).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tokenTagQuotaHourCoverage{}, nil
	}
	if err != nil {
		return tokenTagQuotaHourCoverage{}, err
	}
	return tokenTagQuotaHourCoverage{From: record.CoveredFrom, To: record.CoveredTo}, nil
}

func saveTokenTagQuotaHourCoverage(tx *gorm.DB, coverage tokenTagQuotaHourCoverage) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"covered_from": coverage.From,
			"covered_to":   coverage.To,
			"synced_at":    common.GetTimestamp(),
		}),
	}).Create(&TokenTagQuotaHourCursor{
		Id:          1,
		CoveredFrom: coverage.From,
		CoveredTo:   coverage.To,
		SyncedAt:    common.GetTimestamp(),
	}).Error
}

func queryTokenTagHourlyLogs(ctx context.Context, startTime int64, endTime int64, scope tokenTagQuotaScope) ([]tokenTagHourlyAggregate, error) {
	logDB := LOG_DB
	if logDB == nil {
		logDB = DB
	}
	query, err := buildTokenTagLogQuery(logDB.WithContext(ctx), startTime, endTime, scope)
	if err != nil {
		return nil, err
	}
	rows := make([]tokenTagHourlyAggregate, 0)
	err = query.
		Select("logs.user_id, logs.username, logs.token_id, max(logs.token_name) as token_name, logs.model_name, " + tokenTagHourBucketExpression + " AS hour, count(*) AS count, coalesce(sum(logs.quota), 0) AS quota, coalesce(sum(logs.prompt_tokens + logs.completion_tokens), 0) AS token_used, max(logs.created_at) AS last_used_at").
		Group("logs.user_id, logs.username, logs.token_id, logs.model_name, " + tokenTagHourBucketExpression).
		Order(tokenTagHourBucketExpression + " ASC, logs.token_id ASC").
		Scan(&rows).Error
	return rows, err
}

func queryTokenTagHourlyRollup(ctx context.Context, startTime int64, endTime int64, scope tokenTagQuotaScope) ([]tokenTagHourlyAggregate, error) {
	query := DB.WithContext(ctx).Table("token_tag_quota_hours").
		Where("token_tag_quota_hours.hour >= ? and token_tag_quota_hours.hour < ?", startTime, endTime).
		Where("token_tag_quota_hours.token_id > 0")
	query, err := applyTokenTagQuotaScope(query, "token_tag_quota_hours", scope)
	if err != nil {
		return nil, err
	}
	rows := make([]tokenTagHourlyAggregate, 0)
	// 汇总表按 (小时, 用户, 密钥, 模型, 用户名) 唯一，无需再分组，直接读出即可。
	err = query.
		Select("user_id, username, token_id, model_name, token_name, hour, token_used, count, quota, last_used_at").
		Order("hour ASC, token_id ASC").
		Scan(&rows).Error
	return rows, err
}

// collectTokenTagHourly 优先读小时汇总，只把不足一小时的边界范围和汇总尚未覆盖的尾部交给 logs。
// 返回结果按 (小时, 密钥) 升序，调用方据此推导明细与趋势。
func collectTokenTagHourly(ctx context.Context, startTime int64, endTime int64, scope tokenTagQuotaScope) ([]tokenTagHourlyAggregate, error) {
	if endTime < startTime {
		return queryTokenTagHourlyLogs(ctx, startTime, endTime, scope)
	}
	covered, err := getTokenTagQuotaHourCoverage(ctx)
	if err != nil {
		tokenTagQuotaHourFallbackOnce.Do(func() {
			common.SysError("token tag quota hour rollup unavailable, falling back to logs: " + err.Error())
		})
		return queryTokenTagHourlyLogs(ctx, startTime, endTime, scope)
	}
	if covered.To <= covered.From {
		return queryTokenTagHourlyLogs(ctx, startTime, endTime, scope)
	}

	firstHour := startTime - startTime%3600
	rollStart := firstHour
	if startTime > firstHour {
		rollStart = firstHour + 3600
	}
	if rollStart < covered.From {
		rollStart = covered.From
	}
	lastHour := endTime - endTime%3600
	rollEnd := lastHour
	if endTime >= lastHour+3599 {
		rollEnd = lastHour + 3600
	}
	if rollEnd > covered.To {
		rollEnd = covered.To
	}
	if rollStart >= rollEnd {
		return queryTokenTagHourlyLogs(ctx, startTime, endTime, scope)
	}

	rollup, err := queryTokenTagHourlyRollup(ctx, rollStart, rollEnd, scope)
	if err != nil {
		tokenTagQuotaHourFallbackOnce.Do(func() {
			common.SysError("token tag quota hour rollup query failed, falling back to logs: " + err.Error())
		})
		return queryTokenTagHourlyLogs(ctx, startTime, endTime, scope)
	}

	hourly := make([]tokenTagHourlyAggregate, 0, len(rollup))
	headEnd := rollStart - 1
	if headEnd > endTime {
		headEnd = endTime
	}
	if startTime <= headEnd {
		head, err := queryTokenTagHourlyLogs(ctx, startTime, headEnd, scope)
		if err != nil {
			return nil, err
		}
		hourly = append(hourly, head...)
	}
	hourly = append(hourly, rollup...)
	tailStart := rollEnd
	if tailStart < startTime {
		tailStart = startTime
	}
	if tailStart <= endTime {
		tail, err := queryTokenTagHourlyLogs(ctx, tailStart, endTime, scope)
		if err != nil {
			return nil, err
		}
		hourly = append(hourly, tail...)
	}
	return hourly, nil
}
