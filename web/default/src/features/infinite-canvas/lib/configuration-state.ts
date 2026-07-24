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
import type { ManagedInfiniteCanvasConfiguration } from './configuration-storage'

export interface InfiniteCanvasAppliedConfiguration extends ManagedInfiniteCanvasConfiguration {
  tokenId: number | null
  revision: number
}

type InfiniteCanvasConfiguration = Omit<
  InfiniteCanvasAppliedConfiguration,
  'revision'
>

export function reconcileInfiniteCanvasConfiguration(
  current: InfiniteCanvasAppliedConfiguration | null,
  next: InfiniteCanvasConfiguration
): InfiniteCanvasAppliedConfiguration {
  if (current?.mode === next.mode && current.tokenId === next.tokenId) {
    return current
  }

  return {
    ...next,
    revision: (current?.revision ?? 0) + 1,
  }
}

export function isInfiniteCanvasInitialLoadPending(
  statusLoading: boolean,
  keysLoading: boolean,
  configuration: InfiniteCanvasAppliedConfiguration | null
): boolean {
  return statusLoading || (keysLoading && configuration === null)
}
