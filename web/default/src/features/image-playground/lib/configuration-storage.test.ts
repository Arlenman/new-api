import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  normalizeImagePlaygroundApiUrl,
  parseImagePlaygroundConfiguration,
  serializeImagePlaygroundConfiguration,
} from './configuration-storage.ts'

describe('image playground custom API URL normalization', () => {
  test('accepts HTTP URLs and removes trailing slashes', () => {
    assert.equal(
      normalizeImagePlaygroundApiUrl(' https://api.example.com/v1/// '),
      'https://api.example.com/v1'
    )
    assert.equal(
      normalizeImagePlaygroundApiUrl('http://localhost:8080/v1/'),
      'http://localhost:8080/v1'
    )
  })

  test('rejects empty, malformed, and non-HTTP URLs', () => {
    assert.equal(normalizeImagePlaygroundApiUrl(''), null)
    assert.equal(normalizeImagePlaygroundApiUrl('api.example.com/v1'), null)
    assert.equal(normalizeImagePlaygroundApiUrl('file:///tmp/api'), null)
  })
})

describe('remembered image playground configuration', () => {
  test('round-trips the selected mode and custom API values for one user', () => {
    const serialized = serializeImagePlaygroundConfiguration(7, {
      mode: 'tool',
      customApiUrl: ' https://api.example.com/v1 ',
      customApiKey: ' sk-custom ',
    })

    assert.deepEqual(parseImagePlaygroundConfiguration(serialized, 7), {
      mode: 'tool',
      customApiUrl: 'https://api.example.com/v1',
      customApiKey: 'sk-custom',
    })
    assert.equal(parseImagePlaygroundConfiguration(serialized, 8), null)
  })

  test('ignores malformed or incomplete storage values', () => {
    assert.equal(parseImagePlaygroundConfiguration('{broken', 7), null)
    assert.equal(
      parseImagePlaygroundConfiguration(
        JSON.stringify({ userId: 7, mode: 'unknown' }),
        7
      ),
      null
    )
  })
})
