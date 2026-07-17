package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalcNextTokenQuotaResetTimeAlignsNaturalBoundaries(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	base := time.Date(2026, time.July, 17, 14, 35, 20, 0, location)

	tests := []struct {
		name     string
		period   string
		expected time.Time
	}{
		{name: "hourly", period: TokenQuotaResetHourly, expected: time.Date(2026, time.July, 17, 15, 0, 0, 0, location)},
		{name: "daily", period: TokenQuotaResetDaily, expected: time.Date(2026, time.July, 18, 0, 0, 0, 0, location)},
		{name: "weekly", period: TokenQuotaResetWeekly, expected: time.Date(2026, time.July, 20, 0, 0, 0, 0, location)},
		{name: "monthly", period: TokenQuotaResetMonthly, expected: time.Date(2026, time.August, 1, 0, 0, 0, 0, location)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.Unix(), calcNextTokenQuotaResetTime(base, tt.period, 0))
		})
	}
}

func TestCalcNextTokenQuotaResetTimeUsesRollingCustomHourInterval(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	base := time.Date(2026, time.July, 17, 14, 35, 20, 0, location)

	assert.Equal(
		t,
		base.Add(6*time.Hour).Unix(),
		calcNextTokenQuotaResetTime(base, TokenQuotaResetCustomHours, 6),
	)
}

func TestApplyTokenQuotaResetConfigRejectsInvalidCustomHourInterval(t *testing.T) {
	token := Token{RemainQuota: 100}
	config := TokenQuotaResetConfig{
		Enabled:       true,
		Period:        TokenQuotaResetCustomHours,
		Amount:        50,
		IntervalHours: 0,
		CarryOver:     true,
	}

	_, err := token.ApplyQuotaResetConfig(config, time.Now())
	require.ErrorContains(t, err, "interval hours must be greater than zero")
}

func TestCustomHourQuotaResetCarriesEveryElapsedInterval(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 35, 20, 0, time.Local)
	token := Token{
		UnlimitedQuota:          true,
		QuotaResetEnabled:       true,
		QuotaResetPeriod:        TokenQuotaResetCustomHours,
		QuotaResetIntervalHours: 6,
		QuotaResetAmount:        20,
		QuotaResetRemaining:     5,
		QuotaResetCarryOver:     true,
		QuotaResetLastTime:      now.Unix(),
		QuotaResetNextTime:      now.Add(6 * time.Hour).Unix(),
		QuotaResetVersion:       3,
	}

	changed := resetTokenQuotaIfDue(&token, now.Add(19*time.Hour).Unix())

	assert.True(t, changed)
	assert.Equal(t, 65, token.QuotaResetRemaining)
	assert.Equal(t, now.Add(18*time.Hour).Unix(), token.QuotaResetLastTime)
	assert.Equal(t, now.Add(24*time.Hour).Unix(), token.QuotaResetNextTime)
	assert.Equal(t, int64(6), token.QuotaResetVersion)
}

func TestApplyTokenQuotaResetConfigTreatsIntervalAsConfiguration(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 35, 20, 0, time.Local)
	token := Token{UnlimitedQuota: true}
	config := TokenQuotaResetConfig{
		Enabled:       true,
		Period:        TokenQuotaResetCustomHours,
		Amount:        50,
		IntervalHours: 6,
		CarryOver:     true,
	}

	changed, err := token.ApplyQuotaResetConfig(config, now)
	require.NoError(t, err)
	require.True(t, changed)
	token.QuotaResetRemaining = 7

	changed, err = token.ApplyQuotaResetConfig(config, now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, 7, token.QuotaResetRemaining)

	config.IntervalHours = 12
	changed, err = token.ApplyQuotaResetConfig(config, now.Add(2*time.Hour))
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 50, token.QuotaResetRemaining)
	assert.Equal(t, 12, token.QuotaResetIntervalHours)
	assert.Equal(t, now.Add(14*time.Hour).Unix(), token.QuotaResetNextTime)
}

func TestApplyTokenQuotaResetConfigInitializesAndDoesNotReplenishUnchangedConfig(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, time.July, 17, 14, 35, 20, 0, location)
	token := Token{RemainQuota: 80}
	config := TokenQuotaResetConfig{
		Enabled:   true,
		Period:    TokenQuotaResetDaily,
		Amount:    100,
		CarryOver: true,
	}

	changed, err := token.ApplyQuotaResetConfig(config, now)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, token.QuotaResetEnabled)
	assert.Equal(t, 80, token.QuotaResetRemaining)
	assert.Equal(t, now.Unix(), token.QuotaResetLastTime)
	assert.Equal(t, time.Date(2026, time.July, 18, 0, 0, 0, 0, location).Unix(), token.QuotaResetNextTime)
	assert.Equal(t, int64(1), token.QuotaResetVersion)

	token.QuotaResetRemaining = 25
	changed, err = token.ApplyQuotaResetConfig(config, now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, 25, token.QuotaResetRemaining)
	assert.Equal(t, int64(1), token.QuotaResetVersion)
}

func TestApplyTokenQuotaResetConfigDisablesRuntimeStateAndKeepsSettings(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 0, 0, 0, time.Local)
	token := Token{
		RemainQuota:         200,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 40,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  now.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  now.Unix(),
		QuotaResetVersion:   5,
	}

	changed, err := token.ApplyQuotaResetConfig(TokenQuotaResetConfig{
		Enabled:   false,
		Period:    token.QuotaResetPeriod,
		Amount:    token.QuotaResetAmount,
		CarryOver: token.QuotaResetCarryOver,
	}, now)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.False(t, token.QuotaResetEnabled)
	assert.Equal(t, TokenQuotaResetHourly, token.QuotaResetPeriod)
	assert.Equal(t, 100, token.QuotaResetAmount)
	assert.True(t, token.QuotaResetCarryOver)
	assert.Zero(t, token.QuotaResetRemaining)
	assert.Zero(t, token.QuotaResetLastTime)
	assert.Zero(t, token.QuotaResetNextTime)
	assert.Equal(t, int64(6), token.QuotaResetVersion)
}

func TestResetTokenQuotaCarriesEveryElapsedPeriodAndCapsFiniteBalance(t *testing.T) {
	truncateTables(t)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	common.BatchUpdateEnabled = false
	next := time.Date(2026, time.July, 17, 10, 0, 0, 0, location)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-carry",
		Name:                "carry",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         120,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    50,
		QuotaResetRemaining: 20,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  next.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  next.Unix(),
		QuotaResetVersion:   7,
	}
	require.NoError(t, DB.Create(&token).Error)

	updated, reset, err := ResetTokenQuotaIfDue(token.Id, time.Date(2026, time.July, 17, 12, 30, 0, 0, location).Unix())
	require.NoError(t, err)
	assert.True(t, reset)
	assert.Equal(t, 120, updated.QuotaResetRemaining)
	assert.Equal(t, time.Date(2026, time.July, 17, 12, 0, 0, 0, location).Unix(), updated.QuotaResetLastTime)
	assert.Equal(t, time.Date(2026, time.July, 17, 13, 0, 0, 0, location).Unix(), updated.QuotaResetNextTime)
	assert.Equal(t, int64(10), updated.QuotaResetVersion)
}

func TestConsumeAndRefundTokenQuotaHonorsResetVersion(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = false
	now := time.Now()
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-refund",
		Name:                "refund",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		UsedQuota:           0,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    60,
		QuotaResetRemaining: 60,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  now.Unix(),
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   3,
	}
	require.NoError(t, DB.Create(&token).Error)

	charge, err := ConsumeTokenQuota(token.Id, token.Key, 40)
	require.NoError(t, err)
	assert.Equal(t, TokenQuotaCharge{Amount: 40, ResetVersion: 3, TotalDeducted: true, PeriodDeducted: true}, *charge)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 60, stored.RemainQuota)
	assert.Equal(t, 20, stored.QuotaResetRemaining)
	assert.Equal(t, 40, stored.UsedQuota)

	require.NoError(t, RefundTokenQuota(token.Id, token.Key, 20, charge))
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 80, stored.RemainQuota)
	assert.Equal(t, 40, stored.QuotaResetRemaining)
	assert.Equal(t, 20, stored.UsedQuota)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("quota_reset_version", 4).Error)
	require.NoError(t, RefundTokenQuota(token.Id, token.Key, 20, charge))
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 100, stored.RemainQuota)
	assert.Equal(t, 40, stored.QuotaResetRemaining)
	assert.Equal(t, 0, stored.UsedQuota)
}

func TestRefundTokenQuotaAdvancesDuePeriodBeforeCheckingChargeVersion(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = false
	currentHour := time.Now().Truncate(time.Hour)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-refund-due",
		Name:                "refund-due",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         60,
		UsedQuota:           40,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    60,
		QuotaResetRemaining: 20,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  currentHour.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  currentHour.Unix(),
		QuotaResetVersion:   3,
	}
	require.NoError(t, DB.Create(&token).Error)
	charge := &TokenQuotaCharge{
		Amount:         40,
		ResetVersion:   3,
		TotalDeducted:  true,
		PeriodDeducted: true,
	}

	require.NoError(t, RefundTokenQuota(token.Id, token.Key, 20, charge))

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 80, stored.RemainQuota)
	assert.Equal(t, 60, stored.QuotaResetRemaining)
	assert.Equal(t, 20, stored.UsedQuota)
	assert.Equal(t, int64(4), stored.QuotaResetVersion)
	assert.Equal(t, currentHour.Unix(), stored.QuotaResetLastTime)
	assert.Greater(t, stored.QuotaResetNextTime, time.Now().Unix())
}

func TestConsumeUnlimitedTokenOnlyDeductsPeriodBalance(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = false
	now := time.Now()
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-unlimited",
		Name:                "unlimited",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         77,
		UnlimitedQuota:      true,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    50,
		QuotaResetRemaining: 50,
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   2,
	}
	require.NoError(t, DB.Create(&token).Error)

	charge, err := ConsumeTokenQuota(token.Id, token.Key, 20)
	require.NoError(t, err)
	assert.False(t, charge.TotalDeducted)
	assert.True(t, charge.PeriodDeducted)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 77, stored.RemainQuota)
	assert.Equal(t, 30, stored.QuotaResetRemaining)
	assert.Equal(t, 20, stored.UsedQuota)
}

func TestConsumeTokenQuotaRejectsPeriodShortageWithoutPartialDeduction(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = false
	now := time.Now()
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-shortage",
		Name:                "shortage",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    10,
		QuotaResetRemaining: 10,
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	_, err := ConsumeTokenQuota(token.Id, token.Key, 20)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenQuotaResetExhausted))

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, stored.Status)
	assert.Equal(t, 100, stored.RemainQuota)
	assert.Equal(t, 10, stored.QuotaResetRemaining)
	assert.Zero(t, stored.UsedQuota)
}

func TestConsumeTokenQuotaPersistsDueResetWhenChargeIsRejected(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = false
	currentHour := time.Now().Truncate(time.Hour)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-due-shortage",
		Name:                "due-shortage",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    10,
		QuotaResetRemaining: 0,
		QuotaResetCarryOver: false,
		QuotaResetLastTime:  currentHour.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  currentHour.Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	_, err := ConsumeTokenQuota(token.Id, token.Key, 20)
	require.ErrorIs(t, err, ErrTokenQuotaResetExhausted)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 100, stored.RemainQuota)
	assert.Equal(t, 10, stored.QuotaResetRemaining)
	assert.Zero(t, stored.UsedQuota)
	assert.Equal(t, int64(2), stored.QuotaResetVersion)
	assert.Equal(t, currentHour.Unix(), stored.QuotaResetLastTime)
	assert.Greater(t, stored.QuotaResetNextTime, time.Now().Unix())
}

func TestResetTokenQuotaWithoutCarryOverReplacesBalance(t *testing.T) {
	truncateTables(t)
	next := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.Local)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-replace",
		Name:                "replace",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         500,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    50,
		QuotaResetRemaining: 20,
		QuotaResetCarryOver: false,
		QuotaResetLastTime:  next.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  next.Unix(),
		QuotaResetVersion:   2,
	}
	require.NoError(t, DB.Create(&token).Error)

	updated, reset, err := ResetTokenQuotaIfDue(token.Id, next.Add(2*time.Hour+30*time.Minute).Unix())
	require.NoError(t, err)
	assert.True(t, reset)
	assert.Equal(t, 50, updated.QuotaResetRemaining)
	assert.Equal(t, int64(5), updated.QuotaResetVersion)
}

func TestConsumeTokenQuotaRejectsFiniteTotalShortageWithoutPeriodDeduction(t *testing.T) {
	truncateTables(t)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-total-shortage",
		Name:                "total-shortage",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         10,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 100,
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	_, err := ConsumeTokenQuota(token.Id, token.Key, 20)
	require.ErrorIs(t, err, ErrTokenQuotaExhausted)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 10, stored.RemainQuota)
	assert.Equal(t, 100, stored.QuotaResetRemaining)
	assert.Zero(t, stored.UsedQuota)
}

func TestLegacyBatchTokenQuotaUpdateClampsPeriodicBalance(t *testing.T) {
	truncateTables(t)
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = false })
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-batch",
		Name:                "batch",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 90,
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	addNewRecord(BatchUpdateTypeTokenQuota, token.Id, -80)
	batchUpdate()

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 20, stored.RemainQuota)
	assert.Equal(t, 20, stored.QuotaResetRemaining)
	assert.Equal(t, 80, stored.UsedQuota)
}

func TestTokenUpdateClampsPeriodicBalanceOnlyWhenTotalQuotaDrops(t *testing.T) {
	truncateTables(t)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-update",
		Name:                "update",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 80,
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	token.RemainQuota = 50
	require.NoError(t, token.Update())
	assert.Equal(t, 50, token.QuotaResetRemaining)

	token.RemainQuota = 120
	require.NoError(t, token.Update())
	assert.Equal(t, 50, token.QuotaResetRemaining)
}

func TestTokenUpdateWithTagsPreservesConcurrentPeriodicRuntimeState(t *testing.T) {
	truncateTables(t)
	now := time.Now().Truncate(time.Second)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-stale-update",
		Name:                "before",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 80,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  now.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	stale := token
	newLastTime := now.Unix()
	newNextTime := now.Add(2 * time.Hour).Unix()
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_remaining": 50,
		"quota_reset_last_time": newLastTime,
		"quota_reset_next_time": newNextTime,
		"quota_reset_version":   2,
	}).Error)

	stale.Name = "after"
	require.NoError(t, stale.UpdateWithTags(nil, nil))

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, "after", stored.Name)
	assert.Equal(t, 50, stored.QuotaResetRemaining)
	assert.Equal(t, newLastTime, stored.QuotaResetLastTime)
	assert.Equal(t, newNextTime, stored.QuotaResetNextTime)
	assert.Equal(t, int64(2), stored.QuotaResetVersion)
}

func TestTokenUpdateWithTagsWithoutPeriodicPatchPreservesConcurrentConfiguration(t *testing.T) {
	truncateTables(t)
	now := time.Now().Truncate(time.Second)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-stale-config",
		Name:                "before",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         200,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 80,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  now.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	stale := token
	newLastTime := now.Unix()
	newNextTime := now.Add(7 * 24 * time.Hour).Unix()
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_period":     TokenQuotaResetWeekly,
		"quota_reset_amount":     60,
		"quota_reset_remaining":  40,
		"quota_reset_carry_over": false,
		"quota_reset_last_time":  newLastTime,
		"quota_reset_next_time":  newNextTime,
		"quota_reset_version":    2,
	}).Error)

	stale.Name = "after"
	require.NoError(t, stale.UpdateWithTags(nil, nil))

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, "after", stored.Name)
	assert.Equal(t, TokenQuotaResetWeekly, stored.QuotaResetPeriod)
	assert.Equal(t, 60, stored.QuotaResetAmount)
	assert.Equal(t, 40, stored.QuotaResetRemaining)
	assert.False(t, stored.QuotaResetCarryOver)
	assert.Equal(t, newLastTime, stored.QuotaResetLastTime)
	assert.Equal(t, newNextTime, stored.QuotaResetNextTime)
	assert.Equal(t, int64(2), stored.QuotaResetVersion)
}

func TestTokenUpdateWithTagsPeriodicPatchMergesWithLockedConfiguration(t *testing.T) {
	truncateTables(t)
	now := time.Now().Truncate(time.Second)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-partial-patch",
		Name:                "before",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         200,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetDaily,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 80,
		QuotaResetCarryOver: true,
		QuotaResetLastTime:  now.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  now.Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	stale := token
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_period":     TokenQuotaResetWeekly,
		"quota_reset_amount":     60,
		"quota_reset_remaining":  40,
		"quota_reset_carry_over": false,
		"quota_reset_last_time":  now.Unix(),
		"quota_reset_next_time":  now.Add(7 * 24 * time.Hour).Unix(),
		"quota_reset_version":    2,
	}).Error)

	amount := 80
	stale.Name = "after"
	require.NoError(t, stale.UpdateWithTags(nil, &TokenQuotaResetConfigPatch{Amount: &amount}))

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, "after", stored.Name)
	assert.True(t, stored.QuotaResetEnabled)
	assert.Equal(t, TokenQuotaResetWeekly, stored.QuotaResetPeriod)
	assert.Equal(t, 80, stored.QuotaResetAmount)
	assert.Equal(t, 80, stored.QuotaResetRemaining)
	assert.False(t, stored.QuotaResetCarryOver)
	assert.Equal(t, int64(3), stored.QuotaResetVersion)
	assert.GreaterOrEqual(t, stored.QuotaResetLastTime, now.Unix())
	assert.Greater(t, stored.QuotaResetNextTime, stored.QuotaResetLastTime)
}

func TestValidateUserTokenLazilyResetsAndReportsPeriodicExhaustion(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-validate",
		Name:                "validate",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    30,
		QuotaResetRemaining: 0,
		QuotaResetCarryOver: false,
		QuotaResetLastTime:  now.Add(-2 * time.Hour).Unix(),
		QuotaResetNextTime:  now.Add(-time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	validated, err := ValidateUserToken(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, validated.QuotaResetRemaining)
	assert.Equal(t, common.TokenStatusEnabled, validated.Status)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_remaining": 0,
		"quota_reset_next_time": time.Now().Add(time.Hour).Unix(),
	}).Error)
	_, err = ValidateUserToken(token.Key)
	require.ErrorIs(t, err, ErrTokenQuotaResetExhausted)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, stored.Status)
}

func TestResetDueTokenQuotasContinuesPastInvalidDueRows(t *testing.T) {
	truncateTables(t)
	now := time.Now().Truncate(time.Second)
	invalid := Token{
		UserId:              1,
		Key:                 "quota-reset-invalid-due",
		Name:                "invalid-due",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    "invalid",
		QuotaResetAmount:    10,
		QuotaResetRemaining: 0,
		QuotaResetNextTime:  now.Add(-2 * time.Hour).Unix(),
	}
	valid := Token{
		UserId:              1,
		Key:                 "quota-reset-valid-due",
		Name:                "valid-due",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    10,
		QuotaResetRemaining: 0,
		QuotaResetNextTime:  now.Add(-time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&invalid).Error)
	require.NoError(t, DB.Create(&valid).Error)

	count, err := ResetDueTokenQuotas(now.Unix(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var stored Token
	require.NoError(t, DB.First(&stored, valid.Id).Error)
	assert.Equal(t, 10, stored.QuotaResetRemaining)
	assert.Greater(t, stored.QuotaResetNextTime, now.Unix())
}

func TestGetAllUserTokensLazilyResetsDueBalances(t *testing.T) {
	truncateTables(t)
	now := time.Now().Truncate(time.Second)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-list-lazy",
		Name:                "list-lazy",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    30,
		QuotaResetRemaining: 0,
		QuotaResetCarryOver: false,
		QuotaResetNextTime:  now.Add(-time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&token).Error)

	tokens, err := GetAllUserTokens(token.UserId, 0, 10)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, 30, tokens[0].QuotaResetRemaining)
	assert.Greater(t, tokens[0].QuotaResetNextTime, now.Unix())
}

func TestConcurrentTokenQuotaChargesCannotExceedPeriodicBalance(t *testing.T) {
	truncateTables(t)
	token := Token{
		UserId:              1,
		Key:                 "quota-reset-concurrent",
		Name:                "concurrent",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    TokenQuotaResetHourly,
		QuotaResetAmount:    100,
		QuotaResetRemaining: 100,
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, DB.Create(&token).Error)

	const (
		workers = 10
		charge  = 15
	)
	errorsByWorker := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := ConsumeTokenQuota(token.Id, token.Key, charge)
			errorsByWorker <- err
		}()
	}
	waitGroup.Wait()
	close(errorsByWorker)

	successes := 0
	periodShortages := 0
	for err := range errorsByWorker {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTokenQuotaResetExhausted):
			periodShortages++
		default:
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 6, successes)
	assert.Equal(t, 4, periodShortages)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 10, stored.RemainQuota)
	assert.Equal(t, 10, stored.QuotaResetRemaining)
	assert.Equal(t, 90, stored.UsedQuota)
}
