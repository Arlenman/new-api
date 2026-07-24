import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY,
  getImagePlaygroundStreamImagesStorageKey,
  persistImagePlaygroundStreamImages,
  readImagePlaygroundStreamImages,
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

describe('image playground streaming preference', () => {
  test('defaults to enabled and persists only a boolean per New API user', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    }

    assert.equal(readImagePlaygroundStreamImages(storage, 7), true)

    persistImagePlaygroundStreamImages(storage, 7, false)

    assert.equal(readImagePlaygroundStreamImages(storage, 7), false)
    assert.equal(readImagePlaygroundStreamImages(storage, 8), true)
    assert.deepEqual(
      [...values.entries()],
      [[getImagePlaygroundStreamImagesStorageKey(7), 'false']]
    )
  })

  test('fails closed to the enabled default for invalid or unavailable storage', () => {
    const invalidValues = new Map<string, string>([
      [getImagePlaygroundStreamImagesStorageKey(7), '"not-a-boolean"'],
    ])

    assert.equal(
      readImagePlaygroundStreamImages(
        { getItem: (key: string) => invalidValues.get(key) ?? null },
        7
      ),
      true
    )
    assert.equal(
      readImagePlaygroundStreamImages(
        {
          getItem: () => {
            throw new Error('storage unavailable')
          },
        },
        7
      ),
      true
    )
    assert.doesNotThrow(() =>
      persistImagePlaygroundStreamImages(
        {
          setItem: () => {
            throw new Error('storage unavailable')
          },
        },
        7,
        false
      )
    )
  })
})
