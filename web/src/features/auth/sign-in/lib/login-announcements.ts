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
import type { SystemStatus } from '@/features/auth/types'
import type { AnnouncementItem } from '@/features/dashboard/types'

type AnnouncementSource = SystemStatus | null | undefined

function getStatusValue<T>(status: AnnouncementSource, key: string): T {
  return (status?.[key] ?? status?.data?.[key]) as T
}

export function getLoginAnnouncements(
  status: AnnouncementSource
): AnnouncementItem[] {
  const enabled = getStatusValue<boolean | undefined>(
    status,
    'announcements_enabled'
  )

  const announcements = getStatusValue<unknown>(status, 'announcements')
  const visibleAnnouncements =
    enabled !== false && Array.isArray(announcements)
      ? announcements.filter((item): item is AnnouncementItem => {
          if (!item || typeof item !== 'object') return false

          const content = (item as { content?: unknown }).content
          return typeof content === 'string' && content.trim().length > 0
        })
      : []

  if (visibleAnnouncements.length > 0) return visibleAnnouncements

  const legacyNotice = getStatusValue<unknown>(status, 'notice')
  if (typeof legacyNotice !== 'string' || legacyNotice.trim().length === 0) {
    return []
  }

  return [
    {
      id: 0,
      content: legacyNotice,
      type: 'default',
    },
  ]
}
