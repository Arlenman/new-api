import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  isRecoverableImageGenerationErrorMessage,
  normalizeImageGenerationMetadata,
  normalizeImageGenerationRetryableMessage,
} from './image-generation-error-utils.ts'

describe('image generation error utilities', () => {
  test('does not infer image mode from a generic 524 chat error', () => {
    const message: Message = {
      key: 'assistant-legacy',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      createdAt: 1000,
      startedAt: 1000,
      completedAt: 2000,
      versions: [
        {
          id: 'version-1',
          content: 'Request error occurred: Request failed with status code 524',
        },
      ],
    }

    assert.equal(isRecoverableImageGenerationErrorMessage(message), false)
    assert.equal(normalizeImageGenerationRetryableMessage(message), message)
  })

  test('does not infer image mode from generic gateway and timeout errors', () => {
    const contents = [
      'Request error occurred: Request failed with status code 504',
      'HTTP 502 Bad Gateway from upstream',
      'Gateway Timeout',
      'Network Error',
      'proxy timed out',
    ]

    for (const content of contents) {
      const message: Message = {
        key: `assistant-${content}`,
        from: MESSAGE_ROLES.ASSISTANT,
        status: MESSAGE_STATUS.ERROR,
        versions: [{ id: 'version-1', content }],
      }

      assert.equal(isRecoverableImageGenerationErrorMessage(message), false)
      assert.equal(normalizeImageGenerationRetryableMessage(message), message)
    }
  })

  test('does not infer image mode when a legacy error has no role or image metadata', () => {
    const message = {
      key: 'assistant-missing-role',
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-1',
          content: 'Request error occurred: Request failed with status code 524',
        },
      ],
    } as Message

    assert.equal(isRecoverableImageGenerationErrorMessage(message), false)
    assert.equal(normalizeImageGenerationRetryableMessage(message), message)
  })

  test('keeps PDF chat request failures out of image generation state', () => {
    const message: Message = {
      key: 'assistant-pdf-chat',
      from: MESSAGE_ROLES.ASSISTANT,
      mode: 'chat',
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-pdf-chat',
          content: 'Request error occurred: PDF could not be processed',
        },
      ],
    }

    assert.equal(isRecoverableImageGenerationErrorMessage(message), false)
    assert.equal(normalizeImageGenerationRetryableMessage(message), message)
    assert.equal(message.mode, 'chat')
    assert.equal(message.imageGeneration, undefined)
  })

  test('fills missing image generation metadata on retryable server messages', () => {
    const message = {
      key: 'assistant-partial-image-state',
      from: MESSAGE_ROLES.ASSISTANT,
      mode: 'image',
      status: MESSAGE_STATUS.COMPLETE,
      imageGeneration: {
        status: 'retryable',
      },
      versions: [
        {
          id: 'version-1',
          content: '图片生成未完成，可以点击重新生成。',
        },
      ],
    } as Message

    const normalized = normalizeImageGenerationMetadata(message)

    assert.equal(normalized.mode, 'image')
    assert.equal(
      normalized.imageGeneration?.taskId,
      'image-assistant-partial-image-state'
    )
    assert.equal(normalized.imageGeneration?.prompt, '')
    assert.equal(normalized.imageGeneration?.size, 'auto')
    assert.equal(normalized.imageGeneration?.status, 'retryable')
  })

  test('does not normalize unrelated chat errors', () => {
    const message: Message = {
      key: 'assistant-chat',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      versions: [{ id: 'version-1', content: 'Model is unavailable' }],
    }

    assert.equal(isRecoverableImageGenerationErrorMessage(message), false)
    assert.equal(normalizeImageGenerationRetryableMessage(message), message)
  })
})
