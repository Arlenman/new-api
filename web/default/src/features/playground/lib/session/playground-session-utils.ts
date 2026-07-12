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
import type { Message, PlaygroundSession } from '../../types'

export const DEFAULT_PLAYGROUND_SESSION_TITLE = 'New conversation'

type BuildInitialSessionsOptions = {
  storedSessions: PlaygroundSession[] | null
  activeSessionId: string | null
  legacyMessages: Message[]
  now?: number
  id?: () => string
}

type SessionMutationOptions = {
  now?: number
  id?: () => string
}

type DeleteSessionResult = {
  sessions: PlaygroundSession[]
  activeSessionId: string
}

function fallbackId(): string {
  return crypto.randomUUID()
}

function sortSessionsByUpdatedAt(
  sessions: PlaygroundSession[]
): PlaygroundSession[] {
  return [...sessions].sort((a, b) => b.updatedAt - a.updatedAt)
}

export function createPlaygroundSession({
  id = fallbackId(),
  now = Date.now(),
  title = DEFAULT_PLAYGROUND_SESSION_TITLE,
  messages = [],
}: {
  id?: string
  now?: number
  title?: string
  messages?: Message[]
} = {}): PlaygroundSession {
  return {
    id,
    title,
    messages,
    createdAt: now,
    updatedAt: now,
  }
}

export function buildInitialPlaygroundSessions({
  storedSessions,
  activeSessionId,
  legacyMessages,
  now = Date.now(),
  id = fallbackId,
}: BuildInitialSessionsOptions): DeleteSessionResult {
  const validStoredSessions = storedSessions?.filter((session) => session.id)

  if (validStoredSessions?.length) {
    const sorted = sortSessionsByUpdatedAt(validStoredSessions)
    const resolvedActiveId = sorted.some(
      (session) => session.id === activeSessionId
    )
      ? activeSessionId!
      : sorted[0].id

    return {
      sessions: sorted,
      activeSessionId: resolvedActiveId,
    }
  }

  const initialSession = createPlaygroundSession({
    id: id(),
    now,
    messages: legacyMessages,
  })

  return {
    sessions: [initialSession],
    activeSessionId: initialSession.id,
  }
}

export function updatePlaygroundSessionMessages(
  sessions: PlaygroundSession[],
  sessionId: string,
  messages: Message[],
  now: number = Date.now()
): PlaygroundSession[] {
  const updated = sessions.map((session) =>
    session.id === sessionId
      ? {
          ...session,
          messages,
          updatedAt: now,
        }
      : session
  )

  return sortSessionsByUpdatedAt(updated)
}

export function renamePlaygroundSession(
  sessions: PlaygroundSession[],
  sessionId: string,
  title: string,
  now: number = Date.now()
): PlaygroundSession[] {
  const trimmedTitle = title.trim()
  if (!trimmedTitle) {
    return sessions
  }

  return sessions.map((session) =>
    session.id === sessionId
      ? {
          ...session,
          title: trimmedTitle,
          updatedAt: now,
        }
      : session
  )
}

export function deletePlaygroundSession(
  sessions: PlaygroundSession[],
  sessionId: string,
  options: SessionMutationOptions = {}
): DeleteSessionResult {
  const remaining = sortSessionsByUpdatedAt(
    sessions.filter((session) => session.id !== sessionId)
  )

  if (remaining.length > 0) {
    return {
      sessions: remaining,
      activeSessionId: remaining[0].id,
    }
  }

  const fallbackSession = createPlaygroundSession({
    id: options.id?.() ?? fallbackId(),
    now: options.now ?? Date.now(),
  })

  return {
    sessions: [fallbackSession],
    activeSessionId: fallbackSession.id,
  }
}

export function getSessionTitleFromMessage(content: string): string {
  const normalized = content.replace(/\s+/g, ' ').trim()

  if (!normalized) {
    return DEFAULT_PLAYGROUND_SESSION_TITLE
  }

  return normalized.length > 32 ? `${normalized.slice(0, 32)}...` : normalized
}
