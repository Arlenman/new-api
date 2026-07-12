import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { normalizeApiKeyTags } from './api-key-tags.ts'

describe('api key form tags', () => {
  test('normalizes and de-duplicates tags for payloads', () => {
    assert.deepEqual(normalizeApiKeyTags([' Client A ', 'client a', 'Batch 1', '']), [
      'Client A',
      'Batch 1',
    ])
  })
})
