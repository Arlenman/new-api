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
import { XIcon, ZoomIn, ZoomOut } from 'lucide-react'
import { type PointerEvent, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'

import {
  DEFAULT_IMAGE_PREVIEW_ZOOM,
  MAX_IMAGE_PREVIEW_ZOOM,
  MIN_IMAGE_PREVIEW_ZOOM,
  getNextImagePreviewZoom,
  getPreviousImagePreviewZoom,
  getResetImagePreviewZoom,
} from './image-preview-utils'

type ImagePreviewDialogProps = {
  alt: string
  open: boolean
  src: string
  onOpenChange: (open: boolean) => void
}

export function ImagePreviewDialog({
  alt,
  open,
  src,
  onOpenChange,
}: ImagePreviewDialogProps) {
  const { t } = useTranslation()
  const [zoom, setZoom] = useState(DEFAULT_IMAGE_PREVIEW_ZOOM)
  const controlButtonClassName =
    'border-white/15 bg-white/10 text-white hover:bg-white/20 hover:text-white disabled:border-white/5 disabled:bg-white/5 disabled:text-white/35'

  useEffect(() => {
    if (open) {
      setZoom(DEFAULT_IMAGE_PREVIEW_ZOOM)
    }
  }, [open, src])

  const handleBackdropPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) {
      return
    }

    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        onPointerDown={handleBackdropPointerDown}
        className='!flex h-[100svh] max-h-[100svh] w-[100vw] max-w-[100vw] flex-col overflow-hidden rounded-none border-0 bg-black/90 p-0 text-white ring-0 backdrop-blur-sm sm:max-w-[100vw]'
      >
        <DialogTitle className='sr-only'>{t('Image Preview')}</DialogTitle>
        <DialogDescription className='sr-only'>
          {alt || t('Generated image')}
        </DialogDescription>

        <div className='pointer-events-none absolute inset-x-0 top-0 z-20 flex items-center justify-center px-4 py-3'>
          <div className='pointer-events-auto flex max-w-full items-center gap-1 rounded-full border border-white/10 bg-black/55 px-2 py-1.5 shadow-2xl backdrop-blur-md'>
            <Button
              aria-label={t('Zoom out')}
              className={controlButtonClassName}
              disabled={zoom <= MIN_IMAGE_PREVIEW_ZOOM}
              onClick={() =>
                setZoom((current) => getPreviousImagePreviewZoom(current))
              }
              size='icon-sm'
              type='button'
              variant='outline'
            >
              <ZoomOut className='size-4' />
            </Button>
            <Button
              aria-label={t('Reset zoom to 100%')}
              className={`${controlButtonClassName} min-w-14 px-2 tabular-nums`}
              disabled={zoom === DEFAULT_IMAGE_PREVIEW_ZOOM}
              onClick={() => setZoom(getResetImagePreviewZoom())}
              size='sm'
              type='button'
              variant='outline'
            >
              100%
            </Button>
            <Button
              aria-label={t('Zoom in')}
              className={controlButtonClassName}
              disabled={zoom >= MAX_IMAGE_PREVIEW_ZOOM}
              onClick={() =>
                setZoom((current) => getNextImagePreviewZoom(current))
              }
              size='icon-sm'
              type='button'
              variant='outline'
            >
              <ZoomIn className='size-4' />
            </Button>
            <span className='w-12 text-center text-xs tabular-nums text-white/80'>
              {Math.round(zoom * 100)}%
            </span>
          </div>
        </div>

        <DialogClose
          render={
            <Button
              aria-label={t('Close')}
              className='absolute top-3 right-3 z-20 border-white/15 bg-black/45 text-white shadow-2xl backdrop-blur-md hover:bg-white/15 hover:text-white'
              size='icon-sm'
              type='button'
              variant='outline'
            />
          }
        >
          <XIcon className='size-4' />
          <span className='sr-only'>{t('Close')}</span>
        </DialogClose>

        <div
          className='min-h-0 flex-1 overflow-auto'
          onPointerDown={handleBackdropPointerDown}
        >
          <div className='pointer-events-none flex min-h-full min-w-full items-center justify-center px-6 py-20'>
            <img
              alt={alt}
              className='pointer-events-auto block h-auto select-none rounded-lg object-contain shadow-2xl'
              draggable={false}
              src={src}
              style={{
                height: `calc((100svh - 8rem) * ${zoom})`,
                maxHeight: `calc((100svh - 8rem) * ${zoom})`,
                maxWidth: `calc((100vw - 4rem) * ${zoom})`,
              }}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
