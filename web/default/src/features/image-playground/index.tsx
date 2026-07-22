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
  Images,
  KeyRound,
  LoaderCircle,
  Maximize2,
  Minimize2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  createUserToolRuntimeSession,
  getAllUserToolTokens,
  getUserToolPreference,
  updateUserToolPreference,
  type UserToolTokenOption,
} from '@/features/user-tools/api'
import { useStatus } from '@/hooks/use-status'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  createNewApiConfigureMessage,
  createProbeMessage,
  isTrustedImagePlaygroundMessage,
} from './lib/bridge'
import { resolveImagePlaygroundHostMode } from './lib/configuration-storage'
import {
  IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY,
  getApiKeyDisplayLabel,
  getApiKeySelectionOptions,
  isApiKeyAvailable,
  parseRememberedTokenSelection,
  selectPreferredApiKey,
} from './lib/token-selection'

const TOOL_URL = '/_tools/gpt-image-playground/'

type BridgeStatus = 'loading' | 'configuring' | 'ready' | 'error'

interface AppliedConfiguration {
  mode: 'new-api'
  tokenId: number | null
  revision: number
}

interface RuntimeTokenLabel {
  tokenId: number
  displayLabel: string
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
  const [apiKeys, setApiKeys] = useState<UserToolTokenOption[]>([])
  const [keysLoading, setKeysLoading] = useState(true)
  const [appliedConfiguration, setAppliedConfiguration] =
    useState<AppliedConfiguration | null>(null)
  const [keySwitching, setKeySwitching] = useState(false)
  const [runtimeTokenLabel, setRuntimeTokenLabel] =
    useState<RuntimeTokenLabel | null>(null)
  const [bridgeStatus, setBridgeStatus] = useState<BridgeStatus>('loading')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  appliedRevisionRef.current = appliedConfiguration?.revision ?? 0

  useEffect(() => {
    if (!active) return

    const syncFullscreenState = () => {
      onImmersiveChange(document.fullscreenElement === document.documentElement)
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
    if (!active) return

    let cancelled = false

    async function loadApiKeys() {
      const hostMode = resolveImagePlaygroundHostMode(window.localStorage)
      if (!userId) {
        setKeysLoading(false)
        return
      }

      setKeysLoading(true)
      setErrorMessage(null)
      try {
        const response = await getAllUserToolTokens('image-playground')
        if (!response.success || !response.data) {
          throw new Error(response.message || 'Failed to load API keys')
        }
        if (cancelled) return

        const allApiKeys = response.data.items
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
        if (cancelled) return

        const preferredKey = selectPreferredApiKey(allApiKeys, selectedTokenId)

        setApiKeys(allApiKeys)
        setAppliedConfiguration((current) => ({
          mode: hostMode,
          tokenId: preferredKey?.id ?? null,
          revision: (current?.revision ?? 0) + 1,
        }))
        if (preferredKey) {
          if (preferredKey.id !== selectedTokenId) {
            void updateUserToolPreference('image-playground', preferredKey.id)
          }
          window.localStorage.removeItem(IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY)
        }
      } catch {
        if (cancelled) return
        setApiKeys([])
        setAppliedConfiguration((current) => ({
          mode: hostMode,
          tokenId: null,
          revision: (current?.revision ?? 0) + 1,
        }))
        setErrorMessage(t('Failed to load API keys'))
      } finally {
        if (!cancelled) setKeysLoading(false)
      }
    }

    void loadApiKeys()
    return () => {
      cancelled = true
    }
  }, [active, t, userId])

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
            const tokenDisplayLabel = getApiKeyDisplayLabel(
              response.data.token,
              t('Unnamed API key')
            )
            setRuntimeTokenLabel({
              tokenId: response.data.token.id,
              displayLabel: tokenDisplayLabel,
            })
            currentIframeWindow.postMessage(
              createNewApiConfigureMessage(
                window.location.origin,
                runtimeCredential,
                tokenDisplayLabel
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
    () => getApiKeySelectionOptions(apiKeys, t('Unnamed API key')),
    [apiKeys, t]
  )

  const appliedSourceLabel = useMemo(() => {
    if (!appliedConfiguration) return null
    if (runtimeTokenLabel?.tokenId === appliedConfiguration.tokenId) {
      return runtimeTokenLabel.displayLabel
    }
    return (
      keyOptions.find(
        (option) => option.value === String(appliedConfiguration.tokenId)
      )?.label ?? null
    )
  }, [appliedConfiguration, keyOptions, runtimeTokenLabel])

  const switchApiKey = async (value: string | null) => {
    if (!value || !appliedConfiguration || keySwitching) {
      return
    }

    const tokenId = Number(value)
    const selectedKey = apiKeys.find((apiKey) => apiKey.id === tokenId)
    if (
      !Number.isInteger(tokenId) ||
      tokenId <= 0 ||
      !selectedKey ||
      !isApiKeyAvailable(selectedKey) ||
      tokenId === appliedConfiguration.tokenId
    ) {
      return
    }

    setKeySwitching(true)
    setErrorMessage(null)
    try {
      const preference = await updateUserToolPreference(
        'image-playground',
        tokenId
      )
      if (!preference.success) {
        throw new Error(preference.message)
      }

      window.localStorage.removeItem(IMAGE_PLAYGROUND_TOKEN_STORAGE_KEY)
      configurationInFlightRef.current = null
      configuredRevisionRef.current = null
      runtimeCredentialRequestRef.current = null
      runtimeExpiresAtRef.current = 0
      setBridgeStatus('loading')
      setRuntimeTokenLabel(null)
      const nextRevision = appliedConfiguration.revision + 1
      appliedRevisionRef.current = nextRevision
      setAppliedConfiguration({
        ...appliedConfiguration,
        tokenId,
        revision: nextRevision,
      })
    } catch {
      setErrorMessage(t('Failed to save API key selection'))
    } finally {
      setKeySwitching(false)
    }
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
        {keyOptions.length > 0 ? (
          <Select
            items={keyOptions}
            value={
              appliedConfiguration?.tokenId
                ? String(appliedConfiguration.tokenId)
                : null
            }
            disabled={keySwitching}
            onValueChange={(value) => void switchApiKey(value)}
          >
            <SelectTrigger
              size='sm'
              className='flex w-40 sm:w-52'
              aria-label={t('Select an API key')}
              title={appliedSourceLabel ?? t('Select an API key')}
            >
              {keySwitching ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <KeyRound />
              )}
              <SelectValue placeholder={t('Select an API key')} />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {keyOptions.map((option) => (
                <SelectItem
                  key={option.value}
                  value={option.value}
                  disabled={!option.available}
                >
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Button variant='outline' size='sm' render={<Link to='/keys' />}>
            <KeyRound />
            {t('Create an API key first')}
          </Button>
        )}
        <div className='text-muted-foreground flex items-center gap-1.5 text-xs whitespace-nowrap'>
          {bridgeStatusIcon}
          {bridgeStatusLabel}
        </div>
        <Button
          type='button'
          variant='outline'
          size='icon-sm'
          aria-label={immersive ? t('Exit fullscreen') : t('Enter fullscreen')}
          title={immersive ? t('Exit fullscreen') : t('Enter fullscreen')}
          onClick={() => void toggleFullscreen()}
        >
          {immersive ? <Minimize2 /> : <Maximize2 />}
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
              immersive ? 'min-h-0' : 'min-h-[30rem] rounded-lg border'
            )}
          >
            {appliedConfiguration && (
              <>
                {/* oxlint-disable-next-line react/iframe-missing-sandbox -- The tool needs same-origin storage and scripts for bridge configuration and async task recovery. */}
                <iframe
                  key={`${userId}:${appliedConfiguration.revision}`}
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
            {(bridgeStatus === 'loading' || bridgeStatus === 'configuring') && (
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
  )
}
