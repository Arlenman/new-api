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
import { AlertTriangle } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { NativeSelect } from '@/components/ui/native-select'

import {
  getUpstreamAccessTokenRecommendation,
  getUpstreamChannelDefaultName,
  hasUsableUpstreamCredentials,
} from '../lib'
import type {
  CreateUpstreamChannelConfig,
  UpstreamAuthType,
  UpstreamChannel,
  UpstreamProvider,
} from '../types'

interface UpstreamChannelConfigDialogProps {
  channel: UpstreamChannel | null
  open: boolean
  saving: boolean
  accessTokenRequired: boolean
  onOpenChange: (open: boolean) => void
  onSave: (config: CreateUpstreamChannelConfig) => void
}

export function UpstreamChannelConfigDialog({
  channel,
  open,
  saving,
  accessTokenRequired,
  onOpenChange,
  onSave,
}: UpstreamChannelConfigDialogProps) {
  const { t } = useTranslation()
  const [baseURL, setBaseURL] = useState('')
  const [name, setName] = useState('')
  const [provider, setProvider] = useState<UpstreamProvider>('auto')
  const [authType, setAuthType] = useState<UpstreamAuthType>('password')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [balanceThreshold, setBalanceThreshold] = useState('0')
  const [multiplier, setMultiplier] = useState('1')
  const [autoRefreshInterval, setAutoRefreshInterval] = useState('300')
  const [priority, setPriority] = useState('0')
  const savedAuthType = channel?.auth_type || 'password'
  const canReuseSavedCredential =
    channel?.has_password === true && savedAuthType === authType
  const canRefresh =
    provider !== 'other' &&
    hasUsableUpstreamCredentials(username, password, canReuseSavedCredential)
  let submitLabel = t('Save')
  if (saving) {
    submitLabel = t('Saving...')
  } else if (canRefresh) {
    submitLabel = t('Save and refresh')
  }

  useEffect(() => {
    if (!open) return
    const accessTokenRecommendation =
      accessTokenRequired && channel
        ? getUpstreamAccessTokenRecommendation(channel)
        : null
    setBaseURL(channel?.base_url || '')
    setName(channel?.name || '')
    setProvider(
      accessTokenRecommendation?.provider || channel?.provider || 'auto'
    )
    setAuthType(
      accessTokenRecommendation?.authType || channel?.auth_type || 'password'
    )
    setUsername(accessTokenRecommendation?.username ?? channel?.username ?? '')
    setPassword('')
    setBalanceThreshold(String(channel?.balance_threshold ?? 0))
    setMultiplier(String(channel?.multiplier ?? 1))
    setAutoRefreshInterval(String(channel?.auto_refresh_interval ?? 300))
    setPriority(String(channel?.priority ?? 0))
  }, [accessTokenRequired, channel, open])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onSave({
      base_url: baseURL.trim(),
      name: name.trim(),
      provider,
      auth_type: authType,
      username: username.trim(),
      password,
      balance_threshold: Number(balanceThreshold),
      multiplier: Number(multiplier),
      auto_refresh_interval: Number(autoRefreshInterval),
      priority: Number(priority),
    })
  }

  function handleOpenChange(nextOpen: boolean) {
    if (saving) return
    onOpenChange(nextOpen)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {channel
              ? t('Configure upstream channel')
              : t('Add upstream channel')}
          </DialogTitle>
          <DialogDescription className='break-all'>
            {channel?.base_url || t('Add a manually managed upstream channel')}
          </DialogDescription>
        </DialogHeader>
        <form className='space-y-4' onSubmit={handleSubmit}>
          <fieldset className='space-y-4' disabled={saving}>
            {accessTokenRequired && (
              <Alert variant='destructive'>
                <AlertTriangle />
                <AlertTitle>
                  {t('Turnstile requires a management access token')}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'This upstream New-API has Turnstile enabled. Background synchronization cannot use account-password login. Enter the numeric user ID and create a management access token in the upstream account settings.'
                  )}
                </AlertDescription>
              </Alert>
            )}
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-base-url'>{t('Base URL')}</Label>
              <Input
                id='upstream-base-url'
                type='url'
                maxLength={2048}
                value={baseURL}
                onChange={(event) => setBaseURL(event.target.value)}
                readOnly={Boolean(channel)}
                required
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-channel-name'>{t('Channel name')}</Label>
              <Input
                id='upstream-channel-name'
                maxLength={255}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder={getUpstreamChannelDefaultName(baseURL)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-provider'>{t('Provider type')}</Label>
              <NativeSelect
                id='upstream-provider'
                className='w-full'
                value={provider}
                disabled={authType === 'access_token'}
                onChange={(event) =>
                  setProvider(event.target.value as UpstreamProvider)
                }
              >
                <option value='auto'>{t('Auto detect')}</option>
                <option value='new-api'>New-API</option>
                <option value='sub2api'>Sub2API</option>
                <option value='other'>{t('Other')}</option>
              </NativeSelect>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-auth-type'>
                {t('Authentication method')}
              </Label>
              <NativeSelect
                id='upstream-auth-type'
                className='w-full'
                value={authType}
                onChange={(event) => {
                  const nextAuthType = event.target.value as UpstreamAuthType
                  if (nextAuthType === 'access_token') {
                    const recommendation = getUpstreamAccessTokenRecommendation(
                      {
                        provider,
                        username,
                      }
                    )
                    setProvider(recommendation.provider)
                    setAuthType(recommendation.authType)
                    setUsername(recommendation.username)
                  } else {
                    setAuthType(nextAuthType)
                  }
                  setPassword('')
                }}
              >
                <option value='password'>{t('Account password')}</option>
                <option value='access_token'>
                  {t('Management access token')}
                </option>
              </NativeSelect>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-username'>
                {authType === 'access_token'
                  ? t('Upstream numeric user ID')
                  : t('Upstream username or email')}
              </Label>
              <Input
                id='upstream-username'
                autoComplete='username'
                inputMode={authType === 'access_token' ? 'numeric' : undefined}
                maxLength={255}
                value={username}
                onChange={(event) => setUsername(event.target.value)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='upstream-password'>
                {authType === 'access_token'
                  ? t('Management access token')
                  : t('Upstream password')}
              </Label>
              <Input
                id='upstream-password'
                type='password'
                autoComplete='current-password'
                maxLength={2048}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder={
                  canReuseSavedCredential
                    ? t('Leave blank to keep the saved credential')
                    : undefined
                }
              />
              {authType === 'access_token' && (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Use the numeric user ID and management access token from the upstream New-API account for background synchronization.'
                  )}
                </p>
              )}
            </div>
            <div className='grid gap-4 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label htmlFor='upstream-threshold'>
                  {t('Low balance threshold')}
                </Label>
                <Input
                  id='upstream-threshold'
                  type='number'
                  min='0'
                  max='1000000000'
                  step='0.000001'
                  value={balanceThreshold}
                  onChange={(event) => setBalanceThreshold(event.target.value)}
                  required
                />
                <p className='text-muted-foreground text-xs'>
                  {t('Set to 0 to disable low balance notifications')}
                </p>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='upstream-multiplier'>
                  {t('Channel multiplier')}
                </Label>
                <Input
                  id='upstream-multiplier'
                  type='number'
                  min='0.01'
                  max='1000000000'
                  step='0.01'
                  value={multiplier}
                  onChange={(event) => setMultiplier(event.target.value)}
                  required
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='upstream-refresh-interval'>
                  {t('Auto refresh interval (seconds)')}
                </Label>
                <Input
                  id='upstream-refresh-interval'
                  type='number'
                  min='0'
                  max='86400'
                  step='1'
                  value={autoRefreshInterval}
                  onChange={(event) =>
                    setAutoRefreshInterval(event.target.value)
                  }
                  required
                />
                <p className='text-muted-foreground text-xs'>
                  {t('Use 0 to disable, otherwise enter 60 to 86400 seconds')}
                </p>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='upstream-priority'>{t('Priority')}</Label>
                <Input
                  id='upstream-priority'
                  type='number'
                  min='-2147483648'
                  max='2147483647'
                  step='1'
                  value={priority}
                  onChange={(event) => setPriority(event.target.value)}
                  required
                />
              </div>
            </div>
          </fieldset>
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={saving}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={saving}>
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
