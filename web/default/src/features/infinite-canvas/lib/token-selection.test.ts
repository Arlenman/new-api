import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ApiKey } from '@/features/keys/types'

import {
  createApiKeySwitchTarget,
  createApiKeyOptions,
  getApiKeyDisplayLabel,
  isApiKeyAvailable,
  isApiKeySelectionAvailable,
  parseRememberedTokenSelection,
  selectPreferredApiKey,
  serializeRememberedTokenSelection,
} from './token-selection.ts'

function createApiKey(overrides: Partial<ApiKey> = {}): ApiKey {
  return {
    id: 1,
    name: 'Key 1',
    key: 'sk-masked',
    status: 1,
    remain_quota: 100,
    used_quota: 0,
    unlimited_quota: false,
    expired_time: -1,
    created_time: 1,
    accessed_time: 0,
    group: '',
    cross_group_retry: false,
    model_limits_enabled: false,
    model_limits: '',
    allow_ips: '',
    tags: [],
    quota_reset_enabled: false,
    quota_reset_period: 'daily',
    quota_reset_amount: 0,
    quota_reset_remaining: 0,
    quota_reset_carry_over: false,
    quota_reset_last_time: 0,
    quota_reset_next_time: 0,
    ...overrides,
  }
}

describe('infinite canvas API key availability', () => {
  test('accepts enabled non-expired keys with quota', () => {
    const now = 1_700_000_000

    assert.equal(isApiKeyAvailable(createApiKey(), now), true)
    assert.equal(
      isApiKeyAvailable(
        createApiKey({ remain_quota: 0, unlimited_quota: true }),
        now
      ),
      true
    )
  })

  test('accepts and selects an unlimited key with negative tracked quota', () => {
    const now = 1_700_000_000
    const key = createApiKey({
      id: 45,
      name: 'GPT-test',
      status: 1,
      remain_quota: -287622,
      unlimited_quota: true,
      expired_time: -1,
    })

    assert.equal(isApiKeyAvailable(key, now), true)
    assert.equal(selectPreferredApiKey([key], key.id, now)?.id, key.id)
  })

  test('rejects disabled, expired, and exhausted keys', () => {
    const now = 1_700_000_000

    assert.equal(isApiKeyAvailable(createApiKey({ status: 2 }), now), false)
    assert.equal(
      isApiKeyAvailable(createApiKey({ expired_time: now }), now),
      false
    )
    assert.equal(
      isApiKeyAvailable(createApiKey({ remain_quota: 0 }), now),
      false
    )
  })
})

describe('infinite canvas API key selection', () => {
  test('keeps every user key visible and disables unavailable options', () => {
    const now = 1_700_000_000
    const keys = [
      createApiKey({ id: 1, name: 'Primary', group: 'A组' }),
      createApiKey({ id: 2, name: 'Disabled', status: 2 }),
      createApiKey({ id: 3, name: '', group: 'B组', remain_quota: 0 }),
    ]

    assert.deepEqual(createApiKeyOptions(keys, now, 'Unnamed API key'), [
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

    assert.equal(selectPreferredApiKey(keys, 1, 1_700_000_000)?.id, 1)
  })

  test('falls back to the newest available key', () => {
    const keys = [
      createApiKey({ id: 1, created_time: 100 }),
      createApiKey({ id: 2, created_time: 300 }),
      createApiKey({ id: 3, created_time: 200, status: 2 }),
    ]

    assert.equal(selectPreferredApiKey(keys, 99, 1_700_000_000)?.id, 2)
  })

  test('returns null when no key is available', () => {
    const keys = [
      createApiKey({ id: 1, status: 2 }),
      createApiKey({ id: 2, remain_quota: 0 }),
    ]

    assert.equal(selectPreferredApiKey(keys, null, 1_700_000_000), null)
  })

  test('accepts switch targets only when the matching key is currently available', () => {
    const now = 1_700_000_000
    const keys = [
      createApiKey({ id: 1 }),
      createApiKey({ id: 2, status: 2 }),
      createApiKey({ id: 3, expired_time: now }),
    ]

    assert.equal(isApiKeySelectionAvailable(keys, 1, now), true)
    assert.equal(isApiKeySelectionAvailable(keys, 2, now), false)
    assert.equal(isApiKeySelectionAvailable(keys, 3, now), false)
    assert.equal(isApiKeySelectionAvailable(keys, 99, now), false)
  })

  test('switching to another available key advances the iframe revision', () => {
    const now = 1_700_000_000
    const keys = [
      createApiKey({ id: 1 }),
      createApiKey({ id: 2, group: 'B组' }),
      createApiKey({ id: 3, status: 2 }),
    ]

    assert.deepEqual(createApiKeySwitchTarget(keys, '2', 1, 4, now), {
      tokenId: 2,
      revision: 5,
    })
    assert.equal(createApiKeySwitchTarget(keys, '1', 1, 4, now), null)
    assert.equal(createApiKeySwitchTarget(keys, '3', 1, 4, now), null)
    assert.equal(createApiKeySwitchTarget(keys, 'broken', 1, 4, now), null)
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
