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
export type UpstreamProvider = "auto" | "new-api" | "sub2api" | "other";
export type UpstreamAuthType = "password" | "access_token";
export type UpstreamChannelStatus = "unconfigured" | "ready" | "error";
export type UpstreamErrorCode = "upstream_turnstile_requires_access_token";
export type UpstreamKeyInUseStatus =
  | "unlinked"
  | "enabled"
  | "disabled"
  | "auto_disabled";

export interface UpstreamAccount {
  id: number;
  username: string;
  email?: string;
  role?: string;
  group?: string;
  balance: number;
}

export interface UpstreamKey {
  id: number;
  imported: boolean;
  active: boolean;
  linked?: boolean;
  in_use_status?: UpstreamKeyInUseStatus;
  key_fingerprint?: string;
  name: string;
  masked_key: string;
  group?: string;
  group_id?: number;
  status: string;
  quota?: number;
  quota_used?: number;
  remain_quota?: number;
}

export interface UpstreamGroup {
  id?: number;
  name: string;
  description?: string;
  platform?: string;
  ratio: number;
}

export interface UpstreamModelPricingInterval {
  min_tokens: number;
  max_tokens?: number;
  tier_label?: string;
  input_price?: number;
  output_price?: number;
  cache_write_price?: number;
  cache_read_price?: number;
  per_request_price?: number;
}

export interface UpstreamModelPricing {
  source: "new-api" | "sub2api";
  channel_name?: string;
  platform?: string;
  billing_mode?: string;
  model_ratio?: number;
  completion_ratio?: number;
  cache_ratio?: number;
  create_cache_ratio?: number;
  model_price?: number;
  input_price?: number;
  output_price?: number;
  cache_write_price?: number;
  cache_read_price?: number;
  image_input_price?: number;
  image_output_price?: number;
  per_request_price?: number;
  intervals?: UpstreamModelPricingInterval[];
}

export interface UpstreamModel {
  id: string;
  pricing: UpstreamModelPricing[];
}

export interface UpstreamSnapshot {
  provider: Exclude<UpstreamProvider, "auto">;
  balance: number;
  account: UpstreamAccount;
  keys: UpstreamKey[];
  groups: UpstreamGroup[];
  ratios: Record<string, number>;
  models?: UpstreamModel[];
  retrieved_at: number;
}

export interface UpstreamChannel {
  id: number;
  name: string;
  base_url: string;
  provider: UpstreamProvider;
  auth_type: UpstreamAuthType;
  username: string;
  note: string;
  has_password: boolean;
  source_channel_count: number;
  active_source_channel_count: number;
  in_use_key_count: number;
  balance: number;
  availability_24h: number | null;
  average_first_token_latency_ms: number | null;
  balance_updated_time: number;
  balance_threshold: number;
  multiplier: number;
  auto_refresh_interval: number;
  last_sync_time: number;
  last_error: string;
  last_error_code?: UpstreamErrorCode;
  status: UpstreamChannelStatus;
  priority: number;
  selected_group: string;
  default_test_model: string;
  snapshot?: UpstreamSnapshot;
}

export interface UpstreamChannelConfig {
  name: string;
  provider: UpstreamProvider;
  auth_type: UpstreamAuthType;
  username: string;
  password: string;
  balance_threshold: number;
  multiplier: number;
  auto_refresh_interval: number;
  priority: number;
}

export interface CreateUpstreamChannelConfig extends UpstreamChannelConfig {
  base_url: string;
}

export interface ApiResponse<T> {
  success: boolean;
  message?: string;
  error_code?: UpstreamErrorCode;
  data?: T;
}

export interface RefreshAllResult {
  refreshed: number;
  errors: string[];
}

export type UpdateUpstreamKeyGroupRequest =
  | { group: string; group_id?: never }
  | { group?: never; group_id: number };

export interface LinkUpstreamKeysSummary {
  total: number;
  linked: number;
  enabled: number;
  auto_disabled: number;
  disabled: number;
  unlinked: number;
}

export interface LinkUpstreamKeysResult {
  channel: UpstreamChannel;
  summary: LinkUpstreamKeysSummary;
}

export interface UpstreamPrioritySchedule {
  enabled: boolean;
  interval_seconds: number;
  max_test_latency_seconds: number;
}

export type UpstreamPriorityTaskStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed";

export type UpstreamPriorityTaskTrigger = "scheduled" | "manual";

export interface UpstreamPriorityTaskIssue {
  channel_id?: number;
  channel_name?: string;
  provider?: string;
  host?: string;
  stage?: string;
  http_status?: number;
  message?: string;
}

export type UpstreamPriorityTaskActionKind =
  | "refreshed"
  | "ranked"
  | "tested"
  | "priority_updated"
  | "skipped";

export interface UpstreamPriorityTaskAction {
  kind: UpstreamPriorityTaskActionKind;
  channel_id?: number;
  channel_name?: string;
  provider?: string;
  host?: string;
  target_channel_id?: number;
  target_channel_name?: string;
  target_channel_provider?: string;
  target_channel_host?: string;
  model?: string;
  effective_ratio?: string;
  old_priority?: number;
  new_priority?: number;
  latency_ms?: number;
  passed?: boolean;
  message?: string;
}

export interface UpstreamPriorityTaskResult {
  refreshed?: number;
  ranked?: number;
  tested?: number;
  passed?: number;
  priority_updated?: number;
  skipped?: number;
  issues?: UpstreamPriorityTaskIssue[];
  errors?: Array<string | UpstreamPriorityTaskIssue>;
  actions?: UpstreamPriorityTaskAction[];
}

export interface UpstreamPriorityTaskRecord {
  task_id: string;
  type: string;
  status: UpstreamPriorityTaskStatus;
  trigger: UpstreamPriorityTaskTrigger;
  created_at: number;
  started_at?: number | null;
  completed_at?: number | null;
  duration_ms?: number | null;
  result?: UpstreamPriorityTaskResult | null;
  error?: string;
}

export interface UpstreamPriorityTaskPage {
  items: UpstreamPriorityTaskRecord[];
  page: number;
  page_size: number;
  total: number;
}

export interface ClearUpstreamPriorityTasksResult {
  deleted_count: number;
}

export interface UpstreamKeyImportConfiguration {
  groups: string[];
  tag: string;
  name_prefix: string;
  priority: number;
  weight: number;
  test_model: string;
  models?: string[];
  auto_ban: 0 | 1;
  remark: string;
}

export interface ImportUpstreamKeysRequest extends UpstreamKeyImportConfiguration {
  key_ids: number[];
}

export interface ImportUpstreamKeysResult {
  imported: number;
  updated: number;
  skipped: number;
  disabled: number;
  channel_ids: number[];
}

export type AlertMessageFormat = "text" | "markdown" | "card" | "table";
export type AlertEventType = "trigger" | "recovery";
export type AlertComparisonOperator = "lt" | "lte" | "gt" | "gte" | "eq";
export type AlertRuleStateName = "normal" | "pending" | "active";
export type AlertRuleTriggerType =
  | "upstream_channel_effective_balance"
  | "enabled_channel_count";

export interface ApiNoticeProvider {
  name: string;
  default: boolean;
  capabilities: string[];
  ready: boolean;
  reason?: string;
}

export interface AlertRuleProviderCatalog {
  providers: ApiNoticeProvider[];
  api_key_configured: boolean;
}

export interface ApiNoticeConfig {
  base_url: string;
  api_key_configured: boolean;
  api_key_masked: string;
  api_key_source: string;
  persistent_storage_available: boolean;
}

export interface ApiNoticeConfigUpdate {
  base_url: string;
  api_key?: string;
  clear_api_key?: boolean;
}

export interface ApiNoticeAPIKeyReveal {
  api_key: string;
  api_key_source: string;
}

export interface ApiNoticeEndpointStatus {
  status: string;
  http_status: number;
}

export interface ApiNoticeConnectionStatus {
  health: ApiNoticeEndpointStatus;
  ready: ApiNoticeEndpointStatus;
  providers: ApiNoticeProvider[];
  api_key_configured: boolean;
}

export interface ApiNoticeField {
  name: string;
  value: string;
}

export interface ApiNoticeSection {
  title?: string;
  text: string;
}

export interface ApiNoticeAction {
  label: string;
  url: string;
}

export interface ApiNoticeColumn {
  key: string;
  label: string;
}

export interface ApiNoticeTable {
  columns: ApiNoticeColumn[];
  rows: Array<Record<string, string>>;
}

export interface ApiNoticeMessage {
  format: AlertMessageFormat;
  title?: string;
  level?: string;
  text?: string;
  fields?: ApiNoticeField[];
  sections?: ApiNoticeSection[];
  actions?: ApiNoticeAction[];
  table?: ApiNoticeTable;
}

export interface AlertRuleTriggerConfig {
  operator: AlertComparisonOperator;
  threshold: number;
  window_seconds: number;
}

export interface AlertRuleInput {
  name: string;
  enabled: boolean;
  trigger_type: AlertRuleTriggerType;
  trigger_config: AlertRuleTriggerConfig;
  providers: string[];
  message_format: AlertMessageFormat;
  message_template: ApiNoticeMessage;
  consecutive_required: number;
  cooldown_seconds: number;
  send_recovery: boolean;
}

export interface AlertRuleStateSummary {
  state: AlertRuleStateName;
  active_subjects: number;
  pending_subjects: number;
  last_triggered_at: number;
  last_recovered_at: number;
  last_error_summary: string;
}

export interface AlertRule extends AlertRuleInput {
  id: number;
  revision: number;
  created_at: number;
  updated_at: number;
  state: AlertRuleStateSummary;
}

export interface AlertRulePreviewRequest {
  rule: AlertRuleInput;
  event_type: AlertEventType;
  channel_id: number;
}

export interface AlertRuleTestSendRequest extends AlertRulePreviewRequest {
  providers: string[];
}

export interface AlertRuleTestProviderResult {
  provider: string;
  accepted: boolean;
  attempts: number;
  error?: string;
}

export interface AlertRuleTestSendResult {
  http_status: number;
  results: AlertRuleTestProviderResult[];
  error?: string;
}
