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
import { test } from 'node:test'

import { loadAllApiKeyPages } from './all-api-keys.ts'
import type { ApiKey } from './types.ts'

function apiKey(id: number): ApiKey {
  return { id } as ApiKey
}

test('loadAllApiKeyPages loads every backend-capped page', async () => {
  const requestedPages: Array<{ page: number; pageSize: number }> = []
  const response = await loadAllApiKeyPages(async (page, pageSize) => {
    requestedPages.push({ page, pageSize })
    let pageItems: ApiKey[]
    if (page === 1) {
      pageItems = Array.from({ length: 100 }, (_, index) => apiKey(index + 1))
    } else if (page === 2) {
      pageItems = Array.from(
        { length: 100 },
        (_, index) => apiKey(index + 101)
      )
    } else {
      pageItems = [apiKey(201)]
    }

    return {
      success: true,
      data: {
        items: pageItems,
        total: 201,
        page,
        page_size: pageSize,
      },
    }
  })

  assert.equal(response.success, true)
  assert.equal(response.data?.items.length, 201)
  assert.deepEqual(
    response.data?.items.map((item) => item.id),
    Array.from({ length: 201 }, (_, index) => index + 1)
  )
  assert.deepEqual(requestedPages, [
    { page: 1, pageSize: 100 },
    { page: 2, pageSize: 100 },
    { page: 3, pageSize: 100 },
  ])
})
