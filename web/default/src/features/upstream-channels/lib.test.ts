import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  filterAndSortUpstreamChannels,
  getAdjustedUpstreamAmount,
  getEffectiveUpstreamMultiplier,
  getTotalAdjustedUpstreamBalance,
  getUpstreamAccessTokenRecommendation,
  getUpstreamCardTone,
  getUpstreamChannelKeyStats,
  getUpstreamChannelDefaultName,
  getUpstreamChannelDisplayName,
  getUpstreamImportBaseName,
  getUpstreamImportDefaults,
  getUpstreamSelectedGroupMultiplier,
  hasUsableUpstreamCredentials,
  isUpstreamTurnstileAccessTokenRequired,
  isValidUpstreamMultiplier,
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
    username: 'root',
    note: '',
    has_password: true,
    source_channel_count: 0,
    active_source_channel_count: activeSourceChannelCount,
    balance,
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
    assert.equal(hasUsableUpstreamCredentials('', '', false), false)
    assert.equal(hasUsableUpstreamCredentials('root', '', false), false)
    assert.equal(hasUsableUpstreamCredentials('', 'secret', false), false)
    assert.equal(hasUsableUpstreamCredentials('root', 'secret', false), true)
    assert.equal(hasUsableUpstreamCredentials('root', '', true), true)
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

  test('counts all snapshot keys and active imported local channels', () => {
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
          name: 'A',
          masked_key: 'sk-a',
          status: '1',
        },
        {
          id: 2,
          imported: false,
          active: false,
          name: 'B',
          masked_key: 'sk-b',
          status: '1',
        },
        {
          id: 3,
          imported: true,
          active: true,
          name: 'C',
          masked_key: 'sk-c',
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
        total: 4,
        active: 3,
      }
    )
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
