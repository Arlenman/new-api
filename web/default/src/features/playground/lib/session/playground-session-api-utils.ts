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
import type { PlaygroundSession, Message } from '../../types.ts'
import {
  isLegacyImageGeneration524ErrorMessage,
  normalizeImageGenerationRetryableMessage,
} from '../message/image-generation-error-utils.ts'

export type PlaygroundSessionServerItem = {
  id: string
  title: string
  messages?: Message[]
  createdAt?: number
  updatedAt?: number
  created_at?: number
  updated_at?: number
}

export function normalizePlaygroundSessionResponse(
  session: PlaygroundSessionServerItem
): PlaygroundSession {
  const now = Date.now()

  return {
    id: session.id,
    title: session.title || 'New conversation',
    messages: Array.isArray(session.messages)
      ? session.messages.map(normalizeServerMessage)
      : [],
    createdAt: session.createdAt ?? session.created_at ?? now,
    updatedAt: session.updatedAt ?? session.updated_at ?? now,
  }
}

function normalizeServerMessage(message: Message): Message {
  if (isLegacyImageGeneration524ErrorMessage(message)) {
    return normalizeImageGenerationRetryableMessage({
      ...message,
      mode: 'image',
    })
  }

  return normalizeImageGenerationRetryableMessage(message)
}

export function normalizePlaygroundSessionsResponse(
  sessions: PlaygroundSessionServerItem[] | undefined
): PlaygroundSession[] {
  return (sessions ?? [])
    .filter((session) => Boolean(session?.id))
    .map(normalizePlaygroundSessionResponse)
    .sort((a, b) => b.updatedAt - a.updatedAt)
}
