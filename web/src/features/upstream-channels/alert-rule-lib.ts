import type {
  AlertEventType,
  AlertMessageFormat,
  AlertRuleInput,
  AlertRulePreviewRequest,
  AlertRuleTestSendRequest,
  AlertRuleTriggerType,
  ApiNoticeMessage,
  ApiNoticeProvider,
} from './types.ts'

export const alertMessageFormats: AlertMessageFormat[] = [
  'text',
  'markdown',
  'card',
  'table',
]

export function getAlertRuleTriggerTypeLabel(
  triggerType: AlertRuleTriggerType
): string {
  return triggerType === 'enabled_channel_count'
    ? 'Available channel count'
    : 'Upstream channel effective balance'
}

function createMessageTemplate(
  triggerType: AlertRuleTriggerType,
  format: AlertMessageFormat
): ApiNoticeMessage {
  if (triggerType === 'enabled_channel_count') {
    if (format === 'markdown') {
      return {
        format,
        text: '**{{rule.name}}**\n\nEnabled channel count: `{{channel_pool.enabled_count}}`\n\nCondition: `<= {{condition.threshold}}`',
      }
    }
    if (format === 'card') {
      return {
        format,
        title: '{{rule.name}}',
        level: 'warning',
        text: 'Available channel count alert',
        fields: [
          {
            name: 'Enabled channel count',
            value: '{{channel_pool.enabled_count}}',
          },
          { name: 'Threshold', value: '{{condition.threshold}}' },
        ],
        sections: [
          {
            title: 'Condition',
            text: '<= {{condition.threshold}}',
          },
        ],
        actions: [],
      }
    }
    if (format === 'table') {
      return {
        format,
        title: '{{rule.name}}',
        text: 'Available channel count alert',
        table: {
          columns: [
            { key: 'enabled_count', label: 'Enabled channel count' },
            { key: 'threshold', label: 'Threshold' },
          ],
          rows: [
            {
              enabled_count: '{{channel_pool.enabled_count}}',
              threshold: '{{condition.threshold}}',
            },
          ],
        },
      }
    }
    return {
      format: 'text',
      text: '{{rule.name}}：剩余有效渠道: {{channel_pool.enabled_count}} (<= {{condition.threshold}})',
    }
  }

  if (format === 'markdown') {
    return {
      format,
      text: '**{{rule.name}}**\n\n{{channel.name}} effective balance: `{{channel.effective_balance}}`\n\nCondition: `{{condition.operator}} {{condition.threshold}}`',
    }
  }
  if (format === 'card') {
    return {
      format,
      title: '{{rule.name}}',
      level: 'warning',
      text: '{{channel.name}} balance alert',
      fields: [
        { name: 'Effective balance', value: '{{channel.effective_balance}}' },
        { name: 'Threshold', value: '{{condition.threshold}}' },
      ],
      sections: [
        {
          title: 'Condition',
          text: '{{condition.operator}} {{condition.threshold}}',
        },
      ],
      actions: [],
    }
  }
  if (format === 'table') {
    return {
      format,
      title: '{{rule.name}}',
      text: 'Upstream channel balance alert',
      table: {
        columns: [
          { key: 'channel', label: 'Channel' },
          { key: 'balance', label: 'Effective balance' },
          { key: 'threshold', label: 'Threshold' },
        ],
        rows: [
          {
            channel: '{{channel.name}}',
            balance: '{{channel.effective_balance}}',
            threshold: '{{condition.threshold}}',
          },
        ],
      },
    }
  }
  return {
    format: 'text',
    text: '{{rule.name}}: {{channel.name}} effective balance is {{channel.effective_balance}} ({{condition.operator}} {{condition.threshold}})',
  }
}

export function createDefaultAlertRuleInput(
  providers: string[] = [],
  triggerType: AlertRuleTriggerType = 'upstream_channel_effective_balance'
): AlertRuleInput {
  const enabledChannelCount = triggerType === 'enabled_channel_count'
  return {
    name: getAlertRuleTriggerTypeLabel(triggerType),
    enabled: true,
    trigger_type: triggerType,
    trigger_config: {
      operator: 'lte',
      threshold: enabledChannelCount ? 1 : 10,
      window_seconds: enabledChannelCount ? 0 : 300,
    },
    providers: [...providers],
    message_format: 'text',
    message_template: createMessageTemplate(triggerType, 'text'),
    consecutive_required: 1,
    cooldown_seconds: enabledChannelCount ? 0 : 900,
    send_recovery: true,
  }
}

export function switchAlertRuleTriggerType(
  draft: AlertRuleInput,
  triggerType: AlertRuleTriggerType
): AlertRuleInput {
  if (draft.trigger_type === triggerType) return draft
  const defaults = createDefaultAlertRuleInput(draft.providers, triggerType)
  return {
    ...draft,
    name: defaults.name,
    trigger_type: defaults.trigger_type,
    trigger_config: defaults.trigger_config,
    message_format: draft.message_format,
    message_template: createMessageTemplate(triggerType, draft.message_format),
    consecutive_required: defaults.consecutive_required,
    cooldown_seconds: defaults.cooldown_seconds,
  }
}

export function switchAlertMessageFormat(
  draft: AlertRuleInput,
  format: AlertMessageFormat
): AlertRuleInput {
  if (draft.message_format === format) return draft
  return {
    ...draft,
    message_format: format,
    message_template: createMessageTemplate(draft.trigger_type, format),
  }
}

export function getProviderCapabilityIntersection(
  catalog: ApiNoticeProvider[],
  providerNames: string[]
): string[] {
  if (providerNames.length === 0) return []
  const providersByName = new Map(
    catalog.map((provider) => [provider.name, provider])
  )
  const selected = providerNames
    .map((name) => providersByName.get(name))
    .filter((provider): provider is ApiNoticeProvider => provider !== undefined)
  if (selected.length !== providerNames.length || selected.length === 0) {
    return []
  }

  const first = new Set(selected[0].capabilities)
  return [...first].filter((capability) =>
    selected.every((provider) => provider.capabilities.includes(capability))
  )
}

export function getAlertMessageFormatOptions(
  catalog: ApiNoticeProvider[],
  providerNames: string[]
): Array<{ format: AlertMessageFormat; available: boolean }> {
  const capabilities = new Set(
    getProviderCapabilityIntersection(catalog, providerNames)
  )
  return alertMessageFormats.map((format) => ({
    format,
    available: capabilities.has(format),
  }))
}

export function getProviderUnavailableReason(
  provider: ApiNoticeProvider
): string {
  if (provider.ready) return ''
  return provider.reason?.trim() || 'Provider is not ready'
}

export function buildAlertRulePreviewRequest(
  rule: AlertRuleInput,
  eventType: AlertEventType,
  channelID: number
): AlertRulePreviewRequest {
  return { rule, event_type: eventType, channel_id: channelID }
}

export function buildAlertRuleTestSendRequest(
  rule: AlertRuleInput,
  eventType: AlertEventType,
  channelID: number,
  providers: string[]
): AlertRuleTestSendRequest {
  return {
    providers,
    ...buildAlertRulePreviewRequest(rule, eventType, channelID),
  }
}

export function validateAlertRuleDraft(
  draft: AlertRuleInput,
  catalog: ApiNoticeProvider[]
): string[] {
  const errors: string[] = []
  if (!draft.name.trim()) errors.push('Rule name is required')

  if (draft.trigger_type === 'enabled_channel_count') {
    if (
      !Number.isInteger(draft.trigger_config.threshold) ||
      draft.trigger_config.threshold < 0 ||
      draft.trigger_config.threshold > 1_000_000
    ) {
      errors.push(
        'Enabled channel count threshold must be a non-negative integer'
      )
    }
    if (draft.trigger_config.operator !== 'lte') {
      errors.push(
        'Enabled channel count comparison must use less than or equal'
      )
    }
    if (
      draft.trigger_config.window_seconds !== 0 ||
      draft.consecutive_required !== 1 ||
      draft.cooldown_seconds !== 0
    ) {
      errors.push('Enabled channel count condition settings are fixed')
    }
  } else {
    if (
      !Number.isFinite(draft.trigger_config.threshold) ||
      draft.trigger_config.threshold < 0
    ) {
      errors.push('Threshold must be 0 or greater')
    }
    if (
      !Number.isInteger(draft.trigger_config.window_seconds) ||
      draft.trigger_config.window_seconds < 0
    ) {
      errors.push('Statistics window must be a non-negative integer')
    }
    if (
      !Number.isInteger(draft.consecutive_required) ||
      draft.consecutive_required < 1
    ) {
      errors.push('Consecutive matches must be a positive integer')
    }
    if (
      !Number.isInteger(draft.cooldown_seconds) ||
      draft.cooldown_seconds < 0
    ) {
      errors.push('Cooldown must be a non-negative integer')
    }
  }

  if (draft.providers.length === 0) {
    errors.push('Select at least one notification provider')
  }

  const providersByName = new Map(
    catalog.map((provider) => [provider.name, provider])
  )
  const selectedProviders = draft.providers
    .map((name) => providersByName.get(name))
    .filter((provider): provider is ApiNoticeProvider => provider !== undefined)
  if (selectedProviders.length !== draft.providers.length) {
    errors.push('One or more selected providers are unavailable')
  }
  if (selectedProviders.some((provider) => !provider.ready)) {
    errors.push('One or more selected providers are not ready')
  }
  if (
    draft.providers.length > 0 &&
    !getProviderCapabilityIntersection(catalog, draft.providers).includes(
      draft.message_format
    )
  ) {
    errors.push('Selected providers do not all support this message format')
  }

  const message = draft.message_template
  if (message.format !== draft.message_format) {
    errors.push('Message template format does not match the selected format')
  }
  if (draft.message_format === 'text' || draft.message_format === 'markdown') {
    if (!message.text?.trim()) errors.push('Message content is required')
  }
  if (draft.message_format === 'card') {
    if (!message.title?.trim()) errors.push('Card title is required')
    if (!message.text?.trim()) errors.push('Card body is required')
    if (
      message.actions?.some(
        (action) => !action.url.trim().toLowerCase().startsWith('https://')
      )
    ) {
      errors.push('Card action URLs must use HTTPS')
    }
  }
  if (draft.message_format === 'table') {
    if (!message.title?.trim()) errors.push('Table title is required')
    if (!message.table || message.table.columns.length === 0) {
      errors.push('Add at least one table column')
    }
    if (!message.table || message.table.rows.length === 0) {
      errors.push('Add at least one table row')
    }
  }
  return errors
}
