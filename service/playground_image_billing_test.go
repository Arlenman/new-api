package service

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type managedPlaygroundBillingTestSettler struct {
	mu               sync.Mutex
	preConsumedQuota int
	chargedQuota     int
	needsRefund      bool
	settleErr        error
	refundErr        error
	settleCalls      int
	refundCalls      int
}

func (s *managedPlaygroundBillingTestSettler) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settleCalls++
	if s.settleErr != nil {
		return s.settleErr
	}
	s.chargedQuota = actualQuota
	s.needsRefund = false
	return nil
}

func (s *managedPlaygroundBillingTestSettler) Refund(c *gin.Context) {
	_ = s.RefundSync(c)
}

func (s *managedPlaygroundBillingTestSettler) RefundSync(_ *gin.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refundCalls++
	if s.refundErr != nil {
		return s.refundErr
	}
	s.chargedQuota = 0
	s.needsRefund = false
	return nil
}

func (s *managedPlaygroundBillingTestSettler) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefund
}

func (s *managedPlaygroundBillingTestSettler) GetPreConsumedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preConsumedQuota
}

func (s *managedPlaygroundBillingTestSettler) GetChargedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chargedQuota
}

func (s *managedPlaygroundBillingTestSettler) Reserve(int) error {
	return nil
}

func resetManagedPlaygroundBillingTest(t *testing.T) {
	t.Helper()
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserToolImageTask{}))
	require.NoError(t, model.DB.Exec("DELETE FROM user_tool_image_tasks").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM user_tool_image_tasks")
	})
}

func seedManagedPlaygroundBillingTask(t *testing.T, taskID string, userID int) {
	t.Helper()
	task := &model.UserToolImageTask{
		TaskID:          taskID,
		UserID:          userID,
		Tool:            model.UserToolImagePlayground,
		ClientTaskID:    "client_" + taskID,
		RequestSnapshot: model.JSONValue(`{}`),
	}
	require.NoError(t, model.DB.Create(task).Error)
}

func managedPlaygroundBillingContext(taskID, requestID string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyPlaygroundImageTaskID, taskID)
	c.Set(common.RequestIdKey, requestID)
	c.Set("username", "test_user")
	return c
}

func managedPlaygroundBillingRelayInfo(userID, channelID int, requestID string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:                  userID,
		ChannelMeta:             &relaycommon.ChannelMeta{ChannelId: channelID},
		IsPlayground:            true,
		ForcePreConsume:         true,
		OriginModelName:         "gpt-image-2",
		UsingGroup:              "default",
		RequestId:               requestID,
		StartTime:               time.Now(),
		RelayFormat:             types.RelayFormatOpenAIImage,
		FinalRequestRelayFormat: types.RelayFormatOpenAIImage,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
}

func loadManagedPlaygroundBillingTask(t *testing.T, taskID string) model.UserToolImageTask {
	t.Helper()
	var task model.UserToolImageTask
	require.NoError(t, model.DB.Where("task_id = ?", taskID).First(&task).Error)
	return task
}

func countManagedPlaygroundConsumeLogs(t *testing.T, requestID string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.Log{}).
		Where("request_id = ? AND type = ?", requestID, model.LogTypeConsume).
		Count(&count).Error)
	return count
}

func TestPlaygroundImageBillingFinalizeIsIdempotent(t *testing.T) {
	var finalized atomic.Int32
	var refunded atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error { finalized.Add(1); return nil },
		func() error { refunded.Add(1); return nil },
	)

	finalizedNow, err := billing.finalizeOnce()
	require.NoError(t, err)
	require.True(t, finalizedNow)
	finalizedNow, err = billing.finalizeOnce()
	require.NoError(t, err)
	assert.False(t, finalizedNow)
	refundedNow, err := billing.refundOnce()
	require.NoError(t, err)
	assert.False(t, refundedNow)
	assert.Equal(t, int32(1), finalized.Load())
	assert.Zero(t, refunded.Load())
}

func TestPlaygroundImageBillingRefundIsIdempotent(t *testing.T) {
	var finalized atomic.Int32
	var refunded atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error { finalized.Add(1); return nil },
		func() error { refunded.Add(1); return nil },
	)

	refundedNow, err := billing.refundOnce()
	require.NoError(t, err)
	require.True(t, refundedNow)
	refundedNow, err = billing.refundOnce()
	require.NoError(t, err)
	assert.False(t, refundedNow)
	finalizedNow, err := billing.finalizeOnce()
	require.NoError(t, err)
	assert.False(t, finalizedNow)
	assert.Zero(t, finalized.Load())
	assert.Equal(t, int32(1), refunded.Load())
}

func TestPlaygroundImageBillingFinalizeAndRefundRaceSettlesExactlyOnce(t *testing.T) {
	var finalized atomic.Int32
	var refunded atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error { finalized.Add(1); return nil },
		func() error { refunded.Add(1); return nil },
	)

	const workers = 64
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers * 2)
	for range workers {
		go func() {
			defer waitGroup.Done()
			<-start
			_, _ = billing.finalizeOnce()
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			_, _ = billing.refundOnce()
		}()
	}
	close(start)
	waitGroup.Wait()

	assert.Equal(t, int32(1), finalized.Load()+refunded.Load())
	assert.False(t, finalized.Load() > 0 && refunded.Load() > 0)
}

func TestPlaygroundImageBillingFinalizeFailureRemainsRetryable(t *testing.T) {
	settleErr := errors.New("settlement unavailable")
	var attempts atomic.Int32
	var refunded atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error {
			if attempts.Add(1) == 1 {
				return settleErr
			}
			return nil
		},
		func() error { refunded.Add(1); return nil },
	)

	finalizedNow, err := billing.finalizeOnce()
	assert.False(t, finalizedNow)
	assert.ErrorIs(t, err, settleErr)
	assert.Zero(t, refunded.Load())

	finalizedNow, err = billing.finalizeOnce()
	require.NoError(t, err)
	assert.True(t, finalizedNow)
	assert.Equal(t, int32(2), attempts.Load())
	refundedNow, err := billing.refundOnce()
	require.NoError(t, err)
	assert.False(t, refundedNow)
}

func TestPlaygroundImageBillingFinalizeFailureCanRefundOnce(t *testing.T) {
	settleErr := errors.New("settlement unavailable")
	var refunded atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error { return settleErr },
		func() error { refunded.Add(1); return nil },
	)

	finalizedNow, err := billing.finalizeOnce()
	assert.False(t, finalizedNow)
	assert.ErrorIs(t, err, settleErr)
	refundedNow, err := billing.refundOnce()
	require.NoError(t, err)
	require.True(t, refundedNow)
	refundedNow, err = billing.refundOnce()
	require.NoError(t, err)
	assert.False(t, refundedNow)
	assert.Equal(t, int32(1), refunded.Load())

	finalizedNow, err = billing.finalizeOnce()
	require.NoError(t, err)
	assert.False(t, finalizedNow)
}

func TestPlaygroundImageBillingRefundFailureIsNotRetried(t *testing.T) {
	refundErr := errors.New("refund unavailable")
	var attempts atomic.Int32
	billing := newPlaygroundImageBilling(
		func() error { return nil },
		func() error {
			attempts.Add(1)
			return refundErr
		},
	)

	refundedNow, err := billing.refundOnce()
	assert.False(t, refundedNow)
	assert.ErrorIs(t, err, refundErr)
	assert.Equal(t, int32(1), attempts.Load())

	refundedNow, err = billing.refundOnce()
	assert.False(t, refundedNow)
	assert.ErrorIs(t, err, refundErr)
	assert.Equal(t, int32(1), attempts.Load())

	finalizedNow, err := billing.finalizeOnce()
	require.NoError(t, err)
	assert.False(t, finalizedNow)
}

func TestPreConsumeBillingRecordsManagedPlaygroundAudit(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedManagedPlaygroundBillingTask(t, "uitask_preconsume_audit", 1)
	ctx := managedPlaygroundBillingContext("uitask_preconsume_audit", "req_preconsume_audit")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_preconsume_audit")

	apiErr := PreConsumeBilling(ctx, 30, relayInfo)
	require.Nil(t, apiErr)

	task := loadManagedPlaygroundBillingTask(t, "uitask_preconsume_audit")
	assert.Equal(t, model.UserToolImageTaskBillingStatusPreConsumed, task.BillingStatus)
	assert.Equal(t, "req_preconsume_audit", task.BillingRequestID)
	assert.Equal(t, int64(30), task.PreConsumedQuota)
	assert.Equal(t, 970, getUserQuota(t, 1))
}

func TestPreConsumeBillingAuditFailureRefundsSynchronously(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedManagedPlaygroundBillingTask(t, "uitask_preconsume_audit", 1)
	auditErr := errors.New("pre-consume audit unavailable")
	registerManagedPlaygroundBillingUpdateFailure(t, model.UserToolImageTaskBillingStatusPreConsumed, auditErr)
	ctx := managedPlaygroundBillingContext("uitask_preconsume_audit", "req_preconsume_audit")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_preconsume_audit")

	apiErr := PreConsumeBilling(ctx, 30, relayInfo)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeUpdateDataError, apiErr.GetErrorCode())
	assert.ErrorIs(t, apiErr, auditErr)
	assert.Equal(t, 1000, getUserQuota(t, 1))
	require.NotNil(t, relayInfo.Billing)
	assert.False(t, relayInfo.Billing.NeedsRefund())
	task := loadManagedPlaygroundBillingTask(t, "uitask_preconsume_audit")
	assert.Equal(t, model.UserToolImageTaskBillingStatusRefunded, task.BillingStatus)
	assert.Equal(t, "req_preconsume_audit", task.BillingRequestID)
	assert.Equal(t, int64(30), task.PreConsumedQuota)
	assert.Equal(t, int64(30), task.RefundedQuota)
}

func TestPreConsumeBillingAuditAndRefundFailureRecordsRefundFailedEvidence(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedManagedPlaygroundBillingTask(t, "uitask_preconsume_refund_failed", 1)
	auditErr := errors.New("pre-consume audit unavailable")
	refundErr := errors.New("wallet refund unavailable")
	registerManagedPlaygroundBillingUpdateFailure(t, model.UserToolImageTaskBillingStatusPreConsumed, auditErr)
	registerManagedPlaygroundUserQuotaIncreaseFailure(t, refundErr)
	ctx := managedPlaygroundBillingContext("uitask_preconsume_refund_failed", "req_preconsume_refund_failed")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_preconsume_refund_failed")

	apiErr := PreConsumeBilling(ctx, 30, relayInfo)
	require.NotNil(t, apiErr)
	assert.ErrorIs(t, apiErr, auditErr)
	assert.ErrorIs(t, apiErr, refundErr)
	assert.Equal(t, 970, getUserQuota(t, 1))
	task := loadManagedPlaygroundBillingTask(t, "uitask_preconsume_refund_failed")
	assert.Equal(t, model.UserToolImageTaskBillingStatusRefundFailed, task.BillingStatus)
	assert.Equal(t, "req_preconsume_refund_failed", task.BillingRequestID)
	assert.Equal(t, int64(30), task.PreConsumedQuota)
	assert.Zero(t, task.RefundedQuota)
}

func TestPreConsumeBillingWithoutManagedTaskKeepsOrdinarySemantics(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "ordinary-image-token", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_ordinary_image")
	relayInfo.IsPlayground = false
	relayInfo.TokenId = 1
	relayInfo.TokenKey = "ordinary-image-token"

	apiErr := PreConsumeBilling(ctx, 30, relayInfo)
	require.Nil(t, apiErr)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, 970, getTokenRemainQuota(t, 1))

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.UserToolImageTask{}).Count(&taskCount).Error)
	assert.Zero(t, taskCount)

	session, ok := relayInfo.Billing.(*BillingSession)
	require.True(t, ok)
	require.NoError(t, session.RefundSync(ctx))
}

func TestFinalizePlaygroundImageBillingRecordsSettledAuditOnce(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedChannel(t, 1)
	seedManagedPlaygroundBillingTask(t, "uitask_settled", 1)
	ctx := managedPlaygroundBillingContext("uitask_settled", "req_settled")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 1, "req_settled")
	require.Nil(t, PreConsumeBilling(ctx, 30, relayInfo))
	DeferPlaygroundImageBilling(ctx, relayInfo, &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}, nil)

	finalized, err := FinalizePlaygroundImageBilling(ctx)
	require.NoError(t, err)
	require.True(t, finalized)

	task := loadManagedPlaygroundBillingTask(t, "uitask_settled")
	assert.Equal(t, model.UserToolImageTaskBillingStatusSettled, task.BillingStatus)
	assert.Equal(t, int64(30), task.SettledQuota)
	assert.Zero(t, task.RefundedQuota)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settled"))

	finalized, err = FinalizePlaygroundImageBilling(ctx)
	require.NoError(t, err)
	assert.False(t, finalized)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settled"))
}

func TestFinalizePlaygroundImageBillingAuditFailureCannotRefundOrSettleTwice(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedChannel(t, 1)
	seedManagedPlaygroundBillingTask(t, "uitask_settlement_audit_missing", 1)
	ctx := managedPlaygroundBillingContext("uitask_settlement_audit_missing", "req_settlement_audit_missing")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 1, "req_settlement_audit_missing")
	require.Nil(t, PreConsumeBilling(ctx, 30, relayInfo))
	require.NoError(t, model.DB.Where("task_id = ?", "uitask_settlement_audit_missing").Delete(&model.UserToolImageTask{}).Error)
	DeferPlaygroundImageBilling(ctx, relayInfo, &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}, nil)

	finalized, err := FinalizePlaygroundImageBilling(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrManagedPlaygroundImageSettlementAudit)
	assert.True(t, finalized)
	assert.False(t, relayInfo.Billing.NeedsRefund())
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settlement_audit_missing"))

	refunded, refundErr := RefundPlaygroundImageBillingChecked(ctx)
	require.NoError(t, refundErr)
	assert.False(t, refunded)
	assert.Equal(t, 970, getUserQuota(t, 1))

	finalized, err = FinalizePlaygroundImageBilling(ctx)
	require.NoError(t, err)
	assert.False(t, finalized)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settlement_audit_missing"))
}

func TestRetryPlaygroundImageSettlementAuditDoesNotSettleOrLogTwice(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedUser(t, 1, 1000)
	seedChannel(t, 1)
	seedManagedPlaygroundBillingTask(t, "uitask_settlement_audit_retry", 1)
	ctx := managedPlaygroundBillingContext("uitask_settlement_audit_retry", "req_settlement_audit_retry")
	relayInfo := managedPlaygroundBillingRelayInfo(1, 1, "req_settlement_audit_retry")
	require.Nil(t, PreConsumeBilling(ctx, 30, relayInfo))
	auditErr := errors.New("settlement audit unavailable")
	registerManagedPlaygroundBillingUpdateFailure(t, model.UserToolImageTaskBillingStatusSettled, auditErr)
	DeferPlaygroundImageBilling(ctx, relayInfo, &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}, nil)

	finalized, err := FinalizePlaygroundImageBilling(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrManagedPlaygroundImageSettlementAudit)
	assert.True(t, finalized)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settlement_audit_retry"))
	require.NoError(t, model.DB.Callback().Update().Remove("test:managed_playground_billing_update_failure:"+model.UserToolImageTaskBillingStatusSettled))

	retried, err := RetryPlaygroundImageSettlementAudit(ctx)
	require.NoError(t, err)
	assert.True(t, retried)
	task := loadManagedPlaygroundBillingTask(t, "uitask_settlement_audit_retry")
	assert.Equal(t, model.UserToolImageTaskBillingStatusSettled, task.BillingStatus)
	assert.Equal(t, int64(30), task.SettledQuota)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settlement_audit_retry"))

	retried, err = RetryPlaygroundImageSettlementAudit(ctx)
	require.NoError(t, err)
	assert.False(t, retried)
	assert.Equal(t, 970, getUserQuota(t, 1))
	assert.Equal(t, int64(1), countManagedPlaygroundConsumeLogs(t, "req_settlement_audit_retry"))
}

func TestRefundPlaygroundImageBillingCheckedRecordsRefundFailure(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedManagedPlaygroundBillingTask(t, "uitask_refund_failed", 1)
	require.NoError(t, model.MarkUserToolImageTaskPreConsumed("uitask_refund_failed", "req_refund_failed", 30, 0))
	ctx := managedPlaygroundBillingContext("uitask_refund_failed", "req_refund_failed")
	refundErr := errors.New("refund unavailable")
	settler := &managedPlaygroundBillingTestSettler{
		preConsumedQuota: 30,
		chargedQuota:     30,
		needsRefund:      true,
		refundErr:        refundErr,
	}
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_refund_failed")
	relayInfo.Billing = settler
	DeferPlaygroundImageBilling(ctx, relayInfo, &dto.Usage{}, nil)

	refunded, err := RefundPlaygroundImageBillingChecked(ctx)
	assert.False(t, refunded)
	assert.ErrorIs(t, err, refundErr)
	task := loadManagedPlaygroundBillingTask(t, "uitask_refund_failed")
	assert.Equal(t, model.UserToolImageTaskBillingStatusRefundFailed, task.BillingStatus)
	assert.Equal(t, 1, settler.refundCalls)

	refunded, err = RefundPlaygroundImageBillingChecked(ctx)
	assert.False(t, refunded)
	assert.ErrorIs(t, err, refundErr)
	assert.Equal(t, 1, settler.refundCalls)
	assert.False(t, RefundPlaygroundImageBilling(ctx))
	assert.Equal(t, 1, settler.refundCalls)
}

func TestRefundPlaygroundImageBillingCheckedJoinsRefundAndAuditErrors(t *testing.T) {
	resetManagedPlaygroundBillingTest(t)
	seedManagedPlaygroundBillingTask(t, "uitask_refund_audit_failed", 1)
	require.NoError(t, model.MarkUserToolImageTaskPreConsumed("uitask_refund_audit_failed", "req_refund_audit_failed", 30, 0))
	refundErr := errors.New("refund unavailable")
	auditErr := errors.New("refund failure audit unavailable")
	registerManagedPlaygroundBillingUpdateFailure(t, model.UserToolImageTaskBillingStatusRefundFailed, auditErr)
	ctx := managedPlaygroundBillingContext("uitask_refund_audit_failed", "req_refund_audit_failed")
	settler := &managedPlaygroundBillingTestSettler{
		preConsumedQuota: 30,
		chargedQuota:     30,
		needsRefund:      true,
		refundErr:        refundErr,
	}
	relayInfo := managedPlaygroundBillingRelayInfo(1, 0, "req_refund_audit_failed")
	relayInfo.Billing = settler
	DeferPlaygroundImageBilling(ctx, relayInfo, &dto.Usage{}, nil)

	refunded, err := RefundPlaygroundImageBillingChecked(ctx)
	assert.False(t, refunded)
	assert.ErrorIs(t, err, refundErr)
	assert.ErrorIs(t, err, auditErr)
	task := loadManagedPlaygroundBillingTask(t, "uitask_refund_audit_failed")
	assert.Equal(t, model.UserToolImageTaskBillingStatusRefundPending, task.BillingStatus)
	assert.Equal(t, 1, settler.refundCalls)
}

func registerManagedPlaygroundBillingUpdateFailure(t *testing.T, status string, injected error) {
	t.Helper()
	name := "test:managed_playground_billing_update_failure:" + status
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "user_tool_image_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["billing_status"] != status {
			return
		}
		tx.AddError(injected)
	}))
	t.Cleanup(func() {
		model.DB.Callback().Update().Remove(name)
	})
}

func registerManagedPlaygroundUserQuotaIncreaseFailure(t *testing.T, injected error) {
	t.Helper()
	const name = "test:managed_playground_user_quota_increase_failure"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		expr, ok := updates["quota"].(clause.Expr)
		if ok && strings.Contains(expr.SQL, "+") {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() {
		model.DB.Callback().Update().Remove(name)
	})
}
