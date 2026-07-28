import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type {
  UserToolRuntimeSession,
  UserToolTokenOption,
} from '@/features/user-tools/api'

import {
  getApiKeyDisplayLabel,
  getApiKeySelectionOptions,
  isApiKeyAvailable,
  parseRememberedTokenSelection,
  selectPreferredApiKey,
  serializeRememberedTokenSelection,
} from './token-selection.ts'

function createApiKey(
  overrides: Partial<UserToolTokenOption> = {}
): UserToolTokenOption {
  return {
    id: 1,
    name: 'Key 1',
    masked_key: 'sk-****test',
    group: '',
    display_label: 'Key 1',
    created_time: 1,
    available: true,
    ...overrides,
  }
}

describe('image playground API key labels', () => {
  test('uses the runtime-session display label as the authoritative label', () => {
    const runtimeToken: UserToolRuntimeSession['token'] = {
      id: 2,
      name: 'stale-name',
      masked_key: 'sk-****test',
      group: 'stale-group',
      display_label: ' test2 · A组 ',
    }

    assert.equal(
      getApiKeyDisplayLabel(runtimeToken, 'Unnamed API key'),
      'test2 · A组'
    )
  })

  test('formats API key options from their name and group', () => {
    assert.equal(
      getApiKeyDisplayLabel(
        { name: 'test2', group: ' A组 ' },
        'Unnamed API key'
      ),
      'test2 · A组'
    )
    assert.equal(
      getApiKeyDisplayLabel({ name: '', group: '' }, 'Unnamed API key'),
      'Unnamed API key'
    )
  })
})

describe('image playground API key availability', () => {
  test('uses the backend-authoritative availability result', () => {
    assert.equal(isApiKeyAvailable(createApiKey()), true)
    assert.equal(isApiKeyAvailable(createApiKey({ available: false })), false)
  })

  test('keeps every user key in the selector while marking unavailable keys disabled', () => {
    const options = getApiKeySelectionOptions(
      [
        createApiKey({
          id: 1,
          name: 'usable',
          group: 'A组',
          display_label: 'usable · A组',
        }),
        createApiKey({
          id: 2,
          name: 'disabled',
          group: 'B组',
          display_label: 'disabled · B组',
          available: false,
        }),
        createApiKey({
          id: 3,
          name: 'exhausted',
          display_label: 'exhausted',
          available: false,
        }),
      ],
      'Unnamed API key'
    )

    assert.deepEqual(options, [
      { label: 'usable · A组', value: '1', available: true },
      { label: 'disabled · B组', value: '2', available: false },
      { label: 'exhausted', value: '3', available: false },
    ])
  })
})

describe('image playground API key selection', () => {
  test('restores a remembered valid key', () => {
    const keys = [
      createApiKey({ id: 1, created_time: 100 }),
      createApiKey({ id: 2, created_time: 200 }),
    ]

    assert.equal(selectPreferredApiKey(keys, 1)?.id, 1)
  })

  test('falls back to the newest backend-approved key', () => {
    const keys = [
      createApiKey({ id: 1, created_time: 100 }),
      createApiKey({ id: 2, created_time: 300 }),
      createApiKey({ id: 3, created_time: 400, available: false }),
    ]

    assert.equal(selectPreferredApiKey(keys, 99)?.id, 2)
  })

  test('returns null when the backend marks every key unavailable', () => {
    const keys = [
      createApiKey({ id: 1, available: false }),
      createApiKey({ id: 2, available: false }),
    ]

    assert.equal(selectPreferredApiKey(keys, null), null)
  })
})

describe('remembered image playground API key selection', () => {
  test('stores only the user ID and token ID', () => {
    const serialized = serializeRememberedTokenSelection(7, 42)

    assert.deepEqual(JSON.parse(serialized), { userId: 7, tokenId: 42 })
    assert.equal(serialized.includes('sk-'), false)
  })

  test('ignores another user or malformed storage data', () => {
    const serialized = serializeRememberedTokenSelection(7, 42)

    assert.equal(parseRememberedTokenSelection(serialized, 7), 42)
    assert.equal(parseRememberedTokenSelection(serialized, 8), null)
    assert.equal(parseRememberedTokenSelection('{broken', 7), null)
    assert.equal(parseRememberedTokenSelection(null, 7), null)
  })
})
