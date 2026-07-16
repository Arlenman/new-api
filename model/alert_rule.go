package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AlertRuleTriggerTypeUpstreamChannelEffectiveBalance = "upstream_channel_effective_balance"
	AlertRuleTriggerTypeEnabledChannelCount             = "enabled_channel_count"
)

var ErrEnabledChannelCountAlertRuleExists = errors.New("enabled channel count alert rule already exists")

type AlertRule struct {
	ID                  int64   `json:"id" gorm:"primaryKey"`
	Name                string  `json:"name" gorm:"type:varchar(128);not null"`
	Enabled             bool    `json:"enabled" gorm:"not null"`
	TriggerType         string  `json:"trigger_type" gorm:"type:varchar(64);index;not null"`
	SingletonKey        *string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	TriggerConfigJSON   string  `json:"-" gorm:"type:text;not null"`
	ProvidersJSON       string  `json:"-" gorm:"type:text;not null"`
	MessageFormat       string  `json:"message_format" gorm:"type:varchar(16);not null"`
	MessageTemplateJSON string  `json:"-" gorm:"type:text;not null"`
	ConsecutiveRequired int     `json:"consecutive_required" gorm:"not null"`
	CooldownSeconds     int     `json:"cooldown_seconds" gorm:"not null"`
	SendRecovery        bool    `json:"send_recovery" gorm:"not null"`
	Revision            int64   `json:"revision" gorm:"not null"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint;index"`
	UpdatedAt           int64   `json:"updated_at" gorm:"bigint;index"`
}

type AlertRuleState struct {
	ID                    int64  `json:"id" gorm:"primaryKey"`
	RuleID                int64  `json:"rule_id" gorm:"uniqueIndex:idx_alert_rule_subject;index;not null"`
	SubjectKey            string `json:"subject_key" gorm:"type:varchar(128);uniqueIndex:idx_alert_rule_subject;not null"`
	State                 string `json:"state" gorm:"type:varchar(16);index;not null"`
	ConsecutiveMatches    int    `json:"consecutive_matches" gorm:"not null"`
	WindowStartedAt       int64  `json:"window_started_at" gorm:"bigint"`
	LastObservationAt     int64  `json:"last_observation_at" gorm:"bigint;index"`
	IncidentSequence      int64  `json:"incident_sequence" gorm:"bigint;not null"`
	ActiveSince           int64  `json:"active_since" gorm:"bigint"`
	LastTriggeredAt       int64  `json:"last_triggered_at" gorm:"bigint"`
	LastRecoveredAt       int64  `json:"last_recovered_at" gorm:"bigint"`
	LastSentAt            int64  `json:"last_sent_at" gorm:"bigint"`
	TriggerDelivered      bool   `json:"trigger_delivered" gorm:"not null"`
	PendingEventType      string `json:"pending_event_type" gorm:"type:varchar(16);index"`
	PendingIdempotencyKey string `json:"pending_idempotency_key" gorm:"type:varchar(128)"`
	PendingRequestJSON    string `json:"-" gorm:"type:text"`
	NextAttemptAt         int64  `json:"next_attempt_at" gorm:"bigint;index"`
	DeliveryAttempts      int    `json:"delivery_attempts" gorm:"not null"`
	LastErrorSummary      string `json:"last_error_summary" gorm:"type:varchar(255)"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;index"`
}

func (rule *AlertRule) BeforeCreate(_ *gorm.DB) error {
	rule.normalizeSingletonKey()
	now := common.GetTimestamp()
	if rule.Revision <= 0 {
		rule.Revision = 1
	}
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt == 0 {
		rule.UpdatedAt = now
	}
	return nil
}

func (rule *AlertRule) normalizeSingletonKey() {
	if rule.TriggerType != AlertRuleTriggerTypeEnabledChannelCount {
		rule.SingletonKey = nil
		return
	}
	key := AlertRuleTriggerTypeEnabledChannelCount
	rule.SingletonKey = &key
}

func (state *AlertRuleState) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if state.CreatedAt == 0 {
		state.CreatedAt = now
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = now
	}
	return nil
}

func ListAlertRules() ([]*AlertRule, error) {
	var rules []*AlertRule
	err := DB.Order("id asc").Find(&rules).Error
	return rules, err
}

func ListEnabledAlertRules(triggerType string) ([]*AlertRule, error) {
	var rules []*AlertRule
	err := DB.Where("enabled = ? AND trigger_type = ?", true, triggerType).Order("id asc").Find(&rules).Error
	return rules, err
}

func GetAlertRuleByID(id int64) (*AlertRule, error) {
	if id <= 0 {
		return nil, errors.New("invalid alert rule id")
	}
	var rule AlertRule
	if err := DB.First(&rule, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func CreateAlertRule(rule *AlertRule) error {
	if rule == nil {
		return errors.New("alert rule is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if rule.TriggerType == AlertRuleTriggerTypeEnabledChannelCount {
			var count int64
			if err := tx.Model(&AlertRule{}).Where("trigger_type = ?", rule.TriggerType).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrEnabledChannelCountAlertRuleExists
			}
		}
		return tx.Create(rule).Error
	})
}

func UpdateAlertRule(rule *AlertRule) error {
	if rule == nil || rule.ID <= 0 {
		return errors.New("valid alert rule is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing AlertRule
		if err := lockForUpdate(tx).First(&existing, "id = ?", rule.ID).Error; err != nil {
			return err
		}
		if rule.TriggerType == AlertRuleTriggerTypeEnabledChannelCount {
			var count int64
			if err := tx.Model(&AlertRule{}).Where("trigger_type = ? AND id <> ?", rule.TriggerType, rule.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrEnabledChannelCountAlertRuleExists
			}
		}
		rule.normalizeSingletonKey()
		rule.Revision = existing.Revision + 1
		rule.CreatedAt = existing.CreatedAt
		rule.UpdatedAt = common.GetTimestamp()
		if err := tx.Model(&AlertRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
			"name": rule.Name, "enabled": rule.Enabled, "trigger_type": rule.TriggerType,
			"singleton_key": rule.SingletonKey, "trigger_config_json": rule.TriggerConfigJSON, "providers_json": rule.ProvidersJSON,
			"message_format": rule.MessageFormat, "message_template_json": rule.MessageTemplateJSON,
			"consecutive_required": rule.ConsecutiveRequired, "cooldown_seconds": rule.CooldownSeconds,
			"send_recovery": rule.SendRecovery, "revision": rule.Revision, "updated_at": rule.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Where("rule_id = ?", rule.ID).Delete(&AlertRuleState{}).Error
	})
}

func DeleteAlertRule(id int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rule_id = ?", id).Delete(&AlertRuleState{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&AlertRule{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func ListAlertRuleStates(ruleIDs []int64) ([]*AlertRuleState, error) {
	if len(ruleIDs) == 0 {
		return []*AlertRuleState{}, nil
	}
	var states []*AlertRuleState
	err := DB.Where("rule_id IN ?", ruleIDs).Find(&states).Error
	return states, err
}

func MutateAlertRuleState(ruleID int64, subjectKey string, mutate func(*AlertRuleState) error) error {
	if ruleID <= 0 || subjectKey == "" || mutate == nil {
		return errors.New("invalid alert rule state mutation")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		placeholder := AlertRuleState{RuleID: ruleID, SubjectKey: subjectKey, State: "normal"}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "rule_id"}, {Name: "subject_key"}},
			DoNothing: true,
		}).Create(&placeholder).Error; err != nil {
			return err
		}

		var state AlertRuleState
		if err := lockForUpdate(tx).Where("rule_id = ? AND subject_key = ?", ruleID, subjectKey).First(&state).Error; err != nil {
			return err
		}
		if err := mutate(&state); err != nil {
			return err
		}
		state.UpdatedAt = common.GetTimestamp()
		return tx.Save(&state).Error
	})
}

func ListDueAlertRuleStates(now int64, limit int) ([]*AlertRuleState, error) {
	if limit <= 0 {
		limit = 100
	}
	var states []*AlertRuleState
	err := DB.Where("pending_event_type <> '' AND pending_request_json <> '' AND next_attempt_at <= ?", now).
		Order("next_attempt_at asc").Order("id asc").Limit(limit).Find(&states).Error
	return states, err
}

func HasPendingAlertRuleDeliveries() bool {
	var count int64
	if err := DB.Model(&AlertRuleState{}).Where("pending_event_type <> '' AND pending_request_json <> ''").Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}
