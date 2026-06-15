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
import { Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getAnnouncementColorClass } from '@/lib/colors'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Markdown } from '@/components/ui/markdown'
import { getLoginAnnouncements } from '../lib/login-announcements'

function AnnouncementDot(props: { type?: string }) {
  return (
    <span
      className={cn(
        'mt-1.5 inline-block size-2 shrink-0 rounded-full',
        getAnnouncementColorClass(props.type)
      )}
    />
  )
}

export function LoginAnnouncements() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const announcements = getLoginAnnouncements(status)

  if (announcements.length === 0) return null

  return (
    <section
      aria-label={t('Announcements')}
      className='bg-card/80 mx-auto w-full max-w-md rounded-lg border p-3 text-left shadow-sm'
    >
      <div className='flex items-center justify-center gap-2 text-sm font-medium'>
        <Megaphone className='text-muted-foreground size-4' />
        <span>{t('Announcements')}</span>
      </div>

      <div className='mt-2 space-y-3'>
        {announcements.map((item, index) => {
          const publishDate = item.publishDate
            ? new Date(item.publishDate)
            : null
          const absoluteTime =
            publishDate && !Number.isNaN(publishDate.getTime())
              ? formatDateTimeObject(publishDate)
              : ''
          const key = item.id ?? `${item.content}-${index}`

          return (
            <article
              key={key}
              className={cn(
                'flex items-start gap-3',
                index > 0 && 'border-border/60 border-t pt-3'
              )}
            >
              <AnnouncementDot type={item.type} />
              <div className='min-w-0 flex-1 space-y-1.5 text-sm'>
                <Markdown>{item.content}</Markdown>

                {item.extra ? (
                  <div className='text-muted-foreground text-xs'>
                    <Markdown>{item.extra}</Markdown>
                  </div>
                ) : null}

                {absoluteTime ? (
                  <time className='text-muted-foreground block text-xs'>
                    {absoluteTime}
                  </time>
                ) : null}
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}
