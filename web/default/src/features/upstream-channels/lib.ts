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
import type {
  UpstreamChannel,
  UpstreamChannelStatus,
  UpstreamErrorCode,
  UpstreamProvider,
} from './types'

export type UpstreamChannelStatusFilter = 'all' | UpstreamChannelStatus
export type UpstreamChannelSort =
  | 'default'
  | 'balance-desc'
  | 'balance-asc'
  | 'multiplier-desc'
  | 'multiplier-asc'

export const upstreamTurnstileAccessTokenErrorCode: UpstreamErrorCode =
  'upstream_turnstile_requires_access_token'

export function isUpstreamTurnstileAccessTokenRequired(
  errorCode?: string
): boolean {
  return errorCode === upstreamTurnstileAccessTokenErrorCode
}

export function getUpstreamAccessTokenRecommendation(channel: {
  provider: UpstreamProvider
  username: string
}): {
  provider: 'new-api'
  authType: 'access_token'
  username: string
} {
  const username = channel.username.trim()
  return {
    provider: 'new-api',
    authType: 'access_token',
    username: /^[1-9]\d*$/.test(username) ? username : '',
  }
}

export type UpstreamSelectedGroupMultiplier =
  | { status: 'unselected' }
  | { status: 'invalid' }
  | { status: 'valid'; value: number }

export function formatUpstreamBalance(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 6,
  }).format(value)
}

export function formatUpstreamTime(timestamp: number): string {
  if (!timestamp) return '-'
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(timestamp * 1000))
}

export function getEffectiveUpstreamMultiplier(multiplier: number): number {
  return Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1
}

export function getAdjustedUpstreamAmount(
  value: number,
  multiplier: number
): number {
  return value * getEffectiveUpstreamMultiplier(multiplier)
}

export function getUpstreamSelectedGroupMultiplier(
  channel: UpstreamChannel
): UpstreamSelectedGroupMultiplier {
  const selectedGroup = channel.selected_group.trim()
  if (!selectedGroup) return { status: 'unselected' }

  const snapshot = channel.snapshot
  const group = snapshot?.groups.find(
    (item) => item.name.trim() === selectedGroup
  )
  if (!snapshot || !group) return { status: 'invalid' }

  const ratioKey = String(group.id ?? group.name)
  const ratio = snapshot.ratios[ratioKey] ?? group.ratio
  const value = ratio * getEffectiveUpstreamMultiplier(channel.multiplier)
  if (!Number.isFinite(value) || value < 0) return { status: 'invalid' }
  return { status: 'valid', value }
}

export function getTotalAdjustedUpstreamBalance(
  channels: UpstreamChannel[]
): number {
  return channels.reduce(
    (total, channel) =>
      total + getAdjustedUpstreamAmount(channel.balance, channel.multiplier),
    0
  )
}

export function getUpstreamChannelKeyStats(channels: UpstreamChannel[]): {
  total: number
  active: number
} {
  return channels.reduce(
    (stats, channel) => ({
      total: stats.total + (channel.snapshot?.keys.length ?? 0),
      active: stats.active + channel.active_source_channel_count,
    }),
    { total: 0, active: 0 }
  )
}

export function isValidUpstreamMultiplier(multiplier: number): boolean {
  if (
    !Number.isFinite(multiplier) ||
    multiplier <= 0 ||
    multiplier > 1_000_000_000
  ) {
    return false
  }
  const scaled = multiplier * 100
  return Math.abs(scaled - Math.round(scaled)) <= 1e-9
}

const newAPICardTone =
  'border-pink-200/80 border-l-pink-500 bg-background dark:border-pink-900/80 dark:border-l-pink-500'
const sub2APICardTone =
  'border-blue-200/80 border-l-blue-500 bg-blue-50/70 dark:border-blue-900/80 dark:border-l-blue-500 dark:bg-blue-950/25'
const otherCardTone =
  'border-amber-200/80 border-l-amber-500 bg-amber-50/70 dark:border-amber-900/80 dark:border-l-amber-500 dark:bg-amber-950/25'
const unknownCardTone =
  'border-border border-l-muted-foreground/50 bg-muted/20 dark:border-l-muted-foreground/60'
export function getUpstreamCardTone(provider: UpstreamProvider): string {
  if (provider === 'new-api') return newAPICardTone
  if (provider === 'sub2api') return sub2APICardTone
  if (provider === 'other') return otherCardTone
  return unknownCardTone
}

export function getUpstreamImportBaseName(baseURL: string): string {
  const trimmed = baseURL.trim()
  try {
    return new URL(trimmed).host || trimmed
  } catch {
    return trimmed
  }
}

export function getUpstreamChannelDefaultName(baseURL: string): string {
  const trimmed = baseURL.trim()
  try {
    const hostname = new URL(trimmed).hostname.replace(/\.$/, '')
    if (!hostname) return trimmed
    if (hostname.includes(':') || /^\d{1,3}(?:\.\d{1,3}){3}$/.test(hostname)) {
      return hostname
    }
    const parts = hostname.split('.').filter(Boolean)
    let index = 0
    while (index < parts.length - 1) {
      const part = parts[index]?.toLowerCase()
      if (
        part !== 'api' &&
        part !== 'www' &&
        part !== 'ai' &&
        part !== 'sub' &&
        part !== 'sub2api' &&
        part !== 'gateway' &&
        part !== 'vip' &&
        part !== 'openai'
      ) {
        break
      }
      index += 1
    }
    return parts[index] || hostname
  } catch {
    return trimmed
  }
}

export function hasUsableUpstreamCredentials(
  username: string,
  password: string,
  hasSavedPassword: boolean
): boolean {
  return username.trim() !== '' && (password.trim() !== '' || hasSavedPassword)
}

export function getUpstreamChannelDisplayName(
  name: string,
  baseURL: string
): string {
  return name.trim() || getUpstreamChannelDefaultName(baseURL)
}

export function getUpstreamImportDefaults(channel: {
  name: string
  base_url: string
}): { tag: string; namePrefix: string } {
  const tag = getUpstreamChannelDisplayName(channel.name, channel.base_url)
  return {
    tag,
    namePrefix: tag,
  }
}

export function filterAndSortUpstreamChannels(
  channels: UpstreamChannel[],
  statusFilter: UpstreamChannelStatusFilter,
  channelSort: UpstreamChannelSort
): UpstreamChannel[] {
  const filteredChannels =
    statusFilter === 'all'
      ? channels
      : channels.filter((channel) => channel.status === statusFilter)

  if (channelSort === 'default') {
    return filteredChannels.toSorted(
      (left, right) => right.priority - left.priority || left.id - right.id
    )
  }

  if (channelSort === 'balance-desc' || channelSort === 'balance-asc') {
    const direction = channelSort === 'balance-desc' ? -1 : 1
    return filteredChannels.toSorted((left, right) => {
      const balanceDifference =
        (getAdjustedUpstreamAmount(left.balance, left.multiplier) -
          getAdjustedUpstreamAmount(right.balance, right.multiplier)) *
        direction
      return balanceDifference || left.id - right.id
    })
  }

  const direction = channelSort === 'multiplier-desc' ? -1 : 1
  return filteredChannels.toSorted((left, right) => {
    const leftMultiplier = getUpstreamSelectedGroupMultiplier(left)
    const rightMultiplier = getUpstreamSelectedGroupMultiplier(right)
    if (leftMultiplier.status === 'valid') {
      if (rightMultiplier.status !== 'valid') return -1
      const difference =
        (leftMultiplier.value - rightMultiplier.value) * direction
      return difference || left.id - right.id
    }
    if (rightMultiplier.status === 'valid') return 1
    return left.id - right.id
  })
}
