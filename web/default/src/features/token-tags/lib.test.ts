import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  TOKEN_TAGS_CONTENT_CLASS,
  TOKEN_TAGS_FIXED_CONTENT,
  buildTokenTagOptionNames,
  formatTokenTagLastUsedAt,
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
    assert.match(formatTokenTagLastUsedAt(1700000000), /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  test('builds tag options from API options, visible rows, and selected draft', () => {
    const options = [{ id: 1, user_id: 1, name: '云起' }]
    const rows = [
      { tag_id: 2, tag_name: '多肉', token_id: 11 },
      { tag_id: 3, tag_name: '小川', token_id: 12 },
      { tag_id: 4, tag_name: '云起', token_id: 13 },
    ]

    const names = buildTokenTagOptionNames(options, rows, '乐之')

    assert.deepEqual(names, ['乐之', '云起', '多肉', '小川'])
  })
})
