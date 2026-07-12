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
import { nanoid } from 'nanoid'

import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type {
  Message,
  MessageVersion,
  ChatCompletionMessage,
  ContentPart,
  PlaygroundImageFile,
} from '../../types.ts'
import {
  buildAttachmentContextText,
  hasExtractedAttachmentText,
  isPdfAttachment,
} from '../attachment/playground-attachment-text.ts'

/**
 * Create a new message version
 */
export function createMessageVersion(content: string): MessageVersion {
  return {
    id: nanoid(),
    content,
  }
}

/**
 * Get current version from message (always returns the first version)
 */
export function getCurrentVersion(message: Message): MessageVersion {
  return message.versions[0] || { id: 'default', content: '' }
}

/**
 * Get displayable content from the current message version.
 */
export function getMessageContent(message: Message): string {
  return getCurrentVersion(message).content
}

/**
 * Check whether a message has non-empty content in its current version.
 */
export function hasMessageContent(message: Message): boolean {
  return getMessageContent(message).trim() !== ''
}

function getAttachmentImageUrls(
  attachments: PlaygroundImageFile[] = []
): string[] {
  return attachments
    .filter(
      (attachment) =>
        Boolean(attachment.url?.trim()) &&
        Boolean(attachment.mediaType?.startsWith('image/'))
    )
    .map((attachment) => attachment.url?.trim() ?? '')
}

function getAttachmentFileParts(
  attachments: PlaygroundImageFile[] = []
): Extract<ContentPart, { type: 'file' }>[] {
  return attachments
    .filter(
      (attachment) => isPdfAttachment(attachment) && attachment.url?.trim()
    )
    .map((attachment) => ({
      type: 'file' as const,
      file: {
        filename: attachment.filename,
        file_data: attachment.url?.trim() ?? '',
      },
    }))
}

function getMessageTextWithAttachmentContext(
  text: string,
  attachments: PlaygroundImageFile[] = []
): string {
  const attachmentContext = buildAttachmentContextText(attachments)

  if (!attachmentContext) {
    return text
  }

  if (!text.trim()) {
    return attachmentContext
  }

  return `${text}\n\n${attachmentContext}`
}

/**
 * Update current version content in message
 */
export function updateCurrentVersionContent(
  message: Message,
  content: string
): Message {
  const currentVersion = getCurrentVersion(message)
  return {
    ...message,
    versions: [{ ...currentVersion, content }],
  }
}

/**
 * Create a user message
 */
export function createUserMessage(
  content: string,
  createdAt: number = Date.now()
): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.USER,
    versions: [createMessageVersion(content)],
    createdAt,
  }
}

/**
 * Create a loading assistant message
 */
export function createLoadingAssistantMessage(
  startedAt: number = Date.now()
): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.ASSISTANT,
    versions: [createMessageVersion('')],
    createdAt: startedAt,
    startedAt,
    reasoning: undefined,
    isReasoningComplete: false,
    isContentComplete: false,
    isReasoningStreaming: false,
    status: MESSAGE_STATUS.LOADING,
  }
}

/**
 * Build message content with optional images
 */
export function buildMessageContent(
  text: string,
  imageUrls: string[] = [],
  fileParts: Extract<ContentPart, { type: 'file' }>[] = []
): string | ContentPart[] {
  const validImages = imageUrls.filter((url) => url.trim() !== '')

  if (validImages.length === 0 && fileParts.length === 0) {
    return text
  }

  const parts: ContentPart[] = [
    {
      type: 'text',
      text: text || '',
    },
    ...validImages.map((url) => ({
      type: 'image_url' as const,
      image_url: { url: url.trim() },
    })),
    ...fileParts,
  ]

  return parts
}

/**
 * Extract text content from message content
 */
export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === 'string') {
    return content
  }

  if (Array.isArray(content)) {
    const textPart = content.find((part) => part.type === 'text')
    return textPart?.text || ''
  }

  return ''
}

/**
 * Format message for API request
 */
export function formatMessageForAPI(message: Message): ChatCompletionMessage {
  const currentVersion = getCurrentVersion(message)
  const imageUrls = getAttachmentImageUrls(message.attachments)
  const fileParts = getAttachmentFileParts(message.attachments)
  const text = getMessageTextWithAttachmentContext(
    currentVersion.content,
    message.attachments
  )

  return {
    role: message.from,
    content: buildMessageContent(text, imageUrls, fileParts),
  }
}

/**
 * Check if message is valid for API request
 * Excludes loading/streaming assistant messages and empty content
 */
export function isValidMessage(message: Message): boolean {
  if (!message || !message.from || !message.versions.length) return false

  // Exclude empty assistant messages (loading/streaming placeholders)
  if (message.from === MESSAGE_ROLES.ASSISTANT && !hasMessageContent(message)) {
    return false
  }

  if (message.from === MESSAGE_ROLES.USER) {
    return (
      hasMessageContent(message) ||
      getAttachmentImageUrls(message.attachments).length > 0 ||
      getAttachmentFileParts(message.attachments).length > 0 ||
      Boolean(message.attachments?.some(hasExtractedAttachmentText))
    )
  }

  return true
}
