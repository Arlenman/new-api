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
import dayjs from 'dayjs'

import type {
  TokenTagOptionItem,
  TokenTagQuotaDataItem,
  TokenTagQuotaTrendItem,
} from '../dashboard/types'

export type TokenTagSortKey = 'quota' | 'token_used' | 'count' | 'last_used_at'
export type SortDirection = 'asc' | 'desc'
export type TokenTagRankingMetric = 'quota' | 'token_used' | 'count'
export type TokenTagTrendGranularity = 'day' | 'week' | 'month'

export const TOKEN_TAGS_FIXED_CONTENT = false
export const TOKEN_TAGS_CONTENT_CLASS = 'flex flex-col gap-4 pb-4'
export const NO_TAG_FILTER_VALUE = '__new_api_untagged__'

export interface TokenTagSortState {
  key: TokenTagSortKey
  direction: SortDirection
}

export interface TokenTagSearchParamsInput {
  startTimestamp: number
  endTimestamp: number
  username?: string
  includedTags?: string[]
  excludedTags?: string[]
}

export interface TokenTagChartOptions {
  isAdmin: boolean
  noTagLabel: string
  unknownModelLabel: string
}

export interface TokenTagChartCategory {
  key: string
  label: string
  total: number
  username: string
  tagName: string
  tagNames: string[]
  tokenId: number
  tokenName: string
}

export interface TokenTagChartDatum extends TokenTagChartCategory {
  modelName: string
  value: number
  share: number
}

export interface TokenTagChartData {
  categories: TokenTagChartCategory[]
  data: TokenTagChartDatum[]
  models: string[]
}

export interface TokenTagDistributionDatum extends TokenTagChartCategory {
  value: number
  share: number
}

export interface TokenTagDistributionData {
  data: TokenTagDistributionDatum[]
  total: number
}

export interface TokenTagTrendDatum {
  period: string
  periodLabel: string
  seriesKey: string
  seriesLabel: string
  value: number
  tagName: string
  username: string
}

export interface TokenTagTrendSeries {
  key: string
  label: string
  tagName: string
  username: string
}

export interface TokenTagTrendData {
  data: TokenTagTrendDatum[]
  periods: string[]
  series: TokenTagTrendSeries[]
}

interface TokenTagMetricItem {
  quota?: number
  token_used?: number
  count?: number
}

interface TokenTagIdentityItem {
  tag_id: number
  tag_name: string
  user_id?: number
  username?: string
}

function userKey(row: TokenTagIdentityItem): string {
  return row.user_id ? `id:${row.user_id}` : `name:${row.username || ''}`
}

function tagKey(row: TokenTagIdentityItem): string {
  return row.tag_id ? `id:${row.tag_id}` : `name:${row.tag_name || ''}`
}

function metricValue(
  row: TokenTagMetricItem,
  metric: TokenTagRankingMetric
): number {
  return Number(row[metric] || 0)
}

function normalizedModelName(
  row: TokenTagQuotaDataItem,
  unknownModelLabel: string
): string {
  return row.model_name?.trim() || unknownModelLabel
}

function addRowMetrics(
  target: TokenTagQuotaDataItem,
  row: TokenTagQuotaDataItem
): void {
  target.quota = (target.quota || 0) + (row.quota || 0)
  target.token_used = (target.token_used || 0) + (row.token_used || 0)
  target.count = (target.count || 0) + (row.count || 0)
  target.last_used_at = Math.max(
    target.last_used_at || 0,
    row.last_used_at || 0
  )
  target.model_name = ''
}

export function getTodayRange(now = new Date()): { start: Date; end: Date } {
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now)
  end.setHours(23, 59, 59, 999)
  return { start, end }
}

export function buildTokenTagSearchParams(
  input: TokenTagSearchParamsInput
): URLSearchParams {
  const params = new URLSearchParams()
  params.set('start_timestamp', String(input.startTimestamp))
  params.set('end_timestamp', String(input.endTimestamp))
  if (input.username) {
    params.set('username', input.username)
  }
  for (const tag of input.includedTags || []) {
    if (tag === NO_TAG_FILTER_VALUE) {
      params.set('include_untagged', 'true')
    } else {
      params.append('token_tag', tag)
    }
  }
  for (const tag of input.excludedTags || []) {
    if (tag === NO_TAG_FILTER_VALUE) {
      params.set('exclude_untagged', 'true')
    } else {
      params.append('exclude_token_tag', tag)
    }
  }
  return params
}

export function groupTokenTagRows(
  rows: TokenTagQuotaDataItem[]
): TokenTagQuotaDataItem[] {
  const byTag = new Map<string, TokenTagQuotaDataItem>()
  for (const row of rows) {
    const key = `${userKey(row)}\u001f${tagKey(row)}`
    const current = byTag.get(key)
    if (!current) {
      byTag.set(key, {
        ...row,
        token_name: '',
        token_id: 0,
        model_name: '',
      })
      continue
    }
    addRowMetrics(current, row)
  }
  return sortTokenTagRows([...byTag.values()], {
    key: 'quota',
    direction: 'desc',
  })
}

export function buildTokenKeyRows(
  rows: TokenTagQuotaDataItem[]
): TokenTagQuotaDataItem[] {
  const byKey = new Map<string, TokenTagQuotaDataItem>()
  for (const row of rows) {
    const key = `${userKey(row)}\u001f${tagKey(row)}\u001f${row.token_id}`
    const current = byKey.get(key)
    if (!current) {
      byKey.set(key, { ...row, model_name: '' })
      continue
    }
    addRowMetrics(current, row)
  }
  return sortTokenTagRows([...byKey.values()], {
    key: 'quota',
    direction: 'desc',
  })
}

export function sortTokenTagRows(
  rows: TokenTagQuotaDataItem[],
  sort: TokenTagSortState
): TokenTagQuotaDataItem[] {
  const multiplier = sort.direction === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const left = Number(a[sort.key] || 0)
    const right = Number(b[sort.key] || 0)
    if (left !== right) {
      return (left - right) * multiplier
    }
    return [
      a.username || '',
      a.tag_name || '',
      a.token_name || '',
      String(a.token_id || 0),
    ]
      .join('\u001f')
      .localeCompare(
        [
          b.username || '',
          b.tag_name || '',
          b.token_name || '',
          String(b.token_id || 0),
        ].join('\u001f')
      )
  })
}

export function buildTokenTagOptionNames(
  options: TokenTagOptionItem[] | undefined,
  rows: TokenTagQuotaDataItem[],
  selectedTags: string[] = []
): string[] {
  const names = new Set<string>()
  for (const option of options || []) {
    if (option.name) {
      names.add(option.name)
    }
  }
  for (const row of rows) {
    if (row.tag_name) {
      names.add(row.tag_name)
    }
  }
  for (const tag of selectedTags) {
    if (tag) {
      names.add(tag)
    }
  }
  return [...names].sort((a, b) => a.localeCompare(b))
}

export function buildTagRankingChartData(
  rows: TokenTagQuotaDataItem[],
  metric: TokenTagRankingMetric,
  options: TokenTagChartOptions
): TokenTagChartData {
  const categories = new Map<string, TokenTagChartCategory>()
  const values = new Map<string, TokenTagChartDatum>()
  const models = new Set<string>()

  for (const row of rows) {
    const categoryKey = `${userKey(row)}\u001f${tagKey(row)}`
    const tagName = row.tag_name || options.noTagLabel
    const username = row.username || ''
    const label =
      options.isAdmin && username ? `${username} / ${tagName}` : tagName
    const modelName = normalizedModelName(row, options.unknownModelLabel)
    const value = metricValue(row, metric)
    models.add(modelName)

    let category = categories.get(categoryKey)
    if (!category) {
      category = {
        key: categoryKey,
        label,
        total: 0,
        username,
        tagName,
        tagNames: [tagName],
        tokenId: 0,
        tokenName: '',
      }
      categories.set(categoryKey, category)
    }
    category.total += value

    const valueKey = `${categoryKey}\u001f${modelName}`
    const current = values.get(valueKey)
    if (current) {
      current.value += value
      continue
    }
    values.set(valueKey, {
      ...category,
      modelName,
      value,
      share: 0,
    })
  }

  return finalizeChartData(categories, values, models)
}

export function buildKeyRankingChartData(
  rows: TokenTagQuotaDataItem[],
  metric: TokenTagRankingMetric,
  options: TokenTagChartOptions
): TokenTagChartData {
  const tokenMetadata = new Map<
    string,
    {
      username: string
      tokenId: number
      tokenName: string
      tagNames: Set<string>
      models: Map<string, number>
    }
  >()

  for (const row of rows) {
    const key = `${userKey(row)}\u001f${row.token_id}`
    let metadata = tokenMetadata.get(key)
    if (!metadata) {
      metadata = {
        username: row.username || '',
        tokenId: row.token_id,
        tokenName: row.token_name || `#${row.token_id}`,
        tagNames: new Set<string>(),
        models: new Map<string, number>(),
      }
      tokenMetadata.set(key, metadata)
    }
    metadata.tagNames.add(row.tag_name || options.noTagLabel)
    const modelName = normalizedModelName(row, options.unknownModelLabel)
    const value = metricValue(row, metric)
    metadata.models.set(
      modelName,
      Math.max(metadata.models.get(modelName) || 0, value)
    )
  }

  const baseLabels = new Map<string, number>()
  for (const metadata of tokenMetadata.values()) {
    const baseLabel =
      options.isAdmin && metadata.username
        ? `${metadata.username} / ${metadata.tokenName}`
        : metadata.tokenName
    baseLabels.set(baseLabel, (baseLabels.get(baseLabel) || 0) + 1)
  }

  const categories = new Map<string, TokenTagChartCategory>()
  const values = new Map<string, TokenTagChartDatum>()
  const models = new Set<string>()
  for (const [key, metadata] of tokenMetadata) {
    const baseLabel =
      options.isAdmin && metadata.username
        ? `${metadata.username} / ${metadata.tokenName}`
        : metadata.tokenName
    const label =
      (baseLabels.get(baseLabel) || 0) > 1
        ? `${baseLabel} (#${metadata.tokenId})`
        : baseLabel
    const tagNames = [...metadata.tagNames].sort((a, b) => a.localeCompare(b))
    const category: TokenTagChartCategory = {
      key,
      label,
      total: 0,
      username: metadata.username,
      tagName: tagNames.join(', '),
      tagNames,
      tokenId: metadata.tokenId,
      tokenName: metadata.tokenName,
    }
    for (const [modelName, value] of metadata.models) {
      category.total += value
      models.add(modelName)
      values.set(`${key}\u001f${modelName}`, {
        ...category,
        modelName,
        value,
        share: 0,
      })
    }
    categories.set(key, category)
  }

  return finalizeChartData(categories, values, models)
}

export function buildDistributionChartData(
  chart: TokenTagChartData
): TokenTagDistributionData {
  const categories = chart.categories
    .filter((category) => category.total > 0)
    .sort((a, b) => b.total - a.total || a.label.localeCompare(b.label))
  const total = categories.reduce((sum, category) => sum + category.total, 0)
  return {
    total,
    data: categories.map((category) => ({
      ...category,
      value: category.total,
      share: total > 0 ? category.total / total : 0,
    })),
  }
}

function getTrendPeriodStart(
  timestamp: number,
  granularity: TokenTagTrendGranularity
) {
  const value = dayjs(timestamp * 1000)
  if (granularity === 'month') {
    return value.startOf('month')
  }
  if (granularity === 'week') {
    const dayStart = value.startOf('day')
    return dayStart.subtract((dayStart.day() + 6) % 7, 'day')
  }
  return value.startOf('day')
}

function getTrendPeriodKey(
  timestamp: number,
  granularity: TokenTagTrendGranularity
): string {
  const periodStart = getTrendPeriodStart(timestamp, granularity)
  return periodStart.format(granularity === 'month' ? 'YYYY-MM' : 'YYYY-MM-DD')
}

function getTrendPeriodLabel(
  timestamp: number,
  granularity: TokenTagTrendGranularity
): string {
  const periodStart = getTrendPeriodStart(timestamp, granularity)
  if (granularity === 'week') {
    return `${periodStart.format('YYYY-MM-DD')} – ${periodStart
      .add(6, 'day')
      .format('YYYY-MM-DD')}`
  }
  return periodStart.format(granularity === 'month' ? 'YYYY-MM' : 'YYYY-MM-DD')
}

export function buildTagTrendChartData(
  rows: TokenTagQuotaTrendItem[],
  metric: TokenTagRankingMetric,
  granularity: TokenTagTrendGranularity,
  options: TokenTagChartOptions,
  range: { startTimestamp: number; endTimestamp: number }
): TokenTagTrendData {
  const seriesMetadata = new Map<
    string,
    { label: string; tagName: string; username: string }
  >()
  const values = new Map<string, number>()

  for (const row of rows) {
    const seriesKey = `${userKey(row)}\u001f${tagKey(row)}`
    const tagName = row.tag_name || options.noTagLabel
    const username = row.username || ''
    const label =
      options.isAdmin && username ? `${username} / ${tagName}` : tagName
    seriesMetadata.set(seriesKey, { label, tagName, username })
    const period = getTrendPeriodKey(row.created_at, granularity)
    const valueKey = `${seriesKey}\u001f${period}`
    values.set(valueKey, (values.get(valueKey) || 0) + metricValue(row, metric))
  }

  const periodStarts: number[] = []
  let current = getTrendPeriodStart(range.startTimestamp, granularity)
  const end = getTrendPeriodStart(range.endTimestamp, granularity)
  while (current.valueOf() <= end.valueOf()) {
    periodStarts.push(Math.floor(current.valueOf() / 1000))
    if (granularity === 'month') {
      current = current.add(1, 'month')
    } else if (granularity === 'week') {
      current = current.add(1, 'week')
    } else {
      current = current.add(1, 'day')
    }
  }

  const periods = periodStarts.map((timestamp) =>
    getTrendPeriodKey(timestamp, granularity)
  )
  const series = [...seriesMetadata.entries()].sort(
    ([keyA, valueA], [keyB, valueB]) =>
      valueA.label.localeCompare(valueB.label) || keyA.localeCompare(keyB)
  )
  const data: TokenTagTrendDatum[] = []
  for (const [seriesKey, metadata] of series) {
    for (const timestamp of periodStarts) {
      const period = getTrendPeriodKey(timestamp, granularity)
      data.push({
        period,
        periodLabel: getTrendPeriodLabel(timestamp, granularity),
        seriesKey,
        seriesLabel: metadata.label,
        value: values.get(`${seriesKey}\u001f${period}`) || 0,
        tagName: metadata.tagName,
        username: metadata.username,
      })
    }
  }

  return {
    data,
    periods,
    series: series.map(([key, metadata]) => ({ key, ...metadata })),
  }
}

export function filterTagTrendChartData(
  data: TokenTagTrendData,
  hiddenSeriesKeys: ReadonlySet<string>
): TokenTagTrendData {
  if (hiddenSeriesKeys.size === 0) {
    return data
  }
  return {
    ...data,
    data: data.data.filter((item) => !hiddenSeriesKeys.has(item.seriesKey)),
  }
}

export function toggleTagTrendSeriesVisibility(
  hiddenSeriesKeys: ReadonlySet<string>,
  seriesKey: string
): Set<string> {
  const next = new Set(hiddenSeriesKeys)
  if (next.has(seriesKey)) {
    next.delete(seriesKey)
  } else {
    next.add(seriesKey)
  }
  return next
}

function finalizeChartData(
  categoryMap: Map<string, TokenTagChartCategory>,
  valueMap: Map<string, TokenTagChartDatum>,
  modelSet: Set<string>
): TokenTagChartData {
  const categories = [...categoryMap.values()].sort((a, b) => {
    if (a.total !== b.total) {
      return a.total - b.total
    }
    return a.label.localeCompare(b.label)
  })
  const categoryOrder = new Map(
    categories.map((category, index) => [category.key, index])
  )
  const data = [...valueMap.values()]
    .map((item) => {
      const category = categoryMap.get(item.key)
      const total = category?.total || 0
      return {
        ...item,
        total,
        share: total > 0 ? item.value / total : 0,
      }
    })
    .sort((a, b) => {
      const order =
        (categoryOrder.get(a.key) || 0) - (categoryOrder.get(b.key) || 0)
      return order || a.modelName.localeCompare(b.modelName)
    })
  return {
    categories,
    data,
    models: [...modelSet].sort((a, b) => a.localeCompare(b)),
  }
}

export function formatTokenTagLastUsedAt(timestamp?: number): string {
  if (!timestamp || timestamp === -1 || timestamp === 0) {
    return '-'
  }
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm:ss')
}
