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
import { z } from 'zod'

// ============================================================================
// API Key Schema & Types
// ============================================================================

export const API_KEY_QUOTA_RESET_PERIODS = [
  'hourly',
  'daily',
  'weekly',
  'monthly',
  'custom_hours',
] as const

export type ApiKeyQuotaResetPeriod =
  (typeof API_KEY_QUOTA_RESET_PERIODS)[number]

export const API_KEY_QUOTA_RESET_FORM_PERIODS = [
  'daily',
  'weekly',
  'monthly',
  'custom_hours',
] as const

export type ApiKeyQuotaResetFormPeriod =
  (typeof API_KEY_QUOTA_RESET_FORM_PERIODS)[number]

export const apiKeyIPSchema = z.object({
  ip: z.string(),
  country_code: z.string().optional(),
  region: z.string().optional(),
  city: z.string().optional(),
  private: z.boolean().optional(),
})

export type ApiKeyIP = z.infer<typeof apiKeyIPSchema>

export const apiKeySchema = z.object({
  id: z.number(),
  name: z.string(),
  key: z.string(),
  status: z.number(), // 1: enabled, 2: disabled, 3: expired, 4: exhausted
  remain_quota: z.number(),
  used_quota: z.number(),
  unlimited_quota: z.boolean(),
  expired_time: z.number(), // -1 for never expires
  created_time: z.number(),
  accessed_time: z.number(),
  group: z.string().nullish().default(''),
  cross_group_retry: z
    .preprocess((v) => {
      if (v === 1) return true
      if (v === 0) return false
      return v
    }, z.boolean())
    .optional()
    .default(false),
  model_limits_enabled: z.boolean(),
  model_limits: z.string().nullish().default(''),
  allow_ips: z.string().nullish().default(''),
  tags: z.array(z.string()).optional().default([]),
  ips: z.array(apiKeyIPSchema).optional(),
  quota_reset_enabled: z.boolean().optional().default(false),
  quota_reset_period: z
    .preprocess(
      (value) => (value === '' || value == null ? 'daily' : value),
      z.enum(API_KEY_QUOTA_RESET_PERIODS)
    )
    .optional()
    .default('daily'),
  quota_reset_interval_hours: z.number().optional(),
  quota_reset_amount: z.number().optional().default(0),
  quota_reset_remaining: z.number().optional().default(0),
  quota_reset_carry_over: z.boolean().optional().default(false),
  quota_reset_last_time: z.number().optional().default(0),
  quota_reset_next_time: z.number().optional().default(0),
})

export type ApiKey = z.infer<typeof apiKeySchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetApiKeysParams {
  p?: number
  size?: number
}

export interface GetApiKeysResponse {
  success: boolean
  message?: string
  data?: {
    items: ApiKey[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchApiKeysParams {
  keyword?: string
  token?: string
  p?: number
  size?: number
}

export interface ApiKeyFormData {
  name: string
  remain_quota: number
  expired_time: number
  unlimited_quota: boolean
  model_limits_enabled: boolean
  model_limits: string
  allow_ips: string
  group: string
  cross_group_retry: boolean
  tags: string[]
  quota_reset_enabled: boolean
  quota_reset_period: ApiKeyQuotaResetFormPeriod
  quota_reset_interval_hours: number
  quota_reset_amount: number
  quota_reset_carry_over: boolean
}

export interface ApiKeyTag {
  id: number
  user_id: number
  name: string
  created_at?: number
  updated_at?: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type ApiKeysDialogType =
  | 'create'
  | 'update'
  | 'delete'
  | 'batch-delete'
  | 'cc-switch'

export interface FetchApiKeyIPLocationsItem {
  token_id: number
  ip: string
}

export interface ApiKeyIPLocationResult extends FetchApiKeyIPLocationsItem {
  country_code?: string
  region?: string
  city?: string
  private?: boolean
  success: boolean
  message?: string
}
