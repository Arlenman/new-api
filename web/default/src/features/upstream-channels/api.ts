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
import { api } from '@/lib/api'

import type {
  AlertRule,
  AlertRuleInput,
  AlertRulePreviewRequest,
  AlertRuleProviderCatalog,
  AlertRuleTestSendRequest,
  AlertRuleTestSendResult,
  ApiNoticeConfig,
  ApiNoticeConfigUpdate,
  ApiNoticeAPIKeyReveal,
  ApiNoticeConnectionStatus,
  ApiNoticeMessage,
  ApiResponse,
  CreateUpstreamChannelConfig,
  ImportUpstreamKeysRequest,
  ImportUpstreamKeysResult,
  LinkUpstreamKeysResult,
  RefreshAllResult,
  UpstreamChannel,
  UpstreamChannelConfig,
  UpstreamPrioritySchedule,
  UpdateUpstreamKeyGroupRequest,
} from './types'

export async function getManagedUpstreamChannels(): Promise<
  ApiResponse<UpstreamChannel[]>
> {
  const res = await api.get<ApiResponse<UpstreamChannel[]>>(
    '/api/upstream-channels/'
  )
  return res.data
}

export async function createManagedUpstreamChannel(
  config: CreateUpstreamChannelConfig
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.post<ApiResponse<UpstreamChannel>>(
    '/api/upstream-channels/',
    config,
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamChannel(
  id: number,
  config: UpstreamChannelConfig
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.put<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}`,
    config,
    { skipBusinessError: true }
  )
  return res.data
}

export async function deleteManagedUpstreamChannel(
  id: number
): Promise<ApiResponse<null>> {
  const res = await api.delete<ApiResponse<null>>(
    `/api/upstream-channels/${id}`,
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamChannelNote(
  id: number,
  note: string
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.patch<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/note`,
    { note },
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamChannelSelectedGroup(
  id: number,
  selectedGroup: string
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.patch<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/selected-group`,
    { selected_group: selectedGroup },
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamChannelDefaultTestModel(
  id: number,
  defaultTestModel: string
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.patch<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/default-test-model`,
    { default_test_model: defaultTestModel },
    { skipBusinessError: true }
  )
  return res.data
}

export async function getManagedUpstreamPrioritySchedule(): Promise<
  ApiResponse<UpstreamPrioritySchedule>
> {
  const res = await api.get<ApiResponse<UpstreamPrioritySchedule>>(
    '/api/upstream-channels/priority-schedule',
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamPrioritySchedule(
  schedule: UpstreamPrioritySchedule
): Promise<ApiResponse<UpstreamPrioritySchedule>> {
  const res = await api.put<ApiResponse<UpstreamPrioritySchedule>>(
    '/api/upstream-channels/priority-schedule',
    schedule,
    { skipBusinessError: true }
  )
  return res.data
}

export async function refreshManagedUpstreamChannel(
  id: number
): Promise<ApiResponse<UpstreamChannel>> {
  return refreshManagedUpstreamChannelBalance(id)
}

export async function refreshManagedUpstreamChannelBalance(
  id: number
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.post<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/refresh-balance`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function refreshManagedUpstreamChannelKeys(
  id: number
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.post<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/refresh-keys`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function refreshManagedUpstreamChannelGroups(
  id: number
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.post<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/refresh-groups`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function pinManagedUpstreamChannel(
  id: number
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.post<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${id}/pin`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function refreshAllManagedUpstreamChannels(): Promise<
  ApiResponse<RefreshAllResult>
> {
  const res = await api.post<ApiResponse<RefreshAllResult>>(
    '/api/upstream-channels/refresh',
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function linkManagedUpstreamKeys(
  channelId: number
): Promise<ApiResponse<LinkUpstreamKeysResult>> {
  const res = await api.post<ApiResponse<LinkUpstreamKeysResult>>(
    `/api/upstream-channels/${channelId}/keys/link`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateManagedUpstreamKeyGroup(
  channelId: number,
  keyId: number,
  request: UpdateUpstreamKeyGroupRequest
): Promise<ApiResponse<UpstreamChannel>> {
  const res = await api.patch<ApiResponse<UpstreamChannel>>(
    `/api/upstream-channels/${channelId}/keys/${keyId}/group`,
    request,
    { skipBusinessError: true }
  )
  return res.data
}

export async function revealManagedUpstreamKey(
  channelId: number,
  keyId: number
): Promise<ApiResponse<{ key: string }>> {
  const res = await api.post<ApiResponse<{ key: string }>>(
    `/api/upstream-channels/${channelId}/keys/${keyId}`,
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function fetchManagedUpstreamKeyModels(
  channelId: number,
  keyIds: number[]
): Promise<ApiResponse<string[]>> {
  const res = await api.post<ApiResponse<string[]>>(
    `/api/upstream-channels/${channelId}/keys/models`,
    { key_ids: keyIds },
    { skipBusinessError: true }
  )
  return res.data
}

export async function importManagedUpstreamKeys(
  channelId: number,
  request: ImportUpstreamKeysRequest
): Promise<ApiResponse<ImportUpstreamKeysResult>> {
  const res = await api.post<ApiResponse<ImportUpstreamKeysResult>>(
    `/api/upstream-channels/${channelId}/keys/import`,
    request,
    { skipBusinessError: true }
  )
  return res.data
}

export async function getAlertRules(): Promise<ApiResponse<AlertRule[]>> {
  const res = await api.get<ApiResponse<AlertRule[]>>('/api/alert-rules/', {
    skipBusinessError: true,
  })
  return res.data
}

export async function getAlertRuleProviders(): Promise<
  ApiResponse<AlertRuleProviderCatalog>
> {
  const res = await api.get<ApiResponse<AlertRuleProviderCatalog>>(
    '/api/alert-rules/providers',
    { skipBusinessError: true }
  )
  return res.data
}

export async function getApiNoticeConfig(): Promise<
  ApiResponse<ApiNoticeConfig>
> {
  const res = await api.get<ApiResponse<ApiNoticeConfig>>(
    '/api/alert-rules/config',
    { skipBusinessError: true }
  )
  return res.data
}

export async function revealApiNoticeAPIKey(): Promise<
  ApiResponse<ApiNoticeAPIKeyReveal>
> {
  const res = await api.post<ApiResponse<ApiNoticeAPIKeyReveal>>(
    '/api/alert-rules/config/reveal',
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateApiNoticeConfig(
  config: ApiNoticeConfigUpdate
): Promise<ApiResponse<ApiNoticeConfig>> {
  const res = await api.put<ApiResponse<ApiNoticeConfig>>(
    '/api/alert-rules/config',
    config,
    { skipBusinessError: true }
  )
  return res.data
}

export async function createAlertRule(
  rule: AlertRuleInput
): Promise<ApiResponse<AlertRule>> {
  const res = await api.post<ApiResponse<AlertRule>>(
    '/api/alert-rules/',
    rule,
    { skipBusinessError: true }
  )
  return res.data
}

export async function updateAlertRule(
  id: number,
  rule: AlertRuleInput
): Promise<ApiResponse<AlertRule>> {
  const res = await api.put<ApiResponse<AlertRule>>(
    `/api/alert-rules/${id}`,
    rule,
    { skipBusinessError: true }
  )
  return res.data
}

export async function deleteAlertRule(id: number): Promise<ApiResponse<null>> {
  const res = await api.delete<ApiResponse<null>>(`/api/alert-rules/${id}`, {
    skipBusinessError: true,
  })
  return res.data
}

export async function previewAlertRule(
  request: AlertRulePreviewRequest
): Promise<ApiResponse<ApiNoticeMessage>> {
  const res = await api.post<ApiResponse<ApiNoticeMessage>>(
    '/api/alert-rules/preview',
    request,
    { skipBusinessError: true }
  )
  return res.data
}

export async function testAlertRuleConnection(): Promise<
  ApiResponse<ApiNoticeConnectionStatus>
> {
  const res = await api.post<ApiResponse<ApiNoticeConnectionStatus>>(
    '/api/alert-rules/test-connection',
    undefined,
    { skipBusinessError: true }
  )
  return res.data
}

export async function testSendAlertRule(
  request: AlertRuleTestSendRequest
): Promise<ApiResponse<AlertRuleTestSendResult>> {
  const res = await api.post<ApiResponse<AlertRuleTestSendResult>>(
    '/api/alert-rules/test-send',
    request,
    { skipBusinessError: true }
  )
  return res.data
}
