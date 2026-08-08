package service

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type billingSessionSyncRefundFunding struct {
	mu          sync.Mutex
	refundErr   error
	refundCalls int
	started     chan struct{}
	release     chan struct{}
}

func (f *billingSessionSyncRefundFunding) Source() string { return BillingSourceWallet }
func (f *billingSessionSyncRefundFunding) PreConsume(int) error {
	return nil
}
func (f *billingSessionSyncRefundFunding) Settle(int) error {
	return nil
}
func (f *billingSessionSyncRefundFunding) Refund() error {
	f.mu.Lock()
	f.refundCalls++
	started := f.started
	release := f.release
	err := f.refundErr
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}

func (f *billingSessionSyncRefundFunding) setRefundError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refundErr = err
}

func (f *billingSessionSyncRefundFunding) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refundCalls
}

func newManagedPlaygroundRefundSession(funding FundingSource) (*BillingSession, *gin.Context) {
	ctx, _ := gin.CreateTestContext(nil)
	return &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:       101,
			TokenId:      202,
			TokenKey:     "sk-sensitive-token-key",
			IsPlayground: true,
		},
		funding:       funding,
		tokenConsumed: 300,
	}, ctx
}

func TestBillingSessionRefundSyncRetriesFailedAttempt(t *testing.T) {
	refundErr := errors.New("funding refund failed")
	funding := &billingSessionSyncRefundFunding{refundErr: refundErr}
	session, ctx := newManagedPlaygroundRefundSession(funding)

	firstErr := session.RefundSync(ctx)
	funding.setRefundError(nil)
	secondErr := session.RefundSync(ctx)
	thirdErr := session.RefundSync(ctx)

	require.ErrorIs(t, firstErr, refundErr)
	require.NoError(t, secondErr)
	require.NoError(t, thirdErr)
	assert.Equal(t, 2, funding.calls())
	assert.False(t, session.NeedsRefund())
}

func TestBillingSessionRefundSyncConcurrentCallsShareAttempt(t *testing.T) {
	funding := &billingSessionSyncRefundFunding{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	session, ctx := newManagedPlaygroundRefundSession(funding)

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- session.RefundSync(ctx) }()
	<-funding.started
	go func() { secondDone <- session.RefundSync(ctx) }()

	assert.Equal(t, 1, funding.calls())
	close(funding.release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	assert.Equal(t, 1, funding.calls())
}

func TestBillingSessionRefundSyncRetrySkipsSuccessfulStages(t *testing.T) {
	truncate(t)
	funding := &billingSessionSyncRefundFunding{}
	ctx, _ := gin.CreateTestContext(nil)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   101,
			TokenId:  909,
			TokenKey: "retry-token",
		},
		funding:       funding,
		tokenConsumed: 30,
		tokenCharges: []model.TokenQuotaCharge{{
			Amount:         30,
			TotalDeducted:  true,
			PeriodDeducted: true,
		}},
	}

	firstErr := session.RefundSync(ctx)
	require.Error(t, firstErr)
	assert.Equal(t, 1, funding.calls())

	seedToken(t, 909, 101, "retry-token", 0)
	require.NoError(t, session.RefundSync(ctx))
	require.NoError(t, session.RefundSync(ctx))
	assert.Equal(t, 1, funding.calls(), "a successful funding refund stage must not run again")
	assert.Equal(t, 30, getTokenRemainQuota(t, 909))
}

func TestBillingSessionRefundSyncSucceedsOnlyOnce(t *testing.T) {
	funding := &billingSessionSyncRefundFunding{}
	session, ctx := newManagedPlaygroundRefundSession(funding)

	require.NoError(t, session.RefundSync(ctx))
	require.NoError(t, session.RefundSync(ctx))

	assert.Equal(t, 1, funding.calls())
	assert.False(t, session.NeedsRefund())
}

func TestBillingSessionRefundAndRefundSyncShareOneRefundAttempt(t *testing.T) {
	refundErr := errors.New("async funding refund failed")
	funding := &billingSessionSyncRefundFunding{
		refundErr: refundErr,
		started:   make(chan struct{}, 1),
		release:   make(chan struct{}),
	}
	session, ctx := newManagedPlaygroundRefundSession(funding)

	done := make(chan error, 1)
	go func() { done <- session.RefundSync(ctx) }()
	<-funding.started
	session.Refund(ctx)
	assert.Equal(t, 1, funding.calls())
	close(funding.release)

	require.ErrorIs(t, <-done, refundErr)
	assert.Equal(t, 1, funding.calls())
}

func TestBillingSessionRefundSyncDoesNotLogSensitiveRefundError(t *testing.T) {
	const sensitiveMessage = "refund failed with api_key=top-secret"
	funding := &billingSessionSyncRefundFunding{refundErr: errors.New(sensitiveMessage)}
	session, ctx := newManagedPlaygroundRefundSession(funding)
	var output bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultWriter
	oldErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = &output
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = oldWriter
		gin.DefaultErrorWriter = oldErrorWriter
		common.LogWriterMu.Unlock()
	})

	err := session.RefundSync(ctx)

	require.ErrorContains(t, err, sensitiveMessage)
	assert.NotContains(t, output.String(), sensitiveMessage)
	assert.NotContains(t, output.String(), session.relayInfo.TokenKey)
}
