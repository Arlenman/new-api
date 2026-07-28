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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import { DEFAULT_GROUP } from '../constants'
import {
  API_KEY_QUOTA_RESET_FORM_PERIODS,
  type ApiKeyFormData,
  type ApiKey,
} from '../types'
import { normalizeApiKeyTags } from './api-key-tags'

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(t: TFunction) {
  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().optional(),
      unlimited_quota: z.boolean(),
      quota_reset_enabled: z.boolean(),
      quota_reset_period: z.enum(API_KEY_QUOTA_RESET_FORM_PERIODS),
      quota_reset_interval_hours: z.number().optional(),
      quota_reset_amount_dollars: z.number().optional(),
      quota_reset_carry_over: z.boolean(),
      model_limits: z.array(z.string()),
      allow_ips: z.string().optional(),
      group: z.string().optional(),
      cross_group_retry: z.boolean().optional(),
      tags: z.array(z.string()).optional(),
      tokenCount: z.number().min(1).optional(),
    })
    .superRefine((data, ctx) => {
      if (
        !data.unlimited_quota &&
        (data.remain_quota_dollars === undefined ||
          data.remain_quota_dollars < 0)
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['remain_quota_dollars'],
          message: t('Quota must be zero or greater'),
        })
      }

      if (
        data.quota_reset_enabled &&
        (data.quota_reset_amount_dollars === undefined ||
          data.quota_reset_amount_dollars <= 0)
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['quota_reset_amount_dollars'],
          message: t('Periodic quota must be greater than zero'),
        })
      }

      if (
        data.quota_reset_enabled &&
        data.quota_reset_period === 'custom_hours' &&
        (data.quota_reset_interval_hours === undefined ||
          data.quota_reset_interval_hours <= 0 ||
          !Number.isInteger(data.quota_reset_interval_hours))
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['quota_reset_interval_hours'],
          message: t('Reset interval must be a positive whole number'),
        })
      }
    })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: undefined,
  unlimited_quota: true,
  quota_reset_enabled: false,
  quota_reset_period: 'daily',
  quota_reset_interval_hours: 24,
  quota_reset_amount_dollars: 10,
  quota_reset_carry_over: true,
  model_limits: [],
  allow_ips: '',
  group: DEFAULT_GROUP,
  cross_group_retry: true,
  tags: [],
  tokenCount: 1,
}

export function getApiKeyFormDefaultValues(
  defaultUseAutoGroup: boolean
): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
    group: defaultUseAutoGroup ? 'auto' : DEFAULT_GROUP,
    cross_group_retry: defaultUseAutoGroup,
  }
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: ApiKeyFormValues
): ApiKeyFormData {
  return {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    quota_reset_enabled: data.quota_reset_enabled,
    quota_reset_period: data.quota_reset_period,
    quota_reset_interval_hours:
      data.quota_reset_period === 'custom_hours'
        ? (data.quota_reset_interval_hours ?? 0)
        : 0,
    quota_reset_amount: parseQuotaFromDollars(
      data.quota_reset_amount_dollars ?? 0
    ),
    quota_reset_carry_over: data.quota_reset_carry_over,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    allow_ips: data.allow_ips || '',
    group: data.group || '',
    cross_group_retry: data.group === 'auto' ? !!data.cross_group_retry : false,
    tags: normalizeApiKeyTags(data.tags),
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey
): ApiKeyFormValues {
  const defaultQuotaResetIntervalHours =
    API_KEY_FORM_DEFAULT_VALUES.quota_reset_interval_hours ?? 24
  let quotaResetIntervalHours =
    apiKey.quota_reset_interval_hours ?? defaultQuotaResetIntervalHours
  if (apiKey.quota_reset_period === 'hourly') {
    quotaResetIntervalHours = 1
  } else if (quotaResetIntervalHours <= 0) {
    quotaResetIntervalHours = defaultQuotaResetIntervalHours
  }

  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    unlimited_quota: apiKey.unlimited_quota,
    quota_reset_enabled: apiKey.quota_reset_enabled,
    quota_reset_period:
      apiKey.quota_reset_period === 'hourly'
        ? 'custom_hours'
        : apiKey.quota_reset_period || 'daily',
    quota_reset_interval_hours: quotaResetIntervalHours,
    quota_reset_amount_dollars:
      apiKey.quota_reset_amount > 0
        ? quotaUnitsToDollars(apiKey.quota_reset_amount)
        : API_KEY_FORM_DEFAULT_VALUES.quota_reset_amount_dollars,
    quota_reset_carry_over:
      apiKey.quota_reset_amount > 0 ? apiKey.quota_reset_carry_over : true,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    allow_ips: apiKey.allow_ips || '',
    group: apiKey.group || DEFAULT_GROUP,
    cross_group_retry: !!apiKey.cross_group_retry,
    tags: normalizeApiKeyTags(apiKey.tags),
    tokenCount: 1,
  }
}
