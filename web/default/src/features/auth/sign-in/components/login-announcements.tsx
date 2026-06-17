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
import { useTranslation } from 'react-i18next'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Markdown } from '@/components/ui/markdown'
import { getLoginAnnouncements } from '../lib/login-announcements'

type LoginAnnouncementsProps = {
  className?: string
}

export function LoginAnnouncements(props: LoginAnnouncementsProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const announcements = getLoginAnnouncements(status)

  if (announcements.length === 0) return null

  return (
    <section
      aria-label={t('Announcements')}
      data-login-announcements
      className={cn(
        'bg-card/80 mx-auto w-full rounded-lg border p-4 text-left shadow-sm',
        props.className
      )}
    >
      <div className='space-y-3'>
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
                'min-w-0 space-y-1.5 text-sm',
                index > 0 && 'border-border/60 border-t pt-3'
              )}
            >
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
            </article>
          )
        })}
      </div>
    </section>
  )
}
