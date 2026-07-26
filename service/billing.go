package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	if err := RecordManagedPlaygroundImagePreConsume(c, relayInfo); err != nil {
		taskID := playgroundImageTaskID(c)
		requestID := relayInfo.RequestId
		if requestID == "" {
			requestID = c.GetString(common.RequestIdKey)
		}
		quota := int64(session.GetPreConsumedQuota())
		refundErr := session.RefundSync(c)
		var compensationErr error
		if refundErr != nil {
			compensationErr = model.MarkUserToolImageTaskPreConsumeRefundFailed(taskID, requestID, quota, 0)
			err = errors.Join(
				err,
				fmt.Errorf("rollback managed playground image pre-consume: %w", refundErr),
				compensationErr,
			)
		} else {
			compensationErr = model.MarkUserToolImageTaskPreConsumeRefunded(taskID, requestID, quota, 0)
			err = errors.Join(err, compensationErr)
		}
		logger.LogError(c, fmt.Sprintf(
			"managed playground image billing audit failed request_id=%s task_id=%s stage=pre_consume refund_succeeded=%t compensation_audit_succeeded=%t error=%q",
			requestID,
			taskID,
			refundErr == nil,
			compensationErr == nil,
			common.LocalLogPreview(err.Error()),
		))
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	_, err := SettleBillingQuota(ctx, relayInfo, actualQuota)
	return err
}

// SettleBillingQuota 执行计费结算并返回当前实际已扣额度。
// 如果补扣失败，返回值仍是已经成功预扣的额度，供异步任务和消费日志保持一致。
// 如果 RelayInfo 上没有 BillingSession，则回退到旧的 PostConsumeQuota 路径。
func SettleBillingQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) (int, error) {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return relayInfo.Billing.GetChargedQuota(), err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return relayInfo.Billing.GetChargedQuota(), nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		if err := PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true); err != nil {
			return relayInfo.FinalPreConsumedQuota, err
		}
	}
	return actualQuota, nil
}
