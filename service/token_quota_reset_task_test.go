package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunTokenQuotaResetOnceResetsDueTokens(t *testing.T) {
	truncate(t)
	now := time.Now()
	currentHour := now.Truncate(time.Hour)
	token := model.Token{
		UserId:              1,
		Key:                 "periodic-reset-task",
		Name:                "periodic-reset-task",
		Status:              common.TokenStatusEnabled,
		RemainQuota:         100,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    model.TokenQuotaResetHourly,
		QuotaResetAmount:    30,
		QuotaResetRemaining: 0,
		QuotaResetCarryOver: false,
		QuotaResetLastTime:  currentHour.Add(-time.Hour).Unix(),
		QuotaResetNextTime:  currentHour.Unix(),
		QuotaResetVersion:   1,
	}
	require.NoError(t, model.DB.Create(&token).Error)

	runTokenQuotaResetOnce()

	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, 30, stored.QuotaResetRemaining)
	assert.Greater(t, stored.QuotaResetNextTime, now.Unix())
	assert.Equal(t, int64(2), stored.QuotaResetVersion)
}
