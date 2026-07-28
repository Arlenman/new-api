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
import { FileTextIcon, ImageIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { PlaygroundImageFile } from '../../types'

type PlaygroundMessageAttachmentsProps = {
  attachments?: PlaygroundImageFile[]
}

function formatFileSize(size?: number): string {
  if (!size || size <= 0) {
    return ''
  }

  if (size < 1024) {
    return `${size} B`
  }

  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`
  }

  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function getAttachmentStatusLabel(
  attachment: PlaygroundImageFile,
  t: (key: string) => string
): string {
  if (!attachment.extractionStatus) {
    return ''
  }

  const labels: Record<
    NonNullable<PlaygroundImageFile['extractionStatus']>,
    string
  > = {
    pending: t('Reading'),
    complete: t('Readable text extracted'),
    empty: t('No readable text'),
    unsupported: t('Unsupported file'),
    error: t('Read failed'),
  }

  return labels[attachment.extractionStatus]
}

export function PlaygroundMessageAttachments({
  attachments = [],
}: PlaygroundMessageAttachmentsProps) {
  const { t } = useTranslation()
  const visibleAttachments = attachments.filter(
    (attachment) => attachment.url || attachment.filename
  )

  if (visibleAttachments.length === 0) {
    return null
  }

  return (
    <div className='mt-2 grid w-full min-w-0 gap-2'>
      {visibleAttachments.map((attachment, index) => {
        const isImage = attachment.mediaType?.startsWith('image/')
        const filename =
          attachment.filename || (isImage ? t('Image') : t('Attachment'))
        const fileSize = formatFileSize(attachment.size)
        const statusLabel = getAttachmentStatusLabel(attachment, t)
        const preview = (() => {
          if (isImage && attachment.url) {
            return (
              <img
                alt={filename}
                className='size-full object-cover'
                src={attachment.url}
              />
            )
          }

          if (isImage) {
            return <ImageIcon className='size-4' />
          }

          return <FileTextIcon className='size-4' />
        })()

        return (
          <div
            className='border-border/65 bg-background/75 flex min-w-0 items-center gap-2 rounded-md border p-2 text-left shadow-xs'
            key={`${filename}-${attachment.url ?? index}`}
          >
            <div
              className={cn(
                'bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-md',
                isImage && 'bg-background'
              )}
            >
              {preview}
            </div>
            <div className='min-w-0 flex-1'>
              <div className='truncate text-sm font-medium'>{filename}</div>
              <div className='text-muted-foreground flex min-w-0 flex-wrap gap-x-2 gap-y-0.5 text-xs'>
                {attachment.mediaType && (
                  <span className='truncate'>{attachment.mediaType}</span>
                )}
                {fileSize && <span>{fileSize}</span>}
                {statusLabel && <span>{statusLabel}</span>}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
