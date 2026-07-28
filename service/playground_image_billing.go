package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

const playgroundImageBillingContextKey = "pending_playground_image_billing"

var ErrManagedPlaygroundImageSettlementAudit = errors.New("managed playground image settlement succeeded but audit failed")

type playgroundImageBillingState uint8

const (
	playgroundImageBillingPending playgroundImageBillingState = iota
	playgroundImageBillingFinalized
	playgroundImageBillingRefunded
	playgroundImageBillingRefundFailed
)

type playgroundImageBillingFinalizeError struct {
	err error
}

func (e *playgroundImageBillingFinalizeError) Error() string {
	if e == nil || e.err == nil {
		return "playground image billing finalization failed"
	}
	return e.err.Error()
}

func (e *playgroundImageBillingFinalizeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// playgroundImageBilling keeps a Playground image request's pre-consume open
// until the controller has validated and persisted the final image payload.
// Ordinary Images API requests do not use this object and retain their current
// settlement behavior, including client-disconnect semantics.
type playgroundImageBilling struct {
	mu                     sync.Mutex
	state                  playgroundImageBillingState
	finalize               func() error
	refund                 func() error
	retrySettlementAudit   func() error
	settlementAuditPending bool
	refundErr              error
}

// managedPlaygroundImageBillingSettler suppresses the generic asynchronous
// relay refund while a managed Playground task still owns the final outcome.
// The controller uses RefundPlaygroundImageBillingChecked after validation or
// persistence fails, which calls RefundSync on the original billing session.
type managedPlaygroundImageBillingSettler struct {
	delegate relaycommon.BillingSettler
}

func (s *managedPlaygroundImageBillingSettler) Settle(actualQuota int) error {
	return s.delegate.Settle(actualQuota)
}

func (s *managedPlaygroundImageBillingSettler) Refund(_ *gin.Context) {}

func (s *managedPlaygroundImageBillingSettler) RefundSync(c *gin.Context) error {
	refunder, ok := s.delegate.(interface{ RefundSync(*gin.Context) error })
	if !ok {
		return errors.New("managed playground image billing does not support synchronous refund")
	}
	return refunder.RefundSync(c)
}

func (s *managedPlaygroundImageBillingSettler) NeedsRefund() bool {
	return s.delegate.NeedsRefund()
}

func (s *managedPlaygroundImageBillingSettler) GetPreConsumedQuota() int {
	return s.delegate.GetPreConsumedQuota()
}

func (s *managedPlaygroundImageBillingSettler) GetChargedQuota() int {
	return s.delegate.GetChargedQuota()
}

func (s *managedPlaygroundImageBillingSettler) Reserve(targetQuota int) error {
	return s.delegate.Reserve(targetQuota)
}

func newPlaygroundImageBilling(finalize func() error, refund func() error) *playgroundImageBilling {
	return &playgroundImageBilling{
		state:    playgroundImageBillingPending,
		finalize: finalize,
		refund:   refund,
	}
}

func (b *playgroundImageBilling) arm(finalize func() error, refund func() error, retrySettlementAudit func() error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != playgroundImageBillingPending {
		return
	}
	if b.finalize == nil {
		b.finalize = finalize
	}
	if b.refund == nil {
		b.refund = refund
	}
	if b.retrySettlementAudit == nil {
		b.retrySettlementAudit = retrySettlementAudit
	}
}

func (b *playgroundImageBilling) finalizeOnce() (bool, error) {
	if b == nil {
		return false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != playgroundImageBillingPending {
		return false, nil
	}
	if b.finalize != nil {
		if err := b.finalize(); err != nil {
			var finalizedErr *playgroundImageBillingFinalizeError
			if errors.As(err, &finalizedErr) {
				b.state = playgroundImageBillingFinalized
				b.settlementAuditPending = true
				return true, err
			}
			return false, err
		}
	}
	b.state = playgroundImageBillingFinalized
	return true, nil
}

func (b *playgroundImageBilling) retrySettlementAuditOnce() (bool, error) {
	if b == nil {
		return false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != playgroundImageBillingFinalized || !b.settlementAuditPending {
		return false, nil
	}
	if b.retrySettlementAudit != nil {
		if err := b.retrySettlementAudit(); err != nil {
			return false, err
		}
	}
	b.settlementAuditPending = false
	return true, nil
}

func (b *playgroundImageBilling) refundOnce() (bool, error) {
	if b == nil {
		return false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == playgroundImageBillingRefunded {
		return false, nil
	}
	if b.state == playgroundImageBillingRefundFailed {
		return false, b.refundErr
	}
	if b.state != playgroundImageBillingPending {
		return false, nil
	}
	if b.refund != nil {
		if err := b.refund(); err != nil {
			b.state = playgroundImageBillingRefundFailed
			b.refundErr = err
			return false, err
		}
	}
	b.state = playgroundImageBillingRefunded
	return true, nil
}

// PreparePlaygroundImageBilling establishes the managed billing guard before an
// adaptor can fail. Generic relay cleanup is intentionally suppressed only when
// the request carries a managed Playground image task ID; ordinary Images API
// requests keep their existing asynchronous refund behavior.
func PreparePlaygroundImageBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if c == nil || relayInfo == nil || playgroundImageTaskID(c) == "" {
		return
	}
	if getPlaygroundImageBilling(c) != nil {
		return
	}

	if relayInfo.Billing != nil {
		if _, guarded := relayInfo.Billing.(*managedPlaygroundImageBillingSettler); !guarded {
			relayInfo.Billing = &managedPlaygroundImageBillingSettler{delegate: relayInfo.Billing}
		}
	}
	billing := newPlaygroundImageBilling(nil, func() error {
		return RefundManagedPlaygroundImageBilling(c, relayInfo)
	})
	c.Set(playgroundImageBillingContextKey, billing)
}

// DeferPlaygroundImageBilling stores successful upstream usage without settling
// it. The Playground controller finalizes it only after image validation and
// required persistence have succeeded.
func DeferPlaygroundImageBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	if c == nil || relayInfo == nil || usage == nil {
		return
	}
	PreparePlaygroundImageBilling(c, relayInfo)

	usageCopy := *usage
	contentCopy := append([]string(nil), extraContent...)
	finalize := func() error {
		if err := PostTextConsumeQuotaChecked(c, relayInfo, &usageCopy, contentCopy); err != nil {
			return err
		}
		if err := RecordManagedPlaygroundImageSettled(c, relayInfo); err != nil {
			return &playgroundImageBillingFinalizeError{err: errors.Join(ErrManagedPlaygroundImageSettlementAudit, err)}
		}
		return nil
	}
	refund := func() error {
		if playgroundImageTaskID(c) != "" {
			return RefundManagedPlaygroundImageBilling(c, relayInfo)
		}
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		return nil
	}
	retrySettlementAudit := func() error {
		return RecordManagedPlaygroundImageSettled(c, relayInfo)
	}

	billing := getPlaygroundImageBilling(c)
	if billing == nil {
		billing = newPlaygroundImageBilling(nil, nil)
		c.Set(playgroundImageBillingContextKey, billing)
	}
	billing.arm(finalize, refund, retrySettlementAudit)
}

// FinalizePlaygroundImageBilling settles a deferred Playground image charge at
// most once. It returns true only for the call that performed the settlement.
func FinalizePlaygroundImageBilling(c *gin.Context) (bool, error) {
	return getPlaygroundImageBilling(c).finalizeOnce()
}

// RetryPlaygroundImageSettlementAudit retries only the task audit after the
// underlying charge and consume log have already succeeded. It never settles
// or logs usage again, preventing duplicate charges during recovery.
func RetryPlaygroundImageSettlementAudit(c *gin.Context) (bool, error) {
	return getPlaygroundImageBilling(c).retrySettlementAuditOnce()
}

// RefundPlaygroundImageBillingChecked refunds a deferred Playground image
// pre-consume at most once and returns any synchronous managed-task refund
// failure. A finalized charge is never refunded by this path.
func RefundPlaygroundImageBillingChecked(c *gin.Context) (bool, error) {
	return getPlaygroundImageBilling(c).refundOnce()
}

// RefundPlaygroundImageBilling preserves the legacy best-effort call shape for
// non-managed Playground paths. Managed task controllers use the checked form.
func RefundPlaygroundImageBilling(c *gin.Context) bool {
	refunded, _ := RefundPlaygroundImageBillingChecked(c)
	return refunded
}

func getPlaygroundImageBilling(c *gin.Context) *playgroundImageBilling {
	if c == nil {
		return nil
	}
	value, exists := c.Get(playgroundImageBillingContextKey)
	if !exists {
		return nil
	}
	billing, _ := value.(*playgroundImageBilling)
	return billing
}

func playgroundImageTaskID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyPlaygroundImageTaskID))
}

// RecordManagedPlaygroundImagePreConsume persists the amount reserved for a
// managed task immediately after pre-consume succeeds. The zero-quota case is
// recorded too, so every managed task has an auditable billing transition.
func RecordManagedPlaygroundImagePreConsume(c *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	taskID := playgroundImageTaskID(c)
	if taskID == "" || relayInfo == nil {
		return nil
	}
	requestID := strings.TrimSpace(relayInfo.RequestId)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetString(common.RequestIdKey))
	}
	quota := int64(0)
	if relayInfo.Billing != nil {
		quota = int64(relayInfo.Billing.GetPreConsumedQuota())
	}
	if quota < 0 {
		return fmt.Errorf("managed playground image pre-consumed quota is negative")
	}
	return model.MarkUserToolImageTaskPreConsumed(taskID, requestID, quota, 0)
}

// RefundManagedPlaygroundImageBilling synchronously completes the managed
// task's refund audit. It is intentionally separate from ordinary BillingSettler
// behavior: ordinary API requests retain asynchronous refund semantics.
func RefundManagedPlaygroundImageBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	taskID := playgroundImageTaskID(c)
	if taskID == "" || relayInfo == nil {
		return nil
	}

	quota := int64(0)
	if relayInfo.Billing != nil {
		quota = int64(relayInfo.Billing.GetPreConsumedQuota())
	}
	if quota < 0 {
		return errors.New("managed playground image pre-consumed quota is negative")
	}

	if relayInfo.Billing != nil && !relayInfo.Billing.NeedsRefund() {
		// A settled session must never be converted into a refund. A zero-quota
		// session has no money to return but still gets a terminal audit state.
		if relayInfo.Billing.GetChargedQuota() > 0 {
			return model.MarkUserToolImageTaskSettled(taskID, int64(relayInfo.Billing.GetChargedQuota()), 0)
		}
		if err := model.MarkUserToolImageTaskRefundPending(taskID, 0); err != nil {
			if !errors.Is(err, model.ErrUserToolImageTaskBillingConflict) {
				return err
			}
		} else if err := model.MarkUserToolImageTaskRefunded(taskID, 0, 0); err != nil {
			return err
		}
		return nil
	}

	if err := model.MarkUserToolImageTaskRefundPending(taskID, 0); err != nil {
		if !errors.Is(err, model.ErrUserToolImageTaskBillingConflict) {
			return err
		}
	}

	if relayInfo.Billing == nil {
		return model.MarkUserToolImageTaskRefunded(taskID, 0, 0)
	}

	refunder, ok := relayInfo.Billing.(interface{ RefundSync(*gin.Context) error })
	if !ok {
		refundErr := errors.New("managed playground image billing does not support synchronous refund")
		auditErr := model.MarkUserToolImageTaskRefundFailed(taskID, 0)
		err := errors.Join(refundErr, auditErr)
		logger.LogError(c, fmt.Sprintf(
			"managed playground image refund failed request_id=%s task_id=%s stage=refund refund_audit_succeeded=%t error=%q",
			c.GetString(common.RequestIdKey), taskID, auditErr == nil, common.LocalLogPreview(err.Error()),
		))
		return err
	}
	if refundErr := refunder.RefundSync(c); refundErr != nil {
		auditErr := model.MarkUserToolImageTaskRefundFailed(taskID, 0)
		err := errors.Join(refundErr, auditErr)
		logger.LogError(c, fmt.Sprintf(
			"managed playground image refund failed request_id=%s task_id=%s stage=refund refund_audit_succeeded=%t error=%q",
			c.GetString(common.RequestIdKey), taskID, auditErr == nil, common.LocalLogPreview(err.Error()),
		))
		return err
	}
	if err := model.MarkUserToolImageTaskRefunded(taskID, quota, 0); err != nil {
		return err
	}
	return nil
}

// RecordManagedPlaygroundImageSettled records the final charge after the
// response and local image persistence have succeeded. It never refunds on an
// audit write failure because the underlying billing session is already
// settled.
func RecordManagedPlaygroundImageSettled(c *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	taskID := playgroundImageTaskID(c)
	if taskID == "" || relayInfo == nil || relayInfo.Billing == nil {
		return nil
	}
	return model.MarkUserToolImageTaskSettled(taskID, int64(relayInfo.Billing.GetChargedQuota()), 0)
}
