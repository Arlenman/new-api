import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { ERROR_MESSAGES, MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type { Message } from '../../types.ts'
import { sanitizeMessagesOnLoad } from './message-streaming-utils.ts'

describe('message streaming utilities', () => {
  test('turns interrupted image generation into retryable non-error content', () => {
    const messages: Message[] = [
      {
        key: 'assistant-1',
        from: MESSAGE_ROLES.ASSISTANT,
        mode: 'image',
        status: MESSAGE_STATUS.LOADING,
        imageGeneration: {
          taskId: 'task-1',
          prompt: 'cute cat',
          size: 'auto',
          status: 'pending',
          startedAt: 1000,
        },
        versions: [{ id: 'version-1', content: '' }],
      },
    ]

    const sanitized = sanitizeMessagesOnLoad(messages)
    const message = sanitized[0]

    assert.equal(message.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(message.imageGeneration?.status, 'retryable')
    assert.equal(message.imageGeneration?.error, undefined)
    assert.equal(
      message.versions[0].content,
      ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
    )
  })

  test('turns interrupted image generation metadata into retryable state even without mode', () => {
    const messages = sanitizeMessagesOnLoad([
      {
        key: 'assistant-legacy-pending',
        from: MESSAGE_ROLES.ASSISTANT,
        status: MESSAGE_STATUS.LOADING,
        versions: [{ id: 'v1', content: '' }],
        imageGeneration: {
          taskId: 'task-1',
          prompt: 'cute cat',
          size: 'auto',
          status: 'pending',
          startedAt: 1000,
        },
      },
    ])

    const message = messages[0]

    assert.equal(message.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(message.imageGeneration?.status, 'retryable')
    assert.equal(
      message.versions[0].content,
      ERROR_MESSAGES.IMAGE_GENERATION_RETRYABLE
    )
  })
})
