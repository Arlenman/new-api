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
import type { ApiKey } from '@/features/keys/types'

interface RememberedTokenSelection {
  userId: number
  tokenId: number
}

export const IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY =
  'new-api:image-playground:token-selection'

export function isApiKeyAvailable(apiKey: ApiKey, now: number): boolean {
  if (apiKey.status !== 1) return false
  if (apiKey.expired_time !== -1 && apiKey.expired_time <= now) return false
  return apiKey.unlimited_quota || apiKey.remain_quota > 0
}

export function selectPreferredApiKey(
  apiKeys: ApiKey[],
  rememberedTokenId: number | null,
  now: number
): ApiKey | null {
  const availableKeys = apiKeys.filter((apiKey) =>
    isApiKeyAvailable(apiKey, now)
  )
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
