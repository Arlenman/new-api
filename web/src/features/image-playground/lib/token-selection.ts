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

export interface ApiKeySelectionOption {
  label: string
  value: string
  available: boolean
}

export const IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY =
  'new-api:image-playground:token-selection'

export function getApiKeyDisplayLabel(
  source: ApiKeyDisplaySource,
  unnamedLabel: string
): string {
  const authoritativeLabel = source.display_label?.trim()
  if (authoritativeLabel) return authoritativeLabel

  const name = source.name?.trim() || unnamedLabel
  const group = source.group?.trim()
  return group ? `${name} · ${group}` : name
}

export function isApiKeyAvailable(apiKey: UserToolTokenOption): boolean {
  return apiKey.available
}

export function getApiKeySelectionOptions(
  apiKeys: UserToolTokenOption[],
  unnamedLabel: string
): ApiKeySelectionOption[] {
  return apiKeys.map((apiKey) => ({
    label: getApiKeyDisplayLabel(apiKey, unnamedLabel),
    value: String(apiKey.id),
    available: isApiKeyAvailable(apiKey),
  }))
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
