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
import type { GetApiKeysResponse } from './types.ts'

const API_KEY_PAGE_SIZE = 100

type LoadApiKeyPage = (
  page: number,
  pageSize: number
) => Promise<GetApiKeysResponse>

export async function loadAllApiKeyPages(
  loadPage: LoadApiKeyPage
): Promise<GetApiKeysResponse> {
  const firstPage = await loadPage(1, API_KEY_PAGE_SIZE)
  if (!firstPage.success || !firstPage.data) return firstPage

  const items = [...firstPage.data.items]
  const pageCount = Math.ceil(firstPage.data.total / API_KEY_PAGE_SIZE)
  for (let page = 2; page <= pageCount; page += 1) {
    const response = await loadPage(page, API_KEY_PAGE_SIZE)
    if (!response.success || !response.data) return response
    items.push(...response.data.items)
  }

  return {
    ...firstPage,
    data: {
      ...firstPage.data,
      items,
    },
  }
}
