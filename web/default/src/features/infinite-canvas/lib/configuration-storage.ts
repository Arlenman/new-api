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
interface InfiniteCanvasConfigurationStorage {
  removeItem(key: string): void
}

export interface ManagedInfiniteCanvasConfiguration {
  mode: 'new-api'
}

export const INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY =
  'new-api:infinite-canvas:configuration'

export function migrateInfiniteCanvasToManagedMode(
  storage: InfiniteCanvasConfigurationStorage
): ManagedInfiniteCanvasConfiguration {
  storage.removeItem(INFINITE_CANVAS_CONFIGURATION_STORAGE_KEY)
  return { mode: 'new-api' }
}
