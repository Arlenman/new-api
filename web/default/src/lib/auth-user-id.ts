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

type AuthStorage = Pick<Storage, 'getItem' | 'removeItem' | 'setItem'>

const USER_ID_KEY = 'uid'
const USER_KEY = 'user'

function getBrowserStorage(): AuthStorage | null {
  if (typeof window === 'undefined') {
    return null
  }
  return window.localStorage
}

function normalizeUserId(value: unknown): string | null {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return String(value)
  }
  if (typeof value !== 'string') {
    return null
  }
  const trimmed = value.trim()
  return trimmed ? trimmed : null
}

export function readStoredUserId(
  storage: AuthStorage | null = getBrowserStorage()
): string | null {
  if (!storage) {
    return null
  }

  try {
    const explicitUserId = normalizeUserId(storage.getItem(USER_ID_KEY))
    if (explicitUserId) {
      return explicitUserId
    }

    const rawUser = storage.getItem(USER_KEY)
    if (!rawUser) {
      return null
    }

    const user = JSON.parse(rawUser) as { id?: unknown }
    const userId = normalizeUserId(user?.id)
    if (userId) {
      storage.setItem(USER_ID_KEY, userId)
    }
    return userId
  } catch {
    return null
  }
}

export function writeStoredUserId(
  userId: number | string,
  storage: AuthStorage | null = getBrowserStorage()
): void {
  if (!storage) {
    return
  }
  const normalized = normalizeUserId(userId)
  if (!normalized) {
    return
  }
  storage.setItem(USER_ID_KEY, normalized)
}

export function clearStoredAuthIdentity(
  storage: AuthStorage | null = getBrowserStorage()
): void {
  if (!storage) {
    return
  }
  storage.removeItem(USER_ID_KEY)
  storage.removeItem(USER_KEY)
}
