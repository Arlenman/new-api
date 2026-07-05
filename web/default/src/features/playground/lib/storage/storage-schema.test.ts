import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { playgroundSessionsSchema } from './storage-schema.ts'

describe('playground storage schema', () => {
  test('preserves image message mode and generation metadata', () => {
    const parsed = playgroundSessionsSchema.parse([
      {
        id: 'session-1',
        title: 'Image session',
        createdAt: 1000,
        updatedAt: 1000,
        messages: [
          {
            key: 'assistant-1',
            from: 'assistant',
            mode: 'image',
            status: 'loading',
            versions: [{ id: 'version-1', content: '' }],
            imageGeneration: {
              taskId: 'task-1',
              prompt: 'cute cat',
              size: '1024x1024',
              status: 'pending',
              startedAt: 1000,
            },
          },
        ],
      },
    ])

    const message = parsed[0].messages[0]
    assert.equal(message.mode, 'image')
    assert.deepEqual(message.imageGeneration, {
      taskId: 'task-1',
      prompt: 'cute cat',
      size: '1024x1024',
      status: 'pending',
      startedAt: 1000,
    })
  })

  test('accepts retryable image generation state', () => {
    const parsed = playgroundSessionsSchema.parse([
      {
        id: 'session-1',
        title: 'Image session',
        createdAt: 1000,
        updatedAt: 1000,
        messages: [
          {
            key: 'assistant-1',
            from: 'assistant',
            mode: 'image',
            status: 'complete',
            versions: [
              {
                id: 'version-1',
                content: 'Image generation did not finish. You can retry.',
              },
            ],
            imageGeneration: {
              taskId: 'task-1',
              prompt: 'cute cat',
              size: '1024x1024',
              status: 'retryable',
              startedAt: 1000,
              completedAt: 2000,
            },
          },
        ],
      },
    ])

    assert.equal(parsed[0].messages[0].imageGeneration?.status, 'retryable')
  })
})
