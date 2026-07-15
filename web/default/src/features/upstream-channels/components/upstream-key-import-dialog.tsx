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
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, LoaderCircle } from 'lucide-react'
import {
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getGroups } from '@/features/channels/api'
import { ChannelsProvider } from '@/features/channels/components/channels-provider'
import { FetchModelsDialog } from '@/features/channels/components/dialogs/fetch-models-dialog'
import { cn } from '@/lib/utils'

import { fetchManagedUpstreamKeyModels } from '../api'
import { getUpstreamImportDefaults } from '../lib'
import type { UpstreamChannel, UpstreamKeyImportConfiguration } from '../types'

const FIELD_MAX_LENGTH = 255
const GROUP_STORAGE_MAX_LENGTH = 64
const NUMBER_MIN = -2147483648
const NUMBER_MAX = 2147483647

interface UpstreamKeyImportDialogProps {
  channel: UpstreamChannel
  selectedKeyIds: number[]
  open: boolean
  submitting: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (configuration: UpstreamKeyImportConfiguration) => Promise<void>
}

export function UpstreamKeyImportDialog({
  channel,
  selectedKeyIds,
  open,
  submitting,
  onOpenChange,
  onSubmit,
}: UpstreamKeyImportDialogProps) {
  const { t } = useTranslation()
  const [groups, setGroups] = useState<string[]>(['default'])
  const [tag, setTag] = useState('')
  const [namePrefix, setNamePrefix] = useState('')
  const [priority, setPriority] = useState('0')
  const [weight, setWeight] = useState('0')
  const [testModel, setTestModel] = useState('')
  const [models, setModels] = useState<string[] | undefined>()
  const [modelsDialogOpen, setModelsDialogOpen] = useState(false)
  const [autoBan, setAutoBan] = useState(true)
  const [remark, setRemark] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const groupsQuery = useQuery({
    queryKey: ['channel-groups'],
    queryFn: async () => {
      const response = await getGroups()
      if (!response.success) {
        throw new Error(response.message || t('Failed to load groups'))
      }
      return response.data || []
    },
    enabled: open,
    staleTime: 60_000,
  })

  const groupOptions = useMemo(() => {
    const values = new Set(['default', ...(groupsQuery.data || []), ...groups])
    return [...values].map((group) => ({ label: group, value: group }))
  }, [groups, groupsQuery.data])

  const selectedCount = selectedKeyIds.length
  const fetchModels = useCallback(async () => {
    const response = await fetchManagedUpstreamKeyModels(
      channel.id,
      selectedKeyIds
    )
    if (!response.success) {
      throw new Error(response.message || t('Failed to fetch models'))
    }
    return response.data || []
  }, [channel.id, selectedKeyIds, t])

  useEffect(() => {
    if (!open) return
    const defaults = getUpstreamImportDefaults(channel)
    setGroups(['default'])
    setTag(defaults.tag)
    setNamePrefix(defaults.namePrefix)
    setPriority(String(channel.priority ?? 0))
    setWeight('0')
    setTestModel('')
    setModels(undefined)
    setModelsDialogOpen(false)
    setAutoBan(true)
    setRemark('')
    setAdvancedOpen(false)
  }, [channel, open])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalizedGroups = groups
      .map((group) => group.trim())
      .filter(
        (group, index, values) => group && values.indexOf(group) === index
      )
    const normalizedNamePrefix = namePrefix.trim()
    const normalizedPriority = Number(priority)
    const normalizedWeight = Number(weight)

    if (normalizedGroups.length === 0) {
      toast.error(t('Select at least one group'))
      return
    }
    if ([...normalizedGroups.join(',')].length > GROUP_STORAGE_MAX_LENGTH) {
      toast.error(t('Channel groups must not exceed 64 characters'))
      return
    }
    if (!normalizedNamePrefix) {
      toast.error(t('Name prefix is required'))
      return
    }
    if (
      !Number.isInteger(normalizedPriority) ||
      normalizedPriority < NUMBER_MIN ||
      normalizedPriority > NUMBER_MAX
    ) {
      toast.error(
        t('Priority must be an integer between -2147483648 and 2147483647')
      )
      return
    }
    if (
      !Number.isInteger(normalizedWeight) ||
      normalizedWeight < 0 ||
      normalizedWeight > NUMBER_MAX
    ) {
      toast.error(t('Weight must be an integer between 0 and 2147483647'))
      return
    }

    await onSubmit({
      groups: normalizedGroups,
      tag: tag.trim(),
      name_prefix: normalizedNamePrefix,
      priority: normalizedPriority,
      weight: normalizedWeight,
      test_model: testModel.trim(),
      ...(models === undefined ? {} : { models }),
      auto_ban: autoBan ? 1 : 0,
      remark: remark.trim(),
    })
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (submitting) return
    if (!nextOpen) setModelsDialogOpen(false)
    onOpenChange(nextOpen)
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>{t('Import channels')}</DialogTitle>
            <DialogDescription>
              {t('Configure settings for {{count}} selected upstream keys', {
                count: selectedCount,
              })}
            </DialogDescription>
          </DialogHeader>

          <div className='bg-muted/50 rounded-lg border px-3 py-2'>
            <div className='text-muted-foreground text-xs'>{t('Base URL')}</div>
            <div className='mt-0.5 font-mono text-xs break-all'>
              {channel.base_url}
            </div>
          </div>

          <form className='space-y-4' onSubmit={handleSubmit}>
            <fieldset className='space-y-4' disabled={submitting}>
              <div className='space-y-1.5'>
                <Label htmlFor={`upstream-import-groups-${channel.id}`}>
                  {t('Groups')}
                </Label>
                <MultiSelect
                  id={`upstream-import-groups-${channel.id}`}
                  options={groupOptions}
                  selected={groups}
                  onChange={setGroups}
                  placeholder={t('Select groups')}
                  disabled={submitting || groupsQuery.isLoading}
                />
                {groupsQuery.isError && (
                  <p className='text-destructive text-xs'>
                    {groupsQuery.error instanceof Error
                      ? groupsQuery.error.message
                      : t('Failed to load groups')}
                  </p>
                )}
              </div>

              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-1.5'>
                  <Label htmlFor={`upstream-import-tag-${channel.id}`}>
                    {t('Channel tag')}
                  </Label>
                  <Input
                    id={`upstream-import-tag-${channel.id}`}
                    value={tag}
                    onChange={(event) => setTag(event.target.value)}
                    maxLength={FIELD_MAX_LENGTH}
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor={`upstream-import-prefix-${channel.id}`}>
                    {t('Name prefix')}
                  </Label>
                  <Input
                    id={`upstream-import-prefix-${channel.id}`}
                    value={namePrefix}
                    onChange={(event) => setNamePrefix(event.target.value)}
                    maxLength={FIELD_MAX_LENGTH}
                    required
                  />
                </div>
              </div>

              <div className='space-y-1.5 rounded-lg border p-3'>
                <div className='flex items-center justify-between gap-3'>
                  <div>
                    <Label>{t('Models')}</Label>
                    {models === undefined && (
                      <p className='text-muted-foreground mt-1 text-xs'>
                        {t(
                          'Models will be fetched automatically during import'
                        )}
                      </p>
                    )}
                    {models?.length === 0 && (
                      <p className='text-muted-foreground mt-1 text-xs'>
                        {t('No models selected')}
                      </p>
                    )}
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setModelsDialogOpen(true)}
                    disabled={submitting || selectedCount === 0}
                  >
                    {t('Fetch Models')}
                  </Button>
                </div>
                {models && models.length > 0 && (
                  <div className='mt-2 flex max-h-24 flex-wrap gap-1.5 overflow-y-auto pr-1'>
                    {models.map((model) => (
                      <Badge
                        key={model}
                        variant='outline'
                        className='max-w-full font-mono font-normal'
                        title={model}
                      >
                        <span className='truncate'>{model}</span>
                      </Badge>
                    ))}
                  </div>
                )}
              </div>

              <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                <CollapsibleTrigger
                  render={
                    <Button
                      type='button'
                      variant='ghost'
                      className='w-full justify-between px-2'
                    />
                  }
                >
                  <span>{t('Advanced settings')}</span>
                  <ChevronDown
                    className={cn(
                      'transition-transform',
                      advancedOpen && 'rotate-180'
                    )}
                  />
                </CollapsibleTrigger>
                <CollapsibleContent className='space-y-4 pt-3'>
                  <div className='grid gap-4 sm:grid-cols-2'>
                    <div className='space-y-1.5'>
                      <Label htmlFor={`upstream-import-priority-${channel.id}`}>
                        {t('Priority')}
                      </Label>
                      <Input
                        id={`upstream-import-priority-${channel.id}`}
                        type='number'
                        min={NUMBER_MIN}
                        max={NUMBER_MAX}
                        step='1'
                        value={priority}
                        onChange={(event) => setPriority(event.target.value)}
                        required
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <Label htmlFor={`upstream-import-weight-${channel.id}`}>
                        {t('Weight')}
                      </Label>
                      <Input
                        id={`upstream-import-weight-${channel.id}`}
                        type='number'
                        min='0'
                        max={NUMBER_MAX}
                        step='1'
                        value={weight}
                        onChange={(event) => setWeight(event.target.value)}
                        required
                      />
                    </div>
                  </div>

                  <div className='space-y-1.5'>
                    <Label htmlFor={`upstream-import-test-model-${channel.id}`}>
                      {t('Test model')}
                    </Label>
                    <Input
                      id={`upstream-import-test-model-${channel.id}`}
                      value={testModel}
                      onChange={(event) => setTestModel(event.target.value)}
                      maxLength={FIELD_MAX_LENGTH}
                      placeholder={t(
                        'Leave blank to use the first fetched model'
                      )}
                    />
                  </div>

                  <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                    <div className='space-y-1'>
                      <Label htmlFor={`upstream-import-auto-ban-${channel.id}`}>
                        {t('Auto disable')}
                      </Label>
                      <p className='text-muted-foreground text-xs'>
                        {t('Automatically disable the channel after failures')}
                      </p>
                    </div>
                    <Switch
                      id={`upstream-import-auto-ban-${channel.id}`}
                      checked={autoBan}
                      onCheckedChange={setAutoBan}
                    />
                  </div>

                  <div className='space-y-1.5'>
                    <Label htmlFor={`upstream-import-remark-${channel.id}`}>
                      {t('Remark')}
                    </Label>
                    <Textarea
                      id={`upstream-import-remark-${channel.id}`}
                      value={remark}
                      onChange={(event) => setRemark(event.target.value)}
                      maxLength={FIELD_MAX_LENGTH}
                      placeholder={t(
                        'Leave blank to generate an import source remark'
                      )}
                    />
                  </div>
                </CollapsibleContent>
              </Collapsible>
            </fieldset>

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => handleOpenChange(false)}
                disabled={submitting}
              >
                {t('Cancel')}
              </Button>
              <Button
                type='submit'
                disabled={submitting || selectedCount === 0}
              >
                {submitting && <LoaderCircle className='animate-spin' />}
                {submitting ? t('Importing...') : t('Import channels')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <ChannelsProvider>
        <FetchModelsDialog
          open={modelsDialogOpen}
          onOpenChange={setModelsDialogOpen}
          onModelsSelected={setModels}
          customFetcher={fetchModels}
          existingModelsOverride={models ?? []}
          channelName={channel.name}
        />
      </ChannelsProvider>
    </>
  )
}
