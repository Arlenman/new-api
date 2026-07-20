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
const PARENT_SOURCE = 'new-api' as const
const CHILD_SOURCE = 'gpt-image-playground' as const

export type ImagePlaygroundMode = 'new-api' | 'tool'

export interface ImagePlaygroundProbeMessage {
  source: typeof PARENT_SOURCE
  type: 'new-api:image-playground:probe'
}

export interface ImagePlaygroundConfigureMessage {
  source: typeof PARENT_SOURCE
  type: 'new-api:image-playground:configure'
  mode: ImagePlaygroundMode
  apiUrl?: string
  apiKey?: string
  apiMode?: 'images'
  profileName?: string
}

export interface ImagePlaygroundChildMessage {
  source: typeof CHILD_SOURCE
  type: 'new-api:image-playground:ready' | 'new-api:image-playground:configured'
  mode?: ImagePlaygroundMode
}

interface BridgeEventLike {
  origin: string
  source: unknown
  data: unknown
}

export function createProbeMessage(): ImagePlaygroundProbeMessage {
  return {
    source: PARENT_SOURCE,
    type: 'new-api:image-playground:probe',
  }
}

export function createNewApiConfigureMessage(
  origin: string,
  apiKey: string,
  apiKeyDisplayLabel?: string
): ImagePlaygroundConfigureMessage {
  const apiUrl = `${new URL(origin).origin}/pg`
  const normalizedApiKey = apiKey.trim()
  if (!normalizedApiKey.startsWith('utrs_')) {
    throw new Error('New API image playground requires a runtime credential')
  }
  const normalizedApiKeyDisplayLabel = apiKeyDisplayLabel?.trim()
  return {
    source: PARENT_SOURCE,
    type: 'new-api:image-playground:configure',
    mode: 'new-api',
    apiUrl,
    apiKey: normalizedApiKey,
    apiMode: 'images',
    profileName: normalizedApiKeyDisplayLabel
      ? `New API · ${normalizedApiKeyDisplayLabel}`
      : 'New API',
  }
}

export function createToolConfigureMessage(
  apiUrl: string,
  apiKey: string
): ImagePlaygroundConfigureMessage {
  return {
    source: PARENT_SOURCE,
    type: 'new-api:image-playground:configure',
    mode: 'tool',
    apiUrl: apiUrl.trim().replace(/\/+$/, ''),
    apiKey: apiKey.trim(),
    apiMode: 'images',
    profileName: 'Custom API',
  }
}

export function isTrustedImagePlaygroundMessage(
  event: BridgeEventLike,
  expectedSource: unknown,
  expectedOrigin: string
): event is BridgeEventLike & { data: ImagePlaygroundChildMessage } {
  if (event.origin !== expectedOrigin || event.source !== expectedSource) {
    return false
  }
  if (!event.data || typeof event.data !== 'object') return false

  const message = event.data as Record<string, unknown>
  if (message.source !== CHILD_SOURCE) return false
  if (message.type === 'new-api:image-playground:ready') return true
  if (message.type !== 'new-api:image-playground:configured') return false
  return message.mode === 'new-api' || message.mode === 'tool'
}
