import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  createNewApiConfigureMessage,
  createProbeMessage,
  createToolConfigureMessage,
  isTrustedImagePlaygroundMessage,
} from './bridge.ts'

describe('image playground bridge messages', () => {
  test('creates same-origin New API configuration without putting the key in the URL', () => {
    const message = createNewApiConfigureMessage(
      'https://new-api.example.com/',
      'utrs_runtime-session'
    )

    assert.deepEqual(message, {
      source: 'new-api',
      type: 'new-api:image-playground:configure',
      mode: 'new-api',
      apiUrl: 'https://new-api.example.com/pg',
      apiKey: 'utrs_runtime-session',
      apiMode: 'images',
      profileName: 'New API',
    })
    assert.equal(message.apiUrl.includes('utrs_runtime-session'), false)
  })

  test('creates probe and third-party tool configuration messages', () => {
    assert.deepEqual(createProbeMessage(), {
      source: 'new-api',
      type: 'new-api:image-playground:probe',
    })
    assert.deepEqual(
      createToolConfigureMessage('https://api.example.com/v1/', ' sk-custom '),
      {
        source: 'new-api',
        type: 'new-api:image-playground:configure',
        mode: 'tool',
        apiUrl: 'https://api.example.com/v1',
        apiKey: 'sk-custom',
        apiMode: 'images',
        profileName: 'Custom API',
      }
    )
  })
})

describe('image playground bridge trust boundary', () => {
  test('accepts only same-origin messages from the current iframe window', () => {
    const iframeWindow = {}
    const data = {
      source: 'gpt-image-playground',
      type: 'new-api:image-playground:ready',
    }

    assert.equal(
      isTrustedImagePlaygroundMessage(
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
      isTrustedImagePlaygroundMessage(
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
      isTrustedImagePlaygroundMessage(
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

  test('rejects messages with an unknown source or type', () => {
    const iframeWindow = {}

    assert.equal(
      isTrustedImagePlaygroundMessage(
        {
          origin: 'https://new-api.example.com',
          source: iframeWindow,
          data: {
            source: 'new-api',
            type: 'new-api:image-playground:ready',
          },
        },
        iframeWindow,
        'https://new-api.example.com'
      ),
      false
    )
    assert.equal(
      isTrustedImagePlaygroundMessage(
        {
          origin: 'https://new-api.example.com',
          source: iframeWindow,
          data: {
            source: 'gpt-image-playground',
            type: 'unknown',
          },
        },
        iframeWindow,
        'https://new-api.example.com'
      ),
      false
    )
  })
})
