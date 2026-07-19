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
import type { ImagePlaygroundMode } from './bridge'

interface StoredImagePlaygroundConfiguration {
  userId: number
  mode: ImagePlaygroundMode
  customApiUrl: string
  customApiKey: string
}

export interface RememberedImagePlaygroundConfiguration {
  mode: ImagePlaygroundMode
  customApiUrl: string
  customApiKey: string
}

export const IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY =
  'new-api:image-playground:configuration'

export function normalizeImagePlaygroundApiUrl(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return null

  try {
    const url = new URL(trimmed)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    return url.toString().replace(/\/+$/, '')
  } catch {
    return null
  }
}

export function serializeImagePlaygroundConfiguration(
  userId: number,
  configuration: RememberedImagePlaygroundConfiguration
): string {
  return JSON.stringify({
    userId,
    mode: configuration.mode,
    customApiUrl: configuration.customApiUrl.trim(),
    customApiKey: configuration.customApiKey.trim(),
  } satisfies StoredImagePlaygroundConfiguration)
}

export function parseImagePlaygroundConfiguration(
  value: string | null,
  userId: number
): RememberedImagePlaygroundConfiguration | null {
  if (!value) return null

  try {
    const parsed = JSON.parse(
      value
    ) as Partial<StoredImagePlaygroundConfiguration>
    if (parsed.userId !== userId) return null
    if (parsed.mode !== 'new-api' && parsed.mode !== 'tool') return null
    if (
      typeof parsed.customApiUrl !== 'string' ||
      typeof parsed.customApiKey !== 'string'
    ) {
      return null
    }
    return {
      mode: parsed.mode,
      customApiUrl: parsed.customApiUrl,
      customApiKey: parsed.customApiKey,
    }
  } catch {
    return null
  }
}
