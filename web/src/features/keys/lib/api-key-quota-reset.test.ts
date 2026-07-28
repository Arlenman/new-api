import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ApiKey } from '../types'
import {
  getApiKeyPeriodicQuotaView,
  getApiKeyQuotaResetDescription,
} from './api-key-quota-reset.ts'

function createApiKey(overrides: Partial<ApiKey> = {}): ApiKey {
  return {
    id: 1,
    name: 'test key',
    key: 'test-key',
    status: 1,
    remain_quota: 100,
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
    quota_reset_carry_over: true,
    quota_reset_last_time: 0,
    quota_reset_next_time: 0,
    ...overrides,
  }
}

describe('API key periodic quota list view', () => {
  test('reports disabled keys as not enabled', () => {
    assert.deepEqual(getApiKeyPeriodicQuotaView(createApiKey()), {
      enabled: false,
    })
  })

  test('returns periodic balance, cadence, and next reset for finite keys', () => {
    assert.deepEqual(
      getApiKeyPeriodicQuotaView(
        createApiKey({
          quota_reset_enabled: true,
          quota_reset_period: 'weekly',
          quota_reset_amount: 80,
          quota_reset_remaining: 25,
          quota_reset_next_time: 1_800_000_000,
        })
      ),
      {
        enabled: true,
        remaining: 25,
        amount: 80,
        nextTime: 1_800_000_000,
        cadenceLabel: 'Every week',
      }
    )
  })

  test('keeps periodic quota visible for unlimited keys', () => {
    assert.deepEqual(
      getApiKeyPeriodicQuotaView(
        createApiKey({
          unlimited_quota: true,
          quota_reset_enabled: true,
          quota_reset_period: 'hourly',
          quota_reset_amount: 60,
          quota_reset_remaining: 10,
          quota_reset_next_time: 1_800_000_000,
        })
      ),
      {
        enabled: true,
        remaining: 10,
        amount: 60,
        nextTime: 1_800_000_000,
        cadenceLabel: 'Every {{hours}} hours',
        cadenceValues: { hours: 1 },
      }
    )
  })

  test('shows the configured rolling interval for custom hour resets', () => {
    assert.deepEqual(
      getApiKeyPeriodicQuotaView(
        createApiKey({
          quota_reset_enabled: true,
          quota_reset_period: 'custom_hours',
          quota_reset_interval_hours: 6,
          quota_reset_amount: 60,
          quota_reset_remaining: 10,
          quota_reset_next_time: 1_800_000_000,
        })
      ),
      {
        enabled: true,
        remaining: 10,
        amount: 60,
        nextTime: 1_800_000_000,
        cadenceLabel: 'Every {{hours}} hours',
        cadenceValues: { hours: 6 },
      }
    )
  })
})

describe('API key quota reset description', () => {
  const common = {
    intervalHours: 6,
    amount: '10',
    currency: 'USD',
  }

  test('describes each natural reset boundary', () => {
    assert.deepEqual(
      getApiKeyQuotaResetDescription({ ...common, period: 'daily' }),
      {
        key: 'Reset {{amount}} {{currency}} every day at 00:00',
        values: { amount: '10', currency: 'USD' },
      }
    )
    assert.deepEqual(
      getApiKeyQuotaResetDescription({ ...common, period: 'weekly' }),
      {
        key: 'Reset {{amount}} {{currency}} every Monday at 00:00',
        values: { amount: '10', currency: 'USD' },
      }
    )
    assert.deepEqual(
      getApiKeyQuotaResetDescription({ ...common, period: 'monthly' }),
      {
        key: 'Reset {{amount}} {{currency}} on the first day of every month at 00:00',
        values: { amount: '10', currency: 'USD' },
      }
    )
  })

  test('describes a custom interval from the current time', () => {
    assert.deepEqual(
      getApiKeyQuotaResetDescription({
        ...common,
        period: 'custom_hours',
      }),
      {
        key: 'Starting now, reset {{amount}} {{currency}} every {{hours}} hours',
        values: { amount: '10', currency: 'USD', hours: 6 },
      }
    )
  })
})
