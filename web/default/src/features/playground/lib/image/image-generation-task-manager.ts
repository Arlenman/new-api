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
import {
  DEFAULT_IMAGE_SIZE,
  ERROR_MESSAGES,
  MESSAGE_STATUS,
  PLAYGROUND_IMAGE_STREAM_PARTIAL_IMAGES,
} from '../../constants.ts'
import type {
  ImageGenerationRequest,
  ImageGenerationResponse,
  Message,
  PlaygroundImageFile,
  PlaygroundSession,
} from '../../types.ts'
import { updateCurrentVersionContent } from '../message/message-utils.ts'
import { updatePlaygroundSessionMessages } from '../session/playground-session-utils.ts'
import {
  buildImageAssistantContent,
  getImageGenerationUrls,
  shouldStreamPlaygroundImageGeneration,
} from './playground-image-utils.ts'

type RequestImageOptions = {
  payload: ImageGenerationRequest
  files: PlaygroundImageFile[]
  signal: AbortSignal
}

type RequestImage = (
  options: RequestImageOptions
) => Promise<ImageGenerationResponse>

type ImageGenerationTaskManagerOptions = {
  id?: () => string
  now?: () => number
  getSessions?: () => PlaygroundSession[] | null
  saveSessions?: (sessions: PlaygroundSession[]) => void
  saveSessionMessages?: (sessionId: string, messages: Message[]) => void
  requestImage?: RequestImage
  recoverImageMessage?: (
    sessionId: string,
    assistantMessageKey: string
  ) => Promise<Message | null | undefined>
  recoveryPollIntervalMs?: number
  recoveryTimeoutMs?: number
}

export type StartImageGenerationTaskOptions = {
  sessionId: string
  assistantMessageKey: string
  prompt: string
  model: string
  group?: string
  size?: string
  files?: PlaygroundImageFile[]
  sessions?: PlaygroundSession[]
  sessionMessages: Message[]
  requestImage?: RequestImage
}

export type ImageGenerationTaskSnapshot = {
  activeTaskIds: string[]
  sessions?: PlaygroundSession[]
}

type ImageGenerationTaskListener = (
  snapshot: ImageGenerationTaskSnapshot
) => void

type ActiveImageGenerationTask = {
  abortController: AbortController
  done: Promise<void>
  sessionId: string
  assistantMessageKey: string
}

const DEFAULT_IMAGE_RECOVERY_TIMEOUT_MS = 10 * 60 * 1000

function fallbackId(): string {
  return crypto.randomUUID()
}

function isAbortError(error: unknown): boolean {
  return (
    error instanceof DOMException && error.name === 'AbortError'
  )
}

function isLikelyRecoverableImageRequestError(error: unknown): boolean {
  const requestError = error as {
    message?: string
    code?: string
    response?: { status?: number }
  }
  const status = requestError?.response?.status
  if (status === 524 || status === 504) {
    return true
  }
  if (requestError?.code === 'ECONNABORTED') {
    return true
  }
  return requestError?.message === 'Network Error'
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, ms))
}

function isCompletedImageMessage(message: Message | null | undefined): boolean {
  if (!message) {
    return false
  }
  const content = message.versions[0]?.content ?? ''
  return (
    message.from === 'assistant' &&
    message.mode === 'image' &&
    message.status === MESSAGE_STATUS.COMPLETE &&
    message.imageGeneration?.status === 'complete' &&
    content.includes('/api/playground/files/')
  )
}

function isTerminalImageMessage(message: Message | null | undefined): message is Message {
  if (isCompletedImageMessage(message)) {
    return true
  }
  return (
    !!message &&
    message.from === 'assistant' &&
    message.mode === 'image' &&
    (message.status === MESSAGE_STATUS.ERROR ||
      message.imageGeneration?.status === 'retryable' ||
      message.imageGeneration?.status === 'error' ||
      message.imageGeneration?.status === 'cancelled')
  )
}

function isAcceptedImageGenerationResponse(
  response: ImageGenerationResponse
): boolean {
  return response.status === 'pending' && getImageGenerationUrls(response).length === 0
}

function getSnapshot(
  activeTasks: Map<string, ActiveImageGenerationTask>,
  sessions?: PlaygroundSession[]
): ImageGenerationTaskSnapshot {
  return {
    activeTaskIds: [...activeTasks.keys()],
    sessions,
  }
}

function completeImageAssistantTiming(
  message: Message,
  completedAt: number
): Message {
  const startedAt = message.startedAt ?? message.createdAt ?? completedAt

  return {
    ...message,
    startedAt,
    completedAt,
    durationMs: Math.max(0, completedAt - startedAt),
  }
}

function patchMessageByKey(
  messages: Message[],
  messageKey: string,
  updater: (message: Message) => Message
): Message[] {
  return messages.map((message) =>
    message.key === messageKey ? updater(message) : message
  )
}

export function createImageGenerationTaskManager(
  options: ImageGenerationTaskManagerOptions = {}
) {
  const id = options.id ?? fallbackId
  const now = options.now ?? Date.now
  const getSessions = options.getSessions ?? (() => [])
  const persistSessions = options.saveSessions ?? (() => undefined)
  const persistSessionMessages =
    options.saveSessionMessages ?? (() => undefined)
  const recoverImageMessage = options.recoverImageMessage
  const recoveryPollIntervalMs = options.recoveryPollIntervalMs ?? 2000
  const recoveryTimeoutMs =
    options.recoveryTimeoutMs ?? DEFAULT_IMAGE_RECOVERY_TIMEOUT_MS
  const listeners = new Set<ImageGenerationTaskListener>()
  const activeTasks = new Map<string, ActiveImageGenerationTask>()
  let lastSessions: PlaygroundSession[] | null = null

  function notify(sessions?: PlaygroundSession[]): void {
    const snapshot = getSnapshot(activeTasks, sessions)
    listeners.forEach((listener) => listener(snapshot))
  }

  function readSessions(fallbackSessions?: PlaygroundSession[]): PlaygroundSession[] {
    return lastSessions ?? fallbackSessions ?? getSessions() ?? []
  }

  function writeSessionMessages(
    sessionId: string,
    messages: Message[],
    fallbackSessions?: PlaygroundSession[],
    timestamp: number = now()
  ): PlaygroundSession[] {
    const sessions = readSessions(fallbackSessions)
    const updatedSessions = updatePlaygroundSessionMessages(
      sessions,
      sessionId,
      messages,
      timestamp
    )

    persistSessions(updatedSessions)
    persistSessionMessages(sessionId, messages)
    lastSessions = updatedSessions
    notify(updatedSessions)
    return updatedSessions
  }

  function updateTargetMessage(
    taskId: string,
    fallbackSessions: PlaygroundSession[] | undefined,
    updater: (message: Message) => Message
  ): PlaygroundSession[] {
    const task = activeTasks.get(taskId)
    if (!task) {
      return readSessions(fallbackSessions)
    }

    const sessions = readSessions(fallbackSessions)
    const targetSession = sessions.find(
      (session) => session.id === task.sessionId
    )
    if (!targetSession) {
      return sessions
    }

    const messages = patchMessageByKey(
      targetSession.messages,
      task.assistantMessageKey,
      updater
    )

    return writeSessionMessages(
      task.sessionId,
      messages,
      sessions,
      now()
    )
  }

  function markTaskError(
    taskId: string,
    _error: unknown,
    status: 'error' | 'cancelled',
    fallbackSessions?: PlaygroundSession[]
  ): void {
    const completedAt = now()
    const imageStatus =
      status === 'cancelled' ? 'cancelled' : 'retryable'

    updateTargetMessage(taskId, fallbackSessions, (message) =>
      completeImageAssistantTiming(
        {
          ...updateCurrentVersionContent(
            message,
            status === 'cancelled'
              ? ERROR_MESSAGES.INTERRUPTED
              : ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
          ),
          mode: 'image',
          status: MESSAGE_STATUS.COMPLETE,
          isReasoningStreaming: false,
          imageGeneration: message.imageGeneration
            ? {
                ...message.imageGeneration,
                status: imageStatus,
                completedAt,
                error: undefined,
              }
            : undefined,
        },
        completedAt
      )
    )
  }

  function markTaskComplete(
    taskId: string,
    response: ImageGenerationResponse,
    fallbackSessions?: PlaygroundSession[]
  ): void {
    const completedAt = now()

    updateTargetMessage(taskId, fallbackSessions, (message) =>
      completeImageAssistantTiming(
        {
          ...updateCurrentVersionContent(
            message,
            buildImageAssistantContent(response)
          ),
          mode: 'image',
          status: MESSAGE_STATUS.COMPLETE,
          isReasoningStreaming: false,
          imageGeneration: message.imageGeneration
            ? {
                ...message.imageGeneration,
                status: 'complete',
                completedAt,
              }
            : undefined,
        },
        completedAt
      )
    )
  }

  function markTaskRecovered(
    taskId: string,
    recoveredMessage: Message,
    fallbackSessions?: PlaygroundSession[]
  ): void {
    updateTargetMessage(taskId, fallbackSessions, () => recoveredMessage)
  }

  async function recoverTerminalImageMessage(
    task: ActiveImageGenerationTask
  ): Promise<Message | null> {
    if (!recoverImageMessage) {
      return null
    }

    const startedAt = now()
    while (!task.abortController.signal.aborted) {
      const recovered = await recoverImageMessage(
        task.sessionId,
        task.assistantMessageKey
      )
      if (isTerminalImageMessage(recovered)) {
        return recovered
      }
      if (now() - startedAt >= recoveryTimeoutMs) {
        return null
      }
      await delay(recoveryPollIntervalMs)
    }

    return null
  }

  function start(startOptions: StartImageGenerationTaskOptions) {
    const taskId = id()
    const startedAt = now()
    const abortController = new AbortController()
    const requestImage = startOptions.requestImage ?? options.requestImage
    const size = startOptions.size || DEFAULT_IMAGE_SIZE
    const files = startOptions.files ?? []

    if (!requestImage) {
      throw new Error('Image generation request handler is required')
    }

    const messagesWithTask = patchMessageByKey(
      startOptions.sessionMessages,
      startOptions.assistantMessageKey,
      (message) => ({
        ...message,
        mode: 'image',
        status: MESSAGE_STATUS.LOADING,
        imageGeneration: {
          taskId,
          prompt: startOptions.prompt,
          size,
          status: 'pending',
          startedAt,
        },
      })
    )

    writeSessionMessages(
      startOptions.sessionId,
      messagesWithTask,
      startOptions.sessions,
      startedAt
    )

    const payload: ImageGenerationRequest = {
      model: startOptions.model,
      group: startOptions.group,
      prompt: startOptions.prompt,
      n: 1,
      session_id: startOptions.sessionId,
      message_key: startOptions.assistantMessageKey,
      ...(size !== DEFAULT_IMAGE_SIZE ? { size } : {}),
      ...(shouldStreamPlaygroundImageGeneration(startOptions.model)
        ? {
            stream: true,
            partial_images: PLAYGROUND_IMAGE_STREAM_PARTIAL_IMAGES,
          }
        : {}),
    }

    const done = (async () => {
      try {
        const response = await requestImage({
          payload,
          files,
          signal: abortController.signal,
        })

        if (abortController.signal.aborted) {
          return
        }

        if (isAcceptedImageGenerationResponse(response)) {
          const task = activeTasks.get(taskId)
          if (task) {
            const recoveredMessage = await recoverTerminalImageMessage(task)
            if (abortController.signal.aborted) {
              return
            }
            if (recoveredMessage) {
              markTaskRecovered(taskId, recoveredMessage)
              return
            }
          }
          throw new Error('Image generation did not complete before timeout')
        }

        if (getImageGenerationUrls(response).length === 0) {
          throw new Error(ERROR_MESSAGES.API_REQUEST_ERROR)
        }

        markTaskComplete(taskId, response)
      } catch (error) {
        if (abortController.signal.aborted || isAbortError(error)) {
          markTaskError(taskId, error, 'cancelled')
          return
        }

        const task = activeTasks.get(taskId)
        if (task && isLikelyRecoverableImageRequestError(error)) {
          const recoveredMessage = await recoverTerminalImageMessage(task)
          if (abortController.signal.aborted) {
            return
          }
          if (recoveredMessage) {
            markTaskRecovered(taskId, recoveredMessage)
            return
          }
        }

        markTaskError(taskId, error, 'error')
      } finally {
        activeTasks.delete(taskId)
        notify()
      }
    })()

    activeTasks.set(taskId, {
      abortController,
      done,
      sessionId: startOptions.sessionId,
      assistantMessageKey: startOptions.assistantMessageKey,
    })
    notify()

    return {
      taskId,
      done,
    }
  }

  function cancel(taskId: string): void {
    activeTasks.get(taskId)?.abortController.abort()
  }

  function cancelAll(): void {
    activeTasks.forEach((task) => task.abortController.abort())
  }

  function subscribe(listener: ImageGenerationTaskListener): () => void {
    listeners.add(listener)
    return () => listeners.delete(listener)
  }

  return {
    start,
    cancel,
    cancelAll,
    subscribe,
    getSnapshot: () => getSnapshot(activeTasks),
    getActiveTaskCount: () => activeTasks.size,
  }
}
