/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero
General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Loader } from '@/components/ai-elements/loader'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

import type { PlaygroundFileImageReference } from '../../lib/image/playground-image-utils'
import { PlaygroundImagePreviewDialog } from './playground-image-preview-dialog'

type PlaygroundFileImageProps = {
  image: PlaygroundFileImageReference
}

function shouldLoadImageAsBlob(url: string): boolean {
  return url.startsWith('/api/playground/files/')
}

export function PlaygroundFileImage({ image }: PlaygroundFileImageProps) {
  const { t } = useTranslation()
  const [objectUrl, setObjectUrl] = useState('')
  const [hasError, setHasError] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)

  useEffect(() => {
    let cancelled = false
    let createdUrl = ''

    setObjectUrl('')
    setHasError(false)

    if (!shouldLoadImageAsBlob(image.url)) {
      setObjectUrl(image.url)
      return () => {
        cancelled = true
      }
    }

    api
      .get<Blob>(image.url, {
        disableDuplicate: true,
        responseType: 'blob',
        skipErrorHandler: true,
      })
      .then((response) => {
        if (cancelled) {
          return
        }
        createdUrl = URL.createObjectURL(response.data)
        setObjectUrl(createdUrl)
      })
      .catch(() => {
        if (!cancelled) {
          setHasError(true)
        }
      })

    return () => {
      cancelled = true
      if (createdUrl) {
        URL.revokeObjectURL(createdUrl)
      }
    }
  }, [image.url])

  if (hasError) {
    return (
      <span className='border-border/70 text-muted-foreground my-4 inline-flex rounded-md border px-3 py-2 text-xs italic'>
        {image.alt || t('Image not available')}
      </span>
    )
  }

  if (!objectUrl) {
    return (
      <div className='border-border/70 bg-muted/30 flex h-40 max-h-[min(30svh,260px)] w-60 max-w-full items-center justify-center rounded-lg border'>
        <Loader />
      </div>
    )
  }

  return (
    <>
      <button
        aria-label={t('Open image preview')}
        className='border-border/70 bg-muted/10 inline-flex max-h-[min(30svh,260px)] max-w-[min(100%,15rem)] cursor-zoom-in self-start overflow-hidden rounded-lg border text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none'
        onClick={() => setPreviewOpen(true)}
        type='button'
      >
        <img
          alt={image.alt}
          className={cn(
            'block h-auto max-h-[min(30svh,260px)] w-auto max-w-full object-contain transition-opacity hover:opacity-95'
          )}
          loading='lazy'
          src={objectUrl}
        />
      </button>
      <PlaygroundImagePreviewDialog
        alt={image.alt}
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        src={objectUrl}
      />
    </>
  )
}
