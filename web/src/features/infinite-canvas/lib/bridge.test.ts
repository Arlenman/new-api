import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  createNewApiConfigureMessage,
  createProbeMessage,
  isTrustedInfiniteCanvasMessage,
} from './bridge.ts'

describe('infinite canvas bridge messages', () => {
  test('routes New API Images and Responses through /pg and media through /v1', () => {
    const message = createNewApiConfigureMessage(
      'https://new-api.example.com/v1/',
      ' utrs_runtime-session ',
      'test2 · A组'
    )

    assert.deepEqual(message, {
      source: 'new-api',
      type: 'new-api:infinite-canvas:configure',
      mode: 'new-api',
      apiUrl: 'https://new-api.example.com/pg',
      imageApiUrl: 'https://new-api.example.com/pg',
      mediaApiUrl: 'https://new-api.example.com/v1',
      apiKey: 'utrs_runtime-session',
      profileName: 'New API · test2 · A组',
    })
    assert.equal(message.apiUrl.includes('utrs_runtime-session'), false)
    assert.equal(message.apiUrl.includes('/v1/v1'), false)
  })

  test('creates the managed-host probe message', () => {
    assert.deepEqual(createProbeMessage(), {
      source: 'new-api',
      type: 'new-api:infinite-canvas:probe',
    })
  })
})

describe('infinite canvas bridge trust boundary', () => {
  test('accepts only same-origin messages from the current iframe window', () => {
    const iframeWindow = {}
    const data = {
      source: 'infinite-canvas',
      type: 'new-api:infinite-canvas:ready',
    }

    assert.equal(
      isTrustedInfiniteCanvasMessage(
        {
          origin: 'https://new-api.example.com',
          source: iframeWindow,
          data,
        },
        iframeWindow,
        'https://new-api.example.com'
      ),
      true
    )
    assert.equal(
      isTrustedInfiniteCanvasMessage(
        {
          origin: 'https://attacker.example.com',
          source: iframeWindow,
          data,
        },
        iframeWindow,
        'https://new-api.example.com'
      ),
      false
    )
    assert.equal(
      isTrustedInfiniteCanvasMessage(
        {
          origin: 'https://new-api.example.com',
          source: {},
          data,
        },
        iframeWindow,
        'https://new-api.example.com'
      ),
      false
    )
  })

  test('rejects messages with an unknown source, type, or mode', () => {
    const iframeWindow = {}
    const check = (data: unknown) =>
      isTrustedInfiniteCanvasMessage(
        {
          origin: 'https://new-api.example.com',
          source: iframeWindow,
          data,
        },
        iframeWindow,
        'https://new-api.example.com'
      )

    assert.equal(
      check({
        source: 'new-api',
        type: 'new-api:infinite-canvas:ready',
      }),
      false
    )
    assert.equal(check({ source: 'infinite-canvas', type: 'unknown' }), false)
    assert.equal(
      check({
        source: 'infinite-canvas',
        type: 'new-api:infinite-canvas:configured',
        mode: 'unknown',
      }),
      false
    )
    assert.equal(
      check({
        source: 'infinite-canvas',
        type: 'new-api:infinite-canvas:configured',
        mode: 'tool',
      }),
      false
    )
  })
})
