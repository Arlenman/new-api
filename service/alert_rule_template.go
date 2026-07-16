package service

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/dto"
)

const (
	apiNoticeMaxTitleLength = 200
	apiNoticeMaxTextLength  = 20_000
	apiNoticeMaxFields      = 50
	apiNoticeMaxSections    = 20
	apiNoticeMaxActions     = 10
	apiNoticeMaxColumns     = 20
	apiNoticeMaxRows        = 100
)

var alertTemplateTokenPattern = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

type AlertTemplateVariables struct {
	RuleName                string
	EventType               string
	ChannelID               string
	ChannelName             string
	ChannelProvider         string
	ChannelBalance          string
	ChannelEffectiveBalance string
	ChannelPoolEnabledCount string
	ConditionOperator       string
	ConditionThreshold      string
	ObservedAt              string
}

func (variables AlertTemplateVariables) values() map[string]string {
	return map[string]string{
		"rule.name":                  variables.RuleName,
		"event.type":                 variables.EventType,
		"channel.id":                 variables.ChannelID,
		"channel.name":               variables.ChannelName,
		"channel.provider":           variables.ChannelProvider,
		"channel.balance":            variables.ChannelBalance,
		"channel.effective_balance":  variables.ChannelEffectiveBalance,
		"channel_pool.enabled_count": variables.ChannelPoolEnabledCount,
		"condition.operator":         variables.ConditionOperator,
		"condition.threshold":        variables.ConditionThreshold,
		"observed_at":                variables.ObservedAt,
		"observation.observed_at":    variables.ObservedAt,
	}
}

func ValidateAlertMessageTemplate(format string, message dto.ApiNoticeMessage) error {
	if format != message.Format {
		return errors.New("message format does not match the selected format")
	}
	if err := validateAlertTemplateSyntax(message); err != nil {
		return err
	}
	sample := AlertTemplateVariables{
		RuleName: "Example rule", EventType: AlertEventTrigger, ChannelID: "1", ChannelName: "example",
		ChannelProvider: "example", ChannelBalance: "1", ChannelEffectiveBalance: "1", ChannelPoolEnabledCount: "1",
		ConditionOperator: "lte", ConditionThreshold: "1", ObservedAt: "2026-01-01T00:00:00Z",
	}
	rendered, err := renderAlertMessage(message, sample.values())
	if err != nil {
		return err
	}
	return validateRenderedApiNoticeMessage(rendered)
}

func RenderAlertMessage(template dto.ApiNoticeMessage, variables AlertTemplateVariables) (dto.ApiNoticeMessage, error) {
	if err := ValidateAlertMessageTemplate(template.Format, template); err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	message, err := renderAlertMessage(template, variables.values())
	if err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	if err = validateRenderedApiNoticeMessage(message); err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	return message, nil
}

func validateAlertTemplateSyntax(message dto.ApiNoticeMessage) error {
	values := []string{message.Title, message.Level, message.Text}
	for _, field := range message.Fields {
		values = append(values, field.Name, field.Value)
	}
	for _, section := range message.Sections {
		values = append(values, section.Title, section.Text)
	}
	for _, action := range message.Actions {
		values = append(values, action.Label, action.URL)
	}
	if message.Table != nil {
		for _, column := range message.Table.Columns {
			values = append(values, column.Key, column.Label)
		}
		for _, row := range message.Table.Rows {
			for key, value := range row {
				values = append(values, key, value)
			}
		}
	}
	allowed := AlertTemplateVariables{}.values()
	for _, value := range values {
		matches := alertTemplateTokenPattern.FindAllStringSubmatch(value, -1)
		for _, match := range matches {
			name := strings.TrimSpace(match[1])
			if _, ok := allowed[name]; !ok || match[1] != name {
				return fmt.Errorf("unsupported alert template variable %q", match[1])
			}
		}
		remaining := alertTemplateTokenPattern.ReplaceAllString(value, "")
		if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
			return errors.New("invalid alert template syntax")
		}
	}
	return nil
}

func renderAlertMessage(message dto.ApiNoticeMessage, values map[string]string) (dto.ApiNoticeMessage, error) {
	message.Fields = append([]dto.ApiNoticeField(nil), message.Fields...)
	message.Sections = append([]dto.ApiNoticeSection(nil), message.Sections...)
	message.Actions = append([]dto.ApiNoticeAction(nil), message.Actions...)
	if message.Table != nil {
		table := &dto.ApiNoticeTable{
			Columns: append([]dto.ApiNoticeColumn(nil), message.Table.Columns...),
			Rows:    make([]map[string]string, len(message.Table.Rows)),
		}
		for index, row := range message.Table.Rows {
			table.Rows[index] = make(map[string]string, len(row))
			for key, value := range row {
				table.Rows[index][key] = value
			}
		}
		message.Table = table
	}
	var err error
	message.Title, err = renderAlertTemplateValue(message.Title, values)
	if err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	message.Level, err = renderAlertTemplateValue(message.Level, values)
	if err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	message.Text, err = renderAlertTemplateValue(message.Text, values)
	if err != nil {
		return dto.ApiNoticeMessage{}, err
	}
	for index := range message.Fields {
		message.Fields[index].Name, err = renderAlertTemplateValue(message.Fields[index].Name, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
		message.Fields[index].Value, err = renderAlertTemplateValue(message.Fields[index].Value, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
	}
	for index := range message.Sections {
		message.Sections[index].Title, err = renderAlertTemplateValue(message.Sections[index].Title, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
		message.Sections[index].Text, err = renderAlertTemplateValue(message.Sections[index].Text, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
	}
	for index := range message.Actions {
		message.Actions[index].Label, err = renderAlertTemplateValue(message.Actions[index].Label, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
		message.Actions[index].URL, err = renderAlertTemplateValue(message.Actions[index].URL, values)
		if err != nil {
			return dto.ApiNoticeMessage{}, err
		}
	}
	if message.Table != nil {
		table := &dto.ApiNoticeTable{
			Columns: make([]dto.ApiNoticeColumn, len(message.Table.Columns)),
			Rows:    make([]map[string]string, len(message.Table.Rows)),
		}
		for index, column := range message.Table.Columns {
			table.Columns[index].Key, err = renderAlertTemplateValue(column.Key, values)
			if err != nil {
				return dto.ApiNoticeMessage{}, err
			}
			table.Columns[index].Label, err = renderAlertTemplateValue(column.Label, values)
			if err != nil {
				return dto.ApiNoticeMessage{}, err
			}
		}
		for rowIndex, row := range message.Table.Rows {
			table.Rows[rowIndex] = make(map[string]string, len(row))
			for key, value := range row {
				renderedKey, renderErr := renderAlertTemplateValue(key, values)
				if renderErr != nil {
					return dto.ApiNoticeMessage{}, renderErr
				}
				renderedValue, renderErr := renderAlertTemplateValue(value, values)
				if renderErr != nil {
					return dto.ApiNoticeMessage{}, renderErr
				}
				table.Rows[rowIndex][renderedKey] = renderedValue
			}
		}
		message.Table = table
	}
	return message, nil
}

func renderAlertTemplateValue(value string, values map[string]string) (string, error) {
	var renderErr error
	rendered := alertTemplateTokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		match := alertTemplateTokenPattern.FindStringSubmatch(token)
		if len(match) != 2 {
			renderErr = errors.New("invalid alert template syntax")
			return ""
		}
		name := strings.TrimSpace(match[1])
		replacement, ok := values[name]
		if !ok {
			renderErr = fmt.Errorf("unsupported alert template variable %q", name)
			return ""
		}
		return replacement
	})
	if renderErr != nil {
		return "", renderErr
	}
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", errors.New("invalid alert template syntax")
	}
	return rendered, nil
}

func validateRenderedApiNoticeMessage(message dto.ApiNoticeMessage) error {
	switch message.Format {
	case "text", "markdown", "card", "table":
	default:
		return fmt.Errorf("unsupported message format %q", message.Format)
	}
	if utf8.RuneCountInString(message.Title) > apiNoticeMaxTitleLength {
		return fmt.Errorf("message title exceeds %d characters", apiNoticeMaxTitleLength)
	}
	if utf8.RuneCountInString(message.Text) > apiNoticeMaxTextLength {
		return fmt.Errorf("message text exceeds %d characters", apiNoticeMaxTextLength)
	}
	if len(message.Fields) > apiNoticeMaxFields || len(message.Sections) > apiNoticeMaxSections || len(message.Actions) > apiNoticeMaxActions {
		return errors.New("message collection limit exceeded")
	}
	if (message.Format == "text" || message.Format == "markdown") && strings.TrimSpace(message.Text) == "" {
		return errors.New("message text is required")
	}
	if message.Format == "card" {
		if strings.TrimSpace(message.Title) == "" {
			return errors.New("card title is required")
		}
		if strings.TrimSpace(message.Text) == "" && len(message.Fields) == 0 && len(message.Sections) == 0 {
			return errors.New("card content is required")
		}
	}
	if message.Format == "table" {
		if strings.TrimSpace(message.Title) == "" {
			return errors.New("table title is required")
		}
		if message.Table == nil || len(message.Table.Columns) == 0 {
			return errors.New("table columns are required")
		}
		if len(message.Table.Columns) > apiNoticeMaxColumns || len(message.Table.Rows) > apiNoticeMaxRows {
			return errors.New("table dimensions exceed limits")
		}
		for _, column := range message.Table.Columns {
			if strings.TrimSpace(column.Key) == "" || strings.TrimSpace(column.Label) == "" {
				return errors.New("table column key and label are required")
			}
		}
	}
	for _, field := range message.Fields {
		if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Value) == "" {
			return errors.New("card field name and value are required")
		}
	}
	for _, action := range message.Actions {
		parsed, err := url.ParseRequestURI(action.URL)
		if strings.TrimSpace(action.Label) == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("actions require a label and HTTPS URL")
		}
	}
	return nil
}
