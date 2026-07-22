import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ApiKey } from '@/features/keys/types'
import type { UserToolRuntimeSession } from '@/features/user-tools/api'

import {
  getApiKeyDisplayLabel,
  getApiKeySelectionOptions,
  isApiKeyAvailable,
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

  test('formats paginated API key options from their name and group', () => {
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

  test('keeps every user key in the selector while marking unavailable keys disabled', () => {
    const now = 1_700_000_000
    const options = getApiKeySelectionOptions(
      [
        createApiKey({ id: 1, name: 'usable', group: 'A组' }),
        createApiKey({ id: 2, name: 'disabled', group: 'B组', status: 2 }),
        createApiKey({ id: 3, name: 'exhausted', remain_quota: 0 }),
      ],
      now,
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
