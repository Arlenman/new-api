import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { createPlaygroundId } from './playground-id-utils.ts'

describe('playground ids', () => {
  test('uses randomUUID when the browser provides it', () => {
    const id = createPlaygroundId({
      randomUUID: () => '11111111-2222-4333-8444-555555555555',
    })

    assert.equal(id, '11111111-2222-4333-8444-555555555555')
  })

  test('creates an RFC 4122 version 4 id when randomUUID is unavailable', () => {
    const id = createPlaygroundId({
      getRandomValues: (bytes) => {
        bytes.set([
          0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa,
          0xbb, 0xcc, 0xdd, 0xee, 0xff,
        ])
        return bytes
      },
    })

    assert.equal(id, '00112233-4455-4677-8899-aabbccddeeff')
  })
})
