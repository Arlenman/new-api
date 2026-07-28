/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { StatusBadgeProps } from '@/components/status-badge'

import {
  API_KEY_QUOTA_RESET_FORM_PERIODS,
  type ApiKeyQuotaResetPeriod,
} from './types'

// ============================================================================
// API Key Status Configuration
// label values are i18n keys; use t(config.label) in components (e.g. StatusBadge)
// ============================================================================

export const API_KEY_STATUS = {
  ENABLED: 1,
  DISABLED: 2,
  EXPIRED: 3,
  EXHAUSTED: 4,
} as const

export const API_KEY_STATUSES: Record<
  number,
  Pick<StatusBadgeProps, 'variant'> & {
    label: string
    value: number
  }
> = {
  [API_KEY_STATUS.ENABLED]: {
    label: 'Enabled',
    variant: 'success',
    value: API_KEY_STATUS.ENABLED,
  },
  [API_KEY_STATUS.DISABLED]: {
    label: 'Disabled',
    variant: 'neutral',
    value: API_KEY_STATUS.DISABLED,
  },
  [API_KEY_STATUS.EXPIRED]: {
    label: 'Expired',
    variant: 'warning',
    value: API_KEY_STATUS.EXPIRED,
  },
  [API_KEY_STATUS.EXHAUSTED]: {
    label: 'Exhausted',
    variant: 'danger',
    value: API_KEY_STATUS.EXHAUSTED,
  },
} as const

export const API_KEY_STATUS_OPTIONS = Object.values(API_KEY_STATUSES).map(
  (config) => ({
    label: config.label,
    value: String(config.value),
  })
)

// ============================================================================
// Periodic Quota Reset Configuration
// label values are i18n keys; use t(config.label) in components
// ============================================================================

export const API_KEY_QUOTA_RESET_PERIOD_OPTIONS: Record<
  ApiKeyQuotaResetPeriod,
  {
    value: ApiKeyQuotaResetPeriod
    label: string
    cadenceLabel: string
  }
> = {
  hourly: {
    value: 'hourly',
    label: 'Hourly',
    cadenceLabel: 'Every hour',
  },
  daily: {
    value: 'daily',
    label: 'Daily',
    cadenceLabel: 'Every day',
  },
  weekly: {
    value: 'weekly',
    label: 'Weekly',
    cadenceLabel: 'Every week',
  },
  monthly: {
    value: 'monthly',
    label: 'Monthly',
    cadenceLabel: 'Every month',
  },
  custom_hours: {
    value: 'custom_hours',
    label: 'Reset after a number of hours',
    cadenceLabel: 'Every {{hours}} hours',
  },
}

export const API_KEY_QUOTA_RESET_FORM_PERIOD_OPTIONS =
  API_KEY_QUOTA_RESET_FORM_PERIODS.map(
    (period) => API_KEY_QUOTA_RESET_PERIOD_OPTIONS[period]
  )

// ============================================================================
// Default Values
// ============================================================================

export const DEFAULT_GROUP = '' as const

// ============================================================================
// Error Messages (i18n keys: use t(ERROR_MESSAGES.xxx) when displaying)
// ============================================================================

export const ERROR_MESSAGES = {
  UNEXPECTED: 'An unexpected error occurred',
  LOAD_FAILED: 'Failed to load API keys',
  SEARCH_FAILED: 'Failed to search API keys',
  CREATE_FAILED: 'Failed to create API key',
  UPDATE_FAILED: 'Failed to update API key',
  DELETE_FAILED: 'Failed to delete API key',
  BATCH_DELETE_FAILED: 'Failed to delete API keys',
  STATUS_UPDATE_FAILED: 'Failed to update API key status',
} as const

// ============================================================================
// Success Messages (i18n keys: use t(SUCCESS_MESSAGES.xxx) when displaying)
// ============================================================================

export const SUCCESS_MESSAGES = {
  API_KEY_CREATED: 'API Key created successfully',
  API_KEY_UPDATED: 'API Key updated successfully',
  API_KEY_DELETED: 'API Key deleted successfully',
  API_KEY_ENABLED: 'API Key enabled successfully',
  API_KEY_DISABLED: 'API Key disabled successfully',
} as const
