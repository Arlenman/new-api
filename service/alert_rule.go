package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	alertRuleNameMaxLength          = 128
	alertRuleMaxWindowSeconds       = 30 * 24 * 60 * 60
	alertRuleMaxConsecutive         = 1000
	alertRuleMaxCooldownSeconds     = 30 * 24 * 60 * 60
	alertRuleMaxEnabledChannelCount = 1_000_000
	EnabledChannelCountAlertSubject = "channel_pool:enabled"
)

var alertRuleOperators = map[string]struct{}{
	"lt": {}, "lte": {}, "gt": {}, "gte": {}, "eq": {},
}

func ValidateAlertRuleInput(ctx context.Context, input dto.AlertRuleInput, validateProviders bool) (dto.AlertRuleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > alertRuleNameMaxLength {
		return dto.AlertRuleInput{}, fmt.Errorf("rule name is required and must not exceed %d characters", alertRuleNameMaxLength)
	}
	if math.IsNaN(input.TriggerConfig.Threshold) || math.IsInf(input.TriggerConfig.Threshold, 0) || input.TriggerConfig.Threshold < 0 {
		return dto.AlertRuleInput{}, errors.New("threshold must be a non-negative finite number")
	}
	switch input.TriggerType {
	case model.AlertRuleTriggerTypeUpstreamChannelEffectiveBalance:
		if _, ok := alertRuleOperators[input.TriggerConfig.Operator]; !ok {
			return dto.AlertRuleInput{}, fmt.Errorf("unsupported comparison operator %q", input.TriggerConfig.Operator)
		}
		if input.TriggerConfig.Threshold > 1_000_000_000 {
			return dto.AlertRuleInput{}, errors.New("threshold must be between 0 and 1000000000")
		}
		if input.TriggerConfig.WindowSeconds < 0 || input.TriggerConfig.WindowSeconds > alertRuleMaxWindowSeconds {
			return dto.AlertRuleInput{}, errors.New("window_seconds is out of range")
		}
		if input.ConsecutiveRequired < 1 || input.ConsecutiveRequired > alertRuleMaxConsecutive {
			return dto.AlertRuleInput{}, errors.New("consecutive_required is out of range")
		}
		if input.CooldownSeconds < 0 || input.CooldownSeconds > alertRuleMaxCooldownSeconds {
			return dto.AlertRuleInput{}, errors.New("cooldown_seconds is out of range")
		}
	case model.AlertRuleTriggerTypeEnabledChannelCount:
		if input.TriggerConfig.Operator != "lte" {
			return dto.AlertRuleInput{}, errors.New("enabled channel count operator must be lte")
		}
		if input.TriggerConfig.Threshold > alertRuleMaxEnabledChannelCount || math.Trunc(input.TriggerConfig.Threshold) != input.TriggerConfig.Threshold {
			return dto.AlertRuleInput{}, fmt.Errorf("enabled channel count threshold must be an integer between 0 and %d", alertRuleMaxEnabledChannelCount)
		}
		if input.TriggerConfig.WindowSeconds != 0 {
			return dto.AlertRuleInput{}, errors.New("enabled channel count window_seconds must be 0")
		}
		if input.ConsecutiveRequired != 1 {
			return dto.AlertRuleInput{}, errors.New("enabled channel count consecutive_required must be 1")
		}
		if input.CooldownSeconds != 0 {
			return dto.AlertRuleInput{}, errors.New("enabled channel count cooldown_seconds must be 0")
		}
	default:
		return dto.AlertRuleInput{}, fmt.Errorf("unsupported trigger type %q", input.TriggerType)
	}
	if len(input.Providers) == 0 {
		return dto.AlertRuleInput{}, errors.New("at least one notification provider is required")
	}
	providerSet := make(map[string]struct{}, len(input.Providers))
	providers := make([]string, 0, len(input.Providers))
	for _, provider := range input.Providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			return dto.AlertRuleInput{}, errors.New("notification provider name is required")
		}
		if _, exists := providerSet[provider]; exists {
			continue
		}
		providerSet[provider] = struct{}{}
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	input.Providers = providers
	if input.MessageFormat != input.MessageTemplate.Format {
		return dto.AlertRuleInput{}, errors.New("message format does not match message_template.format")
	}
	if err := ValidateAlertMessageTemplate(input.MessageFormat, input.MessageTemplate); err != nil {
		return dto.AlertRuleInput{}, err
	}
	if validateProviders {
		client, err := NewApiNoticeClientFromEnv()
		if err != nil {
			return dto.AlertRuleInput{}, err
		}
		catalog, err := client.GetProviders(ctx)
		if err != nil {
			return dto.AlertRuleInput{}, fmt.Errorf("load api-notice providers: %w", err)
		}
		catalogByName := make(map[string]dto.ApiNoticeProvider, len(catalog))
		for _, provider := range catalog {
			catalogByName[provider.Name] = provider
		}
		for _, providerName := range input.Providers {
			provider, ok := catalogByName[providerName]
			if !ok {
				return dto.AlertRuleInput{}, fmt.Errorf("unknown api-notice provider %q", providerName)
			}
			if !provider.Ready {
				reason := strings.TrimSpace(provider.Reason)
				if reason == "" {
					reason = "provider is not ready"
				}
				return dto.AlertRuleInput{}, fmt.Errorf("api-notice provider %q is not ready: %s", providerName, reason)
			}
			if !containsString(provider.Capabilities, input.MessageFormat) {
				return dto.AlertRuleInput{}, fmt.Errorf("api-notice provider %q does not support %s", providerName, input.MessageFormat)
			}
		}
	}
	return input, nil
}

func NewAlertRuleFromInput(input dto.AlertRuleInput) (*model.AlertRule, error) {
	triggerJSON, err := common.Marshal(input.TriggerConfig)
	if err != nil {
		return nil, err
	}
	providersJSON, err := common.Marshal(input.Providers)
	if err != nil {
		return nil, err
	}
	messageJSON, err := common.Marshal(input.MessageTemplate)
	if err != nil {
		return nil, err
	}
	return &model.AlertRule{
		Name: input.Name, Enabled: input.Enabled, TriggerType: input.TriggerType,
		TriggerConfigJSON: string(triggerJSON), ProvidersJSON: string(providersJSON),
		MessageFormat: input.MessageFormat, MessageTemplateJSON: string(messageJSON),
		ConsecutiveRequired: input.ConsecutiveRequired, CooldownSeconds: input.CooldownSeconds,
		SendRecovery: input.SendRecovery,
	}, nil
}

func AlertRuleToView(rule *model.AlertRule, states []*model.AlertRuleState) (dto.AlertRuleView, error) {
	if rule == nil {
		return dto.AlertRuleView{}, errors.New("alert rule is required")
	}
	var trigger dto.AlertRuleTriggerConfig
	if err := common.UnmarshalJsonStr(rule.TriggerConfigJSON, &trigger); err != nil {
		return dto.AlertRuleView{}, err
	}
	providers := make([]string, 0)
	if err := common.UnmarshalJsonStr(rule.ProvidersJSON, &providers); err != nil {
		return dto.AlertRuleView{}, err
	}
	var message dto.ApiNoticeMessage
	if err := common.UnmarshalJsonStr(rule.MessageTemplateJSON, &message); err != nil {
		return dto.AlertRuleView{}, err
	}
	summary := dto.AlertRuleStateSummary{State: AlertStateNormal}
	var lastErrorUpdatedAt int64
	for _, state := range states {
		if state == nil || state.RuleID != rule.ID {
			continue
		}
		switch state.State {
		case AlertStateActive:
			summary.State = AlertStateActive
			summary.ActiveSubjects++
		case AlertStatePending:
			if summary.State != AlertStateActive {
				summary.State = AlertStatePending
			}
			summary.PendingSubjects++
		}
		if state.LastTriggeredAt > summary.LastTriggeredAt {
			summary.LastTriggeredAt = state.LastTriggeredAt
		}
		if state.LastRecoveredAt > summary.LastRecoveredAt {
			summary.LastRecoveredAt = state.LastRecoveredAt
		}
		if state.LastErrorSummary != "" && state.UpdatedAt >= lastErrorUpdatedAt {
			summary.LastErrorSummary = state.LastErrorSummary
			lastErrorUpdatedAt = state.UpdatedAt
		}
	}
	return dto.AlertRuleView{
		ID: rule.ID, Name: rule.Name, Enabled: rule.Enabled, TriggerType: rule.TriggerType,
		TriggerConfig: trigger, Providers: providers, MessageFormat: rule.MessageFormat,
		MessageTemplate: message, ConsecutiveRequired: rule.ConsecutiveRequired,
		CooldownSeconds: rule.CooldownSeconds, SendRecovery: rule.SendRecovery,
		Revision: rule.Revision, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt, State: summary,
	}, nil
}

func ListAlertRuleViews() ([]dto.AlertRuleView, error) {
	rules, err := model.ListAlertRules()
	if err != nil {
		return nil, err
	}
	ruleIDs := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	states, err := model.ListAlertRuleStates(ruleIDs)
	if err != nil {
		return nil, err
	}
	views := make([]dto.AlertRuleView, 0, len(rules))
	for _, rule := range rules {
		view, viewErr := AlertRuleToView(rule, states)
		if viewErr != nil {
			return nil, viewErr
		}
		views = append(views, view)
	}
	return views, nil
}

func EvaluateUpstreamChannelAlertRules(ctx context.Context, channel *model.UpstreamChannel) error {
	if channel == nil || channel.Id <= 0 || channel.BalanceUpdatedTime <= 0 {
		return nil
	}
	rules, err := model.ListEnabledAlertRules(model.AlertRuleTriggerTypeUpstreamChannelEffectiveBalance)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	effectiveBalance := channel.Balance * channel.EffectiveMultiplier()
	now := time.Now().Unix()
	queued := false
	for _, rule := range rules {
		var trigger dto.AlertRuleTriggerConfig
		if err = common.UnmarshalJsonStr(rule.TriggerConfigJSON, &trigger); err != nil {
			continue
		}
		match := compareAlertValue(effectiveBalance, trigger.Operator, trigger.Threshold)
		subjectKey := fmt.Sprintf("upstream_channel:%d", channel.Id)
		mutationErr := model.MutateAlertRuleState(rule.ID, subjectKey, func(state *model.AlertRuleState) error {
			transition, transitionErr := AdvanceAlertRuleState(*rule, state, AlertObservation{
				Match: match, ObservedAt: channel.BalanceUpdatedTime, Now: now, WindowSeconds: trigger.WindowSeconds,
			})
			if transitionErr != nil || transition.EventType == "" {
				return transitionErr
			}
			request, requestErr := buildAlertNoticeRequest(*rule, channel, trigger, transition)
			if requestErr != nil {
				state.LastErrorSummary = sanitizeApiNoticeError(requestErr.Error())
				clearPendingAlertEvent(state)
				return nil
			}
			requestJSON, marshalErr := common.Marshal(request)
			if marshalErr != nil {
				return marshalErr
			}
			state.PendingRequestJSON = string(requestJSON)
			queued = true
			return nil
		})
		if mutationErr != nil {
			return mutationErr
		}
	}
	if queued {
		_, _, err = EnqueueSystemTask(model.SystemTaskTypeAlertNoticeDelivery, nil)
	}
	return err
}

func EvaluateEnabledChannelCountAlertRules(ctx context.Context) error {
	enabledCount, err := model.CountEnabledChannels()
	if err != nil {
		return err
	}
	return evaluateEnabledChannelCountAlertRules(ctx, enabledCount, false)
}

func EvaluateEnabledChannelCountAlertRulesAfterChannelDisable(ctx context.Context, previousEnabledCount int64) error {
	enabledCount, err := model.CountEnabledChannels()
	if err != nil {
		return err
	}
	if enabledCount >= previousEnabledCount {
		return nil
	}
	return evaluateEnabledChannelCountAlertRules(ctx, enabledCount, true)
}

func evaluateEnabledChannelCountAlertRules(ctx context.Context, enabledCount int64, repeatMatchedTrigger bool) error {
	rules, err := model.ListEnabledAlertRules(model.AlertRuleTriggerTypeEnabledChannelCount)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	now := time.Now().Unix()
	queued := false
	for _, rule := range rules {
		var trigger dto.AlertRuleTriggerConfig
		if err = common.UnmarshalJsonStr(rule.TriggerConfigJSON, &trigger); err != nil {
			continue
		}
		match := enabledCount <= int64(trigger.Threshold)
		mutationErr := model.MutateAlertRuleState(rule.ID, EnabledChannelCountAlertSubject, func(state *model.AlertRuleState) error {
			observedAt := now
			if observedAt <= state.LastObservationAt {
				observedAt = state.LastObservationAt + 1
			}
			transition, transitionErr := AdvanceAlertRuleState(*rule, state, AlertObservation{
				Match: match, ObservedAt: observedAt, Now: now, WindowSeconds: 0,
				RepeatMatchedTrigger: repeatMatchedTrigger,
			})
			if transitionErr != nil || transition.EventType == "" {
				return transitionErr
			}
			request, requestErr := buildEnabledChannelCountAlertNoticeRequest(*rule, enabledCount, trigger, transition, now)
			if requestErr != nil {
				state.LastErrorSummary = sanitizeApiNoticeError(requestErr.Error())
				clearPendingAlertEvent(state)
				return nil
			}
			requestJSON, marshalErr := common.Marshal(request)
			if marshalErr != nil {
				return marshalErr
			}
			state.PendingRequestJSON = string(requestJSON)
			queued = true
			return nil
		})
		if mutationErr != nil {
			return mutationErr
		}
	}
	if queued {
		_, _, err = EnqueueSystemTask(model.SystemTaskTypeAlertNoticeDelivery, nil)
	}
	return err
}

func BuildAlertPreview(ruleInput dto.AlertRuleInput, eventType string, channel *model.UpstreamChannel) (dto.ApiNoticeMessage, error) {
	if eventType == "" {
		eventType = AlertEventTrigger
	}
	if eventType != AlertEventTrigger && eventType != AlertEventRecovery {
		return dto.ApiNoticeMessage{}, errors.New("event_type must be trigger or recovery")
	}
	trigger := ruleInput.TriggerConfig
	enabledChannelCount := ""
	if ruleInput.TriggerType == model.AlertRuleTriggerTypeEnabledChannelCount {
		count, err := model.CountEnabledChannels()
		if err != nil {
			return dto.ApiNoticeMessage{}, fmt.Errorf("count enabled channels for alert preview: %w", err)
		}
		enabledChannelCount = strconv.FormatInt(count, 10)
	}
	variables := AlertTemplateVariables{
		RuleName: ruleInput.Name, EventType: eventType, ChannelID: "1", ChannelName: "Example channel",
		ChannelProvider: "example", ChannelBalance: "10", ChannelEffectiveBalance: "10",
		ConditionOperator: trigger.Operator, ConditionThreshold: strconv.FormatFloat(trigger.Threshold, 'f', -1, 64),
		ChannelPoolEnabledCount: enabledChannelCount, ObservedAt: time.Now().Format(time.RFC3339),
	}
	if channel != nil {
		variables.ChannelID = strconv.Itoa(channel.Id)
		variables.ChannelName = channel.Name
		variables.ChannelProvider = channel.Provider
		variables.ChannelBalance = strconv.FormatFloat(channel.Balance, 'f', -1, 64)
		variables.ChannelEffectiveBalance = strconv.FormatFloat(channel.Balance*channel.EffectiveMultiplier(), 'f', -1, 64)
		if channel.BalanceUpdatedTime > 0 {
			variables.ObservedAt = time.Unix(channel.BalanceUpdatedTime, 0).Format(time.RFC3339)
		}
	}
	return RenderAlertMessage(ruleInput.MessageTemplate, variables)
}

func buildAlertNoticeRequest(rule model.AlertRule, channel *model.UpstreamChannel, trigger dto.AlertRuleTriggerConfig, transition AlertStateTransition) (dto.ApiNoticeRequest, error) {
	var providers []string
	if err := common.UnmarshalJsonStr(rule.ProvidersJSON, &providers); err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	var template dto.ApiNoticeMessage
	if err := common.UnmarshalJsonStr(rule.MessageTemplateJSON, &template); err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	message, err := RenderAlertMessage(template, AlertTemplateVariables{
		RuleName: rule.Name, EventType: transition.EventType, ChannelID: strconv.Itoa(channel.Id),
		ChannelName: channel.Name, ChannelProvider: channel.Provider,
		ChannelBalance:          strconv.FormatFloat(channel.Balance, 'f', -1, 64),
		ChannelEffectiveBalance: strconv.FormatFloat(channel.Balance*channel.EffectiveMultiplier(), 'f', -1, 64),
		ConditionOperator:       trigger.Operator, ConditionThreshold: strconv.FormatFloat(trigger.Threshold, 'f', -1, 64),
		ObservedAt: time.Unix(channel.BalanceUpdatedTime, 0).Format(time.RFC3339),
	})
	if err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	return dto.ApiNoticeRequest{IdempotencyKey: transition.IdempotencyKey, Providers: providers, Message: message}, nil
}

func buildEnabledChannelCountAlertNoticeRequest(rule model.AlertRule, enabledCount int64, trigger dto.AlertRuleTriggerConfig, transition AlertStateTransition, observedAt int64) (dto.ApiNoticeRequest, error) {
	var providers []string
	if err := common.UnmarshalJsonStr(rule.ProvidersJSON, &providers); err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	var template dto.ApiNoticeMessage
	if err := common.UnmarshalJsonStr(rule.MessageTemplateJSON, &template); err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	message, err := RenderAlertMessage(template, AlertTemplateVariables{
		RuleName: rule.Name, EventType: transition.EventType,
		ChannelPoolEnabledCount: strconv.FormatInt(enabledCount, 10),
		ConditionOperator:       "lte", ConditionThreshold: strconv.FormatFloat(trigger.Threshold, 'f', -1, 64),
		ObservedAt: time.Unix(observedAt, 0).Format(time.RFC3339),
	})
	if err != nil {
		return dto.ApiNoticeRequest{}, err
	}
	return dto.ApiNoticeRequest{IdempotencyKey: transition.IdempotencyKey, Providers: providers, Message: message}, nil
}

func compareAlertValue(value float64, operator string, threshold float64) bool {
	switch operator {
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func GetOptionalUpstreamChannel(id int) (*model.UpstreamChannel, error) {
	if id <= 0 {
		return nil, nil
	}
	channel, err := model.GetUpstreamChannelByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return channel, err
}
