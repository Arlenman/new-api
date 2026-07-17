import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { TFunction } from 'i18next'

import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import type { ApiKey } from '../types'
import {
  API_KEY_FORM_DEFAULT_VALUES,
  getApiKeyFormSchema,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from './api-key-form.ts'
import { normalizeApiKeyTags } from './api-key-tags.ts'

const t = ((key: string) => key) as TFunction

function createApiKey(overrides: Partial<ApiKey> = {}): ApiKey {
  return {
    id: 1,
    name: 'test key',
    key: 'test-key',
    status: 1,
    remain_quota: parseQuotaFromDollars(100),
    used_quota: 0,
    unlimited_quota: false,
    expired_time: -1,
    created_time: 1,
    accessed_time: 1,
    group: '',
    cross_group_retry: false,
    model_limits_enabled: false,
    model_limits: '',
    allow_ips: '',
    tags: [],
    quota_reset_enabled: false,
    quota_reset_period: 'daily',
    quota_reset_interval_hours: 0,
    quota_reset_amount: 0,
    quota_reset_remaining: 0,
    quota_reset_carry_over: false,
    quota_reset_last_time: 0,
    quota_reset_next_time: 0,
    ...overrides,
  }
}

describe('api key form tags', () => {
  test('normalizes and de-duplicates tags for payloads', () => {
    assert.deepEqual(
      normalizeApiKeyTags([' Client A ', 'client a', 'Batch 1', '']),
      ['Client A', 'Batch 1']
    )
  })
})

describe('api key periodic quota reset form', () => {
  test('defaults to disabled with carry over enabled', () => {
    assert.equal(API_KEY_FORM_DEFAULT_VALUES.quota_reset_enabled, false)
    assert.equal(API_KEY_FORM_DEFAULT_VALUES.quota_reset_period, 'daily')
    assert.equal(API_KEY_FORM_DEFAULT_VALUES.quota_reset_interval_hours, 24)
    assert.equal(API_KEY_FORM_DEFAULT_VALUES.quota_reset_carry_over, true)
  })

  test('requires a positive integer interval for custom hour resets', () => {
    const schema = getApiKeyFormSchema(t)
    for (const intervalHours of [0, -1, 1.5]) {
      const result = schema.safeParse({
        ...API_KEY_FORM_DEFAULT_VALUES,
        name: 'test key',
        quota_reset_enabled: true,
        quota_reset_period: 'custom_hours',
        quota_reset_interval_hours: intervalHours,
        quota_reset_amount_dollars: 10,
      })

      assert.equal(result.success, false)
      if (!result.success) {
        assert.deepEqual(result.error.issues[0]?.path, [
          'quota_reset_interval_hours',
        ])
      }
    }
  })

  test('requires a positive reset amount only when enabled', () => {
    const schema = getApiKeyFormSchema(t)
    const disabled = schema.safeParse({
      ...API_KEY_FORM_DEFAULT_VALUES,
      name: 'test key',
      quota_reset_amount_dollars: undefined,
    })
    assert.equal(disabled.success, true)

    const enabled = schema.safeParse({
      ...API_KEY_FORM_DEFAULT_VALUES,
      name: 'test key',
      quota_reset_enabled: true,
      quota_reset_amount_dollars: 0,
    })
    assert.equal(enabled.success, false)
    if (!enabled.success) {
      assert.deepEqual(enabled.error.issues[0]?.path, [
        'quota_reset_amount_dollars',
      ])
      assert.equal(
        enabled.error.issues[0]?.message,
        'Periodic quota must be greater than zero'
      )
    }
  })

  test('converts reset quota to internal quota units in payloads', () => {
    const payload = transformFormDataToPayload({
      ...API_KEY_FORM_DEFAULT_VALUES,
      quota_reset_enabled: true,
      quota_reset_period: 'weekly',
      quota_reset_amount_dollars: 25,
      quota_reset_carry_over: false,
    })

    assert.equal(payload.quota_reset_enabled, true)
    assert.equal(payload.quota_reset_period, 'weekly')
    assert.equal(payload.quota_reset_amount, parseQuotaFromDollars(25))
    assert.equal(payload.quota_reset_carry_over, false)
    assert.equal(payload.quota_reset_interval_hours, 0)
  })

  test('includes the custom hour interval in payloads', () => {
    const payload = transformFormDataToPayload({
      ...API_KEY_FORM_DEFAULT_VALUES,
      quota_reset_enabled: true,
      quota_reset_period: 'custom_hours',
      quota_reset_interval_hours: 6,
      quota_reset_amount_dollars: 25,
    })

    assert.equal(payload.quota_reset_period, 'custom_hours')
    assert.equal(payload.quota_reset_interval_hours, 6)
  })

  test('fills reset configuration when editing an enabled key', () => {
    const resetAmount = parseQuotaFromDollars(8)
    const values = transformApiKeyToFormDefaults(
      createApiKey({
        quota_reset_enabled: true,
        quota_reset_period: 'monthly',
        quota_reset_amount: resetAmount,
        quota_reset_remaining: parseQuotaFromDollars(3),
        quota_reset_carry_over: false,
      })
    )

    assert.equal(values.quota_reset_enabled, true)
    assert.equal(values.quota_reset_period, 'monthly')
    assert.equal(
      values.quota_reset_amount_dollars,
      quotaUnitsToDollars(resetAmount)
    )
    assert.equal(values.quota_reset_carry_over, false)
  })

  test('uses carry over defaults for keys without saved reset configuration', () => {
    const values = transformApiKeyToFormDefaults(createApiKey())

    assert.equal(values.quota_reset_enabled, false)
    assert.equal(values.quota_reset_period, 'daily')
    assert.equal(values.quota_reset_amount_dollars, 10)
    assert.equal(values.quota_reset_carry_over, true)
  })

  test('maps legacy hourly keys to a one-hour custom interval', () => {
    const values = transformApiKeyToFormDefaults(
      createApiKey({
        quota_reset_enabled: true,
        quota_reset_period: 'hourly',
        quota_reset_interval_hours: 0,
        quota_reset_amount: parseQuotaFromDollars(10),
      })
    )

    assert.equal(values.quota_reset_period, 'custom_hours')
    assert.equal(values.quota_reset_interval_hours, 1)
  })
})
