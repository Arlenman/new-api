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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const responseImageSource = readFileSync(
  new URL('./response-renderer-image.tsx', import.meta.url),
  'utf8'
)

describe('ResponseImage preview affordance', () => {
  test('renders Markdown images through a clickable zoom preview dialog', () => {
    assert.match(responseImageSource, /ImagePreviewDialog/)
    assert.match(responseImageSource, /Open image preview/)
    assert.match(responseImageSource, /cursor-zoom-in/)
    assert.doesNotMatch(responseImageSource, /mx-auto/)
    assert.match(responseImageSource, /max-h-\[min\(30svh,260px\)\]/)
    assert.match(responseImageSource, /max-w-\[min\(100%,15rem\)\]/)
    assert.doesNotMatch(responseImageSource, /max-h-\[min\(46svh,420px\)\]/)
    assert.doesNotMatch(responseImageSource, /max-w-\[min\(100%,26rem\)\]/)
    assert.doesNotMatch(responseImageSource, /max-h-\[min\(36svh,340px\)\]/)
    assert.doesNotMatch(responseImageSource, /max-w-\[min\(100%,20rem\)\]/)
    assert.match(responseImageSource, /h-auto/)
    assert.match(responseImageSource, /w-auto/)
    assert.match(responseImageSource, /object-contain/)
    assert.doesNotMatch(responseImageSource, /max-h-96/)
  })
})
