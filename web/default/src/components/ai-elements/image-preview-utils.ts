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
export const MIN_IMAGE_PREVIEW_ZOOM = 0.5
export const MAX_IMAGE_PREVIEW_ZOOM = 3
export const DEFAULT_IMAGE_PREVIEW_ZOOM = 1
export const IMAGE_PREVIEW_ZOOM_STEP = 0.25

function clampImagePreviewZoom(zoom: number): number {
  return Math.min(MAX_IMAGE_PREVIEW_ZOOM, Math.max(MIN_IMAGE_PREVIEW_ZOOM, zoom))
}

export function getNextImagePreviewZoom(currentZoom: number): number {
  return clampImagePreviewZoom(currentZoom + IMAGE_PREVIEW_ZOOM_STEP)
}

export function getPreviousImagePreviewZoom(currentZoom: number): number {
  return clampImagePreviewZoom(currentZoom - IMAGE_PREVIEW_ZOOM_STEP)
}

export function getResetImagePreviewZoom(): number {
  return DEFAULT_IMAGE_PREVIEW_ZOOM
}
