package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenQuotaTestFunding struct {
	preConsumeErr    error
	settleErr        error
	preConsumedQuota int
	settledDelta     int
	refunded         bool
}

func (f *tokenQuotaTestFunding) Source() string { return BillingSourceWallet }
func (f *tokenQuotaTestFunding) PreConsume(amount int) error {
	if f.preConsumeErr == nil {
		f.preConsumedQuota += amount
	}
	return f.preConsumeErr
}
func (f *tokenQuotaTestFunding) Settle(delta int) error {
	if f.settleErr != nil {
		return f.settleErr
	}
	f.settledDelta += delta
	return nil
}
func (f *tokenQuotaTestFunding) Refund() error {
	f.refunded = true
	return nil
}

type quotaResetTestBillingSettler struct {
	preConsumedQuota int
	chargedQuota     int
	reservedTarget   int
	reserveCalls     int
}

func (s *quotaResetTestBillingSettler) Settle(int) error    { return nil }
func (s *quotaResetTestBillingSettler) Refund(*gin.Context) {}
func (s *quotaResetTestBillingSettler) NeedsRefund() bool   { return false }
func (s *quotaResetTestBillingSettler) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}
func (s *quotaResetTestBillingSettler) GetChargedQuota() int {
	return s.chargedQuota
}
func (s *quotaResetTestBillingSettler) Reserve(targetQuota int) error {
	s.reserveCalls++
	s.reservedTarget = targetQuota
	return nil
}

func seedPeriodicBillingToken(t *testing.T, key string, remainQuota int, periodicQuota int, version int64) *model.Token {
	t.Helper()
	token := &model.Token{
		UserId:              1,
		Key:                 key,
		Name:                key,
		Status:              common.TokenStatusEnabled,
		RemainQuota:         remainQuota,
		QuotaResetEnabled:   true,
		QuotaResetPeriod:    model.TokenQuotaResetHourly,
		QuotaResetAmount:    periodicQuota,
		QuotaResetRemaining: periodicQuota,
		QuotaResetCarryOver: true,
		QuotaResetNextTime:  time.Now().Add(time.Hour).Unix(),
		QuotaResetVersion:   version,
	}
	require.NoError(t, model.DB.Create(token).Error)
	return token
}

func newPeriodicBillingSession(token *model.Token, funding FundingSource) (*BillingSession, *gin.Context) {
	ctx, _ := gin.CreateTestContext(nil)
	relayInfo := &relaycommon.RelayInfo{
		UserId:         token.UserId,
		TokenId:        token.Id,
		TokenKey:       token.Key,
		TokenUnlimited: token.UnlimitedQuota,
	}
	return &BillingSession{relayInfo: relayInfo, funding: funding}, ctx
}

func loadPeriodicBillingToken(t *testing.T, tokenID int) model.Token {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	return token
}

func loadBillingUser(t *testing.T, userID int) model.User {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	return user
}

func TestPostConsumeQuotaConsumesTotalAndPeriodicQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	token := seedPeriodicBillingToken(t, "post-consume-periodic", 1000, 300, 1)
	relayInfo := &relaycommon.RelayInfo{
		UserId:         token.UserId,
		TokenId:        token.Id,
		TokenKey:       token.Key,
		TokenUnlimited: false,
	}

	require.NoError(t, PostConsumeQuota(relayInfo, 200, 0, false))

	storedToken := loadPeriodicBillingToken(t, token.Id)
	storedUser := loadBillingUser(t, token.UserId)
	assert.Equal(t, 800, storedUser.Quota)
	assert.Equal(t, 800, storedToken.RemainQuota)
	assert.Equal(t, 100, storedToken.QuotaResetRemaining)
	assert.Equal(t, 200, storedToken.UsedQuota)
}

func TestPostConsumeQuotaPeriodicExhaustionDoesNotChangeWalletOrToken(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	token := seedPeriodicBillingToken(t, "post-consume-periodic-exhausted", 1000, 100, 1)
	relayInfo := &relaycommon.RelayInfo{
		UserId:         token.UserId,
		TokenId:        token.Id,
		TokenKey:       token.Key,
		TokenUnlimited: false,
	}

	err := PostConsumeQuota(relayInfo, 200, 0, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, model.ErrTokenQuotaResetExhausted)

	storedToken := loadPeriodicBillingToken(t, token.Id)
	storedUser := loadBillingUser(t, token.UserId)
	assert.Equal(t, 1000, storedUser.Quota)
	assert.Equal(t, 1000, storedToken.RemainQuota)
	assert.Equal(t, 100, storedToken.QuotaResetRemaining)
	assert.Zero(t, storedToken.UsedQuota)
}

func TestPostConsumeQuotaFundingFailureRollsBackPeriodicTokenCharge(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "post-consume-periodic-funding-failure", 1000, 300, 1)
	relayInfo := &relaycommon.RelayInfo{
		UserId:         token.UserId,
		TokenId:        token.Id,
		TokenKey:       token.Key,
		TokenUnlimited: false,
		BillingSource:  BillingSourceSubscription,
		SubscriptionId: 99999,
	}

	require.Error(t, PostConsumeQuota(relayInfo, 200, 0, false))

	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 1000, storedToken.RemainQuota)
	assert.Equal(t, 300, storedToken.QuotaResetRemaining)
	assert.Zero(t, storedToken.UsedQuota)
}

func TestPostConsumeQuotaNegativeAdjustmentDoesNotInjectPeriodicQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 800)
	token := seedPeriodicBillingToken(t, "post-consume-periodic-negative", 800, 100, 1)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("used_quota", 200).Error)
	relayInfo := &relaycommon.RelayInfo{
		UserId:         token.UserId,
		TokenId:        token.Id,
		TokenKey:       token.Key,
		TokenUnlimited: false,
	}

	require.NoError(t, PostConsumeQuota(relayInfo, -50, 0, false))

	storedToken := loadPeriodicBillingToken(t, token.Id)
	storedUser := loadBillingUser(t, token.UserId)
	assert.Equal(t, 850, storedUser.Quota)
	assert.Equal(t, 850, storedToken.RemainQuota)
	assert.Equal(t, 100, storedToken.QuotaResetRemaining)
	assert.Equal(t, 150, storedToken.UsedQuota)
}

func TestBillingSessionReserveChecksPeriodicQuotaBeforeFunding(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-reserve-periodic-exhausted", 1000, 100, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 50))
	err := session.Reserve(150)
	require.Error(t, err)

	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 50, funding.preConsumedQuota)
	assert.Zero(t, funding.settledDelta)
	assert.Equal(t, 950, storedToken.RemainQuota)
	assert.Equal(t, 50, storedToken.QuotaResetRemaining)
	assert.Equal(t, 50, storedToken.UsedQuota)
}

func TestBillingSessionSettleChecksPeriodicQuotaBeforeFunding(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-settle-periodic-exhausted", 1000, 100, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 50))
	err := session.Settle(150)
	require.Error(t, err)

	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Zero(t, funding.settledDelta)
	assert.Equal(t, 950, storedToken.RemainQuota)
	assert.Equal(t, 50, storedToken.QuotaResetRemaining)
	assert.Equal(t, 50, storedToken.UsedQuota)
}

func TestSettleBillingQuotaReturnsPreConsumedQuotaWhenPeriodicTopUpFails(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-settle-charged-periodic-exhausted", 1000, 100, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)
	session.relayInfo.Billing = session

	require.Nil(t, session.preConsume(ctx, 50))
	chargedQuota, err := SettleBillingQuota(ctx, session.relayInfo, 150)
	require.Error(t, err)

	assert.Equal(t, 50, chargedQuota)
	assert.Equal(t, 50, session.GetChargedQuota())
	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Zero(t, funding.settledDelta)
	assert.Equal(t, 950, storedToken.RemainQuota)
	assert.Equal(t, 50, storedToken.QuotaResetRemaining)
	assert.Equal(t, 50, storedToken.UsedQuota)
}

func TestSettleBillingQuotaReturnsActualQuotaAfterSuccessfulTopUp(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-settle-charged-success", 1000, 500, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)
	session.relayInfo.Billing = session

	require.Nil(t, session.preConsume(ctx, 50))
	chargedQuota, err := SettleBillingQuota(ctx, session.relayInfo, 150)
	require.NoError(t, err)

	assert.Equal(t, 150, chargedQuota)
	assert.Equal(t, 150, session.GetChargedQuota())
	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 100, funding.settledDelta)
	assert.Equal(t, 850, storedToken.RemainQuota)
	assert.Equal(t, 350, storedToken.QuotaResetRemaining)
	assert.Equal(t, 150, storedToken.UsedQuota)
}

func TestSettleBillingQuotaKeepsPreConsumedQuotaWhenFundingTopUpFails(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-settle-charged-funding-failure", 1000, 500, 1)
	funding := &tokenQuotaTestFunding{settleErr: errors.New("funding settle failed")}
	session, ctx := newPeriodicBillingSession(token, funding)
	session.relayInfo.Billing = session

	require.Nil(t, session.preConsume(ctx, 50))
	chargedQuota, err := SettleBillingQuota(ctx, session.relayInfo, 150)
	require.Error(t, err)

	assert.Equal(t, 50, chargedQuota)
	assert.Equal(t, 50, session.GetChargedQuota())
	storedToken := loadPeriodicBillingToken(t, token.Id)
	assert.Zero(t, funding.settledDelta)
	assert.Equal(t, 950, storedToken.RemainQuota)
	assert.Equal(t, 450, storedToken.QuotaResetRemaining)
	assert.Equal(t, 50, storedToken.UsedQuota)
}

func TestPreWssConsumeQuotaReservesOnExistingBillingSession(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	token := seedPeriodicBillingToken(t, "wss-periodic-reserve", 1000, 1000, 1)
	billing := &quotaResetTestBillingSettler{preConsumedQuota: 100}
	relayInfo := &relaycommon.RelayInfo{
		UserId:          token.UserId,
		TokenId:         token.Id,
		TokenKey:        token.Key,
		TokenUnlimited:  false,
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		UserGroup:       "default",
		Billing:         billing,
	}
	ctx, _ := gin.CreateTestContext(nil)
	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 10

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, usage))

	assert.Equal(t, 1, billing.reserveCalls)
	assert.Greater(t, billing.reservedTarget, billing.preConsumedQuota)
	storedToken := loadPeriodicBillingToken(t, token.Id)
	storedUser := loadBillingUser(t, token.UserId)
	assert.Equal(t, 1000, storedUser.Quota)
	assert.Equal(t, 1000, storedToken.RemainQuota)
	assert.Equal(t, 1000, storedToken.QuotaResetRemaining)
	assert.Zero(t, storedToken.UsedQuota)
}

func TestBillingSessionSettleRefundsPeriodicQuotaWithinSameVersion(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-same-version", 1000, 1000, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 300))
	require.NoError(t, session.Settle(100))

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 900, stored.RemainQuota)
	assert.Equal(t, 900, stored.QuotaResetRemaining)
	assert.Equal(t, 100, stored.UsedQuota)
	assert.Equal(t, -200, funding.settledDelta)
}

func TestBillingSessionSettleDoesNotRefundPeriodicQuotaAfterVersionChanges(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-version-change", 1000, 1000, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 300))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_version":   2,
		"quota_reset_remaining": 500,
	}).Error)
	require.NoError(t, session.Settle(100))

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 900, stored.RemainQuota)
	assert.Equal(t, 500, stored.QuotaResetRemaining)
	assert.Equal(t, 100, stored.UsedQuota)
}

func TestRefundTokenQuotaChargesIsAtomicAcrossMultipleCredentials(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-atomic-refund", 100, 100, 1)

	firstCharge, err := model.ConsumeTokenQuota(token.Id, token.Key, 20)
	require.NoError(t, err)
	secondCharge, err := model.ConsumeTokenQuota(token.Id, token.Key, 30)
	require.NoError(t, err)
	charges := []model.TokenQuotaCharge{*firstCharge, *secondCharge}

	require.NoError(t, model.DB.Exec(fmt.Sprintf(`
CREATE TRIGGER fail_multi_charge_refund
BEFORE UPDATE OF remain_quota ON tokens
WHEN OLD.id = %d AND NEW.remain_quota > 80
BEGIN
    SELECT RAISE(ABORT, 'forced refund failure');
END`, token.Id)).Error)
	t.Cleanup(func() {
		_ = model.DB.Exec("DROP TRIGGER IF EXISTS fail_multi_charge_refund").Error
	})

	err = refundTokenQuotaCharges(token.Id, token.Key, 50, charges)
	require.ErrorContains(t, err, "forced refund failure")

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 50, stored.RemainQuota)
	assert.Equal(t, 50, stored.QuotaResetRemaining)
	assert.Equal(t, 50, stored.UsedQuota)
	assert.Equal(t, 20, charges[0].Amount)
	assert.Equal(t, 30, charges[1].Amount)
}

func TestBillingSessionPartialRefundUsesNewestTokenChargesFirst(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-lifo", 1000, 1000, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 100))
	require.NoError(t, session.Reserve(300))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Updates(map[string]any{
		"quota_reset_version":   2,
		"quota_reset_remaining": 500,
	}).Error)
	require.NoError(t, session.Reserve(400))
	require.NoError(t, session.Settle(250))

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 750, stored.RemainQuota)
	assert.Equal(t, 500, stored.QuotaResetRemaining)
	assert.Equal(t, 250, stored.UsedQuota)
}

func TestBillingSessionSettleKeepsChargedQuotaWhenTokenRefundFails(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-refund-failure", 100, 100, 1)
	funding := &tokenQuotaTestFunding{}
	session, ctx := newPeriodicBillingSession(token, funding)

	require.Nil(t, session.preConsume(ctx, 100))
	require.NoError(t, model.DB.Exec(fmt.Sprintf(`
CREATE TRIGGER fail_session_token_refund
BEFORE UPDATE OF remain_quota ON tokens
WHEN OLD.id = %d AND NEW.remain_quota > 0
BEGIN
    SELECT RAISE(ABORT, 'forced session refund failure');
END`, token.Id)).Error)
	t.Cleanup(func() {
		_ = model.DB.Exec("DROP TRIGGER IF EXISTS fail_session_token_refund").Error
	})

	err := session.Settle(50)
	require.ErrorContains(t, err, "forced session refund failure")
	assert.Equal(t, 100, session.GetChargedQuota())
	assert.True(t, session.settled)
	assert.Equal(t, -50, funding.settledDelta)
	assert.Equal(t, 100, session.tokenConsumed)
	require.Len(t, session.tokenCharges, 1)
	assert.Equal(t, 100, session.tokenCharges[0].Amount)

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Zero(t, stored.RemainQuota)
	assert.Zero(t, stored.QuotaResetRemaining)
	assert.Equal(t, 100, stored.UsedQuota)
}

func TestBillingSessionFundingFailureRollsBackPeriodicTokenCharge(t *testing.T) {
	truncate(t)
	token := seedPeriodicBillingToken(t, "billing-periodic-funding-rollback", 1000, 1000, 1)
	funding := &tokenQuotaTestFunding{preConsumeErr: errors.New("funding failed")}
	session, ctx := newPeriodicBillingSession(token, funding)

	apiErr := session.preConsume(ctx, 300)
	require.NotNil(t, apiErr)

	stored := loadPeriodicBillingToken(t, token.Id)
	assert.Equal(t, 1000, stored.RemainQuota)
	assert.Equal(t, 1000, stored.QuotaResetRemaining)
	assert.Zero(t, stored.UsedQuota)
}

func TestBillingSessionPeriodicQuotaDisablesTrustBypass(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserQuota:              common.GetTrustQuota() + 1,
			TokenUnlimited:         true,
			TokenQuotaResetEnabled: true,
		},
		funding: &tokenQuotaTestFunding{},
	}

	assert.False(t, session.shouldTrust(ctx))
}

func TestNewTokenQuotaAPIErrorUsesPeriodicQuotaCode(t *testing.T) {
	apiErr := newTokenQuotaAPIError(model.ErrTokenQuotaResetExhausted)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientTokenQuotaReset, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
