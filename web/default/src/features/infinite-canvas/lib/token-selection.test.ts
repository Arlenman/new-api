import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { UserToolTokenOption } from '@/features/user-tools/api'

import {
  createApiKeyOptions,
  createApiKeySwitchTarget,
  getApiKeyDisplayLabel,
  isApiKeyAvailable,
  isApiKeySelectionAvailable,
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

describe('infinite canvas API key availability', () => {
  test('uses the backend-authoritative availability result', () => {
    assert.equal(isApiKeyAvailable(createApiKey()), true)
    assert.equal(isApiKeyAvailable(createApiKey({ available: false })), false)
  })
})

describe('infinite canvas API key selection', () => {
  test('keeps every user key visible and disables unavailable options', () => {
    const keys = [
      createApiKey({
        id: 1,
        name: 'Primary',
        group: 'A组',
        display_label: 'Primary · A组',
      }),
      createApiKey({
        id: 2,
        name: 'Disabled',
        display_label: 'Disabled',
        available: false,
      }),
      createApiKey({
        id: 3,
        name: '',
        group: 'B组',
        display_label: '',
        available: false,
      }),
    ]

    assert.deepEqual(createApiKeyOptions(keys, 'Unnamed API key'), [
      { label: 'Primary · A组', value: '1', disabled: false },
      { label: 'Disabled', value: '2', disabled: true },
      { label: 'Unnamed API key · B组', value: '3', disabled: true },
    ])
  })

  test('uses the authoritative label and trims key names and groups', () => {
    assert.equal(
      getApiKeyDisplayLabel(
        {
          name: 'Stale name',
          group: 'Stale group',
          display_label: 'Current key · GPT组',
        },
        'Unnamed API key'
      ),
      'Current key · GPT组'
    )
    assert.equal(
      getApiKeyDisplayLabel(
        { name: '  Current key  ', group: '  Claude组  ' },
        'Unnamed API key'
      ),
      'Current key · Claude组'
    )
  })

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

  test('accepts switch targets only when the backend approves the matching key', () => {
    const keys = [
      createApiKey({ id: 1 }),
      createApiKey({ id: 2, available: false }),
      createApiKey({ id: 3, available: false }),
    ]

    assert.equal(isApiKeySelectionAvailable(keys, 1), true)
    assert.equal(isApiKeySelectionAvailable(keys, 2), false)
    assert.equal(isApiKeySelectionAvailable(keys, 3), false)
    assert.equal(isApiKeySelectionAvailable(keys, 99), false)
  })

  test('switching to another available key advances the iframe revision', () => {
    const keys = [
      createApiKey({ id: 1 }),
      createApiKey({ id: 2, group: 'B组' }),
      createApiKey({ id: 3, available: false }),
    ]

    assert.deepEqual(createApiKeySwitchTarget(keys, '2', 1, 4), {
      tokenId: 2,
      revision: 5,
    })
    assert.equal(createApiKeySwitchTarget(keys, '1', 1, 4), null)
    assert.equal(createApiKeySwitchTarget(keys, '3', 1, 4), null)
    assert.equal(createApiKeySwitchTarget(keys, 'broken', 1, 4), null)
  })
})

describe('remembered infinite canvas API key selection', () => {
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
