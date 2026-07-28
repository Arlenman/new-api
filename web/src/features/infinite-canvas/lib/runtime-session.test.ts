import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { requestInfiniteCanvasRuntimeSession } from './runtime-session.ts'

describe('infinite canvas runtime session selection', () => {
  test('requests a fresh scoped session for each selected account key', async () => {
    const calls: Array<{ tool: string; tokenId: number }> = []
    const createSession = async (tool: 'infinite-canvas', tokenId: number) => {
      calls.push({ tool, tokenId })
      return {
        success: true,
        data: {
          credential: `utrs_session_${tokenId}`,
          expires_at: 1_800_000_000_000 + tokenId,
          token: {
            name: `Key ${tokenId}`,
            group: tokenId === 1 ? 'A组' : 'B组',
            display_label: '',
          },
        },
      }
    }

    const first = await requestInfiniteCanvasRuntimeSession(
      createSession,
      1,
      'Unnamed API key'
    )
    const second = await requestInfiniteCanvasRuntimeSession(
      createSession,
      2,
      'Unnamed API key'
    )

    assert.deepEqual(calls, [
      { tool: 'infinite-canvas', tokenId: 1 },
      { tool: 'infinite-canvas', tokenId: 2 },
    ])
    assert.deepEqual(first, {
      credential: 'utrs_session_1',
      expiresAt: 1_800_000_000_001,
      displayLabel: 'Key 1 · A组',
    })
    assert.deepEqual(second, {
      credential: 'utrs_session_2',
      expiresAt: 1_800_000_000_002,
      displayLabel: 'Key 2 · B组',
    })
  })

  test('uses the authoritative runtime label and rejects non-runtime credentials', async () => {
    const valid = await requestInfiniteCanvasRuntimeSession(
      async () => ({
        success: true,
        data: {
          credential: '  utrs_scoped-session  ',
          expires_at: 1_800_000_000_000,
          token: {
            name: 'Stale name',
            group: 'Stale group',
            display_label: 'Account key · GPT组',
          },
        },
      }),
      7,
      'Unnamed API key'
    )

    assert.equal(valid.credential, 'utrs_scoped-session')
    assert.equal(valid.displayLabel, 'Account key · GPT组')
    await assert.rejects(
      requestInfiniteCanvasRuntimeSession(
        async () => ({
          success: true,
          data: {
            credential: 'sk-real-key-must-not-reach-iframe',
            expires_at: 1_800_000_000_000,
            token: { name: 'Unsafe key' },
          },
        }),
        7,
        'Unnamed API key'
      ),
      /Failed to create runtime credential/
    )
  })
})
