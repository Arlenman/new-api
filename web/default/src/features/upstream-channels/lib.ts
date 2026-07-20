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
  UpstreamAuthType,
  UpstreamChannelStatus,
  UpstreamErrorCode,
  UpstreamModelPricing,
  UpstreamModelPricingInterval,
  UpstreamProvider,
} from './types'

export type UpstreamChannelStatusFilter = 'all' | UpstreamChannelStatus
export type UpstreamChannelSort =
  | 'default'
  | 'balance-desc'
  | 'balance-asc'
  | 'availability-desc'
  | 'first-token-latency-asc'
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
  last_error?: string
  snapshot?: Pick<NonNullable<UpstreamChannel['snapshot']>, 'provider'>
}): {
  provider: 'new-api' | 'sub2api'
  authType: 'access_token'
  username: string
} {
  const username = channel.username.trim()
  let provider: 'new-api' | 'sub2api' = 'new-api'
  if (channel.provider === 'sub2api' || channel.provider === 'new-api') {
    provider = channel.provider
  } else if (channel.snapshot?.provider === 'sub2api') {
    provider = 'sub2api'
  } else if (/\bsub2api\b/i.test(channel.last_error || '')) {
    provider = 'sub2api'
  }
  let recommendedUsername = username
  if (provider === 'new-api' && !/^[1-9]\d*$/.test(username)) {
    recommendedUsername = ''
  }
  return {
    provider,
    authType: 'access_token',
    username: recommendedUsername,
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

export function formatUpstreamAvailability(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '-'
  return `${new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(value)}%`
}

export function formatUpstreamFirstTokenLatency(value: number | null): string {
  if (value === null || !Number.isFinite(value) || value < 0) return '-'
  if (value < 1000) {
    return `${new Intl.NumberFormat(undefined, {
      maximumFractionDigits: 0,
    }).format(value)} ms`
  }
  return `${new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(value / 1000)} s`
}

export interface UpstreamModelPricingField {
  label: string
  value: string
}

function formatUpstreamPricingNumber(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 8,
  }).format(value)
}

function formatUpstreamTokenPrice(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '-'
  return `$${formatUpstreamPricingNumber(value * 1_000_000)} / 1M tokens`
}

function formatUpstreamRequestPrice(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '-'
  return `$${formatUpstreamPricingNumber(value)} / request`
}

function formatUpstreamFixedPrice(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '-'
  return formatUpstreamPricingNumber(value)
}

export function getUpstreamModelPricingFields(
  pricing: UpstreamModelPricing
): UpstreamModelPricingField[] {
  if (pricing.source === 'new-api') {
    const fields: UpstreamModelPricingField[] = []
    const ratios: Array<[string, number | undefined]> = [
      ['Model ratio', pricing.model_ratio],
      ['Completion ratio', pricing.completion_ratio],
      ['Cache ratio', pricing.cache_ratio],
      ['Cache creation ratio', pricing.create_cache_ratio],
    ]
    for (const [label, value] of ratios) {
      if (value !== undefined && Number.isFinite(value)) {
        fields.push({ label, value: `×${formatUpstreamPricingNumber(value)}` })
      }
    }
    if (
      pricing.model_price !== undefined &&
      Number.isFinite(pricing.model_price)
    ) {
      fields.push({
        label: 'Fixed price',
        value: formatUpstreamFixedPrice(pricing.model_price),
      })
    }
    return fields
  }

  const fields: UpstreamModelPricingField[] = []
  const tokenPrices: Array<[string, number | undefined]> = [
    ['Input price', pricing.input_price],
    ['Output price', pricing.output_price],
    ['Cache write price', pricing.cache_write_price],
    ['Cache read price', pricing.cache_read_price],
  ]
  for (const [label, value] of tokenPrices) {
    if (value !== undefined && Number.isFinite(value)) {
      fields.push({ label, value: formatUpstreamTokenPrice(value) })
    }
  }
  const imagePrices: Array<[string, number | undefined]> = [
    ['Image input price', pricing.image_input_price],
    ['Image output price', pricing.image_output_price],
  ]
  for (const [label, value] of imagePrices) {
    if (value !== undefined && Number.isFinite(value)) {
      fields.push({
        label,
        value: `$${formatUpstreamPricingNumber(value)}`,
      })
    }
  }
  if (
    pricing.per_request_price !== undefined &&
    Number.isFinite(pricing.per_request_price)
  ) {
    fields.push({
      label: 'Per-request price',
      value: formatUpstreamRequestPrice(pricing.per_request_price),
    })
  }
  return fields
}

export interface UpstreamPricingIntervalLabels {
  tokens?: string
  input?: string
  output?: string
  cacheWrite?: string
  cacheRead?: string
  request?: string
}

export function formatUpstreamPricingInterval(
  interval: UpstreamModelPricingInterval,
  labels: UpstreamPricingIntervalLabels = {}
): string {
  const maxTokens =
    interval.max_tokens === undefined ? '∞' : interval.max_tokens
  const label = interval.tier_label?.trim()
  const range = `${interval.min_tokens.toLocaleString()}-${typeof maxTokens === 'number' ? maxTokens.toLocaleString() : maxTokens} ${labels.tokens ?? 'tokens'}`
  const prefix = label ? `${label}: ` : ''
  const prices: string[] = []
  if (
    interval.input_price !== undefined &&
    Number.isFinite(interval.input_price)
  ) {
    prices.push(
      `${labels.input ?? 'input'} ${formatUpstreamTokenPrice(interval.input_price)}`
    )
  }
  if (
    interval.output_price !== undefined &&
    Number.isFinite(interval.output_price)
  ) {
    prices.push(
      `${labels.output ?? 'output'} ${formatUpstreamTokenPrice(interval.output_price)}`
    )
  }
  if (
    interval.cache_write_price !== undefined &&
    Number.isFinite(interval.cache_write_price)
  ) {
    prices.push(
      `${labels.cacheWrite ?? 'cache write'} ${formatUpstreamTokenPrice(interval.cache_write_price)}`
    )
  }
  if (
    interval.cache_read_price !== undefined &&
    Number.isFinite(interval.cache_read_price)
  ) {
    prices.push(
      `${labels.cacheRead ?? 'cache read'} ${formatUpstreamTokenPrice(interval.cache_read_price)}`
    )
  }
  if (
    interval.per_request_price !== undefined &&
    Number.isFinite(interval.per_request_price)
  ) {
    prices.push(
      `${labels.request ?? 'request'} ${formatUpstreamRequestPrice(interval.per_request_price)}`
    )
  }
  return `${prefix}${range}${prices.length > 0 ? ` · ${prices.join(' · ')}` : ''}`
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
  provider: UpstreamProvider,
  authType: UpstreamAuthType,
  username: string,
  password: string,
  hasSavedPassword: boolean
): boolean {
  const requiresUsername = provider !== 'sub2api' || authType !== 'access_token'
  return (
    (!requiresUsername || username.trim() !== '') &&
    (password.trim() !== '' || hasSavedPassword)
  )
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

  if (channelSort === 'availability-desc') {
    return filteredChannels.toSorted((left, right) => {
      const leftValue = left.availability_24h
      const rightValue = right.availability_24h
      const leftAvailable = leftValue !== null && Number.isFinite(leftValue)
      const rightAvailable = rightValue !== null && Number.isFinite(rightValue)
      if (leftAvailable && rightAvailable) {
        return rightValue - leftValue || left.id - right.id
      }
      if (leftAvailable) return -1
      if (rightAvailable) return 1
      return left.id - right.id
    })
  }

  if (channelSort === 'first-token-latency-asc') {
    return filteredChannels.toSorted((left, right) => {
      const leftValue = left.average_first_token_latency_ms
      const rightValue = right.average_first_token_latency_ms
      const leftAvailable = leftValue !== null && Number.isFinite(leftValue)
      const rightAvailable = rightValue !== null && Number.isFinite(rightValue)
      if (leftAvailable && rightAvailable) {
        return leftValue - rightValue || left.id - right.id
      }
      if (leftAvailable) return -1
      if (rightAvailable) return 1
      return left.id - right.id
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
