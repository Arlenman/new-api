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

import { API_ENDPOINTS } from './constants'
import { sanitizePlaygroundMessagesForPersistence } from './lib/attachment/playground-attachment-persistence'
import {
  normalizePlaygroundSessionResponse,
  normalizePlaygroundSessionsResponse,
  type PlaygroundSessionServerItem,
} from './lib/session/playground-session-api-utils'
import type {
  ChatCompletionRequest,
  ChatCompletionResponse,
  ModelOption,
  GroupOption,
  ImageGenerationRequest,
  ImageGenerationResponse,
  PlaygroundImageFile,
  PlaygroundSession,
} from './types'

/**
 * Send chat completion request (non-streaming)
 */
export async function sendChatCompletion(
  payload: ChatCompletionRequest,
  signal?: AbortSignal
): Promise<ChatCompletionResponse> {
  const res = await api.post(API_ENDPOINTS.CHAT_COMPLETIONS, payload, {
    signal,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}

export async function sendImageGeneration(
  payload: ImageGenerationRequest,
  signal?: AbortSignal
): Promise<ImageGenerationResponse> {
  const { session_id, message_key, ...requestPayload } = payload
  const res = await api.post(API_ENDPOINTS.IMAGE_GENERATIONS, requestPayload, {
    signal,
    skipErrorHandler: true,
    headers: playgroundImageHeaders(session_id, message_key),
  } as Record<string, unknown>)
  return res.data
}

export async function sendImageEdit(
  payload: ImageGenerationRequest,
  files: PlaygroundImageFile[],
  signal?: AbortSignal
): Promise<ImageGenerationResponse> {
  const formData = new FormData()

  formData.set('model', payload.model)
  formData.set('prompt', payload.prompt)
  if (payload.group) formData.set('group', payload.group)
  if (payload.n) formData.set('n', String(payload.n))
  if (payload.size) formData.set('size', payload.size)
  if (payload.stream != null) formData.set('stream', String(payload.stream))
  if (payload.partial_images != null) {
    formData.set('partial_images', String(payload.partial_images))
  }
  if (payload.response_format) {
    formData.set('response_format', payload.response_format)
  }

  files.forEach((file, index) => {
    if (!file.url) {
      return
    }

    formData.append(
      'image',
      dataUrlToFile(
        file.url,
        file.filename || `image-${index + 1}.png`,
        file.mediaType || 'image/png'
      )
    )
  })

  const res = await api.post(API_ENDPOINTS.IMAGE_EDITS, formData, {
    signal,
    skipErrorHandler: true,
    headers: playgroundImageHeaders(payload.session_id, payload.message_key),
  } as Record<string, unknown>)
  return res.data
}

function playgroundImageHeaders(
  sessionId?: string,
  messageKey?: string
): Record<string, string> {
  const headers: Record<string, string> = {}
  if (sessionId) {
    headers['X-Playground-Session-Id'] = sessionId
  }
  if (messageKey) {
    headers['X-Playground-Message-Key'] = messageKey
  }
  if (sessionId && messageKey) {
    headers['X-Playground-Async'] = 'true'
  }
  return headers
}

type PlaygroundAPIResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

type ServerSessionsPayload = {
  sessions?: PlaygroundSessionServerItem[]
}

export async function getPlaygroundSessions(): Promise<PlaygroundSession[]> {
  const res = await api.get<PlaygroundAPIResponse<ServerSessionsPayload>>(
    API_ENDPOINTS.PLAYGROUND_SESSIONS,
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return normalizePlaygroundSessionsResponse(res.data.data?.sessions)
}

export async function createPlaygroundSessionRemote(
  session?: Pick<PlaygroundSession, 'id' | 'title' | 'createdAt' | 'updatedAt'>
): Promise<PlaygroundSession> {
  const res = await api.post<
    PlaygroundAPIResponse<PlaygroundSessionServerItem>
  >(
    API_ENDPOINTS.PLAYGROUND_SESSIONS,
    session
      ? {
          id: session.id,
          title: session.title,
          createdAt: session.createdAt,
          updatedAt: session.updatedAt,
        }
      : {},
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return normalizePlaygroundSessionResponse(res.data.data!)
}

export async function renamePlaygroundSessionRemote(
  sessionId: string,
  title: string
): Promise<PlaygroundSession> {
  const res = await api.put<PlaygroundAPIResponse<PlaygroundSessionServerItem>>(
    `${API_ENDPOINTS.PLAYGROUND_SESSIONS}/${encodeURIComponent(sessionId)}`,
    { title },
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return normalizePlaygroundSessionResponse(res.data.data!)
}

export async function deletePlaygroundSessionRemote(
  sessionId: string
): Promise<void> {
  await api.delete(
    `${API_ENDPOINTS.PLAYGROUND_SESSIONS}/${encodeURIComponent(sessionId)}`,
    { skipErrorHandler: true, skipBusinessError: true }
  )
}

export async function savePlaygroundSessionMessages(
  sessionId: string,
  messages: PlaygroundSession['messages']
): Promise<PlaygroundSession> {
  const res = await api.put<PlaygroundAPIResponse<PlaygroundSessionServerItem>>(
    `${API_ENDPOINTS.PLAYGROUND_SESSIONS}/${encodeURIComponent(
      sessionId
    )}/messages`,
    { messages: sanitizePlaygroundMessagesForPersistence(messages) },
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return normalizePlaygroundSessionResponse(res.data.data!)
}

export async function importPlaygroundSessions(
  sessions: PlaygroundSession[]
): Promise<PlaygroundSession[]> {
  const res = await api.post<PlaygroundAPIResponse<ServerSessionsPayload>>(
    API_ENDPOINTS.PLAYGROUND_SESSIONS_IMPORT,
    {
      sessions: sessions.map((session) => ({
        ...session,
        messages: sanitizePlaygroundMessagesForPersistence(session.messages),
      })),
    },
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return normalizePlaygroundSessionsResponse(res.data.data?.sessions)
}

function dataUrlToFile(dataUrl: string, filename: string, mediaType: string) {
  const [metadata, base64 = ''] = dataUrl.split(',')
  const resolvedMediaType =
    metadata.match(/^data:(.*?);base64$/)?.[1] || mediaType
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)

  for (let index = 0; index < binary.length; index++) {
    bytes[index] = binary.charCodeAt(index)
  }

  return new File([bytes], filename, { type: resolvedMediaType })
}

/**
 * Get user available models
 */
export async function getUserModels(group: string): Promise<ModelOption[]> {
  const res = await api.get(API_ENDPOINTS.USER_MODELS, {
    params: { group },
  })
  const { data } = res

  if (!data.success || !Array.isArray(data.data)) {
    return []
  }

  return data.data.map((model: string) => ({
    label: model,
    value: model,
  }))
}

/**
 * Get user groups
 */
export async function getUserGroups(): Promise<GroupOption[]> {
  const res = await api.get(API_ENDPOINTS.USER_GROUPS)
  const { data } = res

  if (!data.success || !data.data) {
    return []
  }

  const groupData = data.data as Record<string, { desc: string; ratio: number }>

  // label is for button display (name only); desc is for dropdown content
  return Object.entries(groupData).map(([group, info]) => ({
    label: group,
    value: group,
    ratio: info.ratio,
    desc: info.desc,
  }))
}
