package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	alertNoticeDeliveryInterval    = 15 * time.Second
	alertNoticeDeliveryBatchSize   = 100
	alertNoticeDeliveryMaxAttempts = 5
)

type alertNoticeDeliveryHandler struct {
	newClient func() (*ApiNoticeClient, error)
}

type alertNoticeDeliverySummary struct {
	Processed int `json:"processed"`
	Delivered int `json:"delivered"`
	Retrying  int `json:"retrying"`
	Failed    int `json:"failed"`
}

func (alertNoticeDeliveryHandler) Type() string { return model.SystemTaskTypeAlertNoticeDelivery }

func (alertNoticeDeliveryHandler) Enabled() bool { return model.HasPendingAlertRuleDeliveries() }

func (alertNoticeDeliveryHandler) Interval() time.Duration { return alertNoticeDeliveryInterval }

func (alertNoticeDeliveryHandler) NewPayload() any { return nil }

func (handler alertNoticeDeliveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := alertNoticeDeliverySummary{}
	newClient := handler.newClient
	if newClient == nil {
		newClient = NewApiNoticeClientFromEnv
	}
	client, clientErr := newClient()
	states, err := model.ListDueAlertRuleStates(time.Now().Unix(), alertNoticeDeliveryBatchSize)
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, summary, "alert delivery query failed")
		return
	}
	for _, state := range states {
		if ctx.Err() != nil {
			_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, summary, "alert delivery canceled")
			return
		}
		if state == nil {
			continue
		}
		summary.Processed++
		deliveryErr := clientErr
		if deliveryErr == nil {
			_, deliveryErr = client.SendNoticeRaw(ctx, []byte(state.PendingRequestJSON))
		}
		outcome, persistErr := persistAlertNoticeDeliveryOutcome(state, deliveryErr)
		if persistErr != nil {
			summary.Failed++
			continue
		}
		switch outcome {
		case "delivered":
			summary.Delivered++
		case "retrying":
			summary.Retrying++
		default:
			summary.Failed++
		}
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, summary, "")
}

func persistAlertNoticeDeliveryOutcome(snapshot *model.AlertRuleState, deliveryErr error) (string, error) {
	outcome := "failed"
	err := model.MutateAlertRuleState(snapshot.RuleID, snapshot.SubjectKey, func(state *model.AlertRuleState) error {
		if state.PendingIdempotencyKey != snapshot.PendingIdempotencyKey || state.PendingRequestJSON != snapshot.PendingRequestJSON {
			outcome = "stale"
			return nil
		}
		now := time.Now().Unix()
		if deliveryErr == nil {
			if state.PendingEventType == AlertEventTrigger {
				state.TriggerDelivered = true
			} else if state.PendingEventType == AlertEventRecovery {
				state.TriggerDelivered = false
			}
			state.LastSentAt = now
			state.PendingEventType = ""
			state.PendingIdempotencyKey = ""
			state.PendingRequestJSON = ""
			state.NextAttemptAt = 0
			state.DeliveryAttempts = 0
			state.LastErrorSummary = ""
			outcome = "delivered"
			return nil
		}

		if errors.Is(deliveryErr, ErrApiNoticeAPIKeyNotConfigured) {
			state.LastErrorSummary = "api_key_not_configured"
			state.NextAttemptAt = now + int64(time.Minute.Seconds())
			outcome = "retrying"
			return nil
		}

		state.DeliveryAttempts++
		retryable, retryAfter, summary := classifyAlertNoticeDeliveryError(deliveryErr)
		state.LastErrorSummary = summary
		if retryable && state.DeliveryAttempts < alertNoticeDeliveryMaxAttempts {
			if retryAfter <= 0 {
				retryAfter = alertNoticeRetryDelay(state.DeliveryAttempts)
			}
			state.NextAttemptAt = now + int64(retryAfter.Seconds())
			outcome = "retrying"
			return nil
		}
		state.PendingEventType = ""
		state.PendingIdempotencyKey = ""
		state.PendingRequestJSON = ""
		state.NextAttemptAt = 0
		outcome = "failed"
		return nil
	})
	return outcome, err
}

func classifyAlertNoticeDeliveryError(err error) (bool, time.Duration, string) {
	if err == nil {
		return false, 0, ""
	}
	var httpErr *ApiNoticeHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Retryable, httpErr.RetryAfter, sanitizeApiNoticeError(httpErr.Summary)
	}
	if errors.Is(err, ErrApiNoticeAPIKeyNotConfigured) {
		return true, time.Minute, "api_key_not_configured"
	}
	return true, 0, "transport_error"
}

func alertNoticeRetryDelay(attempt int) time.Duration {
	delays := []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute}
	if attempt <= 0 {
		return delays[0]
	}
	index := attempt - 1
	if index >= len(delays) {
		index = len(delays) - 1
	}
	return delays[index]
}

func init() {
	RegisterSystemTaskHandler(alertNoticeDeliveryHandler{})
}
