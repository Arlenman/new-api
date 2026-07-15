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

import { getReasoningEffortMeta } from './reasoning-effort.ts'

describe('reasoning effort display metadata', () => {
  test('maps standard effort levels to progressively stronger badge variants', () => {
    assert.deepEqual(getReasoningEffortMeta('none'), {
      label: 'none',
      variant: 'neutral',
    })
    assert.deepEqual(getReasoningEffortMeta('minimal'), {
      label: 'minimal',
      variant: 'green',
    })
    assert.deepEqual(getReasoningEffortMeta('low'), {
      label: 'low',
      variant: 'green',
    })
    assert.deepEqual(getReasoningEffortMeta('medium'), {
      label: 'medium',
      variant: 'yellow',
    })
    assert.deepEqual(getReasoningEffortMeta('high'), {
      label: 'high',
      variant: 'orange',
    })
    assert.deepEqual(getReasoningEffortMeta('xhigh'), {
      label: 'xhigh',
      variant: 'orange',
    })
    assert.deepEqual(getReasoningEffortMeta('max'), {
      label: 'max',
      variant: 'orange',
    })
  })

  test('trims values, handles case-insensitively, and preserves provider labels', () => {
    assert.deepEqual(getReasoningEffortMeta(' HIGH '), {
      label: 'HIGH',
      variant: 'orange',
    })
    assert.deepEqual(getReasoningEffortMeta('custom'), {
      label: 'custom',
      variant: 'green',
    })
    assert.equal(getReasoningEffortMeta('  '), null)
    assert.equal(getReasoningEffortMeta(undefined), null)
  })
})
