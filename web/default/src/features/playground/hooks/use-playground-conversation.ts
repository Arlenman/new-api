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
import { useCallback, useState } from 'react'

import {
  appendUserMessagePair,
  appendUserImageMessagePair,
  applyMessageEdit,
  createRegenerateMessageAction,
  isImageGenerationModel,
  removeMessageByKey,
  shouldBlockImageActionForModel,
  shouldBlockImageSubmissionForModel,
  shouldUseImageGenerationPath,
} from '../lib'
import type {
  Message,
  PlaygroundImageFile,
  PlaygroundSession,
  PlaygroundSubmitPayload,
} from '../types'

type CommitActiveSessionMessages = (
  messages: Message[],
  titleContent?: string
) => {
  sessionId: string
  sessions: PlaygroundSession[]
} | null

type SendImageOptions = {
  sessionId: string
  assistantMessageKey: string
  prompt: string
  files?: PlaygroundImageFile[]
  imageSize?: string
  sessions: PlaygroundSession[]
  sessionMessages: Message[]
}

type UsePlaygroundConversationOptions = {
  messages: Message[]
  updateMessages: (
    updater: Message[] | ((prev: Message[]) => Message[])
  ) => void
  sendChat: (messages: Message[]) => void
  sendImage: (options: SendImageOptions) => void
  commitActiveSessionMessages: CommitActiveSessionMessages
  model: string
  imageSize: string
  onFirstMessage?: (content: string) => void
  onInvalidImageModel?: () => void
}

function getLastAssistantMessageKey(messages: Message[]): string | null {
  const message = messages.at(-1)
  return message?.from === 'assistant' ? message.key : null
}

export function usePlaygroundConversation({
  messages,
  updateMessages,
  sendChat,
  sendImage,
  commitActiveSessionMessages,
  model,
  imageSize,
  onFirstMessage,
  onInvalidImageModel,
}: UsePlaygroundConversationOptions) {
  const [editingMessageKey, setEditingMessageKey] = useState<string | null>(
    null
  )

  const handleSendMessage = useCallback(
    (payload: PlaygroundSubmitPayload | string) => {
      const text = typeof payload === 'string' ? payload : payload.text
      const files = typeof payload === 'string' ? [] : (payload.files ?? [])
      const hasImageFiles = files.length > 0
      const shouldGenerateImage = shouldUseImageGenerationPath(
        model,
        text,
        hasImageFiles
      )

      if (shouldBlockImageSubmissionForModel(model, text, hasImageFiles)) {
        onInvalidImageModel?.()
        return
      }

      onFirstMessage?.(text)

      if (shouldGenerateImage) {
        const nextMessages = appendUserImageMessagePair(messages, text)
        const assistantMessageKey = getLastAssistantMessageKey(nextMessages)
        const commitResult = commitActiveSessionMessages(nextMessages, text)
        if (!assistantMessageKey || !commitResult) {
          updateMessages(nextMessages)
          return
        }

        sendImage({
          sessionId: commitResult.sessionId,
          assistantMessageKey,
          prompt: text,
          files,
          imageSize:
            typeof payload === 'string' ? imageSize : payload.imageSize,
          sessions: commitResult.sessions,
          sessionMessages: nextMessages,
        })
        return
      }

      const nextMessages = appendUserMessagePair(messages, text)
      updateMessages(nextMessages)
      sendChat(nextMessages)
    },
    [
      messages,
      model,
      onFirstMessage,
      commitActiveSessionMessages,
      imageSize,
      onInvalidImageModel,
      sendChat,
      sendImage,
      updateMessages,
    ]
  )

  const handleRegenerateMessage = useCallback(
    (message: Message) => {
      const action = createRegenerateMessageAction(messages, message.key, {
        forceImage: isImageGenerationModel(model),
      })
      if (!action) return

      updateMessages(action.messages)
      if (action.mode === 'image') {
        if (shouldBlockImageActionForModel(action.mode, model)) {
          onInvalidImageModel?.()
          return
        }

        const assistantMessageKey = getLastAssistantMessageKey(action.messages)
        const commitResult = commitActiveSessionMessages(action.messages)
        if (!assistantMessageKey || !commitResult) {
          return
        }

        sendImage({
          sessionId: commitResult.sessionId,
          assistantMessageKey,
          prompt: action.prompt,
          imageSize,
          sessions: commitResult.sessions,
          sessionMessages: action.messages,
        })
        return
      }

      sendChat(action.messages)
    },
    [
      messages,
      model,
      updateMessages,
      sendChat,
      sendImage,
      commitActiveSessionMessages,
      imageSize,
      onInvalidImageModel,
    ]
  )

  const handleEditMessage = useCallback((message: Message) => {
    setEditingMessageKey(message.key)
  }, [])

  const handleEditOpenChange = useCallback((open: boolean) => {
    if (!open) {
      setEditingMessageKey(null)
    }
  }, [])

  const applyEdit = useCallback(
    (newContent: string, shouldSubmit: boolean) => {
      if (!editingMessageKey) return

      const editResult = applyMessageEdit(
        messages,
        editingMessageKey,
        newContent,
        shouldSubmit,
        {
          forceImage: isImageGenerationModel(model),
        }
      )
      if (!editResult) return

      setEditingMessageKey(null)
      updateMessages(editResult.messages)

      if (editResult.shouldSend) {
        if (editResult.mode === 'image') {
          if (shouldBlockImageActionForModel(editResult.mode, model)) {
            onInvalidImageModel?.()
            return
          }

          const assistantMessageKey = getLastAssistantMessageKey(
            editResult.messages
          )
          const commitResult = commitActiveSessionMessages(editResult.messages)
          if (!assistantMessageKey || !commitResult) {
            return
          }

          sendImage({
            sessionId: commitResult.sessionId,
            assistantMessageKey,
            prompt: editResult.prompt,
            imageSize,
            sessions: commitResult.sessions,
            sessionMessages: editResult.messages,
          })
          return
        }

        sendChat(editResult.messages)
      }
    },
    [
      editingMessageKey,
      messages,
      model,
      updateMessages,
      sendChat,
      sendImage,
      commitActiveSessionMessages,
      imageSize,
      onInvalidImageModel,
    ]
  )

  const handleDeleteMessage = useCallback(
    (message: Message) => {
      updateMessages((previousMessages) =>
        removeMessageByKey(previousMessages, message.key)
      )
    },
    [updateMessages]
  )

  return {
    editingMessageKey,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
  }
}
