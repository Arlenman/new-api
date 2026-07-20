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
  Copy,
  Download,
  Eye,
  EyeOff,
  LoaderCircle,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { importManagedUpstreamKeys, revealManagedUpstreamKey } from '../api'
import {
  formatUpstreamBalance,
  getEffectiveUpstreamMultiplier,
  hasUsableUpstreamCredentials,
} from '../lib'
import type {
  UpstreamChannel,
  UpstreamKeyImportConfiguration,
  UpstreamSnapshot,
} from '../types'
import { UpstreamKeyImportDialog } from './upstream-key-import-dialog'

interface UpstreamKeysTableProps {
  channel: UpstreamChannel
  snapshot: UpstreamSnapshot
  refreshing: boolean
  onRefresh: (channel: UpstreamChannel) => void
  onImported: () => Promise<void>
}

export function UpstreamKeysTable({
  channel,
  snapshot,
  refreshing,
  onRefresh,
  onImported,
}: UpstreamKeysTableProps) {
  const { t } = useTranslation()
  const [revealedKeys, setRevealedKeys] = useState<Record<number, string>>({})
  const [revealingKeyId, setRevealingKeyId] = useState<number | null>(null)
  const [selectedKeyIds, setSelectedKeyIds] = useState<Set<number>>(new Set())
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importing, setImporting] = useState(false)
  const keys = snapshot.keys
  const hasUsableCredentials = hasUsableUpstreamCredentials(
    channel.provider,
    channel.auth_type,
    channel.username,
    '',
    channel.has_password
  )
  const allSelected = keys.length > 0 && selectedKeyIds.size === keys.length
  const someSelected = selectedKeyIds.size > 0 && !allSelected
  const selectedKeyIdList = useMemo(
    () => [...selectedKeyIds].sort((left, right) => left - right),
    [selectedKeyIds]
  )

  useEffect(() => {
    const availableIds = new Set(keys.map((key) => key.id))
    setSelectedKeyIds((current) => {
      const next = new Set(
        [...current].filter((keyId) => availableIds.has(keyId))
      )
      return next.size === current.size ? current : next
    })
  }, [keys])

  async function revealKey(keyId: number) {
    if (revealedKeys[keyId]) {
      setRevealedKeys((current) => {
        const next = { ...current }
        delete next[keyId]
        return next
      })
      return
    }

    setRevealingKeyId(keyId)
    try {
      const response = await revealManagedUpstreamKey(channel.id, keyId)
      if (!response.success || !response.data?.key) {
        toast.error(response.message || t('Failed to reveal upstream key'))
        return
      }
      const fullKey = response.data.key
      setRevealedKeys((current) => ({ ...current, [keyId]: fullKey }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setRevealingKeyId(null)
    }
  }

  async function copyKey(keyId: number) {
    const key = revealedKeys[keyId]
    if (!key) return
    try {
      await navigator.clipboard.writeText(key)
      toast.success(t('Copied'))
    } catch {
      toast.error(t('Failed to copy'))
    }
  }

  function toggleKey(keyId: number, checked: boolean) {
    setSelectedKeyIds((current) => {
      const next = new Set(current)
      if (checked) next.add(keyId)
      else next.delete(keyId)
      return next
    })
  }

  function toggleAll(checked: boolean) {
    setSelectedKeyIds(checked ? new Set(keys.map((key) => key.id)) : new Set())
  }

  async function importSelectedKeys(
    configuration: UpstreamKeyImportConfiguration
  ) {
    if (selectedKeyIds.size === 0) return
    setImporting(true)
    try {
      const response = await importManagedUpstreamKeys(channel.id, {
        key_ids: selectedKeyIdList,
        ...configuration,
      })
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to import upstream keys'))
        return
      }
      await onImported()
      setImportDialogOpen(false)
      setSelectedKeyIds(new Set())
      toast.success(
        t(
          'Import completed: {{imported}} imported, {{updated}} overwritten, {{disabled}} disabled',
          {
            imported: response.data.imported,
            updated: response.data.updated,
            disabled: response.data.disabled,
          }
        )
      )
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to import upstream keys')
      )
    } finally {
      setImporting(false)
    }
  }

  return (
    <>
      <div className='space-y-1.5'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <p className='text-muted-foreground text-sm'>
            {keys.length === 0
              ? t('No upstream keys')
              : t('{{count}} keys selected', { count: selectedKeyIds.size })}
          </p>
          <div className='flex items-center gap-1'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => onRefresh(channel)}
              disabled={
                refreshing ||
                channel.provider === 'other' ||
                !hasUsableCredentials
              }
            >
              {refreshing ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <RefreshCw />
              )}
              {t('Refresh keys')}
            </Button>
            <Button
              onClick={() => setImportDialogOpen(true)}
              disabled={selectedKeyIds.size === 0 || importing}
            >
              <Download />
              {t('Import channels')}
            </Button>
          </div>
        </div>
        {keys.length > 0 && (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='h-9 w-10'>
                    <Checkbox
                      checked={allSelected}
                      indeterminate={someSelected}
                      onCheckedChange={(value) => toggleAll(!!value)}
                      aria-label={t('Select all keys')}
                    />
                  </TableHead>
                  <TableHead className='h-9'>{t('Name')}</TableHead>
                  <TableHead className='h-9'>{t('Key')}</TableHead>
                  <TableHead className='h-9'>{t('Group')}</TableHead>
                  <TableHead className='h-9'>{t('Multiplier')}</TableHead>
                  <TableHead className='h-9'>{t('Status')}</TableHead>
                  <TableHead className='h-9'>{t('Imported')}</TableHead>
                  <TableHead className='h-9 w-28 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody className='[&>tr]:h-11'>
                {keys.map((key) => {
                  const revealed = revealedKeys[key.id]
                  const keyGroup = key.group?.trim() || ''
                  const ratioKey =
                    key.group_id == null ? keyGroup : String(key.group_id)
                  const ratioGroup = snapshot.groups.find(
                    (group) =>
                      (key.group_id != null && group.id === key.group_id) ||
                      (!!keyGroup && group.name.trim() === keyGroup)
                  )
                  const groupName = ratioGroup?.name.trim() || keyGroup
                  const upstreamRatio =
                    snapshot.ratios[ratioKey] ?? ratioGroup?.ratio
                  const ratio =
                    typeof upstreamRatio === 'number'
                      ? upstreamRatio *
                        getEffectiveUpstreamMultiplier(channel.multiplier)
                      : undefined
                  let revealIcon = <Eye />
                  if (revealingKeyId === key.id) {
                    revealIcon = <LoaderCircle className='animate-spin' />
                  } else if (revealed) {
                    revealIcon = <EyeOff />
                  }
                  return (
                    <TableRow key={key.id}>
                      <TableCell className='py-1'>
                        <Checkbox
                          checked={selectedKeyIds.has(key.id)}
                          onCheckedChange={(value) =>
                            toggleKey(key.id, !!value)
                          }
                          aria-label={t('Select key')}
                        />
                      </TableCell>
                      <TableCell className='max-w-48 truncate py-1 font-medium'>
                        {key.name || '-'}
                      </TableCell>
                      <TableCell className='py-1 font-mono text-xs'>
                        {revealed || key.masked_key || '****'}
                      </TableCell>
                      <TableCell className='py-1'>
                        {groupName || key.group_id || '-'}
                      </TableCell>
                      <TableCell className='py-1'>
                        {typeof ratio === 'number'
                          ? `×${formatUpstreamBalance(ratio)}`
                          : '-'}
                      </TableCell>
                      <TableCell className='py-1'>
                        {key.status || '-'}
                      </TableCell>
                      <TableCell className='py-1'>
                        {key.imported ? t('Yes') : t('No')}
                      </TableCell>
                      <TableCell className='py-1'>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label={
                              revealed ? t('Hide key') : t('Reveal key')
                            }
                            onClick={() => void revealKey(key.id)}
                            disabled={revealingKeyId === key.id}
                          >
                            {revealIcon}
                          </Button>
                          {revealed && (
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Copy key')}
                              onClick={() => void copyKey(key.id)}
                            >
                              <Copy />
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
      <UpstreamKeyImportDialog
        channel={channel}
        selectedKeyIds={selectedKeyIdList}
        open={importDialogOpen}
        submitting={importing}
        onOpenChange={setImportDialogOpen}
        onSubmit={importSelectedKeys}
      />
    </>
  )
}
