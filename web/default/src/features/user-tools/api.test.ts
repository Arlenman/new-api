import assert from 'node:assert/strict'
import { test } from 'node:test'

import type { UserToolRuntimeSession } from './api.ts'

test('runtime-session token metadata includes its group and authoritative label', () => {
  const session = {
    credential: 'utrs_runtime-credential',
    expires_at: 1_700_000_000,
    token: {
      id: 42,
      name: 'stale-name',
      masked_key: 'sk-****abcd',
      group: 'default',
      display_label: 'Production key · default',
    },
  } satisfies UserToolRuntimeSession

  assert.equal(session.token.group, 'default')
  assert.equal(session.token.display_label, 'Production key · default')
  assert.match(session.credential, /^utrs_/)
})

test('runtime-session token contract does not expose a raw API key', () => {
  type TokenHasRawKey = 'key' extends keyof UserToolRuntimeSession['token']
    ? true
    : false
  const tokenHasRawKey: TokenHasRawKey = false

  assert.equal(tokenHasRawKey, false)
})
