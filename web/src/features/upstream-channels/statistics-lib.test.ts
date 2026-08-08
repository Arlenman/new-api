/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildUpstreamChannelRankingData,
  buildUpstreamChannelStatisticsSearchParams,
  buildUpstreamChannelTrendData,
  filterUpstreamChannelTrendData,
  getUpstreamChannelStatisticsTodayRange,
  sortUpstreamChannelStatisticsRows,
  toggleUpstreamChannelTrendSeriesVisibility,
} from './statistics-lib.ts'
import type {
  UpstreamChannelStatisticsItem,
  UpstreamChannelStatisticsTrendItem,
} from './types.ts'

function localTimestamp(
  year: number,
  monthIndex: number,
  day: number,
  hour = 12
): number {
  return Math.floor(new Date(year, monthIndex, day, hour).getTime() / 1000)
}

function createStatisticsRow(
  channelId: number,
  channelName: string,
  overrides: Partial<UpstreamChannelStatisticsItem> = {}
): UpstreamChannelStatisticsItem {
  return {
    channel_id: channelId,
    channel_name: channelName,
    base_url: `https://channel-${channelId}.example.com`,
    provider: 'new-api',
    quota: 0,
    token_used: 0,
    count: 0,
    last_used_at: 0,
    ...overrides,
  }
}

function createTrendRow(
  channelId: number,
  channelName: string,
  createdAt: number,
  overrides: Partial<UpstreamChannelStatisticsTrendItem> = {}
): UpstreamChannelStatisticsTrendItem {
  return {
    channel_id: channelId,
    channel_name: channelName,
    base_url: `https://channel-${channelId}.example.com`,
    provider: 'new-api',
    created_at: createdAt,
    quota: 0,
    token_used: 0,
    count: 0,
    ...overrides,
  }
}

describe('upstream channel statistics filters and ranking', () => {
  test('uses the complete local current day by default', () => {
    const now = new Date(2026, 6, 27, 14, 35, 22, 456)
    const range = getUpstreamChannelStatisticsTodayRange(now)

    assert.deepEqual(
      [
        range.start.getHours(),
        range.start.getMinutes(),
        range.start.getSeconds(),
        range.start.getMilliseconds(),
      ],
      [0, 0, 0, 0]
    )
    assert.deepEqual(
      [
        range.end.getHours(),
        range.end.getMinutes(),
        range.end.getSeconds(),
        range.end.getMilliseconds(),
      ],
      [23, 59, 59, 999]
    )
    assert.equal(range.start.getDate(), 27)
    assert.equal(range.end.getDate(), 27)
    assert.equal(now.getHours(), 14)
  })

  test('builds the statistics time-range query parameters', () => {
    const params = buildUpstreamChannelStatisticsSearchParams({
      startTimestamp: 1_700_000_000,
      endTimestamp: 1_700_086_399,
    })

    assert.deepEqual(
      [...params.entries()],
      [
        ['start_timestamp', '1700000000'],
        ['end_timestamp', '1700086399'],
      ]
    )
  })

  test('sorts numeric columns without mutating or dropping source rows', () => {
    const rows = [
      createStatisticsRow(1, 'Zero', { quota: 0 }),
      createStatisticsRow(2, 'Largest', { quota: 30 }),
      createStatisticsRow(3, 'Middle', { quota: 20 }),
    ]

    const sorted = sortUpstreamChannelStatisticsRows(rows, {
      key: 'quota',
      direction: 'desc',
    })

    assert.deepEqual(
      sorted.map((row) => row.channel_name),
      ['Largest', 'Middle', 'Zero']
    )
    assert.deepEqual(
      rows.map((row) => row.channel_name),
      ['Zero', 'Largest', 'Middle']
    )
    assert.equal(sorted.length, rows.length)
  })

  test('ranks the selected metric and calculates shares from charted rows', () => {
    const ranking = buildUpstreamChannelRankingData(
      [
        createStatisticsRow(1, 'Primary', { token_used: 300 }),
        createStatisticsRow(2, 'Backup', { token_used: 100 }),
      ],
      'token_used'
    )

    assert.equal(ranking.total, 400)
    assert.deepEqual(
      ranking.data.map((row) => ({
        label: row.label,
        value: row.value,
        share: row.share,
      })),
      [
        { label: 'Primary', value: 300, share: 0.75 },
        { label: 'Backup', value: 100, share: 0.25 },
      ]
    )
  })

  test('excludes zero values from chart data without removing list rows', () => {
    const rows = [
      createStatisticsRow(1, 'Used', { count: 5 }),
      createStatisticsRow(2, 'Unused', { count: 0 }),
    ]

    const ranking = buildUpstreamChannelRankingData(rows, 'count')
    const listRows = sortUpstreamChannelStatisticsRows(rows, {
      key: 'count',
      direction: 'desc',
    })

    assert.deepEqual(
      ranking.data.map((row) => row.channelName),
      ['Used']
    )
    assert.deepEqual(
      listRows.map((row) => row.channel_name),
      ['Used', 'Unused']
    )
  })

  test('disambiguates channels with the same display name', () => {
    const ranking = buildUpstreamChannelRankingData(
      [
        createStatisticsRow(11, 'Shared', { quota: 20 }),
        createStatisticsRow(12, 'Shared', { quota: 10 }),
      ],
      'quota'
    )

    assert.deepEqual(
      ranking.data.map((row) => row.label),
      ['Shared (#11)', 'Shared (#12)']
    )
  })
})

describe('upstream channel statistics trends', () => {
  test('aggregates daily values and fills missing days with zero', () => {
    const trend = buildUpstreamChannelTrendData(
      [
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 1, 9), {
          quota: 10,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 1, 18), {
          quota: 5,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 3), {
          quota: 20,
        }),
      ],
      'quota',
      'day',
      {
        startTimestamp: localTimestamp(2026, 6, 1, 0),
        endTimestamp: localTimestamp(2026, 6, 3, 23),
      }
    )

    assert.deepEqual(trend.periods, ['2026-07-01', '2026-07-02', '2026-07-03'])
    assert.deepEqual(
      trend.data.map((row) => [row.period, row.value]),
      [
        ['2026-07-01', 15],
        ['2026-07-02', 0],
        ['2026-07-03', 20],
      ]
    )
  })

  test('keeps a unique channel label stable across multiple trend points', () => {
    const trend = buildUpstreamChannelTrendData(
      [
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 1, 9), {
          quota: 10,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 2, 9), {
          quota: 20,
        }),
      ],
      'quota',
      'day',
      {
        startTimestamp: localTimestamp(2026, 6, 1, 0),
        endTimestamp: localTimestamp(2026, 6, 2, 23),
      }
    )

    assert.deepEqual(
      trend.series.map((series) => series.label),
      ['Primary']
    )
  })

  test('aggregates weeks from Monday and fills missing weeks with zero', () => {
    const trend = buildUpstreamChannelTrendData(
      [
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 1), {
          count: 2,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 5), {
          count: 3,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 13), {
          count: 7,
        }),
      ],
      'count',
      'week',
      {
        startTimestamp: localTimestamp(2026, 6, 1, 0),
        endTimestamp: localTimestamp(2026, 6, 19, 23),
      }
    )

    assert.deepEqual(trend.periods, ['2026-06-29', '2026-07-06', '2026-07-13'])
    assert.deepEqual(
      trend.data.map((row) => [row.period, row.value]),
      [
        ['2026-06-29', 5],
        ['2026-07-06', 0],
        ['2026-07-13', 7],
      ]
    )
  })

  test('aggregates monthly values and fills missing months with zero', () => {
    const trend = buildUpstreamChannelTrendData(
      [
        createTrendRow(1, 'Primary', localTimestamp(2026, 0, 20), {
          token_used: 40,
        }),
        createTrendRow(1, 'Primary', localTimestamp(2026, 2, 5), {
          token_used: 60,
        }),
      ],
      'token_used',
      'month',
      {
        startTimestamp: localTimestamp(2026, 0, 15, 0),
        endTimestamp: localTimestamp(2026, 2, 20, 23),
      }
    )

    assert.deepEqual(trend.periods, ['2026-01', '2026-02', '2026-03'])
    assert.deepEqual(
      trend.data.map((row) => [row.period, row.value]),
      [
        ['2026-01', 40],
        ['2026-02', 0],
        ['2026-03', 60],
      ]
    )
  })

  test('toggles series visibility without mutating the prior selection', () => {
    const trend = buildUpstreamChannelTrendData(
      [
        createTrendRow(1, 'Primary', localTimestamp(2026, 6, 1), {
          count: 2,
        }),
        createTrendRow(2, 'Backup', localTimestamp(2026, 6, 1), {
          count: 1,
        }),
      ],
      'count',
      'day',
      {
        startTimestamp: localTimestamp(2026, 6, 1, 0),
        endTimestamp: localTimestamp(2026, 6, 1, 23),
      }
    )
    const hidden = new Set<string>()
    const hiddenPrimary = toggleUpstreamChannelTrendSeriesVisibility(
      hidden,
      '1'
    )
    const filtered = filterUpstreamChannelTrendData(trend, hiddenPrimary)
    const shownAgain = toggleUpstreamChannelTrendSeriesVisibility(
      hiddenPrimary,
      '1'
    )

    assert.deepEqual([...hidden], [])
    assert.deepEqual([...hiddenPrimary], ['1'])
    assert.deepEqual(
      filtered.data.map((row) => row.seriesKey),
      ['2']
    )
    assert.deepEqual([...shownAgain], [])
  })
})
