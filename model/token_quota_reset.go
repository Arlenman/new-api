package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	TokenQuotaResetHourly      = "hourly"
	TokenQuotaResetDaily       = "daily"
	TokenQuotaResetWeekly      = "weekly"
	TokenQuotaResetMonthly     = "monthly"
	TokenQuotaResetCustomHours = "custom_hours"

	maxTokenQuotaResetIntervalHours = int64(^uint64(0)>>1) / int64(time.Hour)
)

type TokenQuotaResetConfig struct {
	Enabled       bool
	Period        string
	IntervalHours int
	Amount        int
	CarryOver     bool
}

type TokenQuotaResetConfigPatch struct {
	Enabled       *bool
	Period        *string
	IntervalHours *int
	Amount        *int
	CarryOver     *bool
}

func (patch *TokenQuotaResetConfigPatch) Resolve(current *Token) TokenQuotaResetConfig {
	config := TokenQuotaResetConfig{}
	if current != nil {
		config.Enabled = current.QuotaResetEnabled
		config.Period = current.QuotaResetPeriod
		config.IntervalHours = current.QuotaResetIntervalHours
		config.Amount = current.QuotaResetAmount
		config.CarryOver = current.QuotaResetCarryOver
	}
	if patch == nil {
		return config
	}
	if patch.Enabled != nil {
		config.Enabled = *patch.Enabled
	}
	if patch.Period != nil {
		config.Period = *patch.Period
	}
	if patch.IntervalHours != nil {
		config.IntervalHours = *patch.IntervalHours
	}
	if patch.Amount != nil {
		config.Amount = *patch.Amount
	}
	if patch.CarryOver != nil {
		config.CarryOver = *patch.CarryOver
	} else if config.Enabled && (current == nil || current.QuotaResetPeriod == "") {
		config.CarryOver = true
	}
	return config
}

type TokenQuotaCharge struct {
	Amount         int   `json:"amount"`
	ResetVersion   int64 `json:"reset_version,omitempty"`
	TotalDeducted  bool  `json:"total_deducted,omitempty"`
	PeriodDeducted bool  `json:"period_deducted,omitempty"`
}

func IsValidTokenQuotaResetPeriod(period string) bool {
	switch period {
	case TokenQuotaResetHourly, TokenQuotaResetDaily, TokenQuotaResetWeekly, TokenQuotaResetMonthly, TokenQuotaResetCustomHours:
		return true
	default:
		return false
	}
}

func calcNextTokenQuotaResetTime(base time.Time, period string, intervalHours int) int64 {
	location := base.Location()
	switch period {
	case TokenQuotaResetHourly:
		return time.Date(base.Year(), base.Month(), base.Day(), base.Hour()+1, 0, 0, 0, location).Unix()
	case TokenQuotaResetDaily:
		return time.Date(base.Year(), base.Month(), base.Day()+1, 0, 0, 0, 0, location).Unix()
	case TokenQuotaResetWeekly:
		daysUntilMonday := (int(time.Monday) - int(base.Weekday()) + 7) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return time.Date(base.Year(), base.Month(), base.Day()+daysUntilMonday, 0, 0, 0, 0, location).Unix()
	case TokenQuotaResetMonthly:
		return time.Date(base.Year(), base.Month()+1, 1, 0, 0, 0, 0, location).Unix()
	case TokenQuotaResetCustomHours:
		if intervalHours <= 0 || int64(intervalHours) > maxTokenQuotaResetIntervalHours {
			return 0
		}
		return base.Add(time.Duration(intervalHours) * time.Hour).Unix()
	default:
		return 0
	}
}

func (token *Token) ApplyQuotaResetConfig(config TokenQuotaResetConfig, now time.Time) (bool, error) {
	if config.Period != TokenQuotaResetCustomHours {
		config.IntervalHours = 0
	}
	if config.Enabled {
		if !IsValidTokenQuotaResetPeriod(config.Period) {
			return false, fmt.Errorf("invalid token quota reset period: %s", config.Period)
		}
		if config.Amount <= 0 || config.Amount > common.MaxQuota {
			return false, fmt.Errorf("token quota reset amount must be between 1 and %d", common.MaxQuota)
		}
		if config.Period == TokenQuotaResetCustomHours && (config.IntervalHours <= 0 || int64(config.IntervalHours) > maxTokenQuotaResetIntervalHours) {
			return false, fmt.Errorf("token quota reset interval hours must be greater than zero and no more than %d", maxTokenQuotaResetIntervalHours)
		}
	}

	configChanged := token.QuotaResetPeriod != config.Period ||
		token.QuotaResetIntervalHours != config.IntervalHours ||
		token.QuotaResetAmount != config.Amount ||
		token.QuotaResetCarryOver != config.CarryOver
	enabledChanged := token.QuotaResetEnabled != config.Enabled
	if !configChanged && !enabledChanged {
		return false, nil
	}

	token.QuotaResetEnabled = config.Enabled
	token.QuotaResetPeriod = config.Period
	token.QuotaResetIntervalHours = config.IntervalHours
	token.QuotaResetAmount = config.Amount
	token.QuotaResetCarryOver = config.CarryOver
	token.QuotaResetVersion++

	if !config.Enabled {
		token.QuotaResetRemaining = 0
		token.QuotaResetLastTime = 0
		token.QuotaResetNextTime = 0
		return true, nil
	}

	token.QuotaResetRemaining = config.Amount
	if !token.UnlimitedQuota && token.QuotaResetRemaining > token.RemainQuota {
		token.QuotaResetRemaining = max(token.RemainQuota, 0)
	}
	token.QuotaResetLastTime = now.Unix()
	token.QuotaResetNextTime = calcNextTokenQuotaResetTime(now, config.Period, config.IntervalHours)
	return true, nil
}

func (token *Token) clampQuotaResetRemainingToTotal() {
	if token == nil || !token.QuotaResetEnabled || token.UnlimitedQuota || token.QuotaResetRemaining <= token.RemainQuota {
		return
	}
	token.QuotaResetRemaining = max(token.RemainQuota, 0)
}

func ResetTokenQuotaIfDue(tokenID int, now int64) (*Token, bool, error) {
	var token Token
	reset := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", tokenID).First(&token).Error; err != nil {
			return err
		}
		if !resetTokenQuotaIfDue(&token, now) {
			return nil
		}
		reset = true
		return updateTokenQuotaRuntime(tx, &token)
	})
	if err != nil {
		return nil, false, err
	}
	if reset {
		invalidateTokenCache(token.Key)
	}
	return &token, reset, nil
}

func ResetDueTokenQuotas(now int64, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	type dueToken struct {
		Id                 int
		QuotaResetNextTime int64
	}
	resetCount := 0
	lastNextTime := int64(0)
	lastID := 0
	for {
		var dueTokens []dueToken
		query := DB.Model(&Token{}).
			Where("quota_reset_enabled = ? AND quota_reset_next_time > 0 AND quota_reset_next_time <= ?", true, now).
			Order("quota_reset_next_time ASC, id ASC")
		if lastNextTime > 0 {
			query = query.Where("quota_reset_next_time > ? OR (quota_reset_next_time = ? AND id > ?)", lastNextTime, lastNextTime, lastID)
		}
		err := query.
			Limit(limit).
			Find(&dueTokens).Error
		if err != nil {
			return resetCount, err
		}
		if len(dueTokens) == 0 {
			return resetCount, nil
		}
		for _, due := range dueTokens {
			_, reset, err := ResetTokenQuotaIfDue(due.Id, now)
			if err != nil {
				return resetCount, err
			}
			if reset {
				resetCount++
			}
		}
		last := dueTokens[len(dueTokens)-1]
		lastNextTime = last.QuotaResetNextTime
		lastID = last.Id
		if len(dueTokens) < limit {
			return resetCount, nil
		}
	}
}

func ConsumeTokenQuota(tokenID int, _ string, quota int) (*TokenQuotaCharge, error) {
	if quota < 0 {
		return nil, errors.New("quota 不能为负数！")
	}
	charge := &TokenQuotaCharge{Amount: quota}
	if quota == 0 {
		return charge, nil
	}

	var token Token
	var quotaErr error
	runtimeChanged := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", tokenID).First(&token).Error; err != nil {
			return err
		}

		runtimeChanged = resetTokenQuotaIfDue(&token, common.GetTimestamp())
		if token.QuotaResetEnabled && token.QuotaResetRemaining < quota {
			quotaErr = ErrTokenQuotaResetExhausted
			if runtimeChanged {
				return updateTokenQuotaRuntime(tx, &token)
			}
			return nil
		}
		if !token.UnlimitedQuota && token.RemainQuota < quota {
			quotaErr = ErrTokenQuotaExhausted
			if runtimeChanged {
				return updateTokenQuotaRuntime(tx, &token)
			}
			return nil
		}

		if !token.UnlimitedQuota {
			token.RemainQuota -= quota
			charge.TotalDeducted = true
		}
		if token.QuotaResetEnabled {
			token.QuotaResetRemaining -= quota
			charge.ResetVersion = token.QuotaResetVersion
			charge.PeriodDeducted = true
		}
		token.UsedQuota = safeTokenQuotaAdd(token.UsedQuota, quota, "token used quota")
		token.AccessedTime = common.GetTimestamp()
		return updateTokenQuotaRuntime(tx, &token)
	})
	if err != nil {
		return nil, err
	}
	if quotaErr != nil {
		if runtimeChanged {
			invalidateTokenCache(token.Key)
		}
		return nil, quotaErr
	}
	invalidateTokenCache(token.Key)
	return charge, nil
}

func RefundTokenQuota(tokenID int, _ string, quota int, charge *TokenQuotaCharge) error {
	if charge == nil || charge.Amount <= 0 || quota > charge.Amount {
		return errors.New("invalid token quota charge")
	}
	_, err := RefundTokenQuotaCharges(tokenID, quota, []TokenQuotaCharge{*charge})
	return err
}

func RefundTokenQuotaCharges(tokenID int, quota int, charges []TokenQuotaCharge) ([]TokenQuotaCharge, error) {
	if quota < 0 {
		return nil, errors.New("quota 不能为负数！")
	}
	updatedCharges := make([]TokenQuotaCharge, len(charges))
	copy(updatedCharges, charges)
	if quota == 0 {
		return updatedCharges, nil
	}

	var token Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", tokenID).First(&token).Error; err != nil {
			return err
		}

		resetTokenQuotaIfDue(&token, common.GetTimestamp())
		remaining := quota
		totalRefund := 0
		periodRefund := 0
		for index := len(updatedCharges) - 1; index >= 0 && remaining > 0; index-- {
			charge := &updatedCharges[index]
			if charge.Amount <= 0 {
				continue
			}
			refund := min(charge.Amount, remaining)
			if charge.TotalDeducted {
				totalRefund += refund
			}
			if charge.PeriodDeducted && token.QuotaResetEnabled && token.QuotaResetVersion == charge.ResetVersion {
				periodRefund += refund
			}
			charge.Amount -= refund
			remaining -= refund
		}

		// No credential means a legacy charge. Preserve the old compatibility
		// behavior by returning the unmatched amount to the lifetime quota only.
		totalRefund += remaining
		if totalRefund > 0 {
			token.RemainQuota = safeTokenQuotaAdd(token.RemainQuota, totalRefund, "token remaining quota refund")
		}
		if periodRefund > 0 {
			token.QuotaResetRemaining = safeTokenQuotaAdd(token.QuotaResetRemaining, periodRefund, "token periodic quota refund")
			token.clampQuotaResetRemainingToTotal()
		}
		token.UsedQuota = max(token.UsedQuota-quota, 0)
		token.AccessedTime = common.GetTimestamp()
		return updateTokenQuotaRuntime(tx, &token)
	})
	if err != nil {
		return nil, err
	}
	invalidateTokenCache(token.Key)
	return updatedCharges, nil
}

func resetTokenQuotaIfDue(token *Token, now int64) bool {
	if token == nil || !token.QuotaResetEnabled || token.QuotaResetNextTime <= 0 || token.QuotaResetNextTime > now {
		return false
	}
	if !IsValidTokenQuotaResetPeriod(token.QuotaResetPeriod) || token.QuotaResetAmount <= 0 ||
		(token.QuotaResetPeriod == TokenQuotaResetCustomHours &&
			(token.QuotaResetIntervalHours <= 0 || int64(token.QuotaResetIntervalHours) > maxTokenQuotaResetIntervalHours)) {
		common.SysError(fmt.Sprintf("invalid periodic quota configuration for token %d", token.Id))
		return false
	}

	changed := false
	for token.QuotaResetNextTime > 0 && token.QuotaResetNextTime <= now {
		boundary := token.QuotaResetNextTime
		if token.QuotaResetCarryOver {
			token.QuotaResetRemaining = safeTokenQuotaAdd(token.QuotaResetRemaining, token.QuotaResetAmount, "token periodic quota carry over")
		} else {
			token.QuotaResetRemaining = token.QuotaResetAmount
		}
		token.clampQuotaResetRemainingToTotal()
		token.QuotaResetVersion++
		token.QuotaResetLastTime = boundary
		token.QuotaResetNextTime = calcNextTokenQuotaResetTime(
			time.Unix(boundary, 0).In(time.Local),
			token.QuotaResetPeriod,
			token.QuotaResetIntervalHours,
		)
		changed = true
	}
	return changed
}

func updateTokenQuotaRuntime(tx *gorm.DB, token *Token) error {
	return tx.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]interface{}{
		"remain_quota":          token.RemainQuota,
		"used_quota":            token.UsedQuota,
		"accessed_time":         token.AccessedTime,
		"quota_reset_remaining": token.QuotaResetRemaining,
		"quota_reset_last_time": token.QuotaResetLastTime,
		"quota_reset_next_time": token.QuotaResetNextTime,
		"quota_reset_version":   token.QuotaResetVersion,
	}).Error
}

func safeTokenQuotaAdd(current int, delta int, operation string) int {
	value := int64(current) + int64(delta)
	if value > int64(common.MaxQuota) {
		common.SysError(fmt.Sprintf("%s overflow: current=%d delta=%d", operation, current, delta))
		return common.MaxQuota
	}
	if value < 0 {
		common.SysError(fmt.Sprintf("%s underflow: current=%d delta=%d", operation, current, delta))
		return 0
	}
	return int(value)
}

func invalidateTokenCache(key string) {
	if !common.RedisEnabled || key == "" {
		return
	}
	if err := cacheDeleteToken(key); err != nil {
		common.SysLog("failed to invalidate token cache: " + err.Error())
	}
}

func resetDueTokenQuota(token *Token) (*Token, error) {
	if token == nil || !token.QuotaResetEnabled || token.QuotaResetNextTime <= 0 || token.QuotaResetNextTime > common.GetTimestamp() {
		return token, nil
	}
	updated, _, err := ResetTokenQuotaIfDue(token.Id, common.GetTimestamp())
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func resetDueTokenQuotaList(tokens []*Token) error {
	for index, token := range tokens {
		updated, err := resetDueTokenQuota(token)
		if err != nil {
			return err
		}
		tokens[index] = updated
	}
	return nil
}
