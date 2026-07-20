import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY,
  migrateInfiniteCanvasToManagedMode,
} from './configuration-storage.ts'

describe('infinite canvas managed account configuration migration', () => {
  test('removes a remembered third-party tool configuration and forces new-api mode', () => {
    const values = new Map([
      [
        INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY,
        JSON.stringify({
          userId: 7,
          mode: 'tool',
          customApiUrl: 'https://api.example.com/v1',
          customApiKey: 'sk-third-party',
        }),
      ],
    ])

    const configuration = migrateInfiniteCanvasToManagedMode({
      removeItem(key) {
        values.delete(key)
      },
    })

    assert.deepEqual(configuration, { mode: 'new-api' })
    assert.equal(values.has(INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY), false)
    assert.equal('customApiUrl' in configuration, false)
    assert.equal('customApiKey' in configuration, false)
  })

  test('keeps managed mode when no historical configuration exists', () => {
    const removedKeys: string[] = []

    const configuration = migrateInfiniteCanvasToManagedMode({
      removeItem(key) {
        removedKeys.push(key)
      },
    })

    assert.deepEqual(configuration, { mode: 'new-api' })
    assert.deepEqual(removedKeys, [INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY])
  })
})
