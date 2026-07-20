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
  Infinity as InfinityIcon,
  KeyRound,
  LoaderCircle,
  Maximize2,
  Minimize2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { getAllApiKeys } from '@/features/keys/api'
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
  isTrustedInfiniteCanvasMessage,
} from './lib/bridge'
import {
  type ManagedInfiniteCanvasConfiguration,
  migrateInfiniteCanvasToManagedMode,
} from './lib/configuration-storage'
import { requestInfiniteCanvasRuntimeSession } from './lib/runtime-session'
import {
  createApiKeySwitchTarget,
  createApiKeyOptions,
  INFINITE_CANVAS_TOKEN_STORAGE_KEY,
  parseRememberedTokenSelection,
  selectPreferredApiKey,
} from './lib/token-selection'

const TOOL_URL = '/_tools/infinite-canvas/'

type BridgeStatus = 'loading' | 'configuring' | 'ready' | 'error'

interface AppliedConfiguration extends ManagedInfiniteCanvasConfiguration {
  tokenId: number | null
  revision: number
}

type InfiniteCanvasProps = {
  maximized: boolean
  onMaximizedChange: (maximized: boolean) => void
}

export function InfiniteCanvas(props: InfiniteCanvasProps) {
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
  const [appliedConfiguration, setAppliedConfiguration] =
    useState<AppliedConfiguration | null>(null)
  const [keySwitching, setKeySwitching] = useState(false)
  const [bridgeStatus, setBridgeStatus] = useState<BridgeStatus>('loading')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  appliedRevisionRef.current = appliedConfiguration?.revision ?? 0

  useEffect(() => {
    let cancelled = false

    async function loadApiKeys() {
      const managedConfiguration = migrateInfiniteCanvasToManagedMode(
        window.localStorage
      )
      if (!userId) {
        setKeysLoading(false)
        return
      }

      setKeysLoading(true)
      setErrorMessage(null)
      try {
        const response = await getAllApiKeys()
        if (!response.success || !response.data) {
          throw new Error(response.message || 'Failed to load API keys')
        }
        if (cancelled) return

        const now = Math.floor(Date.now() / 1000)
        const legacyRememberedTokenId = parseRememberedTokenSelection(
          window.localStorage.getItem(INFINITE_CANVAS_TOKEN_STORAGE_KEY),
          userId
        )
        let selectedTokenId = legacyRememberedTokenId
        try {
          const preference = await getUserToolPreference('infinite-canvas')
          if (preference.success && preference.data.selected_token_id > 0) {
            selectedTokenId = preference.data.selected_token_id
          }
        } catch {
          selectedTokenId = legacyRememberedTokenId
        }
        const preferredKey = selectPreferredApiKey(
          response.data.items,
          selectedTokenId,
          now
        )

        setApiKeys(response.data.items)
        setAppliedConfiguration({
          ...managedConfiguration,
          tokenId: preferredKey?.id ?? null,
          revision: 1,
        })
        if (preferredKey) {
          if (preferredKey.id !== selectedTokenId) {
            void updateUserToolPreference('infinite-canvas', preferredKey.id)
          }
          window.localStorage.removeItem(INFINITE_CANVAS_TOKEN_STORAGE_KEY)
        }
      } catch {
        if (cancelled) return
        setApiKeys([])
        setAppliedConfiguration({
          ...managedConfiguration,
          tokenId: null,
          revision: 1,
        })
        setErrorMessage(t('Failed to load API keys'))
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
        !isTrustedInfiniteCanvasMessage(
          event,
          iframeWindow,
          window.location.origin
        )
      ) {
        return
      }

      if (event.data.type === 'new-api:infinite-canvas:configured') {
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
            const runtimeSession = await requestInfiniteCanvasRuntimeSession(
              createUserToolRuntimeSession,
              tokenId,
              t('Unnamed API key')
            )
            const currentIframeWindow = iframeRef.current?.contentWindow
            if (
              appliedRevisionRef.current !== revision ||
              !currentIframeWindow ||
              currentIframeWindow !== sourceWindow
            ) {
              return
            }

            runtimeExpiresAtRef.current = runtimeSession.expiresAt
            currentIframeWindow.postMessage(
              createNewApiConfigureMessage(
                window.location.origin,
                runtimeSession.credential,
                runtimeSession.displayLabel
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
      createApiKeyOptions(
        apiKeys,
        Math.floor(Date.now() / 1000),
        t('Unnamed API key')
      ),
    [apiKeys, t]
  )

  const appliedSourceLabel = useMemo(() => {
    if (!appliedConfiguration) return null
    return (
      keyOptions.find(
        (option) => option.value === String(appliedConfiguration.tokenId)
      )?.label ?? null
    )
  }, [appliedConfiguration, keyOptions])

  const switchApiKey = async (value: string | null) => {
    if (!value || !appliedConfiguration || keySwitching) {
      return
    }

    const switchTarget = createApiKeySwitchTarget(
      apiKeys,
      value,
      appliedConfiguration.tokenId,
      appliedConfiguration.revision,
      Math.floor(Date.now() / 1000)
    )
    if (!switchTarget) return

    setKeySwitching(true)
    setErrorMessage(null)
    try {
      const preference = await updateUserToolPreference(
        'infinite-canvas',
        switchTarget.tokenId
      )
      if (!preference.success) {
        throw new Error(preference.message)
      }

      window.localStorage.removeItem(INFINITE_CANVAS_TOKEN_STORAGE_KEY)
      configurationInFlightRef.current = null
      configuredRevisionRef.current = null
      runtimeCredentialRequestRef.current = null
      runtimeExpiresAtRef.current = 0
      setBridgeStatus('loading')
      appliedRevisionRef.current = switchTarget.revision
      setAppliedConfiguration({
        ...appliedConfiguration,
        tokenId: switchTarget.tokenId,
        revision: switchTarget.revision,
      })
    } catch {
      setErrorMessage(t('Failed to save API key selection'))
    } finally {
      setKeySwitching(false)
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

  const infiniteCanvasAvailable = status?.infinite_canvas_available === true
  const infiniteCanvasVersion =
    typeof status?.infinite_canvas_version === 'string'
      ? status.infinite_canvas_version
      : null

  if (statusLoading || keysLoading) {
    return (
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Infinite Canvas')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='flex h-full items-center justify-center'>
            <LoaderCircle className='text-muted-foreground size-8 animate-spin' />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (!infiniteCanvasAvailable) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Infinite Canvas')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <Alert variant='destructive'>
            <CircleAlert />
            <AlertTitle>{t('Infinite canvas is unavailable')}</AlertTitle>
            <AlertDescription>
              {t('This deployment does not include the infinite canvas build.')}
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
    <SectionPageLayout fixedContent immersive={props.maximized}>
      <SectionPageLayout.Title>
        <span className='inline-flex items-center gap-2'>
          <InfinityIcon className='size-5' />
          {t('Infinite Canvas')}
          {!props.maximized && infiniteCanvasVersion && (
            <Badge variant='outline'>v{infiniteCanvasVersion}</Badge>
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
                  disabled={option.disabled}
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
        {!props.maximized && (
          <div className='text-muted-foreground flex items-center gap-1.5 text-xs whitespace-nowrap'>
            {bridgeStatusIcon}
            {bridgeStatusLabel}
          </div>
        )}
        <Button
          type='button'
          variant='outline'
          size='icon-sm'
          aria-label={
            props.maximized ? t('Restore canvas') : t('Maximize canvas')
          }
          title={props.maximized ? t('Restore canvas') : t('Maximize canvas')}
          aria-pressed={props.maximized}
          onClick={() => props.onMaximizedChange(!props.maximized)}
        >
          {props.maximized ? <Minimize2 /> : <Maximize2 />}
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
              'bg-background relative min-h-[30rem] flex-1 overflow-hidden rounded-lg border',
              props.maximized && 'min-h-0 rounded-none border-0'
            )}
          >
            {appliedConfiguration && (
              <>
                {/* oxlint-disable-next-line react/iframe-missing-sandbox -- The tool needs same-origin storage and scripts for bridge configuration and async task recovery. */}
                <iframe
                  key={`${userId}:${appliedConfiguration.revision}`}
                  ref={iframeRef}
                  src={TOOL_URL}
                  title={t('Infinite Canvas')}
                  className={cn(
                    'h-full min-h-[30rem] w-full border-0',
                    props.maximized && 'min-h-0'
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
