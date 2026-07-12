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
  MESSAGE_ROLES,
  MESSAGE_STATUS,
} from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  getMessageContent,
  updateCurrentVersionContent,
} from './message-utils.ts'
import { completeAssistantTiming } from './message-timing-utils.ts'

const RECOVERABLE_IMAGE_ERROR_PATTERNS = [
  /Request error occurred/i,
  /Request failed with status code 5\d\d/i,
  /HTTP 5\d\d/i,
  /\b5(?:00|02|03|04|20|22|24)\b/,
  /gateway timeout/i,
  /bad gateway/i,
  /service unavailable/i,
  /upstream/i,
  /proxy/i,
  /Network Error/i,
  /timeout/i,
  /timed out/i,
  /ECONNABORTED/i,
  /Image generation did not complete before timeout/i,
  /image generation failed/i,
  /empty image response/i,
  /empty image stream response/i,
]

function hasRecoverableImageErrorContent(content: string): boolean {
  return RECOVERABLE_IMAGE_ERROR_PATTERNS.some((pattern) =>
    pattern.test(content)
  )
}

function isAssistantOrUnknownMessage(message: Message): boolean {
  return message.from === MESSAGE_ROLES.ASSISTANT || !message.from
}

export function normalizeImageGenerationMetadata(message: Message): Message {
  if (!message.imageGeneration) {
    return message
  }

  const imageGeneration = message.imageGeneration
  const normalizedImageGeneration = {
    ...imageGeneration,
    taskId: imageGeneration.taskId || `image-${message.key}`,
    prompt: imageGeneration.prompt || '',
    size: imageGeneration.size || DEFAULT_IMAGE_SIZE,
    status: imageGeneration.status || 'pending',
  }

  if (
    normalizedImageGeneration.taskId === imageGeneration.taskId &&
    normalizedImageGeneration.prompt === imageGeneration.prompt &&
    normalizedImageGeneration.size === imageGeneration.size &&
    normalizedImageGeneration.status === imageGeneration.status &&
    message.mode
  ) {
    return message
  }

  return {
    ...message,
    mode: message.mode ?? 'image',
    imageGeneration: normalizedImageGeneration,
  }
}

export function isRecoverableImageGenerationErrorMessage(
  message: Message
): boolean {
  if (!isAssistantOrUnknownMessage(message)) {
    return false
  }

  const content = getMessageContent(message)
  const hasImageState =
    message.mode === 'image' || message.imageGeneration != null

  if (!hasImageState) {
    return false
  }

  return (
    message.status === MESSAGE_STATUS.ERROR ||
    message.imageGeneration?.status === 'error' ||
    hasRecoverableImageErrorContent(content)
  )
}

export function normalizeImageGenerationRetryableMessage(
  message: Message
): Message {
  const normalizedMetadataMessage = normalizeImageGenerationMetadata(message)

  if (!isRecoverableImageGenerationErrorMessage(normalizedMetadataMessage)) {
    return normalizedMetadataMessage
  }

  const completedAt =
    normalizedMetadataMessage.completedAt ??
    normalizedMetadataMessage.startedAt ??
    normalizedMetadataMessage.createdAt ??
    Date.now()

  return completeAssistantTiming(
    {
      ...updateCurrentVersionContent(
        normalizedMetadataMessage,
        ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
      ),
      from: MESSAGE_ROLES.ASSISTANT,
      mode: 'image',
      status: MESSAGE_STATUS.COMPLETE,
      isReasoningStreaming: false,
      imageGeneration: {
        taskId:
          normalizedMetadataMessage.imageGeneration?.taskId ??
          `retryable-${normalizedMetadataMessage.key}`,
        prompt: normalizedMetadataMessage.imageGeneration?.prompt ?? '',
        size: normalizedMetadataMessage.imageGeneration?.size ?? DEFAULT_IMAGE_SIZE,
        startedAt:
          normalizedMetadataMessage.imageGeneration?.startedAt ??
          normalizedMetadataMessage.startedAt ??
          normalizedMetadataMessage.createdAt,
        completedAt,
        status: 'retryable',
        error: undefined,
      },
    },
    completedAt
  )
}
