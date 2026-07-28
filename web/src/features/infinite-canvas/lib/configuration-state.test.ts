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
  isInfiniteCanvasInitialLoadPending,
  reconcileInfiniteCanvasConfiguration,
  type InfiniteCanvasAppliedConfiguration,
} from './configuration-state.ts'

describe('infinite canvas runtime configuration', () => {
  test('keeps the current revision when returning to the route with unchanged configuration', () => {
    const current: InfiniteCanvasAppliedConfiguration = {
      mode: 'new-api',
      tokenId: 57,
      revision: 3,
    }

    const next = reconcileInfiniteCanvasConfiguration(current, {
      mode: 'new-api',
      tokenId: 57,
    })

    assert.equal(next, current)
    assert.equal(next.revision, 3)
  })

  test('increments the revision only when the effective configuration changes', () => {
    const current: InfiniteCanvasAppliedConfiguration = {
      mode: 'new-api',
      tokenId: 57,
      revision: 3,
    }

    assert.deepEqual(
      reconcileInfiniteCanvasConfiguration(current, {
        mode: 'new-api',
        tokenId: 58,
      }),
      {
        mode: 'new-api',
        tokenId: 58,
        revision: 4,
      }
    )
  })

  test('shows the blocking loader only before the first configuration is available', () => {
    const configuration: InfiniteCanvasAppliedConfiguration = {
      mode: 'new-api',
      tokenId: 57,
      revision: 1,
    }

    assert.equal(isInfiniteCanvasInitialLoadPending(false, true, null), true)
    assert.equal(
      isInfiniteCanvasInitialLoadPending(false, true, configuration),
      false
    )
    assert.equal(
      isInfiniteCanvasInitialLoadPending(true, false, configuration),
      true
    )
  })
})
