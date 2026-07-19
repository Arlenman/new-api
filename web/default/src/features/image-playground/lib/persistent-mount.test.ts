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
  isImagePlaygroundPath,
  shouldKeepImagePlaygroundMounted,
} from './persistent-mount.ts'

describe('persistent image playground lifecycle', () => {
  test('does not load the tool before the route is visited', () => {
    assert.equal(shouldKeepImagePlaygroundMounted(false, false), false)
  })

  test('keeps the tool mounted after leaving the route', () => {
    let hasMounted = false

    hasMounted = shouldKeepImagePlaygroundMounted(hasMounted, true)
    assert.equal(hasMounted, true)

    hasMounted = shouldKeepImagePlaygroundMounted(hasMounted, false)
    assert.equal(hasMounted, true)
  })
})

describe('image playground route matching', () => {
  test('matches the image playground route and its trailing slash', () => {
    assert.equal(isImagePlaygroundPath('/image-playground'), true)
    assert.equal(isImagePlaygroundPath('/image-playground/'), true)
  })

  test('does not hide the normal outlet for other routes', () => {
    assert.equal(isImagePlaygroundPath('/playground'), false)
    assert.equal(isImagePlaygroundPath('/image-playground-old'), false)
  })
})
