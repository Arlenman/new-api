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
  Layers3,
  Link2,
  LoaderCircle,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import {
  importManagedUpstreamKeys,
  linkManagedUpstreamKeys,
  revealManagedUpstreamKey,
  updateManagedUpstreamKeyGroup,
} from '../api'
import {
  formatUpstreamBalance,
  getUpstreamKeyGroupOptions,
  getUpstreamKeyInUseStatus,
  hasUsableUpstreamCredentials,
} from '../lib'
import type {
  UpstreamChannel,
  UpstreamKey,
  UpstreamKeyImportConfiguration,
  UpstreamKeyInUseStatus,
  UpstreamSnapshot,
} from '../types'
import { UpstreamKeyImportDialog } from './upstream-key-import-dialog'

interface UpstreamKeysTableProps {
  channel: UpstreamChannel
  snapshot: UpstreamSnapshot
  refreshing: boolean
  onRefresh: (channel: UpstreamChannel) => void
  onChannelChanged: (channel?: UpstreamChannel) => Promise<void>
}

const inUseStatusPresentation: Record<
  UpstreamKeyInUseStatus,
  { label: string; className: string }
> = {
  unlinked: {
    label: 'Unlinked',
    className: 'text-muted-foreground',
  },
  enabled: {
    label: 'In-use enabled',
    className:
      'border-emerald-600/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  },
  disabled: {
    label: 'In-use disabled',
    className: 'text-muted-foreground bg-muted/40',
  },
  auto_disabled: {
    label: 'In-use auto disabled',
    className:
      'border-amber-600/40 bg-amber-500/10 text-amber-700 dark:text-amber-400',
  },
}

function getKeyGroupValue(
  snapshot: UpstreamSnapshot,
  key: UpstreamKey,
  effectiveProvider: UpstreamChannel['provider']
): string {
  if (effectiveProvider === 'sub2api') {
    if (key.group_id != null) return String(key.group_id)
    const groupName = key.group?.trim()
    const group = snapshot.groups.find(
      (item) => item.name.trim() === groupName && item.id != null
    )
    return group?.id == null ? '' : String(group.id)
  }
  return key.group?.trim() || ''
}

export function UpstreamKeysTable({
  channel,
  snapshot,
  refreshing,
  onRefresh,
  onChannelChanged,
}: UpstreamKeysTableProps) {
  const { t } = useTranslation()
  const [revealedKeys, setRevealedKeys] = useState<Record<number, string>>({})
  const [revealingKeyId, setRevealingKeyId] = useState<number | null>(null)
  const [selectedKeyIds, setSelectedKeyIds] = useState<Set<number>>(new Set())
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importing, setImporting] = useState(false)
  const [linking, setLinking] = useState(false)
  const [updatingGroupKeyIds, setUpdatingGroupKeyIds] = useState<Set<number>>(
    new Set()
  )
  const [pendingGroupValues, setPendingGroupValues] = useState<
    Record<number, string>
  >({})
  const keys = snapshot.keys
  const groupOptions = useMemo(
    () =>
      getUpstreamKeyGroupOptions(
        { multiplier: channel.multiplier, provider: channel.provider },
        snapshot
      ),
    [channel.multiplier, channel.provider, snapshot]
  )
  const effectiveProvider =
    channel.provider === 'auto' ? snapshot.provider : channel.provider
  const hasUsableCredentials = hasUsableUpstreamCredentials(
    effectiveProvider,
    channel.auth_type,
    channel.username,
    '',
    channel.has_password
  )
  let groupChangeDisabledReason = ''
  if (effectiveProvider === 'other') {
    groupChangeDisabledReason = t(
      'This upstream provider does not support changing key groups'
    )
  } else if (!hasUsableCredentials) {
    groupChangeDisabledReason = t(
      'Configure upstream credentials before changing key groups'
    )
  } else if (groupOptions.length === 0) {
    groupChangeDisabledReason = t('No upstream groups available')
  }
  const groupChangeDisabledReasonId = `upstream-key-group-disabled-reason-${channel.id}`
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

  async function linkKeys() {
    if (linking) return
    setLinking(true)
    try {
      const response = await linkManagedUpstreamKeys(channel.id)
      if (!response.success || !response.data?.channel.snapshot) {
        toast.error(
          response.message
            ? t(response.message)
            : t('Failed to link upstream keys')
        )
        return
      }
      await onChannelChanged(response.data.channel)
      const summary = response.data.summary
      toast.success(
        response.message
          ? t(response.message)
          : t(
              'Linked {{linked}} of {{total}} upstream keys: {{enabled}} enabled, {{autoDisabled}} auto disabled, {{disabled}} disabled, {{unlinked}} unlinked',
              {
                linked: summary.linked,
                total: summary.total,
                enabled: summary.enabled,
                autoDisabled: summary.auto_disabled,
                disabled: summary.disabled,
                unlinked: summary.unlinked,
              }
            )
      )
    } catch (error) {
      toast.error(
        error instanceof Error
          ? t(error.message)
          : t('Failed to link upstream keys')
      )
    } finally {
      setLinking(false)
    }
  }

  async function updateKeyGroup(key: UpstreamKey, nextValue: string) {
    if (updatingGroupKeyIds.has(key.id)) return
    const currentValue = getKeyGroupValue(snapshot, key, effectiveProvider)
    if (!nextValue || nextValue === currentValue) return
    const option = groupOptions.find((item) => item.value === nextValue)
    if (!option) {
      toast.error(t('The selected upstream group is no longer available'))
      return
    }

    setPendingGroupValues((current) => ({
      ...current,
      [key.id]: nextValue,
    }))
    setUpdatingGroupKeyIds((current) => new Set(current).add(key.id))
    try {
      const response = await updateManagedUpstreamKeyGroup(
        channel.id,
        key.id,
        option.request
      )
      if (!response.success || !response.data?.snapshot) {
        toast.error(
          response.message
            ? t(response.message)
            : t('Failed to update upstream key group')
        )
        return
      }
      await onChannelChanged(response.data)
      toast.success(t('Upstream key group updated'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? t(error.message)
          : t('Failed to update upstream key group')
      )
    } finally {
      setPendingGroupValues((current) => {
        const next = { ...current }
        delete next[key.id]
        return next
      })
      setUpdatingGroupKeyIds((current) => {
        const next = new Set(current)
        next.delete(key.id)
        return next
      })
    }
  }

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
      await onChannelChanged()
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
              onClick={() => void linkKeys()}
              disabled={
                linking ||
                effectiveProvider === 'other' ||
                !hasUsableCredentials
              }
              aria-busy={linking}
            >
              {linking ? <LoaderCircle className='animate-spin' /> : <Link2 />}
              {linking ? t('Linking upstream keys...') : t('Link')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              onClick={() => onRefresh(channel)}
              disabled={
                refreshing ||
                effectiveProvider === 'other' ||
                !hasUsableCredentials
              }
              aria-busy={refreshing}
            >
              {refreshing ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <RefreshCw />
              )}
              {t('Refresh keys')}
            </Button>
            <Button
              size='sm'
              onClick={() => setImportDialogOpen(true)}
              disabled={selectedKeyIds.size === 0 || importing}
              aria-busy={importing}
            >
              {importing ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <Download />
              )}
              {t('Import channels')}
            </Button>
          </div>
        </div>
        {groupChangeDisabledReason && keys.length > 0 && (
          <p
            id={groupChangeDisabledReasonId}
            className='text-muted-foreground text-xs'
          >
            {groupChangeDisabledReason}
          </p>
        )}
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
                  <TableHead className='h-9 min-w-52'>{t('Group')}</TableHead>
                  <TableHead className='h-9'>{t('Upstream status')}</TableHead>
                  <TableHead className='h-9'>{t('In-use status')}</TableHead>
                  <TableHead className='h-9 w-28 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody className='[&>tr]:h-11'>
                {keys.map((key) => {
                  const revealed = revealedKeys[key.id]
                  const keyAccessibleName =
                    key.name || key.masked_key || String(key.id)
                  const currentGroupValue = getKeyGroupValue(
                    snapshot,
                    key,
                    effectiveProvider
                  )
                  const selectedGroupValue =
                    pendingGroupValues[key.id] ?? currentGroupValue
                  const currentGroupOption = groupOptions.find(
                    (item) => item.value === currentGroupValue
                  )
                  const selectedGroupOption = groupOptions.find(
                    (item) => item.value === selectedGroupValue
                  )
                  const groupUpdating = updatingGroupKeyIds.has(key.id)
                  const inUseStatus = getUpstreamKeyInUseStatus(key)
                  const inUsePresentation = inUseStatusPresentation[inUseStatus]
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
                          aria-label={t('Select key {{name}}', {
                            name: keyAccessibleName,
                          })}
                        />
                      </TableCell>
                      <TableCell className='max-w-48 truncate py-1 font-medium'>
                        {key.name || '-'}
                      </TableCell>
                      <TableCell className='py-1 font-mono text-xs'>
                        {revealed || key.masked_key || '****'}
                      </TableCell>
                      <TableCell className='py-1'>
                        <div className='flex min-w-48 items-center gap-1.5'>
                          <Select<string>
                            value={selectedGroupValue || null}
                            disabled={
                              groupUpdating || !!groupChangeDisabledReason
                            }
                            onValueChange={(value) => {
                              if (value) void updateKeyGroup(key, value)
                            }}
                          >
                            <SelectTrigger
                              className='min-w-52 flex-1'
                              size='sm'
                              aria-label={t('Select group for {{name}}', {
                                name: keyAccessibleName,
                              })}
                              aria-describedby={
                                groupChangeDisabledReason
                                  ? groupChangeDisabledReasonId
                                  : undefined
                              }
                              aria-busy={groupUpdating}
                              title={
                                groupChangeDisabledReason || t('Select group')
                              }
                            >
                              <SelectValue className='min-w-0'>
                                <Layers3
                                  className='text-sky-600 dark:text-sky-400'
                                  aria-hidden='true'
                                />
                                <span className='min-w-0 truncate'>
                                  {selectedGroupOption?.name ||
                                    key.group?.trim() ||
                                    t('Select group')}
                                </span>
                                {selectedGroupOption && (
                                  <span className='text-muted-foreground ml-auto shrink-0 tabular-nums'>
                                    {formatUpstreamBalance(
                                      selectedGroupOption.ratio
                                    )}
                                    x
                                  </span>
                                )}
                              </SelectValue>
                            </SelectTrigger>
                            <SelectContent alignItemWithTrigger={false}>
                              <SelectGroup>
                                {!currentGroupOption && currentGroupValue && (
                                  <SelectItem value={currentGroupValue}>
                                    <Layers3
                                      className='text-sky-600 dark:text-sky-400'
                                      aria-hidden='true'
                                    />
                                    <span className='min-w-0 flex-1 truncate'>
                                      {key.group?.trim() || key.group_id || '-'}
                                    </span>
                                  </SelectItem>
                                )}
                                {groupOptions.map((option) => (
                                  <SelectItem
                                    key={option.value}
                                    value={option.value}
                                  >
                                    <Layers3
                                      className='text-sky-600 dark:text-sky-400'
                                      aria-hidden='true'
                                    />
                                    <span className='min-w-0 flex-1 truncate'>
                                      {option.name}
                                    </span>
                                    <span className='text-muted-foreground ml-auto shrink-0 tabular-nums'>
                                      {formatUpstreamBalance(option.ratio)}x
                                    </span>
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                          {groupUpdating && (
                            <LoaderCircle
                              className='text-muted-foreground size-4 shrink-0 animate-spin'
                              aria-label={t('Updating upstream key group...')}
                            />
                          )}
                        </div>
                      </TableCell>
                      <TableCell className='py-1'>
                        <span
                          role='status'
                          aria-label={t(key.active ? 'Active' : 'Disabled')}
                          className={cn(
                            'block size-2.5 rounded-full',
                            key.active
                              ? 'bg-emerald-500'
                              : 'bg-muted-foreground/40'
                          )}
                        />
                      </TableCell>
                      <TableCell className='py-1'>
                        <Badge
                          variant='outline'
                          className={cn(inUsePresentation.className)}
                        >
                          {t(inUsePresentation.label)}
                        </Badge>
                      </TableCell>
                      <TableCell className='py-1'>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label={
                              revealed
                                ? t('Hide key {{name}}', {
                                    name: keyAccessibleName,
                                  })
                                : t('Reveal key {{name}}', {
                                    name: keyAccessibleName,
                                  })
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
                              aria-label={t('Copy key {{name}}', {
                                name: keyAccessibleName,
                              })}
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
