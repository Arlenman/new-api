import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { ERROR_MESSAGES, MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  isRecoverableImageGenerationErrorMessage,
  normalizeImageGenerationMetadata,
  normalizeImageGenerationRetryableMessage,
} from './image-generation-error-utils.ts'

describe('image generation error utilities', () => {
  test('normalizes legacy 524 errors without image mode', () => {
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

    assert.equal(isRecoverableImageGenerationErrorMessage(message), true)

    const normalized = normalizeImageGenerationRetryableMessage(message)

    assert.equal(normalized.mode, 'image')
    assert.equal(normalized.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(normalized.imageGeneration?.status, 'retryable')
    assert.equal(normalized.imageGeneration?.error, undefined)
    assert.equal(
      normalized.versions[0].content,
      ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
    )
    assert.doesNotMatch(normalized.versions[0].content, /524|Request error/)
  })

  test('normalizes gateway and timeout failures without image mode', () => {
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

      const normalized = normalizeImageGenerationRetryableMessage(message)

      assert.equal(normalized.mode, 'image')
      assert.equal(normalized.status, MESSAGE_STATUS.COMPLETE)
      assert.equal(normalized.imageGeneration?.status, 'retryable')
      assert.equal(
        normalized.versions[0].content,
        ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
      )
    }
  })

  test('normalizes legacy 524 errors with missing role', () => {
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

    const normalized = normalizeImageGenerationRetryableMessage(message)

    assert.equal(normalized.from, MESSAGE_ROLES.ASSISTANT)
    assert.equal(normalized.mode, 'image')
    assert.equal(normalized.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(normalized.imageGeneration?.status, 'retryable')
    assert.equal(
      normalized.versions[0].content,
      ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
    )
    assert.doesNotMatch(normalized.versions[0].content, /524|Request error/)
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
