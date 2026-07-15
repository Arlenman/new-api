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
export type UpstreamProvider = 'auto' | 'new-api' | 'sub2api' | 'other'
export type UpstreamAuthType = 'password' | 'access_token'
export type UpstreamChannelStatus = 'unconfigured' | 'ready' | 'error'
export type UpstreamErrorCode =
  | 'upstream_turnstile_requires_access_token'

export interface UpstreamAccount {
  id: number
  username: string
  email?: string
  role?: string
  group?: string
  balance: number
}

export interface UpstreamKey {
  id: number
  imported: boolean
  active: boolean
  name: string
  masked_key: string
  group?: string
  group_id?: number
  status: string
  quota?: number
  quota_used?: number
  remain_quota?: number
}

export interface UpstreamGroup {
  id?: number
  name: string
  description?: string
  platform?: string
  ratio: number
}

export interface UpstreamSnapshot {
  provider: Exclude<UpstreamProvider, 'auto'>
  balance: number
  account: UpstreamAccount
  keys: UpstreamKey[]
  groups: UpstreamGroup[]
  ratios: Record<string, number>
  retrieved_at: number
}

export interface UpstreamChannel {
  id: number
  name: string
  base_url: string
  provider: UpstreamProvider
  auth_type: UpstreamAuthType
  username: string
  note: string
  has_password: boolean
  source_channel_count: number
  active_source_channel_count: number
  balance: number
  balance_updated_time: number
  balance_threshold: number
  multiplier: number
  auto_refresh_interval: number
  last_sync_time: number
  last_error: string
  last_error_code?: UpstreamErrorCode
  status: UpstreamChannelStatus
  priority: number
  selected_group: string
  snapshot?: UpstreamSnapshot
}

export interface UpstreamChannelConfig {
  name: string
  provider: UpstreamProvider
  auth_type: UpstreamAuthType
  username: string
  password: string
  balance_threshold: number
  multiplier: number
  auto_refresh_interval: number
  priority: number
}

export interface CreateUpstreamChannelConfig extends UpstreamChannelConfig {
  base_url: string
}

export interface ApiResponse<T> {
  success: boolean
  message?: string
  error_code?: UpstreamErrorCode
  data?: T
}

export interface RefreshAllResult {
  refreshed: number
  errors: string[]
}

export interface UpstreamKeyImportConfiguration {
  groups: string[]
  tag: string
  name_prefix: string
  priority: number
  weight: number
  test_model: string
  models?: string[]
  auto_ban: 0 | 1
  remark: string
}

export interface ImportUpstreamKeysRequest extends UpstreamKeyImportConfiguration {
  key_ids: number[]
}

export interface ImportUpstreamKeysResult {
  imported: number
  updated: number
  skipped: number
  disabled: number
  channel_ids: number[]
}
