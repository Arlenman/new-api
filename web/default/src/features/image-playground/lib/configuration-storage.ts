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
interface ImagePlaygroundConfigurationStorage {
  getItem(key: string): string | null
  removeItem(key: string): void
}

interface ImagePlaygroundPreferenceStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export const IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY =
  'new-api:image-playground:configuration'

const IMAGE_PLAYGROUND_STREAM_IMAGES_STORAGE_KEY_PREFIX =
  'new-api:image-playground:stream-images:user:'

export function getImagePlaygroundStreamImagesStorageKey(
  userId: number
): string {
  return `${IMAGE_PLAYGROUND_STREAM_IMAGES_STORAGE_KEY_PREFIX}${userId}`
}

export function readImagePlaygroundStreamImages(
  storage: Pick<ImagePlaygroundPreferenceStorage, 'getItem'>,
  userId: number
): boolean {
  if (!Number.isInteger(userId) || userId <= 0) return true

  try {
    const persistedValue = storage.getItem(
      getImagePlaygroundStreamImagesStorageKey(userId)
    )
    if (persistedValue === null) return true
    const streamImages: unknown = JSON.parse(persistedValue)
    return typeof streamImages === 'boolean' ? streamImages : true
  } catch {
    return true
  }
}

export function persistImagePlaygroundStreamImages(
  storage: Pick<ImagePlaygroundPreferenceStorage, 'setItem'>,
  userId: number,
  streamImages: boolean
): void {
  if (!Number.isInteger(userId) || userId <= 0) return

  try {
    storage.setItem(
      getImagePlaygroundStreamImagesStorageKey(userId),
      JSON.stringify(streamImages)
    )
  } catch {
    // The default remains enabled when browser storage is unavailable.
  }
}

export function resolveImagePlaygroundHostMode(
  storage: ImagePlaygroundConfigurationStorage
): 'new-api' {
  if (storage.getItem(IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY) !== null) {
    storage.removeItem(IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY)
  }
  return 'new-api'
}
