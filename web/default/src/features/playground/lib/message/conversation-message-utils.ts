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
import { MESSAGE_ROLES } from '../../constants.ts'
import type { Message, PlaygroundImageFile } from '../../types.ts'
import {
  createLoadingAssistantMessage,
  createUserMessage,
  getMessageContent,
  updateCurrentVersionContent,
} from './message-utils.ts'

type ApplyMessageEditResult =
  | {
      messages: Message[]
      shouldSend: false
    }
  | {
      messages: Message[]
      shouldSend: true
      mode: 'chat'
    }
  | {
      messages: Message[]
      shouldSend: true
      mode: 'image'
      prompt: string
    }

type ImageActionOptions = {
  forceImage?: boolean
}

export type RegenerateMessageAction =
  | {
      messages: Message[]
      mode: 'chat'
    }
  | {
      messages: Message[]
      mode: 'image'
      prompt: string
    }

type ChatMessageRenderState = {
  alwaysShowActions: boolean
  content: string
  isEditing: boolean
}

export function appendUserMessagePair(
  messages: Message[],
  content: string,
  attachments: PlaygroundImageFile[] = []
): Message[] {
  const submittedAt = Date.now()
  const userMessage = createUserMessage(content, submittedAt)

  return [
    ...messages,
    attachments.length > 0 ? { ...userMessage, attachments } : userMessage,
    createLoadingAssistantMessage(submittedAt),
  ]
}

export function appendUserImageMessagePair(
  messages: Message[],
  content: string,
  attachments: PlaygroundImageFile[] = []
): Message[] {
  const submittedAt = Date.now()
  const userMessage = {
    ...createUserMessage(content, submittedAt),
    mode: 'image' as const,
  }

  return [
    ...messages,
    attachments.length > 0 ? { ...userMessage, attachments } : userMessage,
    { ...createLoadingAssistantMessage(submittedAt), mode: 'image' },
  ]
}

export function createRegeneratedMessages(
  messages: Message[],
  messageKey: string
): Message[] | null {
  const messageIndex = messages.findIndex(
    (message) => message.key === messageKey
  )

  if (messageIndex === -1) {
    return null
  }

  if (messages[messageIndex].from === MESSAGE_ROLES.USER) {
    return [
      ...messages.slice(0, messageIndex + 1),
      {
        ...createLoadingAssistantMessage(),
        mode: messages[messageIndex].mode,
      },
    ]
  }

  return [
    ...messages.slice(0, messageIndex),
    {
      ...createLoadingAssistantMessage(),
      mode: messages[messageIndex].mode,
    },
  ]
}

export function createRegenerateMessageAction(
  messages: Message[],
  messageKey: string,
  options: ImageActionOptions = {}
): RegenerateMessageAction | null {
  const messageIndex = messages.findIndex(
    (message) => message.key === messageKey
  )

  if (messageIndex === -1) {
    return null
  }

  const regeneratedMessages = createRegeneratedMessages(messages, messageKey)
  if (!regeneratedMessages) {
    return null
  }

  const targetMessage = messages[messageIndex]
  const promptMessage =
    targetMessage.from === MESSAGE_ROLES.USER
      ? targetMessage
      : getPreviousUserMessage(messages, messageIndex)
  const shouldUseImage =
    options.forceImage ||
    targetMessage.mode === 'image' ||
    promptMessage?.mode === 'image'

  if (!shouldUseImage) {
    return { messages: regeneratedMessages, mode: 'chat' }
  }

  if (!promptMessage) {
    return { messages: regeneratedMessages, mode: 'chat' }
  }

  return {
    messages: regeneratedMessages.map((message, index) =>
      index === regeneratedMessages.length - 1
        ? {
            ...message,
            mode: 'image',
          }
        : message
    ),
    mode: 'image',
    prompt: getMessageContent(promptMessage),
  }
}

export function removeMessageByKey(
  messages: Message[],
  messageKey: string
): Message[] {
  return messages.filter((message) => message.key !== messageKey)
}

export function getPreviousUserMessage(
  messages: Message[],
  beforeIndex: number
): Message | null {
  for (let index = beforeIndex - 1; index >= 0; index--) {
    if (messages[index].from === MESSAGE_ROLES.USER) {
      return messages[index]
    }
  }

  return null
}

export function applyMessageEdit(
  messages: Message[],
  messageKey: string,
  content: string,
  shouldSubmit: boolean,
  options: ImageActionOptions = {}
): ApplyMessageEditResult | null {
  const submittedAt = Date.now()
  const messageIndex = messages.findIndex(
    (message) => message.key === messageKey
  )

  if (messageIndex === -1) {
    return null
  }

  const updatedMessages = messages.map((message) =>
    message.key === messageKey
      ? {
          ...updateCurrentVersionContent(message, content),
          createdAt: shouldSubmit ? submittedAt : message.createdAt,
        }
      : message
  )

  if (
    !shouldSubmit ||
    updatedMessages[messageIndex].from !== MESSAGE_ROLES.USER
  ) {
    return { messages: updatedMessages, shouldSend: false }
  }

  const shouldUseImage =
    options.forceImage || updatedMessages[messageIndex].mode === 'image'
  const loadingMessage: Message = shouldUseImage
    ? {
        ...createLoadingAssistantMessage(submittedAt),
        mode: 'image',
      }
    : createLoadingAssistantMessage(submittedAt)

  return {
    messages: [...updatedMessages.slice(0, messageIndex + 1), loadingMessage],
    shouldSend: true,
    ...(shouldUseImage
      ? {
          mode: 'image' as const,
          prompt: content,
        }
      : { mode: 'chat' as const }),
  }
}

export function getEditingMessageContent(
  messages: Message[],
  editingKey?: string | null
): string {
  if (!editingKey) {
    return ''
  }

  const message = messages.find((item) => item.key === editingKey)
  return message ? getMessageContent(message) : ''
}

export function getChatMessageRenderState(
  messages: Message[],
  message: Message,
  messageIndex: number,
  editingKey?: string | null
): ChatMessageRenderState {
  return {
    alwaysShowActions:
      messageIndex === messages.length - 1 &&
      message.from === MESSAGE_ROLES.ASSISTANT,
    content: getMessageContent(message),
    isEditing: editingKey === message.key,
  }
}
