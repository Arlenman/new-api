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
import { ImageIcon, SparklesIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Shimmer } from '@/components/ai-elements/shimmer'
import { cn } from '@/lib/utils'

import type { Message } from '../../types'

type ImageGenerationProgressProps = {
  message: Message
}

const TEXT_STEPS = [
  'Composing the scene',
  'Generating details',
  'Rendering the image',
  'Almost ready',
]

const PLACEHOLDER_GRID_CELLS = Array.from({ length: 16 }, (_, index) => ({
  id: `placeholder-grid-cell-${index}`,
  isHighlighted: index % 3 === 0,
}))

function getAspectRatio(size?: string): string {
  if (!size || size === 'auto') {
    return '1 / 1'
  }

  const match = size.match(/^(\d+)x(\d+)$/)
  if (!match) {
    return '1 / 1'
  }

  return `${match[1]} / ${match[2]}`
}

export function ImageGenerationProgress({
  message,
}: ImageGenerationProgressProps) {
  const { t } = useTranslation()
  const [stepIndex, setStepIndex] = useState(0)
  const aspectRatio = useMemo(
    () => getAspectRatio(message.imageGeneration?.size),
    [message.imageGeneration?.size]
  )

  useEffect(() => {
    const timer = window.setInterval(() => {
      setStepIndex((currentIndex) => (currentIndex + 1) % TEXT_STEPS.length)
    }, 1800)

    return () => window.clearInterval(timer)
  }, [])

  return (
    <div className='w-full max-w-[15rem] py-2'>
      <div
        className='border-border/70 bg-muted/30 relative isolate overflow-hidden rounded-xl border shadow-sm'
        style={{ aspectRatio }}
      >
        <div className='absolute inset-0 bg-[radial-gradient(circle_at_25%_20%,color-mix(in_oklch,var(--primary)_18%,transparent),transparent_34%),radial-gradient(circle_at_70%_75%,color-mix(in_oklch,var(--accent-foreground)_10%,transparent),transparent_32%)]' />
        <div className='absolute inset-0 animate-[pulse_2.8s_ease-in-out_infinite] bg-[linear-gradient(110deg,transparent_0%,color-mix(in_oklch,var(--primary)_10%,transparent)_42%,color-mix(in_oklch,var(--primary)_20%,transparent)_50%,color-mix(in_oklch,var(--primary)_10%,transparent)_58%,transparent_100%)] bg-[length:220%_100%]' />
        <div className='absolute inset-x-0 top-0 h-px animate-[ping_2.4s_cubic-bezier(0,0,0.2,1)_infinite] bg-primary/60' />
        <div className='absolute inset-0 grid grid-cols-4 gap-px opacity-20'>
          {PLACEHOLDER_GRID_CELLS.map((cell) => (
            <div
              key={cell.id}
              className={cn(
                'bg-background/40',
                cell.isHighlighted && 'animate-pulse bg-primary/10'
              )}
            />
          ))}
        </div>
        <div className='absolute inset-0 flex items-center justify-center'>
          <div className='bg-background/85 ring-border/60 flex size-20 items-center justify-center rounded-full shadow-sm ring-1 backdrop-blur'>
            <ImageIcon className='text-primary size-10 animate-pulse' />
          </div>
        </div>
        <SparklesIcon className='text-primary/70 absolute top-5 right-6 size-4 animate-bounce' />
        <SparklesIcon className='text-primary/50 absolute bottom-7 left-7 size-3 animate-pulse' />
      </div>

      <div className='mt-3 flex items-center gap-2 text-sm'>
        <span className='bg-primary/80 size-2 animate-pulse rounded-full' />
        <Shimmer duration={1.4}>
          {t(TEXT_STEPS[stepIndex])}
        </Shimmer>
      </div>
    </div>
  )
}
