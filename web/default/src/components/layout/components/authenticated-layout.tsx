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
import { useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { AnimatedOutlet } from '@/components/page-transition'
import { SkipToMain } from '@/components/skip-to-main'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import { isImagePlaygroundPath } from '@/features/image-playground/lib/persistent-mount'
import { PersistentImagePlayground } from '@/features/image-playground/persistent-image-playground'
import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'

import { AppHeader } from './app-header'
import { AppSidebar } from './app-sidebar'

type AuthenticatedLayoutProps = {
  children?: React.ReactNode
}

export function AuthenticatedLayout(props: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'
  const activeTool = useRouterState({
    select: (state) => {
      const pathname = state.location.pathname
      if (isImagePlaygroundPath(pathname)) return 'image-playground'
      return null
    },
  })
  const [imagePlaygroundImmersive, setImagePlaygroundImmersive] =
    useState(false)

  useEffect(() => {
    if (activeTool === 'image-playground') return

    setImagePlaygroundImmersive(false)
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => undefined)
    }
  }, [activeTool])

  return (
    <LayoutProvider>
      <SearchProvider>
        <SidebarProvider defaultOpen={defaultOpen} className='flex-col'>
          {!imagePlaygroundImmersive && <SkipToMain />}
          {!imagePlaygroundImmersive && <AppHeader />}
          <div
            className={cn(
              'flex min-h-0 w-full flex-1',
              imagePlaygroundImmersive && 'h-svh'
            )}
          >
            {!imagePlaygroundImmersive && <AppSidebar />}
            <SidebarInset
              className={cn(
                '@container/content min-h-0 overflow-hidden',
                imagePlaygroundImmersive
                  ? 'h-svh w-full'
                  : [
                      'h-[calc(100svh-var(--app-header-height,0px))]',
                      'peer-data-[variant=inset]:h-[calc(100svh-var(--app-header-height,0px)-(var(--spacing)*4))]',
                    ]
              )}
            >
              <div className={activeTool ? 'hidden' : 'contents'}>
                {props.children ?? <AnimatedOutlet />}
              </div>
              <PersistentImagePlayground
                active={activeTool === 'image-playground'}
                immersive={imagePlaygroundImmersive}
                onImmersiveChange={setImagePlaygroundImmersive}
              />
            </SidebarInset>
          </div>
        </SidebarProvider>
      </SearchProvider>
    </LayoutProvider>
  )
}
