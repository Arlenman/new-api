import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  filterAndSortUpstreamChannels,
  formatUpstreamPricingInterval,
  getAdjustedUpstreamAmount,
  getEffectiveUpstreamMultiplier,
  getTotalAdjustedUpstreamBalance,
  getUpstreamAccessTokenRecommendation,
  getUpstreamCardTone,
  getUpstreamChannelInUseKeyCount,
  getUpstreamChannelKeyStats,
  getUpstreamKeyGroupOptions,
  getUpstreamKeyInUseStatus,
  getUpstreamChannelDefaultName,
  getUpstreamChannelDisplayName,
  getUpstreamImportBaseName,
  getUpstreamImportDefaults,
  getUpstreamSelectedGroupMultiplier,
  getUpstreamModelPricingFields,
  hasUsableUpstreamCredentials,
  isUpstreamTurnstileAccessTokenRequired,
  isValidUpstreamMultiplier,
  formatUpstreamAvailability,
  formatUpstreamFirstTokenLatency,
} from './lib.ts'
import type { UpstreamChannel, UpstreamChannelStatus } from './types.ts'

function createChannel(
  id: number,
  balance: number,
  status: UpstreamChannelStatus,
  multiplier = 1,
  priority = 0,
  activeSourceChannelCount = 0
): UpstreamChannel {
  return {
    id,
    name: `Channel ${id}`,
    base_url: `https://api${id}.example.com`,
    provider: 'new-api',
    auth_type: 'password',
    selected_group: '',
    default_test_model: '',
    username: 'root',
    note: '',
    has_password: true,
    source_channel_count: 0,
    active_source_channel_count: activeSourceChannelCount,
    in_use_key_count: activeSourceChannelCount,
    balance,
    availability_24h: null,
    average_first_token_latency_ms: null,
    balance_updated_time: 0,
    balance_threshold: 0,
    multiplier,
    auto_refresh_interval: 300,
    last_sync_time: 0,
    last_error: '',
    status,
    priority,
  }
}

describe('upstream channel import defaults', () => {
  test('extracts the complete upstream hostname for fallback values', () => {
    assert.equal(
      getUpstreamImportBaseName('https://api.smallice.xyz/v1'),
      'api.smallice.xyz'
    )
    assert.equal(
      getUpstreamImportBaseName('https://gateway.example.com:8443/api'),
      'gateway.example.com:8443'
    )
  })

  test('falls back to the trimmed base URL when parsing fails', () => {
    assert.equal(getUpstreamImportBaseName(' upstream-host '), 'upstream-host')
  })
})

describe('upstream channel display values', () => {
  test('derives the default name from the first meaningful hostname segment', () => {
    assert.equal(
      getUpstreamChannelDefaultName('https://api.xtokenmirror.cn'),
      'xtokenmirror'
    )
    assert.equal(
      getUpstreamChannelDefaultName('https://api.syncapi.dpdns.org/v1'),
      'syncapi'
    )
    assert.equal(
      getUpstreamChannelDefaultName('https://api.ggbond686.online'),
      'ggbond686'
    )
    assert.equal(getUpstreamChannelDefaultName('https://aimuxr.com'), 'aimuxr')
    assert.equal(
      getUpstreamChannelDefaultName('https://www.aiwanwu.cc'),
      'aiwanwu'
    )
    assert.equal(
      getUpstreamChannelDefaultName('http://127.0.0.1:3000'),
      '127.0.0.1'
    )
  })

  test('uses the configured channel name and falls back to the default name', () => {
    assert.equal(
      getUpstreamChannelDisplayName(
        'Friendly upstream',
        'https://api.example.com/v1'
      ),
      'Friendly upstream'
    )
    assert.equal(
      getUpstreamChannelDisplayName(' ', 'https://api.example.com/v1'),
      'example'
    )
  })

  test('uses the channel name as the default tag and name prefix', () => {
    assert.deepEqual(
      getUpstreamImportDefaults({
        name: 'Friendly upstream',
        base_url: 'https://api.example.com/v1',
      }),
      {
        tag: 'Friendly upstream',
        namePrefix: 'Friendly upstream',
      }
    )
  })

  test('only refreshes after saving when complete credentials are available', () => {
    assert.equal(
      hasUsableUpstreamCredentials('new-api', 'password', '', '', false),
      false
    )
    assert.equal(
      hasUsableUpstreamCredentials('new-api', 'password', 'root', '', false),
      false
    )
    assert.equal(
      hasUsableUpstreamCredentials('new-api', 'password', '', 'secret', false),
      false
    )
    assert.equal(
      hasUsableUpstreamCredentials(
        'new-api',
        'password',
        'root',
        'secret',
        false
      ),
      true
    )
    assert.equal(
      hasUsableUpstreamCredentials('new-api', 'access_token', '1', '', true),
      true
    )
  })

  test('allows Sub2API access-token refresh without a username', () => {
    assert.equal(
      hasUsableUpstreamCredentials(
        'sub2api',
        'access_token',
        '',
        'token',
        false
      ),
      true
    )
    assert.equal(
      hasUsableUpstreamCredentials('sub2api', 'access_token', '', '', true),
      true
    )
    assert.equal(
      hasUsableUpstreamCredentials('sub2api', 'password', '', 'token', false),
      false
    )
  })
})

describe('upstream Turnstile authentication recovery', () => {
  test('recognizes only the stable Turnstile error code', () => {
    assert.equal(
      isUpstreamTurnstileAccessTokenRequired(
        'upstream_turnstile_requires_access_token'
      ),
      true
    )
    assert.equal(isUpstreamTurnstileAccessTokenRequired(''), false)
    assert.equal(isUpstreamTurnstileAccessTokenRequired(undefined), false)
    assert.equal(
      isUpstreamTurnstileAccessTokenRequired('some_other_error'),
      false
    )
  })

  test('switches to New-API access-token auth and clears a nonnumeric username', () => {
    assert.deepEqual(
      getUpstreamAccessTokenRecommendation({
        provider: 'auto',
        username: 'yunqi',
      }),
      {
        provider: 'new-api',
        authType: 'access_token',
        username: '',
      }
    )
  })

  test('preserves an existing positive numeric user ID', () => {
    assert.deepEqual(
      getUpstreamAccessTokenRecommendation({
        provider: 'new-api',
        username: ' 42 ',
      }),
      {
        provider: 'new-api',
        authType: 'access_token',
        username: '42',
      }
    )
    assert.equal(
      getUpstreamAccessTokenRecommendation({
        provider: 'new-api',
        username: '0',
      }).username,
      ''
    )
  })

  test('preserves a Sub2API account name when recommending access-token auth', () => {
    assert.deepEqual(
      getUpstreamAccessTokenRecommendation({
        provider: 'sub2api',
        username: ' owner@example.com ',
      }),
      {
        provider: 'sub2api',
        authType: 'access_token',
        username: 'owner@example.com',
      }
    )
  })

  test('uses the Sub2API error source when an auto-detected channel hits Turnstile', () => {
    assert.deepEqual(
      getUpstreamAccessTokenRecommendation({
        provider: 'auto',
        username: ' owner@example.com ',
        last_error:
          'sub2api has Turnstile enabled; use a browser-issued access token instead of account-password login',
      }),
      {
        provider: 'sub2api',
        authType: 'access_token',
        username: 'owner@example.com',
      }
    )
  })
})

describe('upstream channel filtering and sorting', () => {
  test('uses priority descending and ID ascending for the default order', () => {
    const channels = [
      createChannel(3, 10, 'ready', 1, 2),
      createChannel(2, 20, 'ready', 1, 5),
      createChannel(1, 30, 'ready', 1, 5),
    ]

    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'default').map(
        (channel) => channel.id
      ),
      [1, 2, 3]
    )
  })

  test('filters by status and sorts balances without mutating the source list', () => {
    const channels = [
      createChannel(1, 10, 'ready', 4),
      createChannel(2, 30, 'error'),
      createChannel(3, 20, 'ready'),
    ]

    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'balance-desc').map(
        (channel) => channel.id
      ),
      [1, 2, 3]
    )
    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'ready', 'balance-asc').map(
        (channel) => channel.id
      ),
      [3, 1]
    )
    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'error', 'default').map(
        (channel) => channel.id
      ),
      [2]
    )
    assert.deepEqual(
      channels.map((channel) => channel.id),
      [1, 2, 3]
    )
  })

  test('balance sorting ignores priority and uses ID ascending for ties', () => {
    const channels = [
      createChannel(3, 10, 'ready', 1, 100),
      createChannel(2, 20, 'ready', 1, -100),
      createChannel(1, 20, 'ready', 1, 50),
    ]

    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'balance-desc').map(
        (channel) => channel.id
      ),
      [1, 2, 3]
    )
    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'balance-asc').map(
        (channel) => channel.id
      ),
      [3, 1, 2]
    )
  })

  test('sorts recent availability and first-token latency with missing metrics last', () => {
    const channels = [
      createChannel(3, 10, 'ready'),
      createChannel(2, 10, 'ready'),
      createChannel(1, 10, 'ready'),
      createChannel(4, 10, 'ready'),
    ]
    channels[0].availability_24h = 95
    channels[0].average_first_token_latency_ms = 300
    channels[1].availability_24h = 99.5
    channels[1].average_first_token_latency_ms = 800
    channels[2].availability_24h = 99.5
    channels[2].average_first_token_latency_ms = 120

    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'availability-desc').map(
        (channel) => channel.id
      ),
      [1, 2, 3, 4]
    )
    assert.deepEqual(
      filterAndSortUpstreamChannels(
        channels,
        'all',
        'first-token-latency-asc'
      ).map((channel) => channel.id),
      [1, 3, 2, 4]
    )
  })

  test('sorts selected group multipliers and keeps unavailable values last', () => {
    const createRateChannel = (
      id: number,
      selectedGroup: string,
      ratio: number | null,
      multiplier = 1
    ) => {
      const channel = createChannel(
        id,
        10,
        'ready',
        multiplier
      ) as UpstreamChannel & {
        selected_group: string
      }
      channel.selected_group = selectedGroup
      channel.snapshot = {
        provider: 'new-api',
        balance: 10,
        account: { id, username: 'root', balance: 10 },
        keys: [],
        groups: ratio === null ? [] : [{ name: selectedGroup, ratio }],
        ratios: ratio === null ? {} : { [selectedGroup]: ratio },
        retrieved_at: 0,
      }
      return channel
    }
    const channels = [
      createRateChannel(1, 'gpt-pro', 0.12),
      createRateChannel(2, 'Claude', 0.5),
      createRateChannel(3, 'expired', null),
      createRateChannel(4, '', null),
      createRateChannel(5, 'gpt', 0.08, 2),
    ]

    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'multiplier-desc').map(
        (channel) => channel.id
      ),
      [2, 5, 1, 3, 4]
    )
    assert.deepEqual(
      filterAndSortUpstreamChannels(channels, 'all', 'multiplier-asc').map(
        (channel) => channel.id
      ),
      [1, 5, 2, 3, 4]
    )
  })
})

describe('upstream channel request metrics', () => {
  test('formats availability and first-token latency for compact badges', () => {
    assert.equal(formatUpstreamAvailability(99.456), '99.46%')
    assert.equal(formatUpstreamAvailability(null), '-')
    assert.equal(formatUpstreamFirstTokenLatency(123.4), '123 ms')
    assert.equal(formatUpstreamFirstTokenLatency(1234), '1.23 s')
    assert.equal(formatUpstreamFirstTokenLatency(null), '-')
  })
})

describe('upstream model pricing', () => {
  test('keeps New-API zero ratios and formats all ratio fields', () => {
    assert.deepEqual(
      getUpstreamModelPricingFields({
        source: 'new-api',
        model_ratio: 1.25,
        completion_ratio: 0,
        cache_ratio: 0.5,
        create_cache_ratio: 2,
        model_price: 0.01,
      }),
      [
        { label: 'Model ratio', value: '×1.25' },
        { label: 'Completion ratio', value: '×0' },
        { label: 'Cache ratio', value: '×0.5' },
        { label: 'Cache creation ratio', value: '×2' },
        { label: 'Fixed price', value: '0.01' },
      ]
    )
  })

  test('formats Sub2API token, image, and per-request prices', () => {
    assert.deepEqual(
      getUpstreamModelPricingFields({
        source: 'sub2api',
        input_price: 0.000003,
        output_price: 0.000015,
        cache_write_price: 0,
        cache_read_price: 0.0000003,
        image_input_price: 0.02,
        image_output_price: 0.04,
        per_request_price: 0.02,
      }),
      [
        { label: 'Input price', value: '$3 / 1M tokens' },
        { label: 'Output price', value: '$15 / 1M tokens' },
        { label: 'Cache write price', value: '$0 / 1M tokens' },
        { label: 'Cache read price', value: '$0.3 / 1M tokens' },
        { label: 'Image input price', value: '$0.02' },
        { label: 'Image output price', value: '$0.04' },
        { label: 'Per-request price', value: '$0.02 / request' },
      ]
    )
  })

  test('formats tier ranges and ignores non-finite prices', () => {
    assert.equal(
      formatUpstreamPricingInterval(
        {
          min_tokens: 0,
          max_tokens: 200000,
          tier_label: 'standard',
          input_price: 0.000003,
          output_price: 0.000015,
          cache_read_price: Number.NaN,
        },
        {
          tokens: 'tokens',
          input: 'input',
          output: 'output',
          cacheRead: 'cache read',
        }
      ),
      'standard: 0-200,000 tokens · input $3 / 1M tokens · output $15 / 1M tokens'
    )
    assert.deepEqual(
      getUpstreamModelPricingFields({
        source: 'sub2api',
        input_price: Number.NaN,
        output_price: Number.POSITIVE_INFINITY,
      }),
      []
    )
  })
})

describe('upstream channel multiplier', () => {
  test('tracks the selected group ratio and marks a removed group invalid', () => {
    const channel = createChannel(1, 10, 'ready', 1.5) as UpstreamChannel & {
      selected_group: string
    }
    channel.selected_group = 'gpt-pro'
    channel.snapshot = {
      provider: 'new-api',
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [],
      groups: [
        { name: 'gpt', ratio: 0.085 },
        { name: 'gpt-pro', ratio: 0.12 },
      ],
      ratios: { gpt: 0.085, 'gpt-pro': 0.12 },
      retrieved_at: 0,
    }

    assert.deepEqual(getUpstreamSelectedGroupMultiplier(channel), {
      status: 'valid',
      value: 0.18,
    })

    channel.snapshot.groups = [{ name: 'gpt', ratio: 0.085 }]
    delete channel.snapshot.ratios['gpt-pro']
    assert.deepEqual(getUpstreamSelectedGroupMultiplier(channel), {
      status: 'invalid',
    })

    channel.selected_group = ''
    assert.deepEqual(getUpstreamSelectedGroupMultiplier(channel), {
      status: 'unselected',
    })
  })

  test('matches a legacy snapshot group after trimming surrounding spaces', () => {
    const channel = createChannel(1, 10, 'ready', 1) as UpstreamChannel & {
      selected_group: string
    }
    channel.selected_group = '005'
    channel.snapshot = {
      provider: 'sub2api',
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [],
      groups: [{ id: 24, name: '005 ', ratio: 0.05 }],
      ratios: { '24': 0.05 },
      retrieved_at: 0,
    }

    assert.deepEqual(getUpstreamSelectedGroupMultiplier(channel), {
      status: 'valid',
      value: 0.05,
    })
  })

  test('uses one for legacy values and adjusts displayed amounts', () => {
    assert.equal(getEffectiveUpstreamMultiplier(0), 1)
    assert.equal(getEffectiveUpstreamMultiplier(Number.NaN), 1)
    assert.equal(getAdjustedUpstreamAmount(12.5, 1.2), 15)
  })

  test('accepts positive values with at most two decimal places', () => {
    assert.equal(isValidUpstreamMultiplier(1), true)
    assert.equal(isValidUpstreamMultiplier(1.25), true)
    assert.equal(isValidUpstreamMultiplier(0), false)
    assert.equal(isValidUpstreamMultiplier(1.001), false)
  })

  test('sums every channel balance after applying its multiplier', () => {
    const channels = [
      createChannel(1, 12.5, 'ready', 1.2),
      createChannel(2, 8, 'error', 0),
      createChannel(3, 2, 'unconfigured', 2),
    ]

    assert.equal(getTotalAdjustedUpstreamBalance(channels), 27)
  })

  test('counts all snapshot keys and only enabled in-use keys', () => {
    const firstChannel = createChannel(1, 10, 'ready', 1, 0, 2)
    firstChannel.snapshot = {
      provider: 'new-api',
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [
        {
          id: 1,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'enabled-key-a',
          name: 'A',
          masked_key: 'sk-a',
          status: '1',
        },
        {
          id: 2,
          imported: false,
          active: false,
          in_use_status: 'unlinked',
          name: 'B',
          masked_key: 'sk-b',
          status: '1',
        },
        {
          id: 3,
          imported: true,
          active: false,
          in_use_status: 'auto_disabled',
          name: 'C',
          masked_key: 'sk-c',
          status: '1',
        },
        {
          id: 5,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'same-enabled-key',
          name: 'A duplicate',
          masked_key: 'sk-a...',
          status: '1',
        },
        {
          id: 6,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'same-enabled-key',
          name: 'A duplicate again',
          masked_key: 'sk-a...',
          status: '1',
        },
      ],
      groups: [],
      ratios: {},
      retrieved_at: 0,
    }
    const secondChannel = createChannel(2, 20, 'ready', 1, 0, 1)
    secondChannel.snapshot = {
      provider: 'sub2api',
      balance: 20,
      account: { id: 2, username: 'root', balance: 20 },
      keys: [
        {
          id: 4,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'enabled-key-d',
          name: 'D',
          masked_key: 'sk-d',
          status: '1',
        },
      ],
      groups: [],
      ratios: {},
      retrieved_at: 0,
    }

    assert.deepEqual(
      getUpstreamChannelKeyStats([firstChannel, secondChannel]),
      {
        total: 6,
        active: 3,
      }
    )
  })

  test('uses the backend count without a snapshot and ignores enabled keys without fingerprints', () => {
    const channel = createChannel(1, 10, 'ready')
    channel.in_use_key_count = 4

    assert.equal(getUpstreamChannelInUseKeyCount(channel), 4)

    channel.snapshot = {
      provider: 'new-api',
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [
        {
          id: 1,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          name: 'Missing fingerprint',
          masked_key: 'sk-missing',
          status: '1',
        },
        {
          id: 2,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'linked-key',
          name: 'Linked key',
          masked_key: 'sk-linked',
          status: '1',
        },
        {
          id: 3,
          imported: true,
          active: true,
          in_use_status: 'enabled',
          key_fingerprint: 'linked-key',
          name: 'Duplicate linked key',
          masked_key: 'sk-linked...',
          status: '1',
        },
      ],
      groups: [],
      ratios: {},
      retrieved_at: 0,
    }

    assert.equal(getUpstreamChannelInUseKeyCount(channel), 1)
  })
})

describe('upstream channel card tones', () => {
  test('uses provider-specific accent bars and a white new-api background', () => {
    const sub2APITone = getUpstreamCardTone('sub2api')
    const newAPITone = getUpstreamCardTone('new-api')
    const otherTone = getUpstreamCardTone('other')
    const unknownTone = getUpstreamCardTone('auto')

    assert.match(sub2APITone, /border-l-blue-500/)
    assert.match(newAPITone, /border-l-pink-500/)
    assert.match(newAPITone, /bg-background/)
    assert.doesNotMatch(newAPITone, /bg-pink-/)
    assert.match(otherTone, /border-l-amber-500/)
    assert.match(unknownTone, /border-l-muted-foreground/)
    assert.notEqual(sub2APITone, newAPITone)
    assert.notEqual(otherTone, newAPITone)
    assert.notEqual(unknownTone, newAPITone)
    assert.notEqual(unknownTone, sub2APITone)
  })
})

describe('upstream key management display values', () => {
  test('uses the explicit linked-channel status and keeps legacy snapshots compatible', () => {
    assert.equal(
      getUpstreamKeyInUseStatus({
        imported: false,
        active: false,
        in_use_status: 'auto_disabled',
      }),
      'auto_disabled'
    )
    assert.equal(
      getUpstreamKeyInUseStatus({ imported: false, active: false }),
      'unlinked'
    )
    assert.equal(
      getUpstreamKeyInUseStatus({ imported: true, active: false }),
      'disabled'
    )
    assert.equal(
      getUpstreamKeyInUseStatus({ imported: true, active: true }),
      'enabled'
    )
    assert.equal(
      getUpstreamKeyInUseStatus({
        imported: false,
        linked: true,
        active: false,
      }),
      'disabled'
    )
    assert.equal(
      getUpstreamKeyInUseStatus({
        imported: true,
        linked: false,
        active: true,
      }),
      'unlinked'
    )
  })

  test('builds provider-specific group choices with the effective multiplier', () => {
    const newAPIChannel = createChannel(1, 10, 'ready', 1.5)
    const newAPISnapshot = {
      provider: 'new-api' as const,
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [],
      groups: [
        { name: 'cloud', ratio: 0.03 },
        { name: 'default', ratio: 1 },
      ],
      ratios: { cloud: 0.03, default: 1 },
      retrieved_at: 0,
    }

    assert.deepEqual(
      getUpstreamKeyGroupOptions(newAPIChannel, newAPISnapshot),
      [
        {
          value: 'cloud',
          name: 'cloud',
          ratio: 0.045,
          request: { group: 'cloud' },
        },
        {
          value: 'default',
          name: 'default',
          ratio: 1.5,
          request: { group: 'default' },
        },
      ]
    )

    const sub2APIChannel = createChannel(2, 10, 'ready', 1)
    sub2APIChannel.provider = 'sub2api'
    const sub2APISnapshot = {
      ...newAPISnapshot,
      provider: 'sub2api' as const,
      groups: [{ id: 24, name: 'cloud', ratio: 0.05 }],
      ratios: { '24': 0.05 },
    }
    assert.deepEqual(
      getUpstreamKeyGroupOptions(sub2APIChannel, sub2APISnapshot),
      [
        {
          value: '24',
          name: 'cloud',
          ratio: 0.05,
          request: { group_id: 24 },
        },
      ]
    )
  })

  test('uses the detected provider for auto channels without mixing request fields', () => {
    const autoChannel = createChannel(1, 10, 'ready', 1)
    autoChannel.provider = 'auto'
    const baseSnapshot = {
      balance: 10,
      account: { id: 1, username: 'root', balance: 10 },
      keys: [],
      retrieved_at: 0,
    }

    assert.deepEqual(
      getUpstreamKeyGroupOptions(autoChannel, {
        ...baseSnapshot,
        provider: 'new-api',
        groups: [{ id: 24, name: 'cloud', ratio: 0.045 }],
        ratios: { cloud: 0.045 },
      }),
      [
        {
          value: 'cloud',
          name: 'cloud',
          ratio: 0.045,
          request: { group: 'cloud' },
        },
      ]
    )

    assert.deepEqual(
      getUpstreamKeyGroupOptions(autoChannel, {
        ...baseSnapshot,
        provider: 'sub2api',
        groups: [{ id: 24, name: 'cloud', ratio: 0.045 }],
        ratios: { '24': 0.045 },
      }),
      [
        {
          value: '24',
          name: 'cloud',
          ratio: 0.045,
          request: { group_id: 24 },
        },
      ]
    )
  })
})
