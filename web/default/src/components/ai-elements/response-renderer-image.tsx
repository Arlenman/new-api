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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { sanitizeImageSrc, type ImageNode } from 'stream-markdown-parser'

import { ImagePreviewDialog } from './image-preview-dialog'

type ResponseImageProps = {
  node: ImageNode
}

export function ResponseImage(props: ResponseImageProps) {
  const { t } = useTranslation()
  const [hasError, setHasError] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const src = sanitizeImageSrc(props.node.src)
  const previewAlt = props.node.alt || t('Generated image')

  if (!src || hasError) {
    return (
      <span className='border-border/70 text-muted-foreground my-4 inline-flex rounded-md border px-3 py-2 text-xs italic'>
        {props.node.alt || t('Image not available')}
      </span>
    )
  }

  return (
    <>
      <button
        aria-label={t('Open image preview')}
        className='border-border/70 bg-muted/10 my-2 inline-flex max-h-[min(30svh,260px)] max-w-[min(100%,15rem)] cursor-zoom-in self-start overflow-hidden rounded-lg border text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none'
        onClick={() => setPreviewOpen(true)}
        type='button'
      >
        <img
          alt={props.node.alt}
          className='block h-auto max-h-[min(30svh,260px)] w-auto max-w-full object-contain transition-opacity hover:opacity-95'
          loading='lazy'
          onError={() => setHasError(true)}
          src={src}
          title={props.node.title ?? undefined}
        />
      </button>
      <ImagePreviewDialog
        alt={previewAlt}
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        src={src}
      />
    </>
  )
}
