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
import type { UserToolTokenOption } from '@/features/user-tools/api'

interface RememberedTokenSelection {
  userId: number
  tokenId: number
}

interface ApiKeyDisplaySource {
  name?: string | null
  group?: string | null
  display_label?: string | null
}

export interface ApiKeyOption {
  label: string
  value: string
  disabled: boolean
}

export interface ApiKeySwitchTarget {
  tokenId: number
  revision: number
}

export const INFINITE_CANVAS_TOKEN_STORAGE_KEY =
  'new-api:infinite-canvas:token-selection'

export function getApiKeyDisplayLabel(
  source: ApiKeyDisplaySource,
  unnamedApiKeyLabel: string
): string {
  const authoritativeLabel = source.display_label?.trim()
  if (authoritativeLabel) return authoritativeLabel

  const name = source.name?.trim() || unnamedApiKeyLabel
  const group = source.group?.trim()
  return group ? `${name} · ${group}` : name
}

export function isApiKeyAvailable(apiKey: UserToolTokenOption): boolean {
  return apiKey.available
}

export function createApiKeyOptions(
  apiKeys: UserToolTokenOption[],
  unnamedApiKeyLabel: string
): ApiKeyOption[] {
  return apiKeys.map((apiKey) => ({
    label: getApiKeyDisplayLabel(apiKey, unnamedApiKeyLabel),
    value: String(apiKey.id),
    disabled: !isApiKeyAvailable(apiKey),
  }))
}

export function isApiKeySelectionAvailable(
  apiKeys: UserToolTokenOption[],
  tokenId: number
): boolean {
  const selectedApiKey = apiKeys.find((apiKey) => apiKey.id === tokenId)
  return selectedApiKey ? isApiKeyAvailable(selectedApiKey) : false
}

export function createApiKeySwitchTarget(
  apiKeys: UserToolTokenOption[],
  value: string | null,
  currentTokenId: number | null,
  currentRevision: number
): ApiKeySwitchTarget | null {
  if (!value) return null

  const tokenId = Number(value)
  if (
    !Number.isInteger(tokenId) ||
    tokenId <= 0 ||
    tokenId === currentTokenId ||
    !isApiKeySelectionAvailable(apiKeys, tokenId)
  ) {
    return null
  }

  return {
    tokenId,
    revision: currentRevision + 1,
  }
}

export function selectPreferredApiKey(
  apiKeys: UserToolTokenOption[],
  rememberedTokenId: number | null
): UserToolTokenOption | null {
  const availableKeys = apiKeys.filter(isApiKeyAvailable)
  if (rememberedTokenId !== null) {
    const rememberedKey = availableKeys.find(
      (apiKey) => apiKey.id === rememberedTokenId
    )
    if (rememberedKey) return rememberedKey
  }

  return (
    availableKeys.toSorted(
      (left, right) =>
        right.created_time - left.created_time || right.id - left.id
    )[0] ?? null
  )
}

export function serializeRememberedTokenSelection(
  userId: number,
  tokenId: number
): string {
  return JSON.stringify({ userId, tokenId } satisfies RememberedTokenSelection)
}

export function parseRememberedTokenSelection(
  value: string | null,
  userId: number
): number | null {
  if (!value) return null

  try {
    const parsed = JSON.parse(value) as Partial<RememberedTokenSelection>
    if (parsed.userId !== userId) return null
    if (!Number.isInteger(parsed.tokenId) || Number(parsed.tokenId) <= 0) {
      return null
    }
    return Number(parsed.tokenId)
  } catch {
    return null
  }
}
