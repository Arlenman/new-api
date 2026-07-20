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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { sendChatCompletion, sendImageEdit, sendImageGeneration } from '../api'
import { DEFAULT_IMAGE_SIZE, ERROR_MESSAGES } from '../constants'
import {
  applyStreamingChunk,
  buildChatCompletionPayload,
  updateAssistantMessageWithError,
  updateLastAssistantMessage,
  parseRequestErrorDetails,
  applyChatCompletionResponse,
  completeAssistantMessage,
  hasChatCompletionChoice,
  isAssistantMessageFinal,
  isAssistantMessagePending,
  getSafeMessageErrorContent,
} from '../lib'
import { imageGenerationTaskManager } from '../lib/image/image-generation-task-manager-singleton'
import type {
  Message,
  PlaygroundConfig,
  ParameterEnabled,
  PlaygroundImageFile,
  PlaygroundSession,
} from '../types'
import { useStreamRequest } from './use-stream-request'

interface UseChatHandlerOptions {
  activeSessionId: string | null
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  onMessageUpdate: (updater: (prev: Message[]) => Message[]) => void
}

const KNOWN_ERROR_MESSAGES = new Set<string>(Object.values(ERROR_MESSAGES))
const STREAM_UPDATE_FLUSH_MS = 50

type PendingStreamChunks = {
  content: string
  reasoning: string
}

type SendImageOptions = {
  sessionId: string
  assistantMessageKey: string
  prompt: string
  files?: PlaygroundImageFile[]
  imageSize?: string
  sessions: PlaygroundSession[]
  sessionMessages: Message[]
}

function mergePendingStreamChunk(
  currentChunk: string,
  nextChunk: string
): string {
  if (!currentChunk || !nextChunk.startsWith(currentChunk)) {
    return currentChunk + nextChunk
  }

  return nextChunk
}

/**
 * Hook for handling chat message sending and receiving
 */
export function useChatHandler({
  activeSessionId,
  config,
  parameterEnabled,
  onMessageUpdate,
}: UseChatHandlerOptions) {
  const { t } = useTranslation()
  const { sendStreamRequest, stopStream, isStreaming } = useStreamRequest()
  const [isRequesting, setIsRequesting] = useState(false)
  const [activeImageSessionIds, setActiveImageSessionIds] = useState(
    () => imageGenerationTaskManager.getSnapshot().activeSessionIds
  )
  const abortControllerRef = useRef<AbortController | null>(null)
  const requestIdRef = useRef(0)
  const pendingStreamChunksRef = useRef<PendingStreamChunks>({
    content: '',
    reasoning: '',
  })
  const streamFlushTimerRef = useRef<number | null>(null)

  const flushStreamUpdates = useCallback(() => {
    if (streamFlushTimerRef.current !== null) {
      window.clearTimeout(streamFlushTimerRef.current)
      streamFlushTimerRef.current = null
    }

    const pendingChunks = pendingStreamChunksRef.current
    if (!pendingChunks.reasoning && !pendingChunks.content) {
      return
    }

    pendingStreamChunksRef.current = { content: '', reasoning: '' }
    onMessageUpdate((prev) =>
      updateLastAssistantMessage(prev, (message) => {
        let updatedMessage = message

        if (pendingChunks.reasoning) {
          updatedMessage = applyStreamingChunk(
            updatedMessage,
            'reasoning',
            pendingChunks.reasoning
          )
        }

        if (pendingChunks.content) {
          updatedMessage = applyStreamingChunk(
            updatedMessage,
            'content',
            pendingChunks.content
          )
        }

        return updatedMessage
      })
    )
  }, [onMessageUpdate])

  const scheduleStreamFlush = useCallback(() => {
    if (streamFlushTimerRef.current !== null) {
      return
    }

    streamFlushTimerRef.current = window.setTimeout(
      flushStreamUpdates,
      STREAM_UPDATE_FLUSH_MS
    )
  }, [flushStreamUpdates])

  useEffect(
    () => () => {
      if (streamFlushTimerRef.current !== null) {
        window.clearTimeout(streamFlushTimerRef.current)
      }
    },
    []
  )

  useEffect(
    () =>
      imageGenerationTaskManager.subscribe((snapshot) => {
        setActiveImageSessionIds(snapshot.activeSessionIds)
      }),
    []
  )

  const getDisplayError = useCallback(
    (error: string) => {
      if (KNOWN_ERROR_MESSAGES.has(error)) {
        return t(error)
      }

      const connectionClosedSuffix = `: ${ERROR_MESSAGES.CONNECTION_CLOSED}`
      if (error.endsWith(connectionClosedSuffix)) {
        return `${error.slice(0, -ERROR_MESSAGES.CONNECTION_CLOSED.length)}${t(
          ERROR_MESSAGES.CONNECTION_CLOSED
        )}`
      }

      return error
    },
    [t]
  )

  // Handle stream update
  const handleStreamUpdate = useCallback(
    (type: 'reasoning' | 'content', chunk: string) => {
      pendingStreamChunksRef.current[type] = mergePendingStreamChunk(
        pendingStreamChunksRef.current[type],
        chunk
      )
      scheduleStreamFlush()
    },
    [scheduleStreamFlush]
  )

  // Handle stream complete
  const handleStreamComplete = useCallback(() => {
    flushStreamUpdates()
    setIsRequesting(false)
    onMessageUpdate((prev) =>
      updateLastAssistantMessage(prev, (message) =>
        isAssistantMessageFinal(message)
          ? message
          : completeAssistantMessage(message)
      )
    )
  }, [flushStreamUpdates, onMessageUpdate])

  // Handle stream error
  const handleStreamError = useCallback(
    (error: string, errorCode?: string) => {
      flushStreamUpdates()
      setIsRequesting(false)
      const displayError = getDisplayError(error)
      const safeDisplayError = getSafeMessageErrorContent(displayError)
      toast.info(t(safeDisplayError))
      const errorTitle = t(ERROR_MESSAGES.API_REQUEST_ERROR)
      onMessageUpdate((prev) =>
        updateAssistantMessageWithError(
          prev,
          safeDisplayError,
          errorCode,
          errorTitle
        )
      )
    },
    [flushStreamUpdates, getDisplayError, onMessageUpdate, t]
  )

  // Send streaming chat request
  const sendStreamingChat = useCallback(
    (messages: Message[]) => {
      setIsRequesting(true)
      const payload = buildChatCompletionPayload(
        messages,
        config,
        parameterEnabled
      )
      sendStreamRequest(
        payload,
        handleStreamUpdate,
        handleStreamComplete,
        handleStreamError
      )
    },
    [
      config,
      parameterEnabled,
      sendStreamRequest,
      handleStreamUpdate,
      handleStreamComplete,
      handleStreamError,
    ]
  )

  // Send non-streaming chat request
  const sendNonStreamingChat = useCallback(
    async (messages: Message[]) => {
      const payload = buildChatCompletionPayload(
        messages,
        config,
        parameterEnabled
      )
      const requestId = requestIdRef.current + 1
      const abortController = new AbortController()

      requestIdRef.current = requestId
      abortControllerRef.current = abortController

      try {
        setIsRequesting(true)
        const response = await sendChatCompletion(
          payload,
          abortController.signal
        )
        if (abortController.signal.aborted) return

        if (!hasChatCompletionChoice(response)) {
          handleStreamError(ERROR_MESSAGES.API_REQUEST_ERROR)
          return
        }

        onMessageUpdate((prev) =>
          updateLastAssistantMessage(prev, (message) => {
            const updatedMessage = applyChatCompletionResponse(
              message,
              response
            )

            return updatedMessage ?? message
          })
        )
      } catch (error: unknown) {
        if (abortController.signal.aborted) return

        const { errorCode, errorMessage } = parseRequestErrorDetails(error)
        handleStreamError(errorMessage, errorCode)
      } finally {
        if (requestIdRef.current === requestId) {
          abortControllerRef.current = null
          setIsRequesting(false)
        }
      }
    },
    [config, parameterEnabled, onMessageUpdate, handleStreamError]
  )

  const sendImage = useCallback(
    (options: SendImageOptions) => {
      imageGenerationTaskManager.start({
        sessionId: options.sessionId,
        assistantMessageKey: options.assistantMessageKey,
        prompt: options.prompt,
        model: config.model,
        group: config.group,
        size: options.imageSize || config.imageSize || DEFAULT_IMAGE_SIZE,
        files: options.files ?? [],
        sessions: options.sessions,
        sessionMessages: options.sessionMessages,
        requestImage: ({ payload, files, signal }) =>
          files.length > 0
            ? sendImageEdit(payload, files, signal)
            : sendImageGeneration(payload, signal),
      })
    },
    [config.group, config.imageSize, config.model]
  )

  // Send chat request (stream or non-stream based on config)
  const sendChat = useCallback(
    (messages: Message[]) => {
      if (config.stream) {
        sendStreamingChat(messages)
      } else {
        sendNonStreamingChat(messages)
      }
    },
    [config.stream, sendStreamingChat, sendNonStreamingChat]
  )

  // Stop generation
  const stopGeneration = useCallback(() => {
    stopStream()
    flushStreamUpdates()
    abortControllerRef.current?.abort()
    abortControllerRef.current = null
    if (activeSessionId) {
      imageGenerationTaskManager.cancelSession(activeSessionId)
    }
    setIsRequesting(false)
    onMessageUpdate((prev) =>
      updateLastAssistantMessage(prev, (message) =>
        isAssistantMessagePending(message) && message.mode !== 'image'
          ? completeAssistantMessage(message)
          : message
      )
    )
  }, [activeSessionId, stopStream, flushStreamUpdates, onMessageUpdate])

  const isChatGenerating = isStreaming || isRequesting
  const isActiveSessionGeneratingImage = Boolean(
    activeSessionId && activeImageSessionIds.includes(activeSessionId)
  )

  return {
    sendChat,
    sendImage,
    stopGeneration,
    activeImageSessionIds,
    isGenerating: isChatGenerating || isActiveSessionGeneratingImage,
    isSessionNavigationDisabled: isChatGenerating,
  }
}
