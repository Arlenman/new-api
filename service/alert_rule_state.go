package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/model"
)

const (
	AlertStateNormal  = "normal"
	AlertStatePending = "pending"
	AlertStateActive  = "active"

	AlertEventTrigger  = "trigger"
	AlertEventRecovery = "recovery"
)

type AlertObservation struct {
	Match                bool
	ObservedAt           int64
	Now                  int64
	WindowSeconds        int
	RepeatMatchedTrigger bool
}

type AlertStateTransition struct {
	EventType      string
	IdempotencyKey string
	NextAttemptAt  int64
}

func AdvanceAlertRuleState(rule model.AlertRule, state *model.AlertRuleState, observation AlertObservation) (AlertStateTransition, error) {
	if state == nil {
		return AlertStateTransition{}, errors.New("alert rule state is required")
	}
	if rule.ID <= 0 || rule.Revision <= 0 {
		return AlertStateTransition{}, errors.New("valid alert rule identity is required")
	}
	if observation.ObservedAt <= 0 || observation.Now <= 0 {
		return AlertStateTransition{}, errors.New("valid alert observation timestamps are required")
	}
	if state.State == "" {
		state.State = AlertStateNormal
	}
	if state.State != AlertStateNormal && state.State != AlertStatePending && state.State != AlertStateActive {
		return AlertStateTransition{}, fmt.Errorf("unsupported alert state %q", state.State)
	}
	if observation.ObservedAt <= state.LastObservationAt {
		return AlertStateTransition{}, nil
	}
	state.LastObservationAt = observation.ObservedAt

	if !observation.Match {
		state.ConsecutiveMatches = 0
		state.WindowStartedAt = 0
		if state.State != AlertStateActive {
			state.State = AlertStateNormal
			return AlertStateTransition{}, nil
		}

		state.State = AlertStateNormal
		state.ActiveSince = 0
		state.LastRecoveredAt = observation.Now
		if !state.TriggerDelivered {
			clearPendingAlertEvent(state)
			return AlertStateTransition{}, nil
		}
		if !rule.SendRecovery {
			state.TriggerDelivered = false
			clearPendingAlertEvent(state)
			return AlertStateTransition{}, nil
		}

		transition := newAlertStateTransition(rule, state, AlertEventRecovery, observation.Now)
		state.PendingEventType = transition.EventType
		state.PendingIdempotencyKey = transition.IdempotencyKey
		state.NextAttemptAt = transition.NextAttemptAt
		state.DeliveryAttempts = 0
		state.LastErrorSummary = ""
		return transition, nil
	}

	if state.State == AlertStateActive {
		if !observation.RepeatMatchedTrigger {
			return AlertStateTransition{}, nil
		}
		state.IncidentSequence++
		state.LastTriggeredAt = observation.Now
		transition := newAlertStateTransition(rule, state, AlertEventTrigger, observation.Now)
		state.PendingEventType = transition.EventType
		state.PendingIdempotencyKey = transition.IdempotencyKey
		state.NextAttemptAt = transition.NextAttemptAt
		state.DeliveryAttempts = 0
		state.LastErrorSummary = ""
		return transition, nil
	}

	required := rule.ConsecutiveRequired
	if required <= 0 {
		required = 1
	}
	windowSeconds := observation.WindowSeconds
	if windowSeconds < 0 {
		windowSeconds = 0
	}
	if state.State == AlertStateNormal || state.WindowStartedAt == 0 ||
		(windowSeconds > 0 && observation.ObservedAt-state.WindowStartedAt > int64(windowSeconds)) {
		state.State = AlertStatePending
		state.ConsecutiveMatches = 1
		state.WindowStartedAt = observation.ObservedAt
	} else {
		state.ConsecutiveMatches++
	}
	if state.ConsecutiveMatches < required {
		return AlertStateTransition{}, nil
	}

	state.State = AlertStateActive
	state.ConsecutiveMatches = 0
	state.WindowStartedAt = 0
	state.IncidentSequence++
	state.ActiveSince = observation.Now
	state.LastTriggeredAt = observation.Now
	state.TriggerDelivered = false
	transition := newAlertStateTransition(rule, state, AlertEventTrigger, observation.Now)
	state.PendingEventType = transition.EventType
	state.PendingIdempotencyKey = transition.IdempotencyKey
	state.NextAttemptAt = transition.NextAttemptAt
	state.DeliveryAttempts = 0
	state.LastErrorSummary = ""
	return transition, nil
}

func newAlertStateTransition(rule model.AlertRule, state *model.AlertRuleState, eventType string, now int64) AlertStateTransition {
	nextAttemptAt := now
	if rule.CooldownSeconds > 0 && state.LastSentAt > 0 {
		cooldownEnd := state.LastSentAt + int64(rule.CooldownSeconds)
		if cooldownEnd > nextAttemptAt {
			nextAttemptAt = cooldownEnd
		}
	}
	return AlertStateTransition{
		EventType:      eventType,
		IdempotencyKey: fmt.Sprintf("nar:%d:r%d:i%d:%s", rule.ID, rule.Revision, state.IncidentSequence, eventType),
		NextAttemptAt:  nextAttemptAt,
	}
}

func clearPendingAlertEvent(state *model.AlertRuleState) {
	state.PendingEventType = ""
	state.PendingIdempotencyKey = ""
	state.PendingRequestJSON = ""
	state.NextAttemptAt = 0
	state.DeliveryAttempts = 0
	state.LastErrorSummary = ""
}
