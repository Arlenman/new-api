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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, LoaderCircle, Plus, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'

import {
  createDefaultAlertRuleInput,
  getAlertRuleTriggerTypeLabel,
} from './alert-rule-lib'
import {
  createAlertRule,
  createManagedUpstreamChannel,
  deleteManagedUpstreamChannel,
  getAlertRuleProviders,
  getAlertRules,
  getManagedUpstreamChannels,
  pinManagedUpstreamChannel,
  refreshAllManagedUpstreamChannels,
  refreshManagedUpstreamChannel,
  updateManagedUpstreamChannel,
  updateManagedUpstreamChannelNote,
  updateAlertRule,
  updateManagedUpstreamChannelSelectedGroup,
} from './api'
import { AlertRuleDialog } from './components/alert-rule-dialog'
import { UpstreamChannelCard } from './components/upstream-channel-card'
import { UpstreamChannelConfigDialog } from './components/upstream-channel-config-dialog'
import {
  filterAndSortUpstreamChannels,
  formatUpstreamBalance,
  getTotalAdjustedUpstreamBalance,
  getUpstreamChannelDisplayName,
  getUpstreamChannelKeyStats,
  hasUsableUpstreamCredentials,
  isUpstreamTurnstileAccessTokenRequired,
  isValidUpstreamMultiplier,
  type UpstreamChannelSort,
  type UpstreamChannelStatusFilter,
} from './lib'
import type {
  AlertRule,
  AlertRuleInput,
  CreateUpstreamChannelConfig,
  UpstreamChannel,
  UpstreamChannelConfig,
} from './types'

const queryKey = ['managed-upstream-channels'] as const
const alertRulesQueryKey = ['alert-rules'] as const
const emptyChannels: UpstreamChannel[] = []

export function UpstreamChannels() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selectedChannel, setSelectedChannel] =
    useState<UpstreamChannel | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const [alertRulesOpen, setAlertRulesOpen] = useState(false)
  const [channelToDelete, setChannelToDelete] =
    useState<UpstreamChannel | null>(null)
  const [accessTokenRequired, setAccessTokenRequired] = useState(false)
  const [refreshingChannelId, setRefreshingChannelId] = useState<number | null>(
    null
  )
  const [autoRefreshUpdatingChannelId, setAutoRefreshUpdatingChannelId] =
    useState<number | null>(null)
  const [pinningChannelId, setPinningChannelId] = useState<number | null>(null)
  const [statusFilter, setStatusFilter] =
    useState<UpstreamChannelStatusFilter>('all')
  const [channelSort, setChannelSort] = useState<UpstreamChannelSort>('default')
  const [selectingGroupChannelId, setSelectingGroupChannelId] = useState<
    number | null
  >(null)
  const [enabledChannelThreshold, setEnabledChannelThreshold] = useState('1')
  const [enabledChannelNoticeEnabled, setEnabledChannelNoticeEnabled] =
    useState(false)
  const deferredEnabledChannelThresholdSaveRef = useRef(false)

  const channelsQuery = useQuery({
    queryKey,
    queryFn: getManagedUpstreamChannels,
    refetchInterval: 30_000,
  })
  const alertRulesQuery = useQuery({
    queryKey: alertRulesQueryKey,
    queryFn: getAlertRules,
  })
  const enabledChannelCountRule = useMemo(
    () =>
      alertRulesQuery.data?.data?.find(
        (rule) => rule.trigger_type === 'enabled_channel_count'
      ),
    [alertRulesQuery.data?.data]
  )

  useEffect(() => {
    if (alertRulesQuery.isLoading) return
    setEnabledChannelThreshold(
      String(enabledChannelCountRule?.trigger_config.threshold ?? 1)
    )
    setEnabledChannelNoticeEnabled(enabledChannelCountRule?.enabled ?? false)
  }, [
    alertRulesQuery.isLoading,
    enabledChannelCountRule?.enabled,
    enabledChannelCountRule?.trigger_config.threshold,
  ])

  const channels = channelsQuery.data?.data ?? emptyChannels
  const totalBalance = useMemo(
    () => getTotalAdjustedUpstreamBalance(channels),
    [channels]
  )
  const keyStats = useMemo(
    () => getUpstreamChannelKeyStats(channels),
    [channels]
  )
  const visibleChannels = useMemo(
    () => filterAndSortUpstreamChannels(channels, statusFilter, channelSort),
    [channelSort, channels, statusFilter]
  )

  const enabledChannelAlertMutation = useMutation({
    mutationFn: async ({
      rule,
      threshold,
      enabled,
    }: {
      rule?: AlertRule
      threshold: number
      enabled: boolean
    }) => {
      let response
      if (rule) {
        const input: AlertRuleInput = {
          name: rule.name,
          enabled,
          trigger_type: 'enabled_channel_count',
          trigger_config: {
            operator: 'lte',
            threshold,
            window_seconds: 0,
          },
          providers: [...rule.providers],
          message_format: rule.message_format,
          message_template: rule.message_template,
          consecutive_required: 1,
          cooldown_seconds: 0,
          send_recovery: rule.send_recovery,
        }
        response = await updateAlertRule(rule.id, input)
      } else {
        const providersResponse = await getAlertRuleProviders()
        if (!providersResponse.success || !providersResponse.data) {
          throw new Error(
            providersResponse.message ||
              'Configure a ready default notification provider in Alert rules first'
          )
        }
        const providers = providersResponse.data.providers
        const provider =
          providers.find((item) => item.default && item.ready) ||
          providers.find((item) => item.ready)
        if (!provider) {
          throw new Error(
            'Configure a ready default notification provider in Alert rules first'
          )
        }

        const input = createDefaultAlertRuleInput(
          [provider.name],
          'enabled_channel_count'
        )
        input.name = t(getAlertRuleTriggerTypeLabel('enabled_channel_count'))
        input.enabled = enabled
        input.trigger_config.threshold = threshold
        response = await createAlertRule(input)
      }
      if (!response.success || !response.data) {
        throw new Error(
          response.message || 'Failed to save available channel alert'
        )
      }
      return response
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: alertRulesQueryKey })
      toast.success(t('Available channel alert saved'))
    },
    onError: (error) => {
      setEnabledChannelThreshold(
        String(enabledChannelCountRule?.trigger_config.threshold ?? 1)
      )
      setEnabledChannelNoticeEnabled(enabledChannelCountRule?.enabled ?? false)
      toast.error(
        error instanceof Error
          ? t(error.message)
          : t('Failed to save available channel alert')
      )
    },
  })

  const refreshAllMutation = useMutation({
    mutationFn: refreshAllManagedUpstreamChannels,
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey })
      if (!response.success || !response.data) {
        toast.error(
          response.message || t('Failed to refresh upstream channels')
        )
        return
      }
      if (response.data.errors.length > 0) {
        toast.warning(
          t('Refreshed {{count}} upstream channels, {{errors}} failed', {
            count: response.data.refreshed,
            errors: response.data.errors.length,
          })
        )
        return
      }
      toast.success(
        t('Refreshed {{count}} upstream channels', {
          count: response.data.refreshed,
        })
      )
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to refresh upstream channels')
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (channel: UpstreamChannel) => {
      const response = await deleteManagedUpstreamChannel(channel.id)
      if (!response.success) {
        throw new Error(t('Failed to delete channel'))
      }
      return response
    },
    onSuccess: async () => {
      setChannelToDelete(null)
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('Channel deleted successfully'))
    },
    onError: () => {
      toast.error(t('Failed to delete channel'))
    },
  })

  const saveMutation = useMutation({
    mutationFn: async ({
      channel,
      config,
    }: {
      channel: UpstreamChannel | null
      config: CreateUpstreamChannelConfig
    }) => {
      if (!channel) return createManagedUpstreamChannel(config)
      const updateConfig: UpstreamChannelConfig = {
        name: config.name,
        provider: config.provider,
        auth_type: config.auth_type,
        username: config.username,
        password: config.password,
        balance_threshold: config.balance_threshold,
        multiplier: config.multiplier,
        auto_refresh_interval: config.auto_refresh_interval,
        priority: config.priority,
      }
      return updateManagedUpstreamChannel(channel.id, updateConfig)
    },
    onSuccess: async (response, variables) => {
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to save upstream channel'))
        return
      }

      setConfigOpen(false)
      setSelectedChannel(null)
      setAccessTokenRequired(false)
      await queryClient.invalidateQueries({ queryKey })

      const savedAuthType = variables.channel?.auth_type || 'password'
      const canReuseSavedCredential =
        variables.channel?.has_password === true &&
        savedAuthType === variables.config.auth_type
      if (
        variables.config.provider === 'other' ||
        !hasUsableUpstreamCredentials(
          variables.config.username,
          variables.config.password,
          canReuseSavedCredential
        )
      ) {
        toast.success(t('Saved successfully'))
        return
      }

      try {
        const refreshed = await refreshManagedUpstreamChannel(response.data.id)
        await queryClient.invalidateQueries({ queryKey })
        if (!refreshed.success) {
          if (
            isUpstreamTurnstileAccessTokenRequired(refreshed.error_code) ||
            isUpstreamTurnstileAccessTokenRequired(
              refreshed.data?.last_error_code
            )
          ) {
            openAccessTokenConfiguration(refreshed.data || response.data)
            return
          }
          toast.warning(
            refreshed.message || t('Upstream channel saved, but refresh failed')
          )
          return
        }
        toast.success(t('Upstream channel saved and refreshed'))
      } catch (error) {
        toast.warning(
          error instanceof Error
            ? error.message
            : t('Upstream channel saved, but refresh failed')
        )
      }
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save upstream channel')
      )
    },
  })

  function openAddConfiguration() {
    setSelectedChannel(null)
    setAccessTokenRequired(false)
    setConfigOpen(true)
  }

  function openConfiguration(channel: UpstreamChannel) {
    setSelectedChannel(channel)
    setAccessTokenRequired(false)
    setConfigOpen(true)
  }

  function openAccessTokenConfiguration(channel: UpstreamChannel) {
    setSelectedChannel(channel)
    setAccessTokenRequired(true)
    setConfigOpen(true)
  }

  function handleConfigOpenChange(open: boolean) {
    setConfigOpen(open)
    if (!open) {
      setSelectedChannel(null)
      setAccessTokenRequired(false)
    }
  }

  async function refreshChannel(channel: UpstreamChannel) {
    setRefreshingChannelId(channel.id)
    try {
      const response = await refreshManagedUpstreamChannel(channel.id)
      await queryClient.invalidateQueries({ queryKey })
      if (!response.success) {
        if (
          isUpstreamTurnstileAccessTokenRequired(response.error_code) ||
          isUpstreamTurnstileAccessTokenRequired(response.data?.last_error_code)
        ) {
          openAccessTokenConfiguration(response.data || channel)
          return
        }
        toast.error(response.message || t('Failed to refresh upstream channel'))
        return
      }
      toast.success(t('Upstream channel refreshed'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to refresh upstream channel')
      )
    } finally {
      setRefreshingChannelId(null)
    }
  }

  async function saveChannelNote(channel: UpstreamChannel, note: string) {
    try {
      const response = await updateManagedUpstreamChannelNote(channel.id, note)
      if (!response.success) {
        toast.error(response.message || t('Failed to save upstream note'))
        return
      }
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('Upstream note saved'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save upstream note')
      )
    }
  }

  async function selectChannelGroup(
    channel: UpstreamChannel,
    selectedGroup: string
  ) {
    setSelectingGroupChannelId(channel.id)
    try {
      const response = await updateManagedUpstreamChannelSelectedGroup(
        channel.id,
        selectedGroup
      )
      if (!response.success) {
        toast.error(
          response.message || t('Failed to update minimum multiplier group')
        )
        return
      }
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('Minimum multiplier group updated'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update minimum multiplier group')
      )
    } finally {
      setSelectingGroupChannelId(null)
    }
  }

  async function toggleAutoRefresh(channel: UpstreamChannel, enabled: boolean) {
    setAutoRefreshUpdatingChannelId(channel.id)
    try {
      const response = await updateManagedUpstreamChannel(channel.id, {
        name: channel.name,
        provider: channel.provider,
        auth_type: channel.auth_type || 'password',
        username: channel.username,
        password: '',
        balance_threshold: channel.balance_threshold,
        multiplier: channel.multiplier,
        auto_refresh_interval: enabled ? 300 : 0,
        priority: channel.priority,
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to save upstream channel')
        )
      }
      await queryClient.invalidateQueries({ queryKey })
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save upstream channel')
      )
      throw error
    } finally {
      setAutoRefreshUpdatingChannelId(null)
    }
  }

  async function pinChannel(channel: UpstreamChannel) {
    setPinningChannelId(channel.id)
    try {
      const response = await pinManagedUpstreamChannel(channel.id)
      if (!response.success) {
        toast.error(response.message || t('Failed to pin upstream channel'))
        return
      }
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('Upstream channel pinned'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to pin upstream channel')
      )
    } finally {
      setPinningChannelId(null)
    }
  }

  function saveChannel(config: CreateUpstreamChannelConfig) {
    const refreshInterval = config.auto_refresh_interval
    const invalidInterval =
      refreshInterval !== 0 && (refreshInterval < 60 || refreshInterval > 86400)
    if (
      !Number.isFinite(config.balance_threshold) ||
      config.balance_threshold < 0
    ) {
      toast.error(t('Low balance threshold must be 0 or greater'))
      return
    }
    if (!isValidUpstreamMultiplier(config.multiplier)) {
      toast.error(
        t(
          'Channel multiplier must be a positive number with at most 2 decimal places'
        )
      )
      return
    }
    if (!Number.isInteger(refreshInterval) || invalidInterval) {
      toast.error(t('Auto refresh interval must be 0 or 60 to 86400 seconds'))
      return
    }
    if (
      !Number.isInteger(config.priority) ||
      config.priority < -2_147_483_648 ||
      config.priority > 2_147_483_647
    ) {
      toast.error(
        t('Priority must be an integer between -2147483648 and 2147483647')
      )
      return
    }
    if (config.auth_type === 'access_token') {
      if (config.provider !== 'new-api') {
        toast.error(
          t(
            'Management access token authentication is only supported for New-API upstream channels'
          )
        )
        return
      }
      if (!/^[1-9]\d*$/.test(config.username.trim())) {
        toast.error(t('Upstream user ID must be a positive integer'))
        return
      }
      const savedAuthType = selectedChannel?.auth_type || 'password'
      const canReuseSavedCredential =
        selectedChannel?.has_password === true &&
        savedAuthType === config.auth_type
      if (!config.password.trim() && !canReuseSavedCredential) {
        toast.error(
          t(
            'Enter a new password or management access token when changing the authentication method'
          )
        )
        return
      }
    }
    saveMutation.mutate({ channel: selectedChannel, config })
  }

  async function refreshChannelList() {
    await queryClient.invalidateQueries({ queryKey })
  }

  function parseEnabledChannelThreshold() {
    if (!/^\d+$/.test(enabledChannelThreshold)) {
      toast.error(
        t('Enabled channel count threshold must be a non-negative integer')
      )
      return null
    }
    const threshold = Number(enabledChannelThreshold)
    if (!Number.isSafeInteger(threshold) || threshold > 1_000_000) {
      toast.error(
        t('Enabled channel count threshold must be a non-negative integer')
      )
      return null
    }
    return threshold
  }

  function saveEnabledChannelAlert(enabled = enabledChannelNoticeEnabled) {
    if (enabledChannelAlertMutation.isPending) return
    const threshold = parseEnabledChannelThreshold()
    if (threshold === null) return
    const savedThreshold =
      enabledChannelCountRule?.trigger_config.threshold ?? 1
    const savedEnabled = enabledChannelCountRule?.enabled ?? false
    if (threshold === savedThreshold && enabled === savedEnabled) return
    enabledChannelAlertMutation.mutate({
      rule: enabledChannelCountRule,
      threshold,
      enabled,
    })
  }

  function toggleEnabledChannelAlert(checked: boolean) {
    deferredEnabledChannelThresholdSaveRef.current = false
    const threshold = parseEnabledChannelThreshold()
    if (threshold === null) return
    setEnabledChannelNoticeEnabled(checked)
    enabledChannelAlertMutation.mutate({
      rule: enabledChannelCountRule,
      threshold,
      enabled: checked,
    })
  }

  return (
    <>
      <SectionPageLayout actionsBelowTitle>
        <SectionPageLayout.Title>
          <span className='inline-flex min-w-0 items-center gap-2'>
            <span className='truncate'>{t('Channel Panel')}</span>
            <Badge variant='outline' className='shrink-0'>
              Root
            </Badge>
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {channels.length > 0 && (
            <>
              <NativeSelect
                size='sm'
                className='w-36'
                aria-label={t('Status')}
                value={statusFilter}
                onChange={(event) =>
                  setStatusFilter(
                    event.target.value as UpstreamChannelStatusFilter
                  )
                }
              >
                <option value='all'>{t('All statuses')}</option>
                <option value='ready'>{t('Ready')}</option>
                <option value='error'>{t('Error')}</option>
                <option value='unconfigured'>{t('Not configured')}</option>
              </NativeSelect>
              <NativeSelect
                size='sm'
                className='w-40'
                aria-label={t('Sort')}
                value={channelSort}
                onChange={(event) =>
                  setChannelSort(event.target.value as UpstreamChannelSort)
                }
              >
                <option value='default'>{t('Default order')}</option>
                <option value='balance-desc'>{t('Balance high to low')}</option>
                <option value='balance-asc'>{t('Balance low to high')}</option>
                <option value='multiplier-desc'>
                  {t('Multiplier high to low')}
                </option>
                <option value='multiplier-asc'>
                  {t('Multiplier low to high')}
                </option>
              </NativeSelect>
            </>
          )}
          <Button variant='outline' onClick={() => setAlertRulesOpen(true)}>
            <Bell />
            {t('Alert rules')}
          </Button>
          <Button variant='outline' onClick={openAddConfiguration}>
            <Plus />
            {t('Add configuration')}
          </Button>
          <Button
            onClick={() => refreshAllMutation.mutate()}
            disabled={refreshAllMutation.isPending || channels.length === 0}
          >
            {refreshAllMutation.isPending ? (
              <LoaderCircle className='animate-spin' />
            ) : (
              <RefreshCw />
            )}
            {t('Refresh all')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-2'>
            {!channelsQuery.isLoading && !channelsQuery.isError && (
              <div className='flex flex-wrap items-center gap-x-5 gap-y-2 border-b px-1 pb-2 text-sm'>
                <OverviewMetric
                  label={t('Total balance')}
                  value={formatUpstreamBalance(totalBalance)}
                />
                <OverviewMetric
                  label={t('Channel count')}
                  value={String(channels.length)}
                />
                <OverviewMetric
                  label={t('Active keys')}
                  value={String(keyStats.active)}
                />
                <div className='ml-auto flex flex-wrap items-center justify-end gap-2'>
                  <span className='text-muted-foreground'>
                    {t('Notify when only')}
                  </span>
                  <Input
                    className='h-7 w-20 px-2 text-center tabular-nums'
                    type='number'
                    min='0'
                    max='1000000'
                    step='1'
                    inputMode='numeric'
                    aria-label={t('Available channel count')}
                    value={enabledChannelThreshold}
                    disabled={
                      alertRulesQuery.isLoading ||
                      enabledChannelAlertMutation.isPending
                    }
                    onChange={(event) =>
                      setEnabledChannelThreshold(event.target.value)
                    }
                    onBlur={(event) => {
                      const nextTarget = event.relatedTarget
                      if (
                        nextTarget instanceof HTMLElement &&
                        nextTarget.closest(
                          '[data-enabled-channel-alert-switch="true"]'
                        )
                      ) {
                        deferredEnabledChannelThresholdSaveRef.current = true
                        return
                      }
                      saveEnabledChannelAlert()
                    }}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') event.currentTarget.blur()
                    }}
                  />
                  <span className='text-muted-foreground'>
                    {t('available channels remain')}
                  </span>
                  {enabledChannelAlertMutation.isPending && (
                    <LoaderCircle className='size-4 animate-spin' />
                  )}
                  <Switch
                    checked={enabledChannelNoticeEnabled}
                    disabled={
                      alertRulesQuery.isLoading ||
                      enabledChannelAlertMutation.isPending
                    }
                    data-enabled-channel-alert-switch='true'
                    aria-label={t('Available channel alert')}
                    onBlur={() => {
                      if (!deferredEnabledChannelThresholdSaveRef.current) {
                        return
                      }
                      deferredEnabledChannelThresholdSaveRef.current = false
                      saveEnabledChannelAlert()
                    }}
                    onCheckedChange={toggleEnabledChannelAlert}
                  />
                </div>
              </div>
            )}
            {channelsQuery.isLoading && <LoadingCards />}
            {channelsQuery.isError && (
              <Alert variant='destructive'>
                <AlertTitle>{t('Failed to load upstream channels')}</AlertTitle>
                <AlertDescription>
                  {channelsQuery.error instanceof Error
                    ? channelsQuery.error.message
                    : t('Request failed')}
                </AlertDescription>
              </Alert>
            )}
            {!channelsQuery.isLoading &&
              !channelsQuery.isError &&
              channels.length === 0 && (
                <div className='rounded-xl border border-dashed p-8 text-center'>
                  <p className='font-medium'>
                    {t('No upstream channel configurations')}
                  </p>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    {t(
                      'Add a configuration manually or set a Base URL on an existing channel.'
                    )}
                  </p>
                </div>
              )}
            {channels.length > 0 && visibleChannels.length === 0 && (
              <div className='rounded-xl border border-dashed p-8 text-center'>
                <p className='text-muted-foreground text-sm'>
                  {t('No upstream channels match the selected filters')}
                </p>
              </div>
            )}
            {visibleChannels.length > 0 && (
              <div className='space-y-1'>
                {visibleChannels.map((channel) => (
                  <UpstreamChannelCard
                    key={channel.id}
                    channel={channel}
                    refreshing={refreshingChannelId === channel.id}
                    autoRefreshUpdating={
                      autoRefreshUpdatingChannelId === channel.id
                    }
                    pinning={pinningChannelId === channel.id}
                    selectingGroup={selectingGroupChannelId === channel.id}
                    deleting={
                      deleteMutation.isPending &&
                      channelToDelete?.id === channel.id
                    }
                    onConfigure={openConfiguration}
                    onConfigureAccessToken={openAccessTokenConfiguration}
                    onPin={pinChannel}
                    onDelete={setChannelToDelete}
                    onRefresh={refreshChannel}
                    onToggleAutoRefresh={toggleAutoRefresh}
                    onSaveNote={saveChannelNote}
                    onSelectGroup={selectChannelGroup}
                    onDataChanged={refreshChannelList}
                  />
                ))}
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <AlertRuleDialog
        open={alertRulesOpen}
        channels={channels}
        onOpenChange={(nextOpen) => {
          setAlertRulesOpen(nextOpen)
          if (!nextOpen) {
            void queryClient.invalidateQueries({ queryKey: alertRulesQueryKey })
          }
        }}
      />

      <UpstreamChannelConfigDialog
        channel={selectedChannel}
        open={configOpen}
        saving={saveMutation.isPending}
        accessTokenRequired={accessTokenRequired}
        onOpenChange={handleConfigOpenChange}
        onSave={saveChannel}
      />
      <AlertDialog
        open={channelToDelete !== null}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setChannelToDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Are you sure?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Are you sure you want to delete channel "{{name}}"? This action cannot be undone.',
                {
                  name: channelToDelete
                    ? getUpstreamChannelDisplayName(
                        channelToDelete.name,
                        channelToDelete.base_url
                      )
                    : '',
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending || channelToDelete === null}
              onClick={(event) => {
                event.preventDefault()
                if (channelToDelete) deleteMutation.mutate(channelToDelete)
              }}
            >
              {deleteMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function OverviewMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className='flex items-baseline gap-1.5'>
      <span className='text-muted-foreground'>{label}</span>
      <span className='font-semibold tabular-nums'>{value}</span>
    </div>
  )
}

function LoadingCards() {
  return (
    <div className='space-y-1'>
      {[0, 1, 2, 3].map((index) => (
        <div key={index} className='rounded-lg border px-3 py-2'>
          <Skeleton className='h-7 w-2/3' />
        </div>
      ))}
    </div>
  )
}
