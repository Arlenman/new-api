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
import { useAuthStore } from '@/stores/auth-store'

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
  const userId = useAuthStore((state) => state.auth.user?.id)
  // Keep the current account's iframe mounted across route navigation, but
  // never keep an iframe alive after logout or an account switch.
  const [mountedUserId, setMountedUserId] = useState<number | null>(() =>
    props.active && userId ? userId : null
  )
  const shouldRender = shouldKeepImagePlaygroundMounted(
    mountedUserId,
    userId,
    props.active
  )

  useEffect(() => {
    setMountedUserId((current) => {
      if (!userId) return null
      if (props.active) return userId
      return current === userId ? current : null
    })
  }, [props.active, userId])

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
        key={userId}
        active={props.active}
        immersive={props.immersive}
        onImmersiveChange={props.onImmersiveChange}
      />
    </div>
  )
}
