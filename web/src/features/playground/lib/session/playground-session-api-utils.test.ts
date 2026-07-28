import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_STATUS } from '../../constants.ts'
import { normalizePlaygroundSessionResponse } from './playground-session-api-utils.ts'

describe('playground session api utils', () => {
  test('normalizes server sessions into local playground session shape', () => {
    const session = normalizePlaygroundSessionResponse({
      id: 'session-1',
      title: 'Server session',
      createdAt: 1000,
      updatedAt: 2000,
      messages: [
        {
          key: 'assistant-1',
          from: 'assistant',
          versions: [{ id: 'v1', content: 'hello' }],
        },
      ],
    })

    assert.equal(session.id, 'session-1')
    assert.equal(session.title, 'Server session')
    assert.equal(session.createdAt, 1000)
    assert.equal(session.updatedAt, 2000)
    assert.equal(session.messages[0].key, 'assistant-1')
  })

  test('normalizes server image errors into retryable non-error messages', () => {
    const session = normalizePlaygroundSessionResponse({
      id: 'session-1',
      title: 'Server session',
      messages: [
        {
          key: 'assistant-1',
          from: 'assistant',
          mode: 'image',
          status: MESSAGE_STATUS.ERROR,
          imageGeneration: {
            taskId: 'task-1',
            prompt: 'cute cat',
            size: 'auto',
            status: 'error',
            error: 'Request failed with status code 524',
          },
          versions: [
            {
              id: 'v1',
              content:
                'Request error occurred: Request failed with status code 524',
            },
          ],
        },
      ],
    })

    const message = session.messages[0]

    assert.equal(message.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(message.imageGeneration?.status, 'retryable')
    assert.equal(message.imageGeneration?.error, undefined)
    assert.match(message.versions[0].content, /Image generation did not finish/)
    assert.doesNotMatch(message.versions[0].content, /524|Request error occurred/)
  })

  test('normalizes legacy server 524 errors without image mode', () => {
    const session = normalizePlaygroundSessionResponse({
      id: 'session-legacy',
      title: 'Legacy server session',
      messages: [
        {
          key: 'assistant-legacy',
          from: 'assistant',
          status: MESSAGE_STATUS.ERROR,
          versions: [
            {
              id: 'v1',
              content:
                'Request error occurred: Request failed with status code 524',
            },
          ],
        },
      ],
    })

    const message = session.messages[0]

    assert.equal(message.mode, 'image')
    assert.equal(message.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(message.imageGeneration?.status, 'retryable')
    assert.match(message.versions[0].content, /Image generation did not finish/)
    assert.doesNotMatch(message.versions[0].content, /524|Request error occurred/)
  })

  test('normalizes legacy server 524 errors with missing role', () => {
    const session = normalizePlaygroundSessionResponse({
      id: 'session-missing-role',
      title: 'Legacy server session',
      messages: [
        {
          key: 'assistant-missing-role',
          status: MESSAGE_STATUS.ERROR,
          versions: [
            {
              id: 'v1',
              content:
                'Request error occurred: Request failed with status code 524',
            },
          ],
        } as never,
      ],
    })

    const message = session.messages[0]

    assert.equal(message.from, 'assistant')
    assert.equal(message.mode, 'image')
    assert.equal(message.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(message.imageGeneration?.status, 'retryable')
    assert.match(message.versions[0].content, /Image generation did not finish/)
    assert.doesNotMatch(message.versions[0].content, /524|Request error occurred/)
  })
})
