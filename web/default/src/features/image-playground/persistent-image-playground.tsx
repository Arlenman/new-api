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
import { useEffect, useState } from 'react'

import { cn } from '@/lib/utils'

import { ImagePlayground } from '.'
import { shouldKeepImagePlaygroundMounted } from './lib/persistent-mount'

type PersistentImagePlaygroundProps = {
  active: boolean
  immersive: boolean
  onImmersiveChange: (immersive: boolean) => void
}

export function PersistentImagePlayground(
  props: PersistentImagePlaygroundProps
) {
  // Image generation streams live inside the iframe. Keep it mounted after
  // the first visit so route navigation does not abort in-flight requests.
  const [hasMounted, setHasMounted] = useState(props.active)
  const shouldRender = shouldKeepImagePlaygroundMounted(
    hasMounted,
    props.active
  )

  useEffect(() => {
    if (props.active) setHasMounted(true)
  }, [props.active])

  if (!shouldRender) return null

  return (
    <div
      data-slot='persistent-image-playground'
      className={cn(
        'min-h-0 flex-1 flex-col',
        props.active ? 'flex' : 'hidden'
      )}
    >
      <ImagePlayground
        active={props.active}
        immersive={props.immersive}
        onImmersiveChange={props.onImmersiveChange}
      />
    </div>
  )
}
