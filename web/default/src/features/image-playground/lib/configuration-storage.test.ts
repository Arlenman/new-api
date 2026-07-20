import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY,
  resolveImagePlaygroundHostMode,
} from './configuration-storage.ts'

describe('image playground account-managed configuration', () => {
  test('migrates historical tool configuration and always enters new-api mode', () => {
    const values = new Map<string, string>([
      [
        IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY,
        JSON.stringify({
          userId: 7,
          mode: 'tool',
          customApiUrl: 'https://api.example.com/v1',
          customApiKey: 'sk-third-party',
        }),
      ],
    ])
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      removeItem: (key: string) => {
        values.delete(key)
      },
    }

    assert.equal(resolveImagePlaygroundHostMode(storage), 'new-api')
    assert.equal(
      storage.getItem(IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY),
      null
    )
  })

  test('uses new-api mode when no historical configuration exists', () => {
    const storage = {
      getItem: () => null,
      removeItem: () => {
        throw new Error('removeItem should not be called')
      },
    }

    assert.equal(resolveImagePlaygroundHostMode(storage), 'new-api')
  })
})
