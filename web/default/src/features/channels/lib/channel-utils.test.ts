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
import { after, before, describe, test } from 'node:test'

import { createServer, type ViteDevServer } from 'vite'

import type { Channel } from '../types.ts'

type TagRowResult = Channel & {
  children: Channel[]
  enabledCount: number
}

let server: ViteDevServer
let aggregateChannelsByTag: (channels: Channel[]) => TagRowResult[]

function createChannel(
  id: number,
  status: number,
  overrides: Partial<Channel> = {}
): Channel {
  return {
    id,
    tag: 'shared-tag',
    status,
    used_quota: 0,
    response_time: 0,
    priority: 10,
    weight: 10,
    group: 'default',
    ...overrides,
  } as Channel
}

before(async () => {
  server = await createServer({
    configFile: false,
    root: process.cwd(),
    resolve: { alias: { '@': `${process.cwd()}/src` } },
    server: { middlewareMode: true, hmr: false },
    appType: 'custom',
    logLevel: 'silent',
  })
  const module = await server.ssrLoadModule(
    '/src/features/channels/lib/channel-utils.ts'
  )
  aggregateChannelsByTag = module.aggregateChannelsByTag
})

after(async () => {
  await server.close()
})

describe('channel tag aggregation', () => {
  test('tracks only enabled children for the active badge count', () => {
    const rows = aggregateChannelsByTag([
      createChannel(1, 2),
      createChannel(2, 1),
      createChannel(3, 3),
    ])

    assert.equal(rows.length, 1)
    assert.equal(rows[0].enabledCount, 1)
    assert.equal(rows[0].children.length, 3)
  })

  test('keeps a shared negative priority as the tag priority', () => {
    const rows = aggregateChannelsByTag([
      createChannel(1, 1, { priority: -1 }),
      createChannel(2, 1, { priority: -1 }),
    ])

    assert.equal(rows[0].priority, -1)
  })

  test('uses a mixed marker when tag children have different priorities', () => {
    const rows = aggregateChannelsByTag([
      createChannel(1, 1, { priority: -1 }),
      createChannel(2, 1, { priority: 8 }),
    ])

    assert.equal(rows[0].priority, null)
  })

  test('preserves server tag order and channel order within each tag', () => {
    const input = [
      createChannel(3, 1, { tag: 'high', priority: 30 }),
      createChannel(2, 1, { tag: 'high', priority: 10 }),
      createChannel(4, 1, { tag: 'low', priority: 5 }),
      createChannel(1, 1, { tag: 'low', priority: 1 }),
    ]

    const rows = aggregateChannelsByTag(input)

    assert.deepEqual(
      rows.map((row) => row.tag),
      ['high', 'low']
    )
    assert.deepEqual(
      rows.map((row) => row.children.map((child) => child.id)),
      [
        [3, 2],
        [4, 1],
      ]
    )
    assert.deepEqual(
      input.map((channel) => channel.id),
      [3, 2, 4, 1]
    )
  })
})
