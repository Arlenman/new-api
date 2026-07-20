/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
const PARENT_SOURCE = 'new-api' as const
const CHILD_SOURCE = 'infinite-canvas' as const

export type InfiniteCanvasMode = 'new-api'

export interface InfiniteCanvasProbeMessage {
  source: typeof PARENT_SOURCE
  type: 'new-api:infinite-canvas:probe'
}

export interface InfiniteCanvasConfigureMessage {
  source: typeof PARENT_SOURCE
  type: 'new-api:infinite-canvas:configure'
  mode: InfiniteCanvasMode
  apiUrl: string
  imageApiUrl: string
  mediaApiUrl: string
  apiKey: string
  profileName?: string
}

export interface InfiniteCanvasChildMessage {
  source: typeof CHILD_SOURCE
  type: 'new-api:infinite-canvas:ready' | 'new-api:infinite-canvas:configured'
  mode?: InfiniteCanvasMode
}

interface BridgeEventLike {
  origin: string
  source: unknown
  data: unknown
}

function normalizeNewApiOrigin(value: string): string {
  const url = new URL(value)
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('New API origin must use HTTP or HTTPS')
  }
  return url.origin
}

export function createProbeMessage(): InfiniteCanvasProbeMessage {
  return {
    source: PARENT_SOURCE,
    type: 'new-api:infinite-canvas:probe',
  }
}

export function createNewApiConfigureMessage(
  origin: string,
  apiKey: string,
  apiKeyName?: string
): InfiniteCanvasConfigureMessage {
  const normalizedOrigin = normalizeNewApiOrigin(origin)
  const apiUrl = `${normalizedOrigin}/pg`
  const normalizedApiKeyName = apiKeyName?.trim()
  return {
    source: PARENT_SOURCE,
    type: 'new-api:infinite-canvas:configure',
    mode: 'new-api',
    apiUrl,
    imageApiUrl: apiUrl,
    mediaApiUrl: `${normalizedOrigin}/v1`,
    apiKey: apiKey.trim(),
    profileName: normalizedApiKeyName
      ? `New API · ${normalizedApiKeyName}`
      : 'New API',
  }
}

export function isTrustedInfiniteCanvasMessage(
  event: BridgeEventLike,
  expectedSource: unknown,
  expectedOrigin: string
): event is BridgeEventLike & { data: InfiniteCanvasChildMessage } {
  if (event.origin !== expectedOrigin || event.source !== expectedSource) {
    return false
  }
  if (!event.data || typeof event.data !== 'object') return false

  const message = event.data as Record<string, unknown>
  if (message.source !== CHILD_SOURCE) return false
  if (message.type === 'new-api:infinite-canvas:ready') return true
  if (message.type !== 'new-api:infinite-canvas:configured') return false
  return message.mode === 'new-api'
}
