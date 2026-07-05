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
import {
  Edit3Icon,
  MoreHorizontalIcon,
  PlusIcon,
  Trash2Icon,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'

import { DEFAULT_PLAYGROUND_SESSION_TITLE } from '../../lib'
import type { PlaygroundSession } from '../../types'

type PlaygroundSessionSidebarProps = {
  activeSessionId: string | null
  disabled?: boolean
  sessions: PlaygroundSession[]
  onCreateSession: () => void
  onDeleteSession: (sessionId: string) => void
  onRenameSession: (sessionId: string, title: string) => void
  onSelectSession: (sessionId: string) => void
}

export function PlaygroundSessionSidebar({
  activeSessionId,
  disabled,
  sessions,
  onCreateSession,
  onDeleteSession,
  onRenameSession,
  onSelectSession,
}: PlaygroundSessionSidebarProps) {
  const { t } = useTranslation()
  const [renameSession, setRenameSession] = useState<PlaygroundSession | null>(
    null
  )
  const [deleteSession, setDeleteSession] = useState<PlaygroundSession | null>(
    null
  )
  const [draftTitle, setDraftTitle] = useState('')

  useEffect(() => {
    setDraftTitle(renameSession?.title ?? '')
  }, [renameSession])

  const handleRename = () => {
    if (!renameSession) {
      return
    }

    onRenameSession(renameSession.id, draftTitle)
    setRenameSession(null)
  }

  const handleDelete = () => {
    if (!deleteSession) {
      return
    }

    onDeleteSession(deleteSession.id)
    setDeleteSession(null)
  }

  const getDisplayTitle = (title: string) =>
    title === DEFAULT_PLAYGROUND_SESSION_TITLE ? t(title) : title

  return (
    <aside className='bg-muted/20 border-border/70 flex min-h-0 w-full shrink-0 flex-col border-b md:w-64 md:border-r md:border-b-0'>
      <div className='border-border/70 flex shrink-0 items-center gap-2 border-b p-3'>
        <Button
          className='w-full justify-start'
          disabled={disabled}
          onClick={onCreateSession}
          variant='outline'
        >
          <PlusIcon className='size-4' />
          {t('New conversation')}
        </Button>
      </div>

      <ScrollArea className='min-h-0 flex-1'>
        <div className='flex gap-1 p-2 md:flex-col'>
          {sessions.map((session) => {
            const isActive = session.id === activeSessionId

            return (
              <div
                className={cn(
                  'group/session hover:bg-muted/80 flex min-w-48 items-center rounded-lg transition-colors md:min-w-0',
                  isActive && 'bg-muted text-foreground'
                )}
                key={session.id}
              >
                <button
                  className='min-w-0 flex-1 px-2.5 py-2 text-left text-sm'
                  disabled={disabled}
                  onClick={() => onSelectSession(session.id)}
                  type='button'
                >
                  <span className='block truncate'>
                    {getDisplayTitle(session.title)}
                  </span>
                  <span className='text-muted-foreground block truncate text-xs'>
                    {session.messages.length > 0
                      ? t('{{count}} messages', {
                          count: session.messages.length,
                        })
                      : t('No messages')}
                  </span>
                </button>

                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        aria-label={t('Conversation actions')}
                        className='mr-1 opacity-100 md:opacity-0 md:group-hover/session:opacity-100'
                        disabled={disabled}
                        size='icon-sm'
                        variant='ghost'
                      />
                    }
                  >
                    <MoreHorizontalIcon className='size-4' />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align='end'>
                    <DropdownMenuItem
                      disabled={disabled}
                      onClick={() => setRenameSession(session)}
                    >
                      <Edit3Icon className='mr-2 size-4' />
                      {t('Rename conversation')}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      disabled={disabled}
                      onClick={() => setDeleteSession(session)}
                    >
                      <Trash2Icon className='mr-2 size-4' />
                      {t('Delete conversation')}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            )
          })}
        </div>
      </ScrollArea>

      <Dialog
        open={Boolean(renameSession)}
        onOpenChange={(open) => !open && setRenameSession(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Rename conversation')}</DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            onChange={(event) => setDraftTitle(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                handleRename()
              }
            }}
            value={draftTitle}
          />
          <DialogFooter>
            <Button variant='outline' onClick={() => setRenameSession(null)}>
              {t('Cancel')}
            </Button>
            <Button onClick={handleRename}>{t('Save')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

	    <ConfirmDialog
	      destructive
	      desc={t(
	        'This conversation will be deleted from your account. This cannot be undone.'
	      )}
        confirmText={t('Delete')}
        handleConfirm={handleDelete}
        open={Boolean(deleteSession)}
        onOpenChange={(open) => !open && setDeleteSession(null)}
        title={t('Delete conversation?')}
      />
    </aside>
  )
}
