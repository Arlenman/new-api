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
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  CirclePlus,
  Eye,
  EyeOff,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Send,
  ServerCog,
  Trash2,
  X,
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Markdown } from '@/components/ui/markdown'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  buildAlertRulePreviewRequest,
  buildAlertRuleTestSendRequest,
  createDefaultAlertRuleInput,
  getAlertRuleTriggerTypeLabel,
  getAlertMessageFormatOptions,
  getProviderUnavailableReason,
  switchAlertMessageFormat,
  switchAlertRuleTriggerType,
  validateAlertRuleDraft,
} from '../alert-rule-lib'
import {
  createAlertRule,
  deleteAlertRule,
  getAlertRuleProviders,
  getAlertRules,
  getApiNoticeConfig,
  previewAlertRule,
  revealApiNoticeAPIKey,
  testAlertRuleConnection,
  testSendAlertRule,
  updateApiNoticeConfig,
  updateAlertRule,
} from '../api'
import type {
  AlertEventType,
  AlertMessageFormat,
  AlertRule,
  AlertRuleInput,
  AlertRuleProviderCatalog,
  AlertRuleTestSendResult,
  AlertRuleTriggerType,
  ApiNoticeAction,
  ApiNoticeColumn,
  ApiNoticeConfig,
  ApiNoticeConnectionStatus,
  ApiNoticeField,
  ApiNoticeMessage,
  ApiNoticeProvider,
  ApiNoticeSection,
  UpstreamChannel,
} from '../types'

const alertRulesQueryKey = ['alert-rules'] as const
const alertRuleProvidersQueryKey = ['alert-rule-providers'] as const
const apiNoticeConfigQueryKey = ['api-notice-config'] as const
const providerCatalogRetryFeedbackDelayMs = 500
const emptyRules: AlertRule[] = []
const emptyProviders: ApiNoticeProvider[] = []

interface AlertRuleDialogProps {
  open: boolean
  channels: UpstreamChannel[]
  onOpenChange: (open: boolean) => void
}

function useStableListKeys(length: number, prefix: string) {
  const nextKey = useRef(0)
  const keysRef = useRef<string[]>([])
  while (keysRef.current.length < length) {
    keysRef.current.push(`${prefix}-${nextKey.current}`)
    nextKey.current += 1
  }
  if (keysRef.current.length > length) {
    keysRef.current.length = length
  }
  return {
    keys: keysRef.current,
    append() {
      keysRef.current.push(`${prefix}-${nextKey.current}`)
      nextKey.current += 1
    },
    remove(index: number) {
      keysRef.current.splice(index, 1)
    },
  }
}

function withOccurrenceKeys<T>(
  items: T[],
  identify: (item: T) => string
): Array<{ item: T; key: string }> {
  const occurrences = new Map<string, number>()
  return items.map((item) => {
    const identity = identify(item)
    const occurrence = occurrences.get(identity) || 0
    occurrences.set(identity, occurrence + 1)
    return { item, key: `${identity}-${occurrence}` }
  })
}

export function AlertRuleDialog({
  open,
  channels,
  onOpenChange,
}: AlertRuleDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRuleID, setEditingRuleID] = useState<number | null>(null)
  const [draft, setDraft] = useState<AlertRuleInput | null>(null)
  const [activeProvider, setActiveProvider] = useState('')
  const [previewEventType, setPreviewEventType] =
    useState<AlertEventType>('trigger')
  const [sampleChannelID, setSampleChannelID] = useState(0)
  const [preview, setPreview] = useState<ApiNoticeMessage | null>(null)
  const [connectionStatus, setConnectionStatus] =
    useState<ApiNoticeConnectionStatus | null>(null)
  const [apiNoticeConfig, setApiNoticeConfig] =
    useState<ApiNoticeConfig | null>(null)
  const [apiNoticeBaseURL, setApiNoticeBaseURL] = useState('')
  const [apiNoticeAPIKey, setApiNoticeAPIKey] = useState('')
  const [apiNoticeAPIKeyDirty, setApiNoticeAPIKeyDirty] = useState(false)
  const [testSendResult, setTestSendResult] =
    useState<AlertRuleTestSendResult | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [testingConnection, setTestingConnection] = useState(false)
  const [testingSend, setTestingSend] = useState(false)
  const [retryingProviderCatalog, setRetryingProviderCatalog] = useState(false)
  const [providerCatalogRetryFailed, setProviderCatalogRetryFailed] =
    useState(false)
  const [showValidation, setShowValidation] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [togglingRuleID, setTogglingRuleID] = useState<number | null>(null)

  const rulesQuery = useQuery({
    queryKey: alertRulesQueryKey,
    queryFn: getAlertRules,
    enabled: open,
  })
  const providersQuery = useQuery({
    queryKey: alertRuleProvidersQueryKey,
    queryFn: getAlertRuleProviders,
    enabled: open,
  })
  const configQuery = useQuery({
    queryKey: apiNoticeConfigQueryKey,
    queryFn: getApiNoticeConfig,
    enabled: open,
  })
  const rules = rulesQuery.data?.data ?? emptyRules
  const providerCatalog = providersQuery.data?.data
  const loadedApiNoticeConfig = configQuery.data?.data
  const providers = providerCatalog?.providers ?? emptyProviders

  useEffect(() => {
    if (!open || !loadedApiNoticeConfig) return
    setApiNoticeConfig(loadedApiNoticeConfig)
    setApiNoticeBaseURL(loadedApiNoticeConfig.base_url)
    setApiNoticeAPIKey('')
    setApiNoticeAPIKeyDirty(false)
  }, [open, loadedApiNoticeConfig])

  const selectRule = useCallback(
    (rule: AlertRule) => {
      setEditingRuleID(rule.id)
      setDraft(alertRuleToInput(rule))
      setActiveProvider(rule.providers[0] || providers[0]?.name || '')
      setPreview(null)
      setTestSendResult(null)
      setShowValidation(false)
      setEditorOpen(true)
    },
    [providers]
  )

  const startNewRule = useCallback(
    (catalog: AlertRuleProviderCatalog) => {
      const catalogProviders = catalog.providers
      const defaultProviders = catalogProviders
        .filter((provider) => provider.default && provider.ready)
        .map((provider) => provider.name)
      const providerNames =
        defaultProviders.length > 0
          ? defaultProviders
          : catalogProviders
              .filter((provider) => provider.ready)
              .slice(0, 1)
              .map((provider) => provider.name)
      const nextDraft = createDefaultAlertRuleInput(providerNames)
      setEditingRuleID(null)
      setDraft({
        ...nextDraft,
        name: t(getAlertRuleTriggerTypeLabel(nextDraft.trigger_type)),
      })
      setActiveProvider(providerNames[0] || catalogProviders[0]?.name || '')
      setPreview(null)
      setTestSendResult(null)
      setShowValidation(false)
      setEditorOpen(true)
    },
    [t]
  )

  useEffect(() => {
    if (open) return
    setEditorOpen(false)
    setEditingRuleID(null)
    setDraft(null)
    setActiveProvider('')
    setPreviewEventType('trigger')
    setSampleChannelID(0)
    setPreview(null)
    setConnectionStatus(null)
    setApiNoticeConfig(null)
    setApiNoticeBaseURL('')
    setApiNoticeAPIKey('')
    setApiNoticeAPIKeyDirty(false)
    setTestSendResult(null)
    setRetryingProviderCatalog(false)
    setProviderCatalogRetryFailed(false)
    setShowValidation(false)
  }, [open])

  const validationErrors = useMemo(
    () => (draft ? validateAlertRuleDraft(draft, providers) : []),
    [draft, providers]
  )

  const saveMutation = useMutation({
    mutationFn: async ({
      id,
      input,
    }: {
      id: number | null
      input: AlertRuleInput
    }) => (id === null ? createAlertRule(input) : updateAlertRule(id, input)),
    onSuccess: async (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to save alert rule'))
        return
      }
      setEditingRuleID(response.data.id)
      setDraft(alertRuleToInput(response.data))
      setShowValidation(false)
      await queryClient.invalidateQueries({ queryKey: alertRulesQueryKey })
      setEditorOpen(false)
      toast.success(t('Alert rule saved'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to save alert rule')
      )
    },
  })

  const apiNoticeAPIKeyRevealMutation = useMutation({
    mutationFn: async () => {
      const response = await revealApiNoticeAPIKey()
      if (!response.success || !response.data?.api_key) {
        throw new Error(response.message || t('Failed to reveal upstream key'))
      }
      return response.data.api_key
    },
    onSuccess: (apiKey) => {
      setApiNoticeAPIKey(apiKey)
      setApiNoticeAPIKeyDirty(false)
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to reveal upstream key')
      )
    },
  })

  const apiNoticeConfigMutation = useMutation({
    mutationFn: updateApiNoticeConfig,
    onSuccess: async (response) => {
      if (!response.success || !response.data) {
        toast.error(
          response.message || t('Failed to save api-notice configuration')
        )
        return
      }
      setApiNoticeConfig(response.data)
      setApiNoticeBaseURL(response.data.base_url)
      setApiNoticeAPIKey('')
      setApiNoticeAPIKeyDirty(false)
      await queryClient.invalidateQueries({
        queryKey: apiNoticeConfigQueryKey,
      })
      await queryClient.invalidateQueries({
        queryKey: alertRuleProvidersQueryKey,
      })
      toast.success(t('api-notice configuration saved'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save api-notice configuration')
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteAlertRule,
    onSuccess: async (response) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to delete alert rule'))
        return
      }
      setDeleteOpen(false)
      await queryClient.invalidateQueries({ queryKey: alertRulesQueryKey })
      setEditorOpen(false)
      setEditingRuleID(null)
      setDraft(null)
      toast.success(t('Alert rule deleted'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to delete alert rule')
      )
    },
  })

  const toggleRuleMutation = useMutation({
    mutationFn: async ({
      rule,
      enabled,
    }: {
      rule: AlertRule
      enabled: boolean
    }) =>
      updateAlertRule(rule.id, {
        ...alertRuleToInput(rule),
        enabled,
      }),
    onMutate: ({ rule }) => {
      setTogglingRuleID(rule.id)
    },
    onSuccess: async (response) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to save alert rule'))
        return
      }
      await queryClient.invalidateQueries({ queryKey: alertRulesQueryKey })
      toast.success(t('Alert rule saved'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to save alert rule')
      )
    },
    onSettled: () => {
      setTogglingRuleID(null)
    },
  })

  function resetTransientResults() {
    setPreview(null)
    setTestSendResult(null)
  }

  function updateDraft(updater: (current: AlertRuleInput) => AlertRuleInput) {
    setDraft((current) => (current ? updater(current) : current))
    resetTransientResults()
  }

  function ensureDraftIsValid() {
    if (!draft) return false
    if (validationErrors.length === 0) return true
    setShowValidation(true)
    toast.error(t(validationErrors[0]))
    return false
  }

  function handleSave() {
    if (!draft || !ensureDraftIsValid()) return
    saveMutation.mutate({ id: editingRuleID, input: draft })
  }

  async function handlePreview() {
    if (!draft || !ensureDraftIsValid()) return
    setPreviewing(true)
    try {
      const response = await previewAlertRule(
        buildAlertRulePreviewRequest(
          draft,
          previewEventType,
          draft.trigger_type === 'enabled_channel_count' ? 0 : sampleChannelID
        )
      )
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to preview alert message'))
        return
      }
      setPreview(response.data)
      toast.success(t('Alert message preview updated'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to preview alert message')
      )
    } finally {
      setPreviewing(false)
    }
  }

  async function handleRetryProviderCatalog() {
    if (retryingProviderCatalog || providersQuery.isFetching) return

    setRetryingProviderCatalog(true)
    setProviderCatalogRetryFailed(false)
    const toastID = toast.loading(t('Refreshing...'))

    try {
      const [result] = await Promise.all([
        providersQuery.refetch(),
        new Promise((resolve) =>
          setTimeout(resolve, providerCatalogRetryFeedbackDelayMs)
        ),
      ])
      const response = result.data
      if (!response?.success || !response.data) {
        setProviderCatalogRetryFailed(true)
        const message =
          response?.message ||
          (result.error instanceof Error ? result.error.message : '')
        toast.error(
          message ? `${t('Refresh failed')}: ${message}` : t('Refresh failed'),
          { id: toastID }
        )
        return
      }

      toast.success(t('Updated successfully'), { id: toastID })
    } catch (error) {
      setProviderCatalogRetryFailed(true)
      const message = error instanceof Error ? error.message : ''
      toast.error(
        message ? `${t('Refresh failed')}: ${message}` : t('Refresh failed'),
        { id: toastID }
      )
    } finally {
      setRetryingProviderCatalog(false)
    }
  }

  async function handleTestConnection() {
    setTestingConnection(true)
    try {
      const response = await testAlertRuleConnection()
      if (response.data) setConnectionStatus(response.data)
      await queryClient.invalidateQueries({
        queryKey: alertRuleProvidersQueryKey,
      })
      if (!response.success) {
        toast.error(response.message || t('api-notice connection test failed'))
        return
      }
      toast.success(t('api-notice connection is ready'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('api-notice connection test failed')
      )
    } finally {
      setTestingConnection(false)
    }
  }

  async function handleTestSend(testProviders: string[]) {
    if (!draft || !ensureDraftIsValid()) return
    if (!providerCatalog) {
      toast.error(
        t('Check the api-notice provider catalog and readiness status.')
      )
      return
    }
    if (!apiKeyConfigured) {
      toast.error(t('api-notice API Key is not configured'))
      return
    }
    setTestingSend(true)
    try {
      const response = await testSendAlertRule(
        buildAlertRuleTestSendRequest(
          draft,
          previewEventType,
          draft.trigger_type === 'enabled_channel_count' ? 0 : sampleChannelID,
          testProviders
        )
      )
      if (response.data) setTestSendResult(response.data)
      if (!response.success) {
        toast.error(response.message || t('Failed to send test notification'))
        return
      }
      toast.success(t('Test notification sent'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to send test notification')
      )
    } finally {
      setTestingSend(false)
    }
  }

  function handleOpenChange(nextOpen: boolean) {
    if (
      saveMutation.isPending ||
      deleteMutation.isPending ||
      previewing ||
      testingConnection ||
      testingSend ||
      apiNoticeConfigMutation.isPending ||
      apiNoticeAPIKeyRevealMutation.isPending ||
      toggleRuleMutation.isPending ||
      retryingProviderCatalog
    ) {
      return
    }
    if (!nextOpen) setEditorOpen(false)
    onOpenChange(nextOpen)
  }

  function handleEditorOpenChange(nextOpen: boolean) {
    if (
      saveMutation.isPending ||
      deleteMutation.isPending ||
      previewing ||
      testingConnection ||
      testingSend
    ) {
      return
    }
    setEditorOpen(nextOpen)
    if (nextOpen) return
    setDeleteOpen(false)
    setEditingRuleID(null)
    setDraft(null)
    setShowValidation(false)
    resetTransientResults()
  }

  const providerCatalogError = providerCatalog
    ? undefined
    : (providersQuery.data && !providersQuery.data.success
        ? providersQuery.data.message
        : undefined) ||
      (providersQuery.error instanceof Error
        ? providersQuery.error.message
        : undefined)
  const rulesLoadError =
    (rulesQuery.data && !rulesQuery.data.success
      ? rulesQuery.data.message
      : undefined) ||
    (rulesQuery.error instanceof Error ? rulesQuery.error.message : undefined)
  const configLoadError =
    (configQuery.data && !configQuery.data.success
      ? configQuery.data.message
      : undefined) ||
    (configQuery.error instanceof Error ? configQuery.error.message : undefined)
  const loading = rulesQuery.isLoading || configQuery.isLoading
  const loadError = rulesLoadError || configLoadError
  const providerCatalogLoaded = Boolean(providerCatalog)
  const providerCatalogRefreshing =
    retryingProviderCatalog || providersQuery.isFetching
  const apiKeyConfigured = Boolean(
    providerCatalog?.api_key_configured || apiNoticeConfig?.api_key_configured
  )

  let listDialogBody: ReactNode
  if (loading) {
    listDialogBody = (
      <div className='flex min-h-72 items-center justify-center'>
        <LoaderCircle className='text-muted-foreground size-6 animate-spin' />
      </div>
    )
  } else if (loadError) {
    listDialogBody = (
      <div className='min-h-72 p-4'>
        <Alert variant='destructive'>
          <AlertTitle>
            {t('Failed to load alert rule configuration')}
          </AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      </div>
    )
  } else {
    listDialogBody = (
      <div className='min-h-0 flex-1 space-y-4 overflow-y-auto p-4'>
        {providerCatalogError && (
          <Alert variant='destructive'>
            <AlertTriangle />
            <AlertTitle>
              {t('api-notice provider catalog unavailable')}
            </AlertTitle>
            <AlertDescription
              className='flex flex-wrap items-center gap-3'
              aria-busy={providerCatalogRefreshing}
            >
              <span>
                {providerCatalogRefreshing
                  ? t('Refreshing...')
                  : t(
                      'Check the api-notice provider catalog and readiness status.'
                    )}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={providerCatalogRefreshing}
                onClick={() => void handleRetryProviderCatalog()}
              >
                {providerCatalogRefreshing ? (
                  <LoaderCircle
                    data-icon='inline-start'
                    className='animate-spin'
                    aria-hidden='true'
                  />
                ) : (
                  <RefreshCw data-icon='inline-start' aria-hidden='true' />
                )}
                {providerCatalogRefreshing ? t('Refreshing...') : t('Retry')}
              </Button>
              {providerCatalogRetryFailed && !providerCatalogRefreshing && (
                <span
                  className='w-full text-xs font-medium'
                  role='status'
                  aria-live='polite'
                >
                  {t('Refresh failed')}
                </span>
              )}
            </AlertDescription>
          </Alert>
        )}

        {!apiKeyConfigured && (
          <Alert variant='destructive'>
            <AlertTriangle />
            <AlertTitle>{t('api-notice API Key is not configured')}</AlertTitle>
            <AlertDescription>
              {t(
                'Save the shared API Key here or set API_NOTICE_API_KEY on the new-api backend before sending notifications.'
              )}
            </AlertDescription>
          </Alert>
        )}

        <ApiNoticeConfiguration
          config={apiNoticeConfig}
          baseURL={apiNoticeBaseURL}
          apiKey={apiNoticeAPIKey}
          saving={apiNoticeConfigMutation.isPending}
          revealing={apiNoticeAPIKeyRevealMutation.isPending}
          onBaseURLChange={setApiNoticeBaseURL}
          onApiKeyChange={(value) => {
            setApiNoticeAPIKey(value)
            setApiNoticeAPIKeyDirty(true)
          }}
          onReveal={() => apiNoticeAPIKeyRevealMutation.mutateAsync()}
          onSave={() =>
            apiNoticeConfigMutation.mutate({
              base_url: apiNoticeBaseURL,
              api_key: apiNoticeAPIKeyDirty ? apiNoticeAPIKey : '',
            })
          }
        />

        <section className='rounded-lg border'>
          <div className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3'>
            <div>
              <h3 className='text-sm font-semibold'>{t('Rules')}</h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t(
                  'Select a rule to edit it, or toggle it directly from the list.'
                )}
              </p>
            </div>
            <Button
              type='button'
              size='sm'
              disabled={!providerCatalog}
              onClick={() => providerCatalog && startNewRule(providerCatalog)}
            >
              <CirclePlus />
              {t('New alert rule')}
            </Button>
          </div>
          <RuleList
            rules={rules}
            togglingRuleID={togglingRuleID}
            onEdit={selectRule}
            onToggle={(rule, enabled) =>
              toggleRuleMutation.mutate({ rule, enabled })
            }
          />
        </section>
      </div>
    )
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className='flex max-h-[92vh] w-[min(96vw,64rem)] max-w-none flex-col overflow-hidden p-0 sm:max-w-none'>
          <DialogHeader className='border-b px-5 pt-5 pb-4'>
            <DialogTitle>{t('Alert notification rules')}</DialogTitle>
            <DialogDescription>
              {t(
                'Configure root-only rules that send authenticated notifications through api-notice.'
              )}
            </DialogDescription>
          </DialogHeader>

          {listDialogBody}

          <DialogFooter className='border-t px-5 py-3'>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
            >
              {t('Close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={editorOpen} onOpenChange={handleEditorOpenChange}>
        <DialogContent className='flex max-h-[94vh] w-[min(96vw,78rem)] max-w-none flex-col overflow-hidden p-0 sm:max-w-none'>
          <DialogHeader className='border-b px-5 pt-4 pb-3'>
            <DialogTitle>
              {editingRuleID === null ? t('New alert rule') : t('Edit')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Configure the trigger, providers, message, and test settings.'
              )}
            </DialogDescription>
          </DialogHeader>

          <div className='min-h-0 flex-1 overflow-y-auto p-4'>
            {draft && (
              <div className='space-y-3'>
                {providerCatalogError && (
                  <Alert variant='destructive'>
                    <AlertTriangle />
                    <AlertTitle>
                      {t('api-notice provider catalog unavailable')}
                    </AlertTitle>
                    <AlertDescription>
                      {t(
                        'Check the api-notice provider catalog and readiness status.'
                      )}
                    </AlertDescription>
                  </Alert>
                )}

                <BasicRuleFields draft={draft} onChange={updateDraft} />

                <ProviderSelector
                  providers={providers}
                  draft={draft}
                  activeProvider={activeProvider}
                  onActiveProviderChange={setActiveProvider}
                  onChange={updateDraft}
                />

                <MessageConfiguration
                  draft={draft}
                  providers={providers}
                  onChange={updateDraft}
                />

                <TestAndPreviewPanel
                  channels={channels}
                  triggerType={draft.trigger_type}
                  messageFormat={draft.message_format}
                  eventType={previewEventType}
                  sampleChannelID={sampleChannelID}
                  preview={preview}
                  connectionStatus={connectionStatus}
                  testSendResult={testSendResult}
                  previewing={previewing}
                  testingConnection={testingConnection}
                  testingSend={testingSend}
                  providers={providers}
                  canSendTest={providerCatalogLoaded && apiKeyConfigured}
                  onEventTypeChange={setPreviewEventType}
                  onSampleChannelChange={setSampleChannelID}
                  onPreview={handlePreview}
                  onTestConnection={handleTestConnection}
                  onTestSend={handleTestSend}
                />

                {showValidation && validationErrors.length > 0 && (
                  <Alert variant='destructive'>
                    <AlertTriangle />
                    <AlertTitle>
                      {t('Fix the rule before continuing')}
                    </AlertTitle>
                    <AlertDescription>
                      <ul className='list-disc space-y-1 pl-4'>
                        {validationErrors.map((error) => (
                          <li key={error}>{t(error)}</li>
                        ))}
                      </ul>
                    </AlertDescription>
                  </Alert>
                )}
              </div>
            )}
          </div>

          <DialogFooter className='border-t px-5 py-3'>
            {editingRuleID !== null && (
              <Button
                type='button'
                variant='destructive'
                className='mr-auto'
                disabled={deleteMutation.isPending}
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 />
                {t('Delete rule')}
              </Button>
            )}
            <Button
              type='button'
              variant='outline'
              onClick={() => handleEditorOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={
                !draft || !providerCatalogLoaded || saveMutation.isPending
              }
              onClick={handleSave}
            >
              {saveMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {editingRuleID === null ? t('Create rule') : t('Save rule')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete alert rule?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This deletes the rule and its current alert state. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending || editingRuleID === null}
              onClick={() =>
                editingRuleID !== null && deleteMutation.mutate(editingRuleID)
              }
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

interface RuleListProps {
  rules: AlertRule[]
  togglingRuleID: number | null
  onEdit: (rule: AlertRule) => void
  onToggle: (rule: AlertRule, enabled: boolean) => void
}

function RuleList({ rules, togglingRuleID, onEdit, onToggle }: RuleListProps) {
  const { t } = useTranslation()
  if (rules.length === 0) {
    return (
      <p className='text-muted-foreground px-4 py-8 text-center text-sm'>
        {t('No alert rules configured')}
      </p>
    )
  }

  return (
    <div className='grid gap-2 p-3 md:grid-cols-2'>
      {rules.map((rule) => {
        const toggling = togglingRuleID === rule.id
        return (
          <div
            key={rule.id}
            className='bg-background hover:border-primary/50 flex min-w-0 items-center gap-3 rounded-md border px-3 py-2.5 transition-colors'
          >
            <button
              type='button'
              className='min-w-0 flex-1 text-left'
              onClick={() => onEdit(rule)}
            >
              <span className='flex min-w-0 items-center gap-2'>
                <span className='min-w-0 truncate text-sm font-medium'>
                  {rule.name}
                </span>
                <Badge variant={stateBadgeVariant(rule.state.state)}>
                  {t(rule.state.state)}
                </Badge>
              </span>
              <span className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
                <span>
                  {t(getAlertRuleTriggerTypeLabel(rule.trigger_type))}
                </span>
                <span>
                  {t('{{count}} providers', { count: rule.providers.length })}
                </span>
                {rule.state.last_error_summary && (
                  <span className='text-destructive truncate'>
                    {rule.state.last_error_summary}
                  </span>
                )}
              </span>
            </button>
            <div className='flex shrink-0 items-center gap-2'>
              {toggling && (
                <LoaderCircle className='text-muted-foreground size-4 animate-spin' />
              )}
              <Switch
                checked={rule.enabled}
                disabled={togglingRuleID !== null}
                aria-label={rule.enabled ? t('Disable') : t('Enable')}
                onCheckedChange={(checked) => onToggle(rule, checked)}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                aria-label={t('Edit')}
                onClick={() => onEdit(rule)}
              >
                <Pencil />
              </Button>
            </div>
          </div>
        )
      })}
    </div>
  )
}

interface DraftSectionProps {
  draft: AlertRuleInput
  onChange: (updater: (current: AlertRuleInput) => AlertRuleInput) => void
}

function BasicRuleFields({ draft, onChange }: DraftSectionProps) {
  const { t } = useTranslation()
  const isEnabledChannelCount = draft.trigger_type === 'enabled_channel_count'
  return (
    <Card>
      <CardHeader className='px-4 pt-4 pb-2'>
        <CardTitle>{t('Basic rule')}</CardTitle>
        <CardDescription>
          {t('Define the trigger condition and recovery behavior.')}
        </CardDescription>
      </CardHeader>
      <CardContent className='grid gap-3 px-4 pb-4 md:grid-cols-2 xl:grid-cols-4'>
        <Field label={t('Rule name')} className='xl:col-span-2'>
          <Input
            value={draft.name}
            maxLength={128}
            onChange={(event) =>
              onChange((current) => ({
                ...current,
                name: event.target.value,
              }))
            }
          />
        </Field>
        <Field label={t('Trigger condition type')} className='xl:col-span-2'>
          <NativeSelect
            className='w-full'
            value={draft.trigger_type}
            onChange={(event) => {
              const triggerType = event.target.value as AlertRuleTriggerType
              onChange((current) => {
                const nextDraft = switchAlertRuleTriggerType(
                  current,
                  triggerType
                )
                return {
                  ...nextDraft,
                  name: t(getAlertRuleTriggerTypeLabel(triggerType)),
                }
              })
            }}
          >
            <option value='upstream_channel_effective_balance'>
              {t('Upstream channel effective balance')}
            </option>
            <option value='enabled_channel_count'>
              {t('Available channel count')}
            </option>
          </NativeSelect>
        </Field>
        <Field label={t('Comparison operator')}>
          <NativeSelect
            className='w-full'
            value={draft.trigger_config.operator}
            disabled={isEnabledChannelCount}
            onChange={(event) =>
              onChange((current) => ({
                ...current,
                trigger_config: {
                  ...current.trigger_config,
                  operator: event.target
                    .value as AlertRuleInput['trigger_config']['operator'],
                },
              }))
            }
          >
            <option value='lt'>{t('Less than')}</option>
            <option value='lte'>{t('Less than or equal')}</option>
            <option value='gt'>{t('Greater than')}</option>
            <option value='gte'>{t('Greater than or equal')}</option>
            <option value='eq'>{t('Equal to')}</option>
          </NativeSelect>
        </Field>
        <Field
          label={t('Threshold')}
          description={
            isEnabledChannelCount
              ? t('Only enabled local channels are counted.')
              : undefined
          }
        >
          <Input
            type='number'
            min='0'
            max={isEnabledChannelCount ? '1000000' : '1000000000'}
            step={isEnabledChannelCount ? '1' : 'any'}
            value={draft.trigger_config.threshold}
            onChange={(event) =>
              onChange((current) => ({
                ...current,
                trigger_config: {
                  ...current.trigger_config,
                  threshold: Number(event.target.value),
                },
              }))
            }
          />
        </Field>
        <ToggleField
          label={t('Enabled')}
          description={t('Evaluate this rule when monitored data changes.')}
          checked={draft.enabled}
          onCheckedChange={(checked) =>
            onChange((current) => ({ ...current, enabled: checked }))
          }
        />
        <ToggleField
          label={t('Send recovery notification')}
          description={t('Send once when an active alert returns to normal.')}
          checked={draft.send_recovery}
          onCheckedChange={(checked) =>
            onChange((current) => ({ ...current, send_recovery: checked }))
          }
        />
        {!isEnabledChannelCount && (
          <>
            <Field
              label={t('Statistics window (seconds)')}
              description={t(
                '0 means each refresh is evaluated independently.'
              )}
            >
              <Input
                type='number'
                min='0'
                max='2592000'
                step='1'
                value={draft.trigger_config.window_seconds}
                onChange={(event) =>
                  onChange((current) => ({
                    ...current,
                    trigger_config: {
                      ...current.trigger_config,
                      window_seconds: Number(event.target.value),
                    },
                  }))
                }
              />
            </Field>
            <Field label={t('Consecutive matches')}>
              <Input
                type='number'
                min='1'
                max='1000'
                step='1'
                value={draft.consecutive_required}
                onChange={(event) =>
                  onChange((current) => ({
                    ...current,
                    consecutive_required: Number(event.target.value),
                  }))
                }
              />
            </Field>
            <Field label={t('Cooldown (seconds)')}>
              <Input
                type='number'
                min='0'
                max='2592000'
                step='1'
                value={draft.cooldown_seconds}
                onChange={(event) =>
                  onChange((current) => ({
                    ...current,
                    cooldown_seconds: Number(event.target.value),
                  }))
                }
              />
            </Field>
          </>
        )}
      </CardContent>
    </Card>
  )
}

interface ProviderSelectorProps extends DraftSectionProps {
  providers: ApiNoticeProvider[]
  activeProvider: string
  onActiveProviderChange: (provider: string) => void
}

function ProviderSelector({
  providers,
  draft,
  activeProvider,
  onActiveProviderChange,
  onChange,
}: ProviderSelectorProps) {
  const { t } = useTranslation()

  function toggleProvider(provider: ApiNoticeProvider, checked: boolean) {
    onChange((current) => {
      let nextProviders = checked
        ? [...new Set([...current.providers, provider.name])]
        : current.providers.filter((name) => name !== provider.name)
      nextProviders = nextProviders.sort((left, right) =>
        left.localeCompare(right)
      )
      let nextDraft = { ...current, providers: nextProviders }
      const formatOptions = getAlertMessageFormatOptions(
        providers,
        nextProviders
      )
      if (
        !formatOptions.some(
          (option) =>
            option.format === current.message_format && option.available
        )
      ) {
        const nextFormat = formatOptions.find(
          (option) => option.available
        )?.format
        if (nextFormat) {
          nextDraft = switchAlertMessageFormat(nextDraft, nextFormat)
        }
      }
      return nextDraft
    })
  }

  const selectedProvider =
    providers.find((provider) => provider.name === activeProvider) ||
    providers[0]

  return (
    <Card>
      <CardHeader className='px-4 pt-4 pb-2'>
        <CardTitle>{t('Notification providers')}</CardTitle>
        <CardDescription>
          {t(
            'Provider readiness and capabilities come from api-notice. Base URL and API Key are shared backend configuration.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='px-4 pb-4'>
        {providers.length === 0 ? (
          <Alert variant='destructive'>
            <AlertTriangle />
            <AlertTitle>{t('No notification providers available')}</AlertTitle>
            <AlertDescription>
              {t('Check the api-notice provider catalog and readiness status.')}
            </AlertDescription>
          </Alert>
        ) : (
          <Tabs
            value={selectedProvider?.name || ''}
            onValueChange={onActiveProviderChange as (value: string) => void}
          >
            <TabsList className='max-w-full flex-wrap justify-start'>
              {providers.map((provider) => (
                <TabsTrigger key={provider.name} value={provider.name}>
                  <span
                    className={cn(
                      'size-2 rounded-full',
                      provider.ready ? 'bg-emerald-500' : 'bg-destructive'
                    )}
                  />
                  {provider.name}
                </TabsTrigger>
              ))}
            </TabsList>
            {providers.map((provider) => {
              const selected = draft.providers.includes(provider.name)
              const cannotSelect = !provider.ready && !selected
              return (
                <TabsContent
                  key={provider.name}
                  value={provider.name}
                  className='mt-2 rounded-md border p-3'
                >
                  <div className='flex flex-wrap items-start justify-between gap-3'>
                    <div>
                      <div className='flex flex-wrap items-center gap-2'>
                        <span className='font-medium'>{provider.name}</span>
                        {provider.default && (
                          <Badge variant='secondary'>{t('Default')}</Badge>
                        )}
                        <Badge
                          variant={provider.ready ? 'secondary' : 'destructive'}
                        >
                          {provider.ready ? t('Ready') : t('Not ready')}
                        </Badge>
                      </div>
                      {!provider.ready && (
                        <p className='text-destructive mt-2 text-sm'>
                          {getProviderUnavailableReason(provider)}
                        </p>
                      )}
                    </div>
                    <label className='flex items-center gap-2 text-sm font-medium'>
                      <Checkbox
                        checked={selected}
                        disabled={cannotSelect}
                        onCheckedChange={(checked) =>
                          toggleProvider(provider, checked === true)
                        }
                      />
                      {t('Use this provider')}
                    </label>
                  </div>
                  <div className='mt-3'>
                    <p className='text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase'>
                      {t('Supported message formats')}
                    </p>
                    <div className='flex flex-wrap gap-2'>
                      {provider.capabilities.map((capability) => (
                        <Badge key={capability} variant='outline'>
                          {t(capability)}
                        </Badge>
                      ))}
                    </div>
                  </div>
                </TabsContent>
              )
            })}
          </Tabs>
        )}
      </CardContent>
    </Card>
  )
}

interface MessageConfigurationProps extends DraftSectionProps {
  providers: ApiNoticeProvider[]
}

function MessageConfiguration({
  draft,
  providers,
  onChange,
}: MessageConfigurationProps) {
  const { t } = useTranslation()
  const formatOptions = getAlertMessageFormatOptions(providers, draft.providers)

  function changeMessage(
    updater: (message: ApiNoticeMessage) => ApiNoticeMessage
  ) {
    onChange((current) => ({
      ...current,
      message_template: updater(current.message_template),
    }))
  }

  return (
    <Card>
      <CardHeader className='px-4 pt-4 pb-2'>
        <CardTitle>{t('Message configuration')}</CardTitle>
        <CardDescription>
          {t(
            'Only predefined template variables are supported. Templates never execute code, shell commands, or arbitrary functions.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3 px-4 pb-4'>
        <Tabs
          value={draft.message_format}
          onValueChange={(value) =>
            onChange((current) =>
              switchAlertMessageFormat(current, value as AlertMessageFormat)
            )
          }
        >
          <TabsList className='grid w-full grid-cols-4'>
            {formatOptions.map((option) => (
              <TabsTrigger
                key={option.format}
                value={option.format}
                disabled={!option.available}
              >
                {t(formatLabel(option.format))}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value='text' className='mt-4'>
            <Field label={t('Text content')}>
              <Textarea
                rows={8}
                value={draft.message_template.text || ''}
                onChange={(event) =>
                  changeMessage((message) => ({
                    ...message,
                    text: event.target.value,
                  }))
                }
              />
            </Field>
          </TabsContent>
          <TabsContent value='markdown' className='mt-4'>
            <div className='grid gap-4 lg:grid-cols-2'>
              <Field label={t('Markdown content')}>
                <Textarea
                  rows={12}
                  value={draft.message_template.text || ''}
                  onChange={(event) =>
                    changeMessage((message) => ({
                      ...message,
                      text: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field label={t('Local Markdown preview')}>
                <div className='bg-muted/20 min-h-72 rounded-lg border p-4'>
                  <Markdown>
                    {draft.message_template.text || t('Nothing to preview')}
                  </Markdown>
                </div>
              </Field>
            </div>
          </TabsContent>
          <TabsContent value='card' className='mt-4'>
            <CardMessageEditor
              message={draft.message_template}
              onChange={changeMessage}
            />
          </TabsContent>
          <TabsContent value='table' className='mt-4'>
            <TableMessageEditor
              message={draft.message_template}
              onChange={changeMessage}
            />
          </TabsContent>
        </Tabs>
        <TemplateVariableReference />
      </CardContent>
    </Card>
  )
}

interface MessageEditorProps {
  message: ApiNoticeMessage
  onChange: (updater: (message: ApiNoticeMessage) => ApiNoticeMessage) => void
}

function CardMessageEditor({ message, onChange }: MessageEditorProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <div className='grid gap-4 md:grid-cols-2'>
        <Field label={t('Card title')}>
          <Input
            value={message.title || ''}
            onChange={(event) =>
              onChange((current) => ({ ...current, title: event.target.value }))
            }
          />
        </Field>
        <Field label={t('Card level')}>
          <NativeSelect
            className='w-full'
            value={message.level || 'info'}
            onChange={(event) =>
              onChange((current) => ({ ...current, level: event.target.value }))
            }
          >
            <option value='info'>{t('Info')}</option>
            <option value='warning'>{t('Warning')}</option>
            <option value='critical'>{t('Critical')}</option>
            <option value='success'>{t('Success')}</option>
          </NativeSelect>
        </Field>
      </div>
      <Field label={t('Card body')}>
        <Textarea
          rows={5}
          value={message.text || ''}
          onChange={(event) =>
            onChange((current) => ({ ...current, text: event.target.value }))
          }
        />
      </Field>
      <RepeatingEditor
        title={t('Key-value fields')}
        addLabel={t('Add field')}
        items={message.fields || []}
        createItem={() => ({ name: '', value: '' })}
        onChange={(fields) =>
          onChange((current) => ({
            ...current,
            fields: fields as ApiNoticeField[],
          }))
        }
        renderItem={(item, index, update) => {
          const field = item as ApiNoticeField
          return (
            <div className='grid flex-1 gap-2 md:grid-cols-2'>
              <Input
                aria-label={t('Field name')}
                placeholder={t('Field name')}
                value={field.name}
                onChange={(event) =>
                  update(index, { ...field, name: event.target.value })
                }
              />
              <Input
                aria-label={t('Field value')}
                placeholder={t('Field value')}
                value={field.value}
                onChange={(event) =>
                  update(index, { ...field, value: event.target.value })
                }
              />
            </div>
          )
        }}
      />
      <RepeatingEditor
        title={t('Content sections')}
        addLabel={t('Add section')}
        items={message.sections || []}
        createItem={() => ({ title: '', text: '' })}
        onChange={(sections) =>
          onChange((current) => ({
            ...current,
            sections: sections as ApiNoticeSection[],
          }))
        }
        renderItem={(item, index, update) => {
          const section = item as ApiNoticeSection
          return (
            <div className='grid flex-1 gap-2'>
              <Input
                aria-label={t('Section title')}
                placeholder={t('Section title')}
                value={section.title || ''}
                onChange={(event) =>
                  update(index, { ...section, title: event.target.value })
                }
              />
              <Textarea
                aria-label={t('Section content')}
                placeholder={t('Section content')}
                value={section.text}
                onChange={(event) =>
                  update(index, { ...section, text: event.target.value })
                }
              />
            </div>
          )
        }}
      />
      <RepeatingEditor
        title={t('HTTPS action buttons')}
        addLabel={t('Add action')}
        items={message.actions || []}
        createItem={() => ({ label: '', url: 'https://' })}
        onChange={(actions) =>
          onChange((current) => ({
            ...current,
            actions: actions as ApiNoticeAction[],
          }))
        }
        renderItem={(item, index, update) => {
          const action = item as ApiNoticeAction
          return (
            <div className='grid flex-1 gap-2 md:grid-cols-[0.8fr_1.2fr]'>
              <Input
                aria-label={t('Action label')}
                placeholder={t('Action label')}
                value={action.label}
                onChange={(event) =>
                  update(index, { ...action, label: event.target.value })
                }
              />
              <Input
                aria-label={t('HTTPS URL')}
                placeholder='https://'
                value={action.url}
                onChange={(event) =>
                  update(index, { ...action, url: event.target.value })
                }
              />
            </div>
          )
        }}
      />
    </div>
  )
}

function TableMessageEditor({ message, onChange }: MessageEditorProps) {
  const { t } = useTranslation()
  const columns = message.table?.columns || []
  const rows = message.table?.rows || []
  const columnListKeys = useStableListKeys(columns.length, 'alert-table-column')
  const rowListKeys = useStableListKeys(rows.length, 'alert-table-row')
  const keyedColumns = columns.map((column, index) => ({
    column,
    index,
    key: columnListKeys.keys[index],
  }))
  const keyedRows = rows.map((row, index) => ({
    row,
    index,
    key: rowListKeys.keys[index],
  }))

  function updateTable(
    nextColumns: ApiNoticeColumn[],
    nextRows: Array<Record<string, string>>
  ) {
    onChange((current) => ({
      ...current,
      table: { columns: nextColumns, rows: nextRows },
    }))
  }

  function updateColumn(index: number, column: ApiNoticeColumn) {
    const previous = columns[index]
    const nextColumns = columns.map((item, itemIndex) =>
      itemIndex === index ? column : item
    )
    let nextRows = rows
    if (previous && previous.key !== column.key) {
      nextRows = rows.map((row) => {
        const nextRow = { ...row }
        const previousValue = nextRow[previous.key] || ''
        delete nextRow[previous.key]
        nextRow[column.key] = previousValue
        return nextRow
      })
    }
    updateTable(nextColumns, nextRows)
  }

  function removeColumn(index: number) {
    const removedKey = columns[index]?.key
    const nextColumns = columns.filter((_, itemIndex) => itemIndex !== index)
    const nextRows = rows.map((row) => {
      const nextRow = { ...row }
      if (removedKey) {
        delete nextRow[removedKey]
      }
      return nextRow
    })
    columnListKeys.remove(index)
    updateTable(nextColumns, nextRows)
  }

  function addColumn() {
    let suffix = columns.length + 1
    let key = `column_${suffix}`
    while (columns.some((column) => column.key === key)) {
      suffix += 1
      key = `column_${suffix}`
    }
    columnListKeys.append()
    updateTable(
      [...columns, { key, label: t('Column {{count}}', { count: suffix }) }],
      rows.map((row) => ({ ...row, [key]: '' }))
    )
  }

  function addRow() {
    rowListKeys.append()
    updateTable(columns, [
      ...rows,
      Object.fromEntries(columns.map((column) => [column.key, ''])),
    ])
  }

  function removeRow(index: number) {
    rowListKeys.remove(index)
    updateTable(
      columns,
      rows.filter((_, itemIndex) => itemIndex !== index)
    )
  }

  return (
    <div className='space-y-4'>
      <div className='grid gap-4 md:grid-cols-2'>
        <Field label={t('Table title')}>
          <Input
            value={message.title || ''}
            onChange={(event) =>
              onChange((current) => ({ ...current, title: event.target.value }))
            }
          />
        </Field>
        <Field label={t('Table description')}>
          <Input
            value={message.text || ''}
            onChange={(event) =>
              onChange((current) => ({ ...current, text: event.target.value }))
            }
          />
        </Field>
      </div>
      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label>{t('Columns')}</Label>
          <Button type='button' size='sm' variant='outline' onClick={addColumn}>
            <Plus />
            {t('Add column')}
          </Button>
        </div>
        {keyedColumns.map(({ column, index, key }) => (
          <div key={key} className='flex gap-2'>
            <div className='grid flex-1 gap-2 md:grid-cols-2'>
              <Input
                aria-label={t('Column key')}
                placeholder={t('Column key')}
                value={column.key}
                onChange={(event) =>
                  updateColumn(index, { ...column, key: event.target.value })
                }
              />
              <Input
                aria-label={t('Column label')}
                placeholder={t('Column label')}
                value={column.label}
                onChange={(event) =>
                  updateColumn(index, { ...column, label: event.target.value })
                }
              />
            </div>
            <Button
              type='button'
              size='icon'
              variant='ghost'
              aria-label={t('Remove column')}
              onClick={() => removeColumn(index)}
            >
              <X />
            </Button>
          </div>
        ))}
      </div>
      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label>{t('Rows')}</Label>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={columns.length === 0}
            onClick={addRow}
          >
            <Plus />
            {t('Add row')}
          </Button>
        </div>
        {keyedRows.map(({ row, index: rowIndex, key }) => (
          <div key={key} className='flex gap-2 rounded-lg border p-3'>
            <div className='grid flex-1 gap-2 md:grid-cols-2 xl:grid-cols-3'>
              {columns.map((column) => (
                <Field key={column.key} label={column.label || column.key}>
                  <Input
                    value={row[column.key] || ''}
                    onChange={(event) =>
                      updateTable(
                        columns,
                        rows.map((item, itemIndex) =>
                          itemIndex === rowIndex
                            ? { ...item, [column.key]: event.target.value }
                            : item
                        )
                      )
                    }
                  />
                </Field>
              ))}
            </div>
            <Button
              type='button'
              size='icon'
              variant='ghost'
              aria-label={t('Remove row')}
              onClick={() => removeRow(rowIndex)}
            >
              <X />
            </Button>
          </div>
        ))}
      </div>
    </div>
  )
}

interface RepeatingEditorProps {
  title: string
  addLabel: string
  items: unknown[]
  createItem: () => unknown
  onChange: (items: unknown[]) => void
  renderItem: (
    item: unknown,
    index: number,
    update: (index: number, item: unknown) => void
  ) => ReactNode
}

function RepeatingEditor({
  title,
  addLabel,
  items,
  createItem,
  onChange,
  renderItem,
}: RepeatingEditorProps) {
  const { t } = useTranslation()
  const itemListKeys = useStableListKeys(items.length, 'alert-repeat-item')
  const keyedItems = items.map((item, index) => ({
    item,
    index,
    key: itemListKeys.keys[index],
  }))

  function update(index: number, item: unknown) {
    onChange(
      items.map((current, itemIndex) => (itemIndex === index ? item : current))
    )
  }

  function addItem() {
    itemListKeys.append()
    onChange([...items, createItem()])
  }

  function removeItem(index: number) {
    itemListKeys.remove(index)
    onChange(items.filter((_, itemIndex) => itemIndex !== index))
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-3'>
        <Label>{title}</Label>
        <Button type='button' size='sm' variant='outline' onClick={addItem}>
          <Plus />
          {addLabel}
        </Button>
      </div>
      {keyedItems.map(({ item, index, key }) => (
        <div key={key} className='flex gap-2 rounded-lg border p-3'>
          {renderItem(item, index, update)}
          <Button
            type='button'
            size='icon'
            variant='ghost'
            aria-label={t('Remove item')}
            onClick={() => removeItem(index)}
          >
            <X />
          </Button>
        </div>
      ))}
    </div>
  )
}

function TemplateVariableReference() {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const templateVariables = [
    {
      name: 'rule.name',
      description: t('The current alert rule name.'),
      example: t('Upstream balance alert'),
    },
    {
      name: 'event.type',
      description: t(
        'The event state: trigger when the alert starts, or recovery when it clears.'
      ),
      example: 'trigger',
    },
    {
      name: 'channel.id',
      description: t(
        'The numeric ID of the upstream channel that produced this event.'
      ),
      example: '42',
    },
    {
      name: 'channel.name',
      description: t(
        'The name of the upstream channel that produced this event.'
      ),
      example: t('Primary upstream channel'),
    },
    {
      name: 'channel.provider',
      description: t(
        'The upstream channel provider identifier, such as new-api or sub2api.'
      ),
      example: 'new-api',
    },
    {
      name: 'channel.balance',
      description: t('The raw balance returned by the upstream channel.'),
      example: '5.25',
    },
    {
      name: 'channel.effective_balance',
      description: t(
        'The balance after applying the channel multiplier; this value is used for condition evaluation.'
      ),
      example: '10.5',
    },
    {
      name: 'channel_pool.enabled_count',
      description: t('The number of enabled local channels.'),
      example: '3',
    },
    {
      name: 'condition.operator',
      description: t(
        'The configured comparison operator: lt, lte, gt, gte, or eq.'
      ),
      example: 'lte',
    },
    {
      name: 'condition.threshold',
      description: t('The threshold configured for the alert condition.'),
      example: '12',
    },
    {
      name: 'observed_at',
      description: t(
        'The channel balance observation time in RFC 3339 format.'
      ),
      example: '2026-07-16T01:00:00+08:00',
    },
  ]

  return (
    <div className='bg-muted/30 rounded-lg border p-3'>
      <button
        type='button'
        className='flex w-full items-center justify-between gap-3 text-left text-sm font-medium'
        aria-expanded={expanded}
        aria-label={expanded ? t('Collapse') : t('Expand')}
        onClick={() => setExpanded((current) => !current)}
      >
        <span>{t('Available template variables')}</span>
        {expanded ? <ChevronUp /> : <ChevronDown />}
      </button>
      {expanded && (
        <div className='mt-3 grid gap-2 md:grid-cols-2'>
          {templateVariables.map((variable) => (
            <div
              key={variable.name}
              className='bg-background min-w-0 rounded-md border p-3'
            >
              <code className='text-foreground block overflow-x-auto text-xs font-semibold whitespace-nowrap'>
                {`{{${variable.name}}}`}
              </code>
              <p className='text-muted-foreground mt-2 text-xs leading-5'>
                {variable.description}
              </p>
              <p className='mt-1.5 flex min-w-0 items-baseline gap-1 text-xs'>
                <span className='text-muted-foreground shrink-0'>
                  {t('Example:')}
                </span>
                <code className='text-foreground break-all'>
                  {variable.example}
                </code>
              </p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

interface TestAndPreviewPanelProps {
  channels: UpstreamChannel[]
  triggerType: AlertRuleTriggerType
  messageFormat: AlertMessageFormat
  eventType: AlertEventType
  sampleChannelID: number
  preview: ApiNoticeMessage | null
  connectionStatus: ApiNoticeConnectionStatus | null
  testSendResult: AlertRuleTestSendResult | null
  previewing: boolean
  testingConnection: boolean
  testingSend: boolean
  providers: ApiNoticeProvider[]
  canSendTest: boolean
  onEventTypeChange: Dispatch<SetStateAction<AlertEventType>>
  onSampleChannelChange: Dispatch<SetStateAction<number>>
  onPreview: () => void
  onTestConnection: () => void
  onTestSend: (providers: string[]) => void
}

function ApiNoticeConfiguration({
  config,
  baseURL,
  apiKey,
  saving,
  revealing,
  onBaseURLChange,
  onApiKeyChange,
  onReveal,
  onSave,
}: {
  config: ApiNoticeConfig | null
  baseURL: string
  apiKey: string
  saving: boolean
  revealing: boolean
  onBaseURLChange: (value: string) => void
  onApiKeyChange: (value: string) => void
  onReveal: () => Promise<string>
  onSave: () => void
}) {
  const { t } = useTranslation()
  const [showAPIKey, setShowAPIKey] = useState(false)
  const apiKeyConfigured = config?.api_key_configured ?? false

  useEffect(() => {
    if (!apiKey) setShowAPIKey(false)
  }, [apiKey])

  async function toggleAPIKeyVisibility() {
    if (apiKey) {
      setShowAPIKey((current) => !current)
      return
    }
    try {
      await onReveal()
      setShowAPIKey(true)
    } catch {
      // The mutation displays the request error.
    }
  }

  let apiKeyVisibilityIcon: ReactNode = <Eye />
  if (revealing) {
    apiKeyVisibilityIcon = <LoaderCircle className='animate-spin' />
  } else if (showAPIKey) {
    apiKeyVisibilityIcon = <EyeOff />
  }

  return (
    <Card>
      <CardHeader className='px-4 pt-4 pb-2'>
        <CardTitle>{t('api-notice connection configuration')}</CardTitle>
        <CardDescription>
          {t(
            'Configure the shared server-side Base URL and Bearer API Key used by WeChat, Feishu, and future providers.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3 px-4 pb-4'>
        <div className='grid gap-3 md:grid-cols-2'>
          <Field
            label={t('Base URL')}
            description={t(
              'Example: http://192.168.10.157:18080. Do not include credentials, query, or fragment.'
            )}
          >
            <Input
              value={baseURL}
              onChange={(event) => onBaseURLChange(event.target.value)}
              placeholder='http://192.168.10.157:18080'
              autoComplete='url'
            />
          </Field>
          <Field
            label={t('API Key')}
            description={t(
              'Sent only by the new-api backend as a Bearer token. Leave blank to keep the saved key.'
            )}
          >
            <div className='relative'>
              <Input
                className='pr-10'
                type={showAPIKey ? 'text' : 'password'}
                value={apiKey}
                onChange={(event) => onApiKeyChange(event.target.value)}
                placeholder={
                  apiKeyConfigured
                    ? config?.api_key_masked ||
                      t('Configured; leave blank to keep')
                    : t('Enter API Key')
                }
                autoComplete='new-password'
              />
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                className='absolute top-1/2 right-1 -translate-y-1/2'
                disabled={revealing || (!apiKey && !apiKeyConfigured)}
                aria-label={showAPIKey ? t('Hide key') : t('Show')}
                onClick={toggleAPIKeyVisibility}
              >
                {apiKeyVisibilityIcon}
              </Button>
            </div>
          </Field>
        </div>
        <div className='flex flex-wrap items-center gap-3'>
          <Button type='button' disabled={saving} onClick={onSave}>
            {saving ? <LoaderCircle className='animate-spin' /> : <Save />}
            {t('Save api-notice configuration')}
          </Button>
          {config && (
            <Badge variant={apiKeyConfigured ? 'secondary' : 'destructive'}>
              {apiKeyConfigured
                ? t('API Key configured')
                : t('API Key not configured')}
            </Badge>
          )}
          {config?.api_key_source && apiKeyConfigured && (
            <span className='text-muted-foreground text-xs'>
              {t('API Key source')}: {config.api_key_source}
            </span>
          )}
        </div>
        {!config?.persistent_storage_available && (
          <Alert>
            <AlertTriangle />
            <AlertDescription>
              {t(
                'Set CRYPTO_SECRET or SESSION_SECRET on the new-api backend before saving an API Key from this page.'
              )}
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}

function TestAndPreviewPanel({
  channels,
  triggerType,
  messageFormat,
  eventType,
  sampleChannelID,
  preview,
  connectionStatus,
  testSendResult,
  previewing,
  testingConnection,
  testingSend,
  providers,
  canSendTest,
  onEventTypeChange,
  onSampleChannelChange,
  onPreview,
  onTestConnection,
  onTestSend,
}: TestAndPreviewPanelProps) {
  const { t } = useTranslation()
  const testableProviders = providers.filter(
    (provider) =>
      provider.ready && provider.capabilities.includes(messageFormat)
  )
  return (
    <Card>
      <CardHeader className='px-4 pt-4 pb-2'>
        <CardTitle>{t('Preview and test')}</CardTitle>
        <CardDescription>
          {t(
            'Preview uses the current unsaved form. Test sending sends the same backend-rendered message through the root-only new-api endpoint.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3 px-4 pb-4'>
        <div className='grid gap-3 md:grid-cols-2'>
          <Field label={t('Preview event')}>
            <NativeSelect
              className='w-full'
              value={eventType}
              onChange={(event) =>
                onEventTypeChange(event.target.value as AlertEventType)
              }
            >
              <option value='trigger'>{t('Trigger alert')}</option>
              <option value='recovery'>{t('Recovery')}</option>
            </NativeSelect>
          </Field>
          {triggerType === 'upstream_channel_effective_balance' && (
            <Field
              label={t('Sample upstream channel')}
              description={t('Use example values when no channel is selected.')}
            >
              <NativeSelect
                className='w-full'
                value={sampleChannelID}
                onChange={(event) =>
                  onSampleChannelChange(Number(event.target.value))
                }
              >
                <option value={0}>{t('Example channel')}</option>
                {channels.map((channel) => (
                  <option key={channel.id} value={channel.id}>
                    {channel.name || channel.base_url}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          )}
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={previewing}
            onClick={onPreview}
          >
            {previewing ? <LoaderCircle className='animate-spin' /> : <Eye />}
            {t('Preview message')}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={testingConnection}
            onClick={onTestConnection}
          >
            {testingConnection ? (
              <LoaderCircle className='animate-spin' />
            ) : (
              <ServerCog />
            )}
            {t('Test connection')}
          </Button>
          {testableProviders.map((provider) => {
            let label = t('Test {{provider}}', { provider: provider.name })
            if (provider.name === 'weixin') label = t('Test WeChat')
            if (provider.name === 'feishu') label = t('Test Feishu')
            return (
              <Button
                key={provider.name}
                type='button'
                disabled={testingSend || !canSendTest}
                onClick={() => onTestSend([provider.name])}
              >
                {testingSend ? (
                  <LoaderCircle className='animate-spin' />
                ) : (
                  <Send />
                )}
                {label}
              </Button>
            )
          })}
          {testableProviders.length > 1 && (
            <Button
              type='button'
              variant='secondary'
              disabled={testingSend || !canSendTest}
              onClick={() =>
                onTestSend(testableProviders.map((provider) => provider.name))
              }
            >
              {testingSend ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <Send />
              )}
              {t('Test together')}
            </Button>
          )}
        </div>

        {connectionStatus && <ConnectionStatusView status={connectionStatus} />}
        {testSendResult && <TestSendResultView result={testSendResult} />}
        {preview && <AlertMessagePreview message={preview} />}
      </CardContent>
    </Card>
  )
}

function ConnectionStatusView({
  status,
}: {
  status: ApiNoticeConnectionStatus
}) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-2 rounded-lg border p-3 text-sm md:grid-cols-3'>
      <StatusLine
        label={t('Health check')}
        ready={
          status.health.http_status >= 200 && status.health.http_status < 300
        }
        detail={`${status.health.status || '-'} · HTTP ${status.health.http_status || 0}`}
      />
      <StatusLine
        label={t('Readiness check')}
        ready={
          status.ready.http_status >= 200 && status.ready.http_status < 300
        }
        detail={`${status.ready.status || '-'} · HTTP ${status.ready.http_status || 0}`}
      />
      <StatusLine
        label={t('API Key')}
        ready={status.api_key_configured}
        detail={
          status.api_key_configured ? t('Configured') : t('Not configured')
        }
      />
    </div>
  )
}

function StatusLine({
  label,
  ready,
  detail,
}: {
  label: string
  ready: boolean
  detail: string
}) {
  return (
    <div>
      <p className='flex items-center gap-1.5 font-medium'>
        {ready ? (
          <CheckCircle2 className='size-4 text-emerald-600' />
        ) : (
          <AlertTriangle className='text-destructive size-4' />
        )}
        {label}
      </p>
      <p className='text-muted-foreground mt-1'>{detail}</p>
    </div>
  )
}

function TestSendResultView({ result }: { result: AlertRuleTestSendResult }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2 rounded-lg border p-3 text-sm'>
      <p className='font-medium'>
        {t('Test send result')} · HTTP {result.http_status || 0}
      </p>
      {result.results.map((provider) => (
        <div
          key={provider.provider}
          className='flex flex-wrap items-center gap-x-3 gap-y-1'
        >
          <Badge variant={provider.accepted ? 'secondary' : 'destructive'}>
            {provider.provider}
          </Badge>
          <span>{provider.accepted ? t('Accepted') : t('Rejected')}</span>
          <span className='text-muted-foreground'>
            {t('{{count}} attempts', { count: provider.attempts })}
          </span>
          {provider.error && (
            <code className='text-destructive'>{provider.error}</code>
          )}
        </div>
      ))}
      {result.error && <code className='text-destructive'>{result.error}</code>}
    </div>
  )
}

function AlertMessagePreview({ message }: { message: ApiNoticeMessage }) {
  const { t } = useTranslation()
  let content: ReactNode

  if (message.format === 'markdown') {
    content = <Markdown>{message.text || ''}</Markdown>
  } else if (message.format === 'card') {
    const fields = withOccurrenceKeys(message.fields || [], (field) =>
      JSON.stringify(field)
    )
    const sections = withOccurrenceKeys(message.sections || [], (section) =>
      JSON.stringify(section)
    )
    const actions = withOccurrenceKeys(message.actions || [], (action) =>
      JSON.stringify(action)
    )
    content = (
      <div className='space-y-3'>
        <div>
          <p className='font-semibold'>{message.title}</p>
          {message.level && (
            <Badge variant='outline' className='mt-1'>
              {message.level}
            </Badge>
          )}
        </div>
        <p className='whitespace-pre-wrap'>{message.text}</p>
        {fields.length > 0 && (
          <dl className='grid gap-2 sm:grid-cols-2'>
            {fields.map(({ item: field, key }) => (
              <div key={key} className='rounded border p-2'>
                <dt className='text-muted-foreground text-xs'>{field.name}</dt>
                <dd>{field.value}</dd>
              </div>
            ))}
          </dl>
        )}
        {sections.map(({ item: section, key }) => (
          <section key={key} className='border-t pt-3'>
            {section.title && <p className='font-medium'>{section.title}</p>}
            <p className='mt-1 whitespace-pre-wrap'>{section.text}</p>
          </section>
        ))}
        {actions.length > 0 && (
          <div className='flex flex-wrap gap-2'>
            {actions.map(({ item: action, key }) => (
              <Button
                key={key}
                type='button'
                size='sm'
                variant='outline'
                render={
                  <a href={action.url} target='_blank' rel='noreferrer' />
                }
              >
                {action.label}
              </Button>
            ))}
          </div>
        )}
      </div>
    )
  } else if (message.format === 'table' && message.table) {
    const table = message.table
    const rows = withOccurrenceKeys(table.rows, (row) => JSON.stringify(row))
    content = (
      <div className='overflow-x-auto'>
        <p className='font-semibold'>{message.title}</p>
        {message.text && (
          <p className='text-muted-foreground mt-1 text-sm'>{message.text}</p>
        )}
        <table className='mt-3 w-full border-collapse text-sm'>
          <thead>
            <tr>
              {table.columns.map((column) => (
                <th key={column.key} className='border px-2 py-1.5 text-left'>
                  {column.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map(({ item: row, key }) => (
              <tr key={key}>
                {table.columns.map((column) => (
                  <td key={column.key} className='border px-2 py-1.5'>
                    {row[column.key]}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  } else {
    content = <p className='whitespace-pre-wrap'>{message.text}</p>
  }

  return (
    <div className='rounded-lg border p-4'>
      <div className='mb-3 flex items-center gap-2'>
        <Activity className='size-4' />
        <span className='font-medium'>{t('Rendered backend preview')}</span>
        <Badge variant='outline'>{t(formatLabel(message.format))}</Badge>
      </div>
      {content}
    </div>
  )
}

function Field({
  label,
  description,
  className,
  children,
}: {
  label: string
  description?: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={cn('space-y-1.5', className)}>
      <Label>{label}</Label>
      {children}
      {description && (
        <p className='text-muted-foreground text-xs'>{description}</p>
      )}
    </div>
  )
}

function ToggleField({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className='flex items-center justify-between gap-4 rounded-lg border px-3 py-2'>
      <div>
        <Label>{label}</Label>
        <p className='text-muted-foreground mt-0.5 text-xs'>{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

function alertRuleToInput(rule: AlertRule): AlertRuleInput {
  return {
    name: rule.name,
    enabled: rule.enabled,
    trigger_type: rule.trigger_type,
    trigger_config: { ...rule.trigger_config },
    providers: [...rule.providers],
    message_format: rule.message_format,
    message_template: cloneMessage(rule.message_template),
    consecutive_required: rule.consecutive_required,
    cooldown_seconds: rule.cooldown_seconds,
    send_recovery: rule.send_recovery,
  }
}

function cloneMessage(message: ApiNoticeMessage): ApiNoticeMessage {
  return {
    ...message,
    fields: message.fields?.map((field) => ({ ...field })),
    sections: message.sections?.map((section) => ({ ...section })),
    actions: message.actions?.map((action) => ({ ...action })),
    table: message.table
      ? {
          columns: message.table.columns.map((column) => ({ ...column })),
          rows: message.table.rows.map((row) => ({ ...row })),
        }
      : undefined,
  }
}

function formatLabel(format: AlertMessageFormat) {
  switch (format) {
    case 'markdown':
      return 'Markdown'
    case 'card':
      return 'Card'
    case 'table':
      return 'Table'
    default:
      return 'Text'
  }
}

function stateBadgeVariant(state: AlertRule['state']['state']) {
  if (state === 'active') return 'destructive' as const
  if (state === 'pending') return 'secondary' as const
  return 'outline' as const
}
