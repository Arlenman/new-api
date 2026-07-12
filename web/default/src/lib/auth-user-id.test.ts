import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  clearStoredAuthIdentity,
  readStoredUserId,
  writeStoredUserId,
} from './auth-user-id.ts'

function createMemoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    getItem(key: string) {
      return data.get(key) ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, value)
    },
  }
}

describe('auth user id storage', () => {
  test('uses explicit uid when present', () => {
    const storage = createMemoryStorage({
      uid: '7',
      user: JSON.stringify({ id: 9 }),
    })

    assert.equal(readStoredUserId(storage), '7')
  })

  test('falls back to persisted user id and restores uid', () => {
    const storage = createMemoryStorage({
      user: JSON.stringify({ id: 9 }),
    })

    assert.equal(readStoredUserId(storage), '9')
    assert.equal(storage.getItem('uid'), '9')
  })

  test('clears both user and uid identity values', () => {
    const storage = createMemoryStorage({
      uid: '7',
      user: JSON.stringify({ id: 9 }),
    })

    clearStoredAuthIdentity(storage)

    assert.equal(storage.getItem('uid'), null)
    assert.equal(storage.getItem('user'), null)
  })

  test('writes normalized user id', () => {
    const storage = createMemoryStorage()

    writeStoredUserId(12, storage)

    assert.equal(storage.getItem('uid'), '12')
  })
})
