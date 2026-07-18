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

import { logTypeSearchSchema } from './search-schema.ts'

describe('usage log type search schema', () => {
  test('preserves the sensitive request log type', () => {
    assert.deepEqual(logTypeSearchSchema.parse('8'), ['8'])
    assert.deepEqual(logTypeSearchSchema.parse(['8']), ['8'])
  })

  test('rejects unsupported log types without breaking the route', () => {
    assert.deepEqual(logTypeSearchSchema.parse('9'), [])
  })
})
