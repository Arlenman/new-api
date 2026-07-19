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
import { Link } from '@tanstack/react-router'
import {
  CircleAlert,
  CircleCheck,
  Globe2,
  Images,
  KeyRound,
  LoaderCircle,
  Maximize2,
  Minimize2,
  RefreshCw,
  Settings2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getApiKeys } from '@/features/keys/api'
import type { ApiKey } from '@/features/keys/types'
import {
  createUserToolRuntimeSession,
  getUserToolPreference,
  updateUserToolPreference,
} from '@/features/user-tools/api'
import { useStatus } from '@/hooks/use-status'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  createNewApiConfigureMessage,
  createProbeMessage,
  createToolConfigureMessage,
  isTrustedImagePlaygroundMessage,
  type ImagePlaygroundMode,
} from './lib/bridge'
import {
  IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY,
  normalizeImagePlaygroundApiUrl,
  parseImagePlaygroundConfiguration,
  serializeImagePlaygroundConfiguration,
} from './lib/configuration-storage'
import {
  IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY,
  isApiKeyAvailable,
  parseRememberedTokenSelection,
  selectPreferredApiKey,
} from './lib/token-selection'

const TOOL_URL = '/_tools/gpt-image-playground/'

type BridgeStatus = 'loading' | 'configuring' | 'ready' | 'error'

interface AppliedConfiguration {
  mode: ImagePlaygroundMode
  tokenId: number | null
  customApiUrl: string
  customApiKey: string
  revision: number
}

type ImagePlaygroundProps = {
  active: boolean
  immersive: boolean
  onImmersiveChange: (immersive: boolean) => void
}

export function ImagePlayground({
  active,
  immersive,
  onImmersiveChange,
}: ImagePlaygroundProps) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { status, loading: statusLoading } = useStatus()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const configurationInFlightRef = useRef<number | null>(null)
  const configuredRevisionRef = useRef<number | null>(null)
  const appliedRevisionRef = useRef(0)
  const runtimeExpiresAtRef = useRef(0)
  const refreshRuntimeCredentialRef = useRef<(() => Promise<void>) | null>(null)
  const runtimeCredentialRequestRef = useRef<{
    revision: number
    promise: Promise<void>
  } | null>(null)
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([])
  const [keysLoading, setKeysLoading] = useState(true)
  const [draftMode, setDraftMode] = useState<ImagePlaygroundMode>('new-api')
  const [draftTokenId, setDraftTokenId] = useState<number | null>(null)
  const [draftCustomApiUrl, setDraftCustomApiUrl] = useState('')
  const [draftCustomApiKey, setDraftCustomApiKey] = useState('')
  const [appliedConfiguration, setAppliedConfiguration] =
    useState<AppliedConfiguration | null>(null)
  const [configurationOpen, setConfigurationOpen] = useState(false)
  const [configurationError, setConfigurationError] = useState<string | null>(
    null
  )
  const [bridgeStatus, setBridgeStatus] = useState<BridgeStatus>('loading')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  appliedRevisionRef.current = appliedConfiguration?.revision ?? 0

  useEffect(() => {
    if (!active) return

    const syncFullscreenState = () => {
      onImmersiveChange(
        document.fullscreenElement === document.documentElement
      )
    }

    syncFullscreenState()
    document.addEventListener('fullscreenchange', syncFullscreenState)
    document.addEventListener('visibilitychange', syncFullscreenState)
    window.addEventListener('focus', syncFullscreenState)
    const fullscreenPollTimer = immersive
      ? window.setInterval(syncFullscreenState, 250)
      : null
    return () => {
      document.removeEventListener('fullscreenchange', syncFullscreenState)
      document.removeEventListener('visibilitychange', syncFullscreenState)
      window.removeEventListener('focus', syncFullscreenState)
      if (fullscreenPollTimer) window.clearInterval(fullscreenPollTimer)
    }
  }, [active, immersive, onImmersiveChange])

  useEffect(() => {
    let cancelled = false

    async function loadApiKeys() {
      if (!userId) {
        setKeysLoading(false)
        return
      }

      const rememberedConfiguration = parseImagePlaygroundConfiguration(
        window.localStorage.getItem(IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY),
        userId
      )
      const customApiUrl = rememberedConfiguration?.customApiUrl ?? ''
      const customApiKey = rememberedConfiguration?.customApiKey ?? ''
      const normalizedCustomApiUrl =
        normalizeImagePlaygroundApiUrl(customApiUrl)
      const useRememberedCustomConfiguration =
        rememberedConfiguration?.mode === 'tool' &&
        normalizedCustomApiUrl !== null &&
        customApiKey.trim() !== ''

      setDraftMode(useRememberedCustomConfiguration ? 'tool' : 'new-api')
      setDraftCustomApiUrl(customApiUrl)
      setDraftCustomApiKey(customApiKey)
      setKeysLoading(true)
      setErrorMessage(null)
      try {
        const response = await getApiKeys({ p: 1, size: 100 })
        if (!response.success || !response.data) {
          throw new Error(response.message || 'Failed to load API keys')
        }
        if (cancelled) return

        const now = Math.floor(Date.now() / 1000)
        const availableKeys = response.data.items.filter((apiKey) =>
          isApiKeyAvailable(apiKey, now)
        )
        const legacyRememberedTokenId = parseRememberedTokenSelection(
          window.localStorage.getItem(IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY),
          userId
        )
        let selectedTokenId = legacyRememberedTokenId
        try {
          const preference = await getUserToolPreference('image-playground')
          if (preference.success && preference.data.selected_token_id > 0) {
            selectedTokenId = preference.data.selected_token_id
          }
        } catch {
          selectedTokenId = legacyRememberedTokenId
        }
        const preferredKey = selectPreferredApiKey(
          availableKeys,
          selectedTokenId,
          now
        )

        setApiKeys(availableKeys)
        setDraftTokenId(preferredKey?.id ?? null)
        setAppliedConfiguration({
          mode: useRememberedCustomConfiguration ? 'tool' : 'new-api',
          tokenId: useRememberedCustomConfiguration
            ? null
            : (preferredKey?.id ?? null),
          customApiUrl: normalizedCustomApiUrl ?? customApiUrl,
          customApiKey,
          revision: 1,
        })
        if (preferredKey) {
          if (preferredKey.id !== selectedTokenId) {
            void updateUserToolPreference('image-playground', preferredKey.id)
          }
          window.localStorage.removeItem(IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY)
        }
      } catch {
        if (cancelled) return
        setApiKeys([])
        setDraftTokenId(null)
        setAppliedConfiguration({
          mode: useRememberedCustomConfiguration ? 'tool' : 'new-api',
          tokenId: null,
          customApiUrl: normalizedCustomApiUrl ?? customApiUrl,
          customApiKey,
          revision: 1,
        })
        if (!useRememberedCustomConfiguration) {
          setErrorMessage(t('Failed to load API keys'))
        }
      } finally {
        if (!cancelled) setKeysLoading(false)
      }
    }

    void loadApiKeys()
    return () => {
      cancelled = true
    }
  }, [t, userId])

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (
        runtimeExpiresAtRef.current > 0 &&
        Date.now() + 2 * 60 * 1000 >= runtimeExpiresAtRef.current
      ) {
        void refreshRuntimeCredentialRef.current?.()
      }
    }, 30_000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!appliedConfiguration) return

    const probeIframe = () => {
      iframeRef.current?.contentWindow?.postMessage(
        createProbeMessage(),
        window.location.origin
      )
    }

    const handleMessage = (event: MessageEvent) => {
      const iframeWindow = iframeRef.current?.contentWindow
      if (
        !iframeWindow ||
        !isTrustedImagePlaygroundMessage(
          event,
          iframeWindow,
          window.location.origin
        )
      ) {
        return
      }

      if (event.data.type === 'new-api:image-playground:configured') {
        if (event.data.mode !== appliedConfiguration.mode) return
        configuredRevisionRef.current = appliedConfiguration.revision
        configurationInFlightRef.current = null
        window.clearInterval(probeTimer)
        setBridgeStatus('ready')
        setErrorMessage(null)
        return
      }

      if (
        configuredRevisionRef.current === appliedConfiguration.revision ||
        configurationInFlightRef.current === appliedConfiguration.revision
      ) {
        return
      }

      configurationInFlightRef.current = appliedConfiguration.revision
      setBridgeStatus('configuring')
      setErrorMessage(null)

      if (appliedConfiguration.mode === 'tool') {
        const customApiUrl = normalizeImagePlaygroundApiUrl(
          appliedConfiguration.customApiUrl
        )
        const customApiKey = appliedConfiguration.customApiKey.trim()
        if (!customApiUrl || !customApiKey) {
          configurationInFlightRef.current = null
          setBridgeStatus('error')
          setErrorMessage(
            !customApiUrl ? t('Must be a valid URL') : t('API key is required')
          )
          return
        }
        iframeWindow.postMessage(
          createToolConfigureMessage(customApiUrl, customApiKey),
          window.location.origin
        )
        return
      }

      if (!appliedConfiguration.tokenId) {
        configurationInFlightRef.current = null
        setBridgeStatus('error')
        setErrorMessage(t('No available API keys'))
        return
      }

      const sourceWindow = event.source
      const revision = appliedConfiguration.revision
      const tokenId = appliedConfiguration.tokenId
      const configureRuntimeCredential = () => {
        const currentRequest = runtimeCredentialRequestRef.current
        if (currentRequest?.revision === revision) {
          return currentRequest.promise
        }

        const promise = (async () => {
          try {
            const response = await createUserToolRuntimeSession(
              'image-playground',
              tokenId
            )
            const runtimeCredential = response.data?.credential
            if (!response.success || !runtimeCredential) {
              throw new Error(
                response.message || 'Failed to create runtime credential'
              )
            }
            const currentIframeWindow = iframeRef.current?.contentWindow
            if (
              appliedRevisionRef.current !== revision ||
              !currentIframeWindow ||
              currentIframeWindow !== sourceWindow
            ) {
              return
            }

            runtimeExpiresAtRef.current = response.data.expires_at
            currentIframeWindow.postMessage(
              createNewApiConfigureMessage(
                window.location.origin,
                runtimeCredential
              ),
              window.location.origin
            )
          } catch {
            if (appliedRevisionRef.current !== revision) return
            configurationInFlightRef.current = null
            setBridgeStatus('error')
            setErrorMessage(t('Failed to load API key'))
          }
        })()
        const request = { revision, promise }
        runtimeCredentialRequestRef.current = request
        void promise.finally(() => {
          if (runtimeCredentialRequestRef.current === request) {
            runtimeCredentialRequestRef.current = null
          }
        })
        return promise
      }
      refreshRuntimeCredentialRef.current = configureRuntimeCredential
      void configureRuntimeCredential()
    }

    window.addEventListener('message', handleMessage)
    const probeTimer = window.setInterval(probeIframe, 1_000)
    probeIframe()
    return () => {
      window.removeEventListener('message', handleMessage)
      window.clearInterval(probeTimer)
      refreshRuntimeCredentialRef.current = null
      runtimeCredentialRequestRef.current = null
      runtimeExpiresAtRef.current = 0
    }
  }, [appliedConfiguration, t])

  const keyOptions = useMemo(
    () =>
      apiKeys.map((apiKey) => ({
        label: apiKey.name || t('Unnamed API key'),
        value: String(apiKey.id),
      })),
    [apiKeys, t]
  )

  const appliedSourceLabel = useMemo(() => {
    if (!appliedConfiguration) return null
    if (appliedConfiguration.mode === 'new-api') {
      return (
        apiKeys.find((apiKey) => apiKey.id === appliedConfiguration.tokenId)
          ?.name || t('Unnamed API key')
      )
    }

    const customApiUrl = normalizeImagePlaygroundApiUrl(
      appliedConfiguration.customApiUrl
    )
    if (!customApiUrl) return null
    return new URL(customApiUrl).host
  }, [apiKeys, appliedConfiguration, t])

  const normalizedDraftCustomApiUrl =
    normalizeImagePlaygroundApiUrl(draftCustomApiUrl)
  const draftCustomConfigurationValid =
    normalizedDraftCustomApiUrl !== null && draftCustomApiKey.trim() !== ''

  const applyConfiguration = async () => {
    if (!appliedConfiguration || !userId) return
    if (draftMode === 'new-api' && !draftTokenId) return
    if (draftMode === 'tool' && !normalizedDraftCustomApiUrl) {
      setConfigurationError(t('Must be a valid URL'))
      return
    }
    if (draftMode === 'tool' && !draftCustomApiKey.trim()) {
      setConfigurationError(t('API key is required'))
      return
    }

    if (draftMode === 'new-api' && draftTokenId) {
      try {
        const preference = await updateUserToolPreference(
          'image-playground',
          draftTokenId
        )
        if (!preference.success) {
          throw new Error(preference.message)
        }
        window.localStorage.removeItem(IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY)
      } catch {
        setConfigurationError(t('Failed to save API key selection'))
        return
      }
    }
    window.localStorage.setItem(
      IMAGE_PLAYGROUND_CONFIGURATION_STORAGE_KEY,
      serializeImagePlaygroundConfiguration(userId, {
        mode: draftMode,
        customApiUrl: normalizedDraftCustomApiUrl ?? draftCustomApiUrl,
        customApiKey: draftCustomApiKey,
      })
    )

    configurationInFlightRef.current = null
    configuredRevisionRef.current = null
    setBridgeStatus('loading')
    setErrorMessage(null)
    setConfigurationError(null)
    setAppliedConfiguration({
      mode: draftMode,
      tokenId: draftMode === 'new-api' ? draftTokenId : null,
      customApiUrl: normalizedDraftCustomApiUrl ?? draftCustomApiUrl,
      customApiKey: draftCustomApiKey.trim(),
      revision: appliedConfiguration.revision + 1,
    })
    setConfigurationOpen(false)
  }

  const toggleFullscreen = async () => {
    if (immersive) {
      try {
        await document.exitFullscreen()
      } catch {
        onImmersiveChange(
          document.fullscreenElement === document.documentElement
        )
      }
      return
    }

    try {
      await document.documentElement.requestFullscreen()
      onImmersiveChange(true)
    } catch {
      onImmersiveChange(false)
      toast.error(t('Unable to enter fullscreen'))
    }
  }

  const handleConfigurationOpenChange = (open: boolean) => {
    if (open && appliedConfiguration) {
      setDraftMode(appliedConfiguration.mode)
      setDraftTokenId(appliedConfiguration.tokenId)
      setDraftCustomApiUrl(appliedConfiguration.customApiUrl)
      setDraftCustomApiKey(appliedConfiguration.customApiKey)
      setConfigurationError(null)
    }
    setConfigurationOpen(open)
  }

  const iframeLoaded = () => {
    configurationInFlightRef.current = null
    configuredRevisionRef.current = null
    setBridgeStatus('loading')
    iframeRef.current?.contentWindow?.postMessage(
      createProbeMessage(),
      window.location.origin
    )
  }

  const playgroundAvailable = status?.image_playground_available === true
  const playgroundVersion =
    typeof status?.image_playground_version === 'string'
      ? status.image_playground_version
      : null

  if (statusLoading || keysLoading) {
    return (
      <SectionPageLayout fixedContent immersive={immersive}>
        <SectionPageLayout.Title>
          {t('Image Playground')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='flex h-full items-center justify-center'>
            <LoaderCircle className='text-muted-foreground size-8 animate-spin' />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (!playgroundAvailable) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Image Playground')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <Alert variant='destructive'>
            <CircleAlert />
            <AlertTitle>{t('Image playground is unavailable')}</AlertTitle>
            <AlertDescription>
              {t(
                'This deployment does not include the image playground build.'
              )}
            </AlertDescription>
          </Alert>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  let bridgeStatusIcon = <LoaderCircle className='size-3.5 animate-spin' />
  let bridgeStatusLabel = t('Connecting')
  if (bridgeStatus === 'ready') {
    bridgeStatusIcon = <CircleCheck className='size-3.5 text-emerald-600' />
    bridgeStatusLabel = t('Ready')
  } else if (bridgeStatus === 'error') {
    bridgeStatusIcon = <CircleAlert className='text-destructive size-3.5' />
    bridgeStatusLabel = t('Configuration failed')
  }

  return (
    <>
      <SectionPageLayout fixedContent immersive={immersive}>
        <SectionPageLayout.Title>
          <span className='inline-flex items-center gap-2'>
            <Images className='size-5' />
            {t('Image Playground')}
            {playgroundVersion && (
              <Badge variant='outline'>v{playgroundVersion}</Badge>
            )}
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {appliedSourceLabel && (
            <Badge
              variant='secondary'
              className='hidden max-w-52 justify-start sm:inline-flex'
              title={appliedSourceLabel}
            >
              {appliedConfiguration?.mode === 'new-api' ? (
                <KeyRound />
              ) : (
                <Globe2 />
              )}
              <span className='truncate'>{appliedSourceLabel}</span>
            </Badge>
          )}
          <div className='text-muted-foreground flex items-center gap-1.5 text-xs whitespace-nowrap'>
            {bridgeStatusIcon}
            {bridgeStatusLabel}
          </div>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            aria-label={
              immersive ? t('Exit fullscreen') : t('Enter fullscreen')
            }
            title={
              immersive ? t('Exit fullscreen') : t('Enter fullscreen')
            }
            onClick={() => void toggleFullscreen()}
          >
            {immersive ? <Minimize2 /> : <Maximize2 />}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            aria-label={t('API configuration')}
            title={t('API configuration')}
            onClick={() => handleConfigurationOpenChange(true)}
          >
            <Settings2 />
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-2'>
            {errorMessage && (
              <Alert variant='destructive' className='shrink-0'>
                <CircleAlert />
                <AlertTitle>{t('Configuration failed')}</AlertTitle>
                <AlertDescription>{errorMessage}</AlertDescription>
              </Alert>
            )}

            <div
              className={cn(
                'bg-background relative flex-1 overflow-hidden',
                immersive
                  ? 'min-h-0'
                  : 'min-h-[30rem] rounded-lg border'
              )}
            >
              {appliedConfiguration && (
                <>
                  {/* oxlint-disable-next-line react/iframe-missing-sandbox -- The tool needs same-origin storage and scripts for bridge configuration and async task recovery. */}
                  <iframe
                    key={appliedConfiguration.revision}
                    ref={iframeRef}
                    src={TOOL_URL}
                    title={t('Image Playground')}
                    className={cn(
                      'h-full w-full border-0',
                      immersive ? 'min-h-0' : 'min-h-[30rem]'
                    )}
                    allow='clipboard-read; clipboard-write'
                    referrerPolicy='same-origin'
                    onLoad={iframeLoaded}
                  />
                </>
              )}
              {(bridgeStatus === 'loading' ||
                bridgeStatus === 'configuring') && (
                <div className='bg-background/75 pointer-events-none absolute inset-0 flex items-center justify-center backdrop-blur-[1px]'>
                  <div className='text-muted-foreground bg-background/95 flex items-center gap-2 rounded-full border px-3 py-2 text-sm shadow-sm'>
                    <LoaderCircle className='size-4 animate-spin' />
                    {bridgeStatusLabel}
                  </div>
                </div>
              )}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog
        open={configurationOpen}
        onOpenChange={handleConfigurationOpenChange}
      >
        <DialogContent className='sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>{t('API configuration')}</DialogTitle>
            <DialogDescription>
              {t(
                'Use a New API key from your account, or connect a third-party OpenAI-compatible API.'
              )}
            </DialogDescription>
          </DialogHeader>

          <div className='flex flex-col gap-4'>
            <div className='grid gap-2 sm:grid-cols-2'>
              <Button
                type='button'
                variant={draftMode === 'new-api' ? 'default' : 'outline'}
                className='h-auto justify-start px-3 py-3 text-left whitespace-normal'
                aria-pressed={draftMode === 'new-api'}
                onClick={() => {
                  setDraftMode('new-api')
                  setConfigurationError(null)
                }}
              >
                <KeyRound className='size-4' />
                <span>
                  <span className='block'>{t('Use New API Key')}</span>
                  <span
                    className={cn(
                      'block text-xs font-normal',
                      draftMode === 'new-api'
                        ? 'text-primary-foreground/75'
                        : 'text-muted-foreground'
                    )}
                  >
                    {t('Automatically use a key from your account.')}
                  </span>
                </span>
              </Button>
              <Button
                type='button'
                variant={draftMode === 'tool' ? 'default' : 'outline'}
                className='h-auto justify-start px-3 py-3 text-left whitespace-normal'
                aria-pressed={draftMode === 'tool'}
                onClick={() => {
                  setDraftMode('tool')
                  setConfigurationError(null)
                }}
              >
                <Globe2 className='size-4' />
                <span>
                  <span className='block'>{t('Use third-party API')}</span>
                  <span
                    className={cn(
                      'block text-xs font-normal',
                      draftMode === 'tool'
                        ? 'text-primary-foreground/75'
                        : 'text-muted-foreground'
                    )}
                  >
                    {t('Use a custom Base URL and API key.')}
                  </span>
                </span>
              </Button>
            </div>

            <div className='bg-muted/30 rounded-lg border p-4'>
              {draftMode === 'new-api' ? (
                <div className='min-w-0 space-y-2'>
                  <Label htmlFor='image-key'>{t('API Key')}</Label>
                  {keyOptions.length > 0 ? (
                    <Select
                      items={keyOptions}
                      value={draftTokenId ? String(draftTokenId) : null}
                      onValueChange={(value) =>
                        setDraftTokenId(value ? Number(value) : null)
                      }
                    >
                      <SelectTrigger
                        id='image-key'
                        className='bg-background w-full'
                      >
                        <SelectValue placeholder={t('Select an API key')} />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        {keyOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <Button
                      variant='outline'
                      className='bg-background w-full justify-start'
                      render={<Link to='/keys' />}
                    >
                      <KeyRound />
                      {t('Create an API key first')}
                    </Button>
                  )}
                </div>
              ) : (
                <div className='space-y-4'>
                  <div className='space-y-2'>
                    <Label htmlFor='image-custom-base-url'>
                      {t('Base URL')}
                    </Label>
                    <Input
                      id='image-custom-base-url'
                      type='url'
                      value={draftCustomApiUrl}
                      placeholder='https://api.example.com/v1'
                      className='bg-background'
                      aria-invalid={
                        draftCustomApiUrl !== '' &&
                        normalizedDraftCustomApiUrl === null
                      }
                      onChange={(event) => {
                        setDraftCustomApiUrl(event.target.value)
                        setConfigurationError(null)
                      }}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='image-custom-api-key'>{t('API Key')}</Label>
                    <Input
                      id='image-custom-api-key'
                      type='password'
                      value={draftCustomApiKey}
                      placeholder='sk-...'
                      autoComplete='off'
                      className='bg-background'
                      onChange={(event) => {
                        setDraftCustomApiKey(event.target.value)
                        setConfigurationError(null)
                      }}
                    />
                  </div>
                </div>
              )}
            </div>

            {configurationError && (
              <div className='text-destructive flex items-center gap-2 text-sm'>
                <CircleAlert className='size-4 shrink-0' />
                {configurationError}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setConfigurationOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={
                draftMode === 'new-api'
                  ? !draftTokenId
                  : !draftCustomConfigurationValid
              }
              onClick={applyConfiguration}
            >
              <RefreshCw />
              {t('Apply and reload')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
