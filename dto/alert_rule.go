package dto

type AlertRuleTriggerConfig struct {
	Operator      string  `json:"operator"`
	Threshold     float64 `json:"threshold"`
	WindowSeconds int     `json:"window_seconds"`
}

type AlertRuleInput struct {
	Name                string                 `json:"name"`
	Enabled             bool                   `json:"enabled"`
	TriggerType         string                 `json:"trigger_type"`
	TriggerConfig       AlertRuleTriggerConfig `json:"trigger_config"`
	Providers           []string               `json:"providers"`
	MessageFormat       string                 `json:"message_format"`
	MessageTemplate     ApiNoticeMessage       `json:"message_template"`
	ConsecutiveRequired int                    `json:"consecutive_required"`
	CooldownSeconds     int                    `json:"cooldown_seconds"`
	SendRecovery        bool                   `json:"send_recovery"`
}

type AlertRulePreviewRequest struct {
	Rule      AlertRuleInput `json:"rule"`
	EventType string         `json:"event_type"`
	ChannelID int            `json:"channel_id"`
}

type AlertRuleTestSendRequest struct {
	Providers []string       `json:"providers"`
	Rule      AlertRuleInput `json:"rule"`
	EventType string         `json:"event_type"`
	ChannelID int            `json:"channel_id"`
}

type AlertRuleStateSummary struct {
	State            string `json:"state"`
	ActiveSubjects   int    `json:"active_subjects"`
	PendingSubjects  int    `json:"pending_subjects"`
	LastTriggeredAt  int64  `json:"last_triggered_at"`
	LastRecoveredAt  int64  `json:"last_recovered_at"`
	LastErrorSummary string `json:"last_error_summary"`
}

type AlertRuleView struct {
	ID                  int64                  `json:"id"`
	Name                string                 `json:"name"`
	Enabled             bool                   `json:"enabled"`
	TriggerType         string                 `json:"trigger_type"`
	TriggerConfig       AlertRuleTriggerConfig `json:"trigger_config"`
	Providers           []string               `json:"providers"`
	MessageFormat       string                 `json:"message_format"`
	MessageTemplate     ApiNoticeMessage       `json:"message_template"`
	ConsecutiveRequired int                    `json:"consecutive_required"`
	CooldownSeconds     int                    `json:"cooldown_seconds"`
	SendRecovery        bool                   `json:"send_recovery"`
	Revision            int64                  `json:"revision"`
	CreatedAt           int64                  `json:"created_at"`
	UpdatedAt           int64                  `json:"updated_at"`
	State               AlertRuleStateSummary  `json:"state"`
}
