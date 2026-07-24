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
  AlertTriangle,
  Check,
  ExternalLink,
  KeyRound,
  LoaderCircle,
  Pencil,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { useEffect, useState, type FocusEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  formatUpstreamAvailability,
  formatUpstreamBalance,
  formatUpstreamFirstTokenLatency,
  formatUpstreamPricingInterval,
  formatUpstreamTime,
  getAdjustedUpstreamAmount,
  getEffectiveUpstreamMultiplier,
  getUpstreamAccessTokenRecommendation,
  getUpstreamModelPricingFields,
  getUpstreamSelectedGroupMultiplier,
  getUpstreamCardTone,
  getUpstreamChannelInUseKeyCount,
  getUpstreamChannelDisplayName,
  hasUsableUpstreamCredentials,
  isUpstreamTurnstileAccessTokenRequired,
} from '../lib'
import type {
  UpstreamChannel,
  UpstreamModel,
  UpstreamModelPricing,
  UpstreamSnapshot,
} from '../types'
import { UpstreamKeysTable } from './upstream-keys-table'

interface UpstreamChannelCardProps {
  channel: UpstreamChannel
  refreshing: boolean
  refreshingKeys: boolean
  refreshingGroups: boolean
  autoRefreshUpdating: boolean
  pinning: boolean
  selectingGroup: boolean
  savingDefaultTestModel: boolean
  deleting: boolean
  onConfigure: (channel: UpstreamChannel) => void
  onConfigureAccessToken: (channel: UpstreamChannel) => void
  onPin: (channel: UpstreamChannel) => void
  onDelete: (channel: UpstreamChannel) => void
  onRefresh: (channel: UpstreamChannel) => void
  onRefreshKeys: (channel: UpstreamChannel) => void
  onRefreshGroups: (channel: UpstreamChannel) => void
  onToggleAutoRefresh: (
    channel: UpstreamChannel,
    enabled: boolean
  ) => Promise<void>
  onSaveNote: (channel: UpstreamChannel, note: string) => Promise<void>
  onSaveDefaultTestModel: (
    channel: UpstreamChannel,
    defaultTestModel: string
  ) => Promise<boolean>
  onSelectGroup: (
    channel: UpstreamChannel,
    selectedGroup: string
  ) => Promise<void>
  onDataChanged: (channel?: UpstreamChannel) => Promise<void>
}

export function UpstreamChannelCard({
  channel,
  refreshing,
  refreshingKeys,
  refreshingGroups,
  autoRefreshUpdating,
  pinning,
  selectingGroup,
  savingDefaultTestModel,
  deleting,
  onConfigure,
  onConfigureAccessToken,
  onPin,
  onDelete,
  onRefresh,
  onRefreshKeys,
  onRefreshGroups,
  onToggleAutoRefresh,
  onSaveNote,
  onSaveDefaultTestModel,
  onSelectGroup,
  onDataChanged,
}: UpstreamChannelCardProps) {
  const { t } = useTranslation()
  const [cardOpen, setCardOpen] = useState(false)
  const [groupsOpen, setGroupsOpen] = useState(false)
  const [editingNote, setEditingNote] = useState(false)
  const [noteDraft, setNoteDraft] = useState(channel.note || '')
  const inUseKeyCount = getUpstreamChannelInUseKeyCount(channel)
  const [defaultTestModelDraft, setDefaultTestModelDraft] = useState(
    channel.default_test_model || ''
  )
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(
    channel.auto_refresh_interval > 0
  )
  const snapshot = channel.snapshot
  const displayName = getUpstreamChannelDisplayName(
    channel.name,
    channel.base_url
  )
  const adjustedBalance = getAdjustedUpstreamAmount(
    channel.balance,
    channel.multiplier
  )
  const selectedGroupMultiplier = getUpstreamSelectedGroupMultiplier(channel)
  let selectedGroupMultiplierValue = t('Unselected')
  if (selectedGroupMultiplier.status === 'valid') {
    selectedGroupMultiplierValue = formatUpstreamBalance(
      selectedGroupMultiplier.value
    )
  } else if (selectedGroupMultiplier.status === 'invalid') {
    selectedGroupMultiplierValue = t('Invalid')
  }
  const balanceIsLow =
    channel.balance_threshold > 0 && adjustedBalance < channel.balance_threshold
  const provider = snapshot?.provider || channel.provider
  const accessTokenRequired = isUpstreamTurnstileAccessTokenRequired(
    channel.last_error_code
  )
  const accessTokenRecommendation = accessTokenRequired
    ? getUpstreamAccessTokenRecommendation(channel)
    : null
  const recommendsSub2APIAccessToken =
    accessTokenRecommendation?.provider === 'sub2api'
  const hasUsableCredentials = hasUsableUpstreamCredentials(
    channel.provider,
    channel.auth_type,
    channel.username,
    '',
    channel.has_password
  )
  let statusLabel = t('Not configured')
  let statusVariant: 'outline' | 'default' | 'destructive' = 'outline'
  let statusClassName = ''
  if (channel.status === 'ready') {
    statusLabel = t('Ready')
    statusVariant = 'default'
    statusClassName =
      'border-emerald-600 bg-emerald-600 text-white hover:bg-emerald-600 dark:border-emerald-500 dark:bg-emerald-500 dark:text-emerald-950'
  } else if (channel.status === 'error') {
    statusLabel = t('Error')
    statusVariant = 'destructive'
  }

  useEffect(() => {
    if (!editingNote) setNoteDraft(channel.note || '')
  }, [channel.note, editingNote])

  useEffect(() => {
    if (!savingDefaultTestModel) {
      setDefaultTestModelDraft(channel.default_test_model || '')
    }
  }, [channel.default_test_model, savingDefaultTestModel])

  useEffect(() => {
    setAutoRefreshEnabled(channel.auto_refresh_interval > 0)
  }, [channel.auto_refresh_interval])

  async function saveNote() {
    const nextNote = noteDraft.trim()
    setEditingNote(false)
    setNoteDraft(nextNote)
    if (nextNote === (channel.note || '')) return
    await onSaveNote(channel, nextNote)
  }

  function cancelNoteEditing() {
    setNoteDraft(channel.note || '')
    setEditingNote(false)
  }

  async function saveDefaultTestModel(defaultTestModel: string) {
    const previousDefaultTestModel = channel.default_test_model || ''
    setDefaultTestModelDraft(defaultTestModel)
    if (editingNote) await saveNote()
    const saved = await onSaveDefaultTestModel(channel, defaultTestModel)
    if (!saved) setDefaultTestModelDraft(previousDefaultTestModel)
  }

  async function toggleAutoRefresh(enabled: boolean) {
    const previousEnabled = autoRefreshEnabled
    setAutoRefreshEnabled(enabled)
    try {
      await onToggleAutoRefresh(channel, enabled)
    } catch {
      setAutoRefreshEnabled(previousEnabled)
    }
  }

  const accountPanel = (
    <AccountPanel
      channel={channel}
      snapshot={snapshot}
      editingNote={editingNote}
      noteDraft={noteDraft}
      defaultTestModel={defaultTestModelDraft}
      onEditNote={() => setEditingNote(true)}
      onChangeNote={setNoteDraft}
      onBlurNote={(event) => {
        const nextTarget = event.relatedTarget
        if (
          nextTarget instanceof HTMLElement &&
          nextTarget.closest('[data-default-test-model-select="true"]')
        ) {
          return
        }
        void saveNote()
      }}
      onSaveDefaultTestModel={(defaultTestModel) =>
        void saveDefaultTestModel(defaultTestModel)
      }
      savingDefaultTestModel={savingDefaultTestModel}
      deleting={deleting}
      onCancelNote={cancelNoteEditing}
      onDelete={() => onDelete(channel)}
    />
  )

  return (
    <Card
      className={cn(
        'gap-0 rounded-none border-l-4 py-0',
        getUpstreamCardTone(provider)
      )}
    >
      <Collapsible open={cardOpen} onOpenChange={setCardOpen}>
        <CardHeader className='px-3 py-2'>
          <div className='flex items-center gap-2'>
            <CollapsibleTrigger
              className='focus-visible:ring-ring/50 flex min-w-0 flex-1 cursor-pointer flex-wrap items-center gap-1.5 rounded-sm text-left text-sm leading-snug font-medium outline-none focus-visible:ring-2 sm:text-base'
              aria-label={cardOpen ? t('Collapse') : t('Expand')}
            >
              <span className='truncate'>{displayName}</span>
              <Badge variant={statusVariant} className={statusClassName}>
                {statusLabel}
              </Badge>
              <Badge
                variant='outline'
                className='bg-background/70 h-6 gap-1.5 px-2'
              >
                <span className='text-muted-foreground'>{t('In use')}</span>
                <span className='font-semibold tabular-nums'>
                  {inUseKeyCount}
                </span>
              </Badge>
              <Badge
                variant='outline'
                className='bg-background/70 h-6 gap-1.5 px-2'
              >
                <span className='text-muted-foreground'>{t('Priority')}</span>
                <span className='font-semibold tabular-nums'>
                  {channel.priority}
                </span>
              </Badge>
              <Badge
                variant='outline'
                className='bg-background/70 h-6 gap-1.5 px-2'
              >
                <span className='text-muted-foreground'>{t('Balance')}</span>
                <span className='font-semibold tabular-nums'>
                  {formatUpstreamBalance(adjustedBalance)}
                </span>
              </Badge>
              <Badge
                variant='outline'
                className='bg-background/70 h-6 gap-1.5 px-2'
                title={t('Availability (last 24h)')}
              >
                <span className='text-muted-foreground'>
                  {t('Availability')}
                </span>
                <span className='font-semibold tabular-nums'>
                  {formatUpstreamAvailability(channel.availability_24h)}
                </span>
              </Badge>
              <Badge
                variant='outline'
                className='bg-background/70 h-6 gap-1.5 px-2'
                title={t('Average first-token latency (last 24h)')}
              >
                <span className='text-muted-foreground'>
                  {t('First-token latency')}
                </span>
                <span className='font-semibold tabular-nums'>
                  {formatUpstreamFirstTokenLatency(
                    channel.average_first_token_latency_ms
                  )}
                </span>
              </Badge>
              <Badge
                variant={
                  selectedGroupMultiplier.status === 'invalid'
                    ? 'destructive'
                    : 'outline'
                }
                className={cn(
                  'h-6 px-2 font-semibold tabular-nums',
                  selectedGroupMultiplier.status !== 'invalid' &&
                    'bg-background/70'
                )}
                title={
                  channel.selected_group
                    ? `${t('Group')}: ${channel.selected_group}`
                    : undefined
                }
              >
                {t('Minimum {{value}}', {
                  value: selectedGroupMultiplierValue,
                })}
              </Badge>
              {balanceIsLow && (
                <Badge variant='destructive'>
                  <AlertTriangle />
                  {t('Low balance')}
                </Badge>
              )}
              {!cardOpen && channel.note && (
                <span
                  className='text-muted-foreground min-w-0 flex-1 truncate text-sm font-normal'
                  title={channel.note}
                >
                  {channel.note}
                </span>
              )}
            </CollapsibleTrigger>
            <div className='flex shrink-0 items-center gap-1.5'>
              <Button
                size='icon-sm'
                variant='outline'
                aria-label={t('Pin to top')}
                title={t('Pin to top')}
                onClick={() => onPin(channel)}
                disabled={pinning}
              >
                {pinning ? (
                  <LoaderCircle className='animate-spin' />
                ) : (
                  <span aria-hidden='true' className='text-base leading-none'>
                    🔝
                  </span>
                )}
              </Button>
              <Button
                size='icon-sm'
                variant='outline'
                aria-label={t('Open upstream')}
                title={t('Open upstream')}
                render={
                  <a
                    href={channel.base_url}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                <ExternalLink />
              </Button>
              <Button
                size='icon-sm'
                variant='outline'
                aria-label={t('Configure')}
                title={t('Configure')}
                onClick={() => onConfigure(channel)}
              >
                <Pencil />
              </Button>
              <Button
                size='icon-sm'
                variant='outline'
                aria-label={t('Refresh balance')}
                title={t('Refresh balance')}
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
              </Button>
            </div>
          </div>
        </CardHeader>

        <CollapsibleContent className='border-t border-current/10'>
          <CardContent className='space-y-1.5 px-3 py-2'>
            <div className='grid grid-cols-1 gap-1 sm:grid-cols-3'>
              <Metric
                label={t('Low balance threshold')}
                value={
                  channel.balance_threshold > 0
                    ? formatUpstreamBalance(channel.balance_threshold)
                    : t('Disabled')
                }
              />
              <div className='bg-background/65 flex items-center justify-between gap-2 rounded-md border px-2 py-1.5 backdrop-blur-sm'>
                <div className='min-w-0'>
                  <div className='text-muted-foreground text-[11px]'>
                    {t('Auto refresh')}
                  </div>
                  <div className='mt-0.5 truncate text-sm font-medium'>
                    {autoRefreshEnabled
                      ? t('{{seconds}} seconds', {
                          seconds: channel.auto_refresh_interval || 300,
                        })
                      : t('Disabled')}
                  </div>
                </div>
                <Switch
                  checked={autoRefreshEnabled}
                  disabled={autoRefreshUpdating}
                  aria-label={t('Auto refresh')}
                  onCheckedChange={(enabled) => void toggleAutoRefresh(enabled)}
                />
              </div>
              <Metric
                label={t('Last refreshed')}
                value={formatUpstreamTime(channel.last_sync_time)}
              />
            </div>

            {(channel.last_error || accessTokenRequired) && (
              <Alert variant='destructive'>
                <AlertTriangle />
                {accessTokenRequired ? (
                  <>
                    <AlertTitle>
                      {t('Turnstile requires an access token')}
                    </AlertTitle>
                    <AlertDescription className='space-y-2'>
                      <p>
                        {t(
                          recommendsSub2APIAccessToken
                            ? 'This upstream Sub2API has Turnstile enabled. Background synchronization cannot use account-password login. Sign in through its browser page, then enter the issued access token here.'
                            : 'This upstream New-API has Turnstile enabled. Background synchronization cannot use account-password login. Enter the numeric user ID and create a management access token in the upstream account settings.'
                        )}
                      </p>
                      <Button
                        type='button'
                        size='sm'
                        variant='outline'
                        onClick={() => onConfigureAccessToken(channel)}
                      >
                        {recommendsSub2APIAccessToken
                          ? t('Configure')
                          : t('Configure management access token')}
                      </Button>
                    </AlertDescription>
                  </>
                ) : (
                  <AlertDescription>{channel.last_error}</AlertDescription>
                )}
              </Alert>
            )}

            {snapshot ? (
              <div className='space-y-1.5'>
                {accountPanel}
                <div className='bg-background/65 rounded-md border p-1.5 backdrop-blur-sm'>
                  <Collapsible open={groupsOpen} onOpenChange={setGroupsOpen}>
                    <div className='flex items-center justify-between gap-1'>
                      <CollapsibleTrigger className='hover:bg-muted/50 flex min-w-0 flex-1 cursor-pointer items-center justify-between rounded-sm px-1.5 py-0.5 text-left text-sm font-medium outline-none'>
                        <span>{t('Groups and multipliers')}</span>
                        <span className='text-muted-foreground text-xs'>
                          {groupsOpen ? t('Collapse') : t('Expand')}
                        </span>
                      </CollapsibleTrigger>
                      <Button
                        type='button'
                        size='sm'
                        variant='ghost'
                        className='h-7 shrink-0 px-2'
                        aria-label={t(
                          'Refresh groups, multipliers, models and pricing'
                        )}
                        title={t(
                          'Refresh groups, multipliers, models and pricing'
                        )}
                        disabled={
                          refreshingGroups ||
                          channel.provider === 'other' ||
                          !hasUsableCredentials
                        }
                        onClick={() => onRefreshGroups(channel)}
                      >
                        {refreshingGroups ? (
                          <LoaderCircle className='animate-spin' />
                        ) : (
                          <RefreshCw />
                        )}
                        {t('Refresh')}
                      </Button>
                    </div>
                    <CollapsibleContent className='px-1.5 pt-1.5 pb-0.5'>
                      {snapshot.groups.length === 0 ? (
                        <p className='text-muted-foreground text-sm'>
                          {t('No upstream groups')}
                        </p>
                      ) : (
                        <div className='flex flex-wrap gap-1.5'>
                          {snapshot.groups.map((group) => {
                            const ratio =
                              snapshot.ratios[String(group.id ?? group.name)] ??
                              group.ratio
                            const normalizedGroupName = group.name.trim()
                            const selected =
                              channel.selected_group.trim() ===
                              normalizedGroupName
                            return (
                              <Badge
                                key={`${group.id ?? group.name}-${group.platform ?? ''}`}
                                render={
                                  <button
                                    type='button'
                                    disabled={selectingGroup}
                                  />
                                }
                                variant='outline'
                                className={cn(
                                  'cursor-pointer disabled:cursor-wait disabled:opacity-60',
                                  selected
                                    ? 'border-emerald-600 bg-emerald-600 text-white hover:bg-emerald-600 dark:border-emerald-500 dark:bg-emerald-500 dark:text-emerald-950'
                                    : 'bg-background/70'
                                )}
                                aria-pressed={selected}
                                aria-label={t('Select as minimum multiplier')}
                                title={t('Select as minimum multiplier')}
                                onClick={() =>
                                  void onSelectGroup(
                                    channel,
                                    normalizedGroupName
                                  )
                                }
                              >
                                {selected && <Check />}
                                {normalizedGroupName}: ×
                                {formatUpstreamBalance(
                                  ratio *
                                    getEffectiveUpstreamMultiplier(
                                      channel.multiplier
                                    )
                                )}
                              </Badge>
                            )
                          })}
                        </div>
                      )}
                      <UpstreamModelsPanel models={snapshot.models ?? []} />
                    </CollapsibleContent>
                  </Collapsible>
                </div>
              </div>
            ) : (
              accountPanel
            )}

            {snapshot && (
              <UpstreamKeysTable
                channel={channel}
                snapshot={snapshot}
                onChannelChanged={onDataChanged}
                refreshing={refreshingKeys}
                onRefresh={onRefreshKeys}
              />
            )}
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function UpstreamModelsPanel({ models }: { models: UpstreamModel[] }) {
  const { t } = useTranslation()

  return (
    <section
      className='mt-3 border-t pt-2'
      aria-label={t('Models ({{count}})', { count: models.length })}
    >
      <h3 className='text-sm font-medium'>
        {t('Models ({{count}})', { count: models.length })}
      </h3>
      {models.length === 0 ? (
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('No upstream models found')}
        </p>
      ) : (
        <div className='mt-1.5 max-h-96 space-y-1.5 overflow-y-auto pr-1'>
          {models.map((model) => (
            <UpstreamModelCard key={model.id} model={model} />
          ))}
        </div>
      )}
    </section>
  )
}

function UpstreamModelCard({ model }: { model: UpstreamModel }) {
  const { t } = useTranslation()

  return (
    <div className='bg-background/70 rounded-md border px-2 py-1.5'>
      <code className='block text-sm font-medium break-all'>{model.id}</code>
      {model.pricing.length === 0 ? (
        <p className='text-muted-foreground mt-1 text-xs'>
          {t('Pricing not provided by upstream')}
        </p>
      ) : (
        <div className='mt-1.5 space-y-1.5'>
          {model.pricing.map((pricing) => (
            <UpstreamModelPricingCard
              key={`${pricing.source}-${pricing.channel_name ?? 'default'}-${pricing.platform ?? 'default'}-${pricing.billing_mode ?? 'default'}`}
              pricing={pricing}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function UpstreamModelPricingCard({
  pricing,
}: {
  pricing: UpstreamModelPricing
}) {
  const { t } = useTranslation()
  const fields = getUpstreamModelPricingFields(pricing)
  const intervals = pricing.intervals ?? []
  const hasPricing = fields.length > 0 || intervals.length > 0
  const intervalLabels = {
    tokens: t('tokens'),
    input: t('Input price'),
    output: t('Output price'),
    cacheWrite: t('Cache write price'),
    cacheRead: t('Cache read price'),
    request: t('Per-request price'),
  }

  return (
    <div className='bg-muted/30 rounded border px-2 py-1.5 text-xs'>
      {pricing.source === 'sub2api' && (
        <div className='grid gap-x-3 gap-y-0.5 sm:grid-cols-3'>
          <PricingMetadata label={t('Channel')} value={pricing.channel_name} />
          <PricingMetadata label={t('Platform')} value={pricing.platform} />
          <PricingMetadata
            label={t('Billing mode')}
            value={pricing.billing_mode}
          />
        </div>
      )}
      {!hasPricing ? (
        <p className='text-muted-foreground'>
          {t('Pricing not provided by upstream')}
        </p>
      ) : (
        <>
          {fields.length > 0 && (
            <dl className='mt-1 grid gap-x-3 gap-y-0.5 sm:grid-cols-2'>
              {fields.map((field) => (
                <div
                  key={field.label}
                  className='grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-2'
                >
                  <dt className='text-muted-foreground'>{t(field.label)}</dt>
                  <dd className='truncate text-right font-medium tabular-nums'>
                    {field.value}
                  </dd>
                </div>
              ))}
            </dl>
          )}
          {intervals.length > 0 && (
            <div className='mt-1.5'>
              <div className='text-muted-foreground'>{t('Tier pricing')}</div>
              <ul className='mt-0.5 space-y-0.5'>
                {intervals.map((interval) => (
                  <li
                    key={`${interval.min_tokens}-${interval.max_tokens ?? 'max'}-${interval.tier_label ?? 'tier'}`}
                  >
                    {formatUpstreamPricingInterval(interval, intervalLabels)}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function PricingMetadata({ label, value }: { label: string; value?: string }) {
  return (
    <div className='grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-1'>
      <span className='text-muted-foreground'>{label}</span>
      <span className='truncate'>{value?.trim() || '-'}</span>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className='bg-background/65 rounded-md border px-2 py-1.5 backdrop-blur-sm'>
      <div className='text-muted-foreground text-[11px]'>{label}</div>
      <div className='mt-0.5 truncate text-sm font-medium'>{value}</div>
    </div>
  )
}

interface AccountPanelProps {
  channel: UpstreamChannel
  snapshot?: UpstreamSnapshot
  editingNote: boolean
  noteDraft: string
  defaultTestModel: string
  savingDefaultTestModel: boolean
  deleting: boolean
  onEditNote: () => void
  onChangeNote: (value: string) => void
  onBlurNote: (event: FocusEvent<HTMLTextAreaElement>) => void
  onCancelNote: () => void
  onSaveDefaultTestModel: (defaultTestModel: string) => void
  onDelete: () => void
}

function AccountPanel({
  channel,
  snapshot,
  editingNote,
  noteDraft,
  defaultTestModel,
  savingDefaultTestModel,
  deleting,
  onEditNote,
  onChangeNote,
  onBlurNote,
  onCancelNote,
  onSaveDefaultTestModel,
  onDelete,
}: AccountPanelProps) {
  const { t } = useTranslation()
  const account = snapshot?.account
  const modelSet = new Set(
    (snapshot?.models || [])
      .map((model) => model.id.trim())
      .filter((model) => model !== '')
  )
  if (defaultTestModel.trim()) modelSet.add(defaultTestModel.trim())
  const defaultTestModels = [...modelSet].sort((left, right) =>
    left.localeCompare(right)
  )
  const hasModels = defaultTestModels.length > 0

  return (
    <div className='bg-background/65 space-y-1 rounded-md border p-2 backdrop-blur-sm'>
      <h3 className='flex items-center gap-1.5 text-sm font-medium'>
        <KeyRound className='size-4' />
        {t('Upstream account')}
      </h3>
      <div className='grid gap-1.5 sm:grid-cols-2'>
        <dl className='grid grid-cols-[auto_minmax(0,1fr)] content-start gap-x-3 gap-y-0.5 text-sm'>
          <dt className='text-muted-foreground'>{t('Username')}</dt>
          <dd className='truncate'>
            {account?.username || channel.username || '-'}
          </dd>
        </dl>
        <dl className='grid grid-cols-[auto_minmax(0,1fr)] content-start gap-x-3 gap-y-0.5 text-sm'>
          <dt className='text-muted-foreground'>{t('Email')}</dt>
          <dd className='truncate'>{account?.email || '-'}</dd>
        </dl>
      </div>
      <div className='flex flex-col gap-2 border-t pt-1 sm:flex-row sm:items-end'>
        <div className='min-w-0 flex-1'>
          {editingNote ? (
            <Textarea
              autoFocus
              maxLength={2000}
              className='min-h-14'
              value={noteDraft}
              aria-label={t('Note')}
              placeholder={t('Add a note for this upstream channel')}
              onChange={(event) => onChangeNote(event.target.value)}
              onBlur={onBlurNote}
              onKeyDown={(event) => {
                if (event.key === 'Escape') onCancelNote()
              }}
            />
          ) : (
            <button
              type='button'
              className={cn(
                'hover:bg-muted/50 min-h-8 w-full rounded-sm px-1.5 py-0.5 text-left text-sm whitespace-pre-wrap transition-colors',
                channel.note ? 'text-foreground' : 'text-muted-foreground'
              )}
              onClick={onEditNote}
            >
              {channel.note
                ? t('Note: {{note}}', { note: channel.note })
                : t('Note: (click to edit)')}
            </button>
          )}
        </div>
        <div className='w-full shrink-0 space-y-1 sm:w-64'>
          <div className='flex items-center justify-between gap-2'>
            <label
              className='text-muted-foreground text-xs'
              htmlFor={`upstream-default-test-model-${channel.id}`}
            >
              {t('Default test model')}
            </label>
            {savingDefaultTestModel && (
              <LoaderCircle className='text-muted-foreground size-3.5 animate-spin' />
            )}
          </div>
          <NativeSelect
            id={`upstream-default-test-model-${channel.id}`}
            className='w-full'
            size='sm'
            value={defaultTestModel}
            disabled={!hasModels || savingDefaultTestModel}
            data-default-test-model-select='true'
            aria-label={t('Default test model')}
            title={
              hasModels
                ? t('Select a default test model')
                : t('Refresh groups, multipliers, models and pricing first')
            }
            onChange={(event) => onSaveDefaultTestModel(event.target.value)}
          >
            <NativeSelectOption value=''>
              {hasModels
                ? t('Select a default test model')
                : t('No models available')}
            </NativeSelectOption>
            {defaultTestModels.map((model) => (
              <NativeSelectOption key={model} value={model}>
                {model}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {!hasModels && (
            <p className='text-muted-foreground text-[11px] leading-tight'>
              {t('Refresh groups, multipliers, models and pricing first')}
            </p>
          )}
        </div>
        <Button
          type='button'
          size='sm'
          variant='destructive'
          className='shrink-0 border-red-700 bg-red-600 text-white hover:bg-red-700 dark:border-red-500 dark:bg-red-600 dark:hover:bg-red-700'
          aria-label={t('Delete')}
          title={t('Delete')}
          disabled={deleting}
          onClick={onDelete}
        >
          {deleting ? <LoaderCircle className='animate-spin' /> : <Trash2 />}
          {t('Delete')}
        </Button>
      </div>
    </div>
  )
}
