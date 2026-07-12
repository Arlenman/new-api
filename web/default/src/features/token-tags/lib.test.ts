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
  NO_TAG_FILTER_VALUE,
  TOKEN_TAGS_CONTENT_CLASS,
  TOKEN_TAGS_FIXED_CONTENT,
  buildKeyRankingChartData,
  buildTagRankingChartData,
  buildTokenKeyRows,
  buildTokenTagOptionNames,
  buildTokenTagSearchParams,
  formatTokenTagLastUsedAt,
  getTodayRange,
  groupTokenTagRows,
  sortTokenTagRows,
} from './lib.ts'

describe('token tag analytics helpers', () => {
  test('uses page-level scrolling for long ranking tables', () => {
    assert.equal(TOKEN_TAGS_FIXED_CONTENT, false)
    assert.doesNotMatch(TOKEN_TAGS_CONTENT_CLASS, /\boverflow-hidden\b/)
    assert.doesNotMatch(TOKEN_TAGS_CONTENT_CLASS, /\bh-full\b/)
  })

  test('groups rows by tag and keeps latest use time', () => {
    const rows = [
      {
        tag_id: 1,
        tag_name: 'Client A',
        token_id: 11,
        quota: 100,
        token_used: 40,
        count: 2,
        last_used_at: 1199,
      },
      {
        tag_id: 1,
        tag_name: 'Client A',
        token_id: 12,
        quota: 50,
        token_used: 20,
        count: 1,
        last_used_at: 1299,
      },
      {
        tag_id: 2,
        tag_name: 'Internal',
        token_id: 13,
        quota: 200,
        token_used: 80,
        count: 4,
        last_used_at: 1255,
      },
    ]

    const grouped = groupTokenTagRows(rows)

    assert.equal(grouped.length, 2)
    assert.equal(grouped[0].tag_name, 'Internal')
    assert.equal(grouped[0].quota, 200)
    assert.equal(grouped[1].tag_name, 'Client A')
    assert.equal(grouped[1].quota, 150)
    assert.equal(grouped[1].last_used_at, 1299)
  })

  test('sorts numeric columns without mutating source rows', () => {
    const rows = [
      { tag_id: 1, tag_name: 'A', token_id: 1, quota: 10, count: 3 },
      { tag_id: 2, tag_name: 'B', token_id: 2, quota: 30, count: 1 },
      { tag_id: 3, tag_name: 'C', token_id: 3, quota: 20, count: 2 },
    ]

    const sorted = sortTokenTagRows(rows, { key: 'quota', direction: 'desc' })

    assert.deepEqual(
      sorted.map((row) => row.tag_name),
      ['B', 'C', 'A']
    )
    assert.deepEqual(
      rows.map((row) => row.tag_name),
      ['A', 'B', 'C']
    )
  })

  test('formats missing and present last used timestamps', () => {
    assert.equal(formatTokenTagLastUsedAt(0), '-')
    assert.match(
      formatTokenTagLastUsedAt(1700000000),
      /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/
    )
  })

  test('builds tag options from API options, visible rows, and selected drafts', () => {
    const options = [{ id: 1, user_id: 1, name: '云起' }]
    const rows = [
      { tag_id: 2, tag_name: '多肉', token_id: 11 },
      { tag_id: 3, tag_name: '小川', token_id: 12 },
      { tag_id: 4, tag_name: '云起', token_id: 13 },
    ]

    const names = buildTokenTagOptionNames(options, rows, ['乐之', '小川'])

    assert.deepEqual(names, ['乐之', '云起', '多肉', '小川'])
  })
})

describe('token tag analytics filters and charts', () => {
  const modelRows = [
    {
      tag_id: 1,
      tag_name: 'Client A',
      user_id: 1,
      username: 'alice',
      token_id: 11,
      token_name: 'primary',
      model_name: 'gpt-a',
      quota: 40,
      token_used: 15,
      count: 1,
      last_used_at: 1100,
    },
    {
      tag_id: 1,
      tag_name: 'Client A',
      user_id: 1,
      username: 'alice',
      token_id: 11,
      token_name: 'primary',
      model_name: 'gpt-b',
      quota: 60,
      token_used: 25,
      count: 1,
      last_used_at: 1200,
    },
    {
      tag_id: 2,
      tag_name: 'Shared',
      user_id: 1,
      username: 'alice',
      token_id: 11,
      token_name: 'primary',
      model_name: 'gpt-a',
      quota: 40,
      token_used: 15,
      count: 1,
      last_used_at: 1100,
    },
    {
      tag_id: 2,
      tag_name: 'Shared',
      user_id: 1,
      username: 'alice',
      token_id: 11,
      token_name: 'primary',
      model_name: 'gpt-b',
      quota: 60,
      token_used: 25,
      count: 1,
      last_used_at: 1200,
    },
    {
      tag_id: 3,
      tag_name: 'Internal',
      user_id: 1,
      username: 'alice',
      token_id: 12,
      token_name: 'secondary',
      model_name: '',
      quota: 200,
      token_used: 40,
      count: 2,
      last_used_at: 1300,
    },
  ]

  test('uses the complete local current day by default', () => {
    const now = new Date(2026, 6, 12, 14, 35, 22, 456)
    const range = getTodayRange(now)

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
    assert.equal(range.start.getDate(), 12)
    assert.equal(range.end.getDate(), 12)
  })

  test('encodes included and excluded tags as repeated query parameters', () => {
    const params = buildTokenTagSearchParams({
      startTimestamp: 1000,
      endTimestamp: 2000,
      username: 'alice',
      includedTags: ['Client A', 'Internal'],
      excludedTags: ['Shared', 'Blocked'],
    })

    assert.deepEqual(params.getAll('token_tag'), ['Client A', 'Internal'])
    assert.deepEqual(params.getAll('exclude_token_tag'), ['Shared', 'Blocked'])
    assert.equal(params.get('username'), 'alice')
  })

  test('encodes untagged selections as boolean parameters instead of tag names', () => {
    const params = buildTokenTagSearchParams({
      startTimestamp: 1000,
      endTimestamp: 2000,
      includedTags: ['Client A', NO_TAG_FILTER_VALUE],
      excludedTags: ['Blocked', NO_TAG_FILTER_VALUE],
    })

    assert.deepEqual(params.getAll('token_tag'), ['Client A'])
    assert.deepEqual(params.getAll('exclude_token_tag'), ['Blocked'])
    assert.equal(params.get('include_untagged'), 'true')
    assert.equal(params.get('exclude_untagged'), 'true')
    assert.doesNotMatch(params.toString(), /__new_api_untagged__/)
  })

  test('aggregates model details into tag and key list rows', () => {
    const tagRows = groupTokenTagRows(modelRows)
    const keyRows = buildTokenKeyRows(modelRows)

    assert.equal(tagRows.length, 3)
    assert.equal(tagRows.find((row) => row.tag_name === 'Client A')?.quota, 100)
    assert.equal(
      tagRows.find((row) => row.tag_name === 'Client A')?.last_used_at,
      1200
    )
    assert.equal(keyRows.length, 3)
    assert.equal(keyRows.find((row) => row.tag_name === 'Shared')?.quota, 100)
  })

  test('builds stacked tag chart data and orders largest category last', () => {
    const chart = buildTagRankingChartData(modelRows, 'quota', {
      isAdmin: true,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })

    assert.deepEqual(
      chart.categories.map((item) => item.total),
      [100, 100, 200]
    )
    assert.equal(chart.categories.at(-1)?.tagName, 'Internal')
    assert.deepEqual(chart.models, ['gpt-a', 'gpt-b', 'Unknown model'])
    assert.equal(
      chart.data.find(
        (item) => item.modelName === 'gpt-b' && item.tagName === 'Client A'
      )?.value,
      60
    )
  })

  test('deduplicates key chart values across tags while preserving models and tag set', () => {
    const chart = buildKeyRankingChartData(modelRows, 'token_used', {
      isAdmin: true,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })

    assert.equal(chart.categories.length, 2)
    const primary = chart.categories.find((item) => item.tokenId === 11)
    assert.equal(primary?.total, 40)
    assert.deepEqual(primary?.tagNames, ['Client A', 'Shared'])
    assert.equal(chart.data.filter((item) => item.tokenId === 11).length, 2)
    assert.equal(chart.categories.at(-1)?.tokenId, 12)
  })

  test('switches metrics without changing stable model names and handles unknown values', () => {
    const quotaChart = buildTagRankingChartData(modelRows, 'quota', {
      isAdmin: false,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })
    const requestChart = buildTagRankingChartData(modelRows, 'count', {
      isAdmin: false,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })

    assert.deepEqual(quotaChart.models, requestChart.models)
    assert.equal(
      requestChart.categories.find((item) => item.tagName === 'Internal')
        ?.total,
      2
    )
    assert.equal(
      requestChart.data.find((item) => item.modelName === 'Unknown model')
        ?.value,
      2
    )
  })

  test('keeps same-name tags and keys separate by stable ids', () => {
    const rows = [
      {
        ...modelRows[0],
        user_id: 1,
        username: 'alice',
        tag_id: 10,
        token_id: 21,
        token_name: 'same',
      },
      {
        ...modelRows[0],
        user_id: 2,
        username: 'bob',
        tag_id: 20,
        token_id: 22,
        token_name: 'same',
      },
      {
        ...modelRows[0],
        user_id: 1,
        username: 'alice',
        tag_id: 10,
        token_id: 23,
        token_name: 'same',
      },
    ]
    const tags = buildTagRankingChartData(rows, 'quota', {
      isAdmin: true,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })
    const keys = buildKeyRankingChartData(rows, 'quota', {
      isAdmin: true,
      noTagLabel: 'No tags',
      unknownModelLabel: 'Unknown model',
    })

    assert.equal(tags.categories.length, 2)
    assert.equal(keys.categories.length, 3)
    assert.match(
      keys.categories.find((item) => item.tokenId === 21)?.label || '',
      /#21/
    )
    assert.match(
      keys.categories.find((item) => item.tokenId === 23)?.label || '',
      /#23/
    )
  })
})
