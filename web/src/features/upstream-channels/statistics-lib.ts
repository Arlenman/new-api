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
  UpstreamChannelStatisticsItem,
  UpstreamChannelStatisticsTrendItem,
  UpstreamProvider,
} from './types'

export type UpstreamChannelStatisticsMetric = 'quota' | 'token_used' | 'count'
export type UpstreamChannelStatisticsView = 'table' | 'bar' | 'pie' | 'trend'
export type UpstreamChannelStatisticsGranularity = 'day' | 'week' | 'month'
export type UpstreamChannelStatisticsSortKey =
  | UpstreamChannelStatisticsMetric
  | 'last_used_at'
export type UpstreamChannelStatisticsSortDirection = 'asc' | 'desc'

export interface UpstreamChannelStatisticsSortState {
  key: UpstreamChannelStatisticsSortKey
  direction: UpstreamChannelStatisticsSortDirection
}

export interface UpstreamChannelRankingDatum {
  key: string
  label: string
  channelId: number
  channelName: string
  baseUrl: string
  provider: UpstreamProvider
  value: number
  share: number
}

export interface UpstreamChannelRankingData {
  data: UpstreamChannelRankingDatum[]
  total: number
}

export interface UpstreamChannelTrendDatum {
  period: string
  periodLabel: string
  seriesKey: string
  seriesLabel: string
  channelId: number
  channelName: string
  baseUrl: string
  provider: UpstreamProvider
  value: number
}

export interface UpstreamChannelTrendSeries {
  key: string
  label: string
  channelId: number
  channelName: string
  baseUrl: string
  provider: UpstreamProvider
}

export interface UpstreamChannelTrendData {
  data: UpstreamChannelTrendDatum[]
  periods: string[]
  series: UpstreamChannelTrendSeries[]
}

interface UpstreamChannelMetricItem {
  quota?: number
  token_used?: number
  count?: number
}

export function getUpstreamChannelStatisticsTodayRange(now = new Date()): {
  start: Date
  end: Date
} {
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now)
  end.setHours(23, 59, 59, 999)
  return { start, end }
}

export function buildUpstreamChannelStatisticsSearchParams(input: {
  startTimestamp: number
  endTimestamp: number
}): URLSearchParams {
  const params = new URLSearchParams()
  params.set('start_timestamp', String(input.startTimestamp))
  params.set('end_timestamp', String(input.endTimestamp))
  return params
}

export function getUpstreamChannelStatisticsMetricValue(
  row: UpstreamChannelMetricItem,
  metric: UpstreamChannelStatisticsMetric
): number {
  return Number(row[metric] || 0)
}

export function sortUpstreamChannelStatisticsRows(
  rows: UpstreamChannelStatisticsItem[],
  sort: UpstreamChannelStatisticsSortState
): UpstreamChannelStatisticsItem[] {
  return [...rows].sort((left, right) => {
    const difference =
      Number(left[sort.key] || 0) - Number(right[sort.key] || 0)
    if (difference !== 0) {
      return sort.direction === 'asc' ? difference : -difference
    }
    return (
      left.channel_name.localeCompare(right.channel_name) ||
      left.channel_id - right.channel_id
    )
  })
}

export function getNextUpstreamChannelStatisticsSortState(
  current: UpstreamChannelStatisticsSortState,
  key: UpstreamChannelStatisticsSortKey
): UpstreamChannelStatisticsSortState {
  if (current.key !== key) {
    return { key, direction: 'desc' }
  }
  return {
    key,
    direction: current.direction === 'desc' ? 'asc' : 'desc',
  }
}

export function formatUpstreamChannelStatisticsLastUsedAt(
  timestamp?: number
): string {
  if (!timestamp) return '-'
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm:ss')
}

function buildUpstreamChannelLabels(
  rows: Array<{ channel_id: number; channel_name: string }>
): Map<number, string> {
  const channelNames = new Map<number, string>()
  for (const row of rows) {
    if (!channelNames.has(row.channel_id)) {
      channelNames.set(
        row.channel_id,
        row.channel_name.trim() || `#${row.channel_id}`
      )
    }
  }

  const nameCounts = new Map<string, number>()
  for (const channelName of channelNames.values()) {
    nameCounts.set(channelName, (nameCounts.get(channelName) || 0) + 1)
  }

  return new Map(
    [...channelNames].map(([channelId, channelName]) => {
      const label =
        (nameCounts.get(channelName) || 0) > 1
          ? `${channelName} (#${channelId})`
          : channelName
      return [channelId, label]
    })
  )
}

export function buildUpstreamChannelRankingData(
  rows: UpstreamChannelStatisticsItem[],
  metric: UpstreamChannelStatisticsMetric
): UpstreamChannelRankingData {
  const labels = buildUpstreamChannelLabels(rows)
  const data = rows
    .map((row) => ({
      key: String(row.channel_id),
      label: labels.get(row.channel_id) || `#${row.channel_id}`,
      channelId: row.channel_id,
      channelName: row.channel_name,
      baseUrl: row.base_url,
      provider: row.provider,
      value: getUpstreamChannelStatisticsMetricValue(row, metric),
      share: 0,
    }))
    .filter((row) => row.value > 0)
    .sort((left, right) =>
      right.value !== left.value
        ? right.value - left.value
        : left.label.localeCompare(right.label)
    )
  const total = data.reduce((sum, row) => sum + row.value, 0)

  return {
    total,
    data: data.map((row) => ({
      ...row,
      share: total > 0 ? row.value / total : 0,
    })),
  }
}

function getUpstreamChannelTrendPeriodStart(
  timestamp: number,
  granularity: UpstreamChannelStatisticsGranularity
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

function getUpstreamChannelTrendPeriodKey(
  timestamp: number,
  granularity: UpstreamChannelStatisticsGranularity
): string {
  const periodStart = getUpstreamChannelTrendPeriodStart(timestamp, granularity)
  return periodStart.format(granularity === 'month' ? 'YYYY-MM' : 'YYYY-MM-DD')
}

function getUpstreamChannelTrendPeriodLabel(
  timestamp: number,
  granularity: UpstreamChannelStatisticsGranularity
): string {
  const periodStart = getUpstreamChannelTrendPeriodStart(timestamp, granularity)
  if (granularity === 'week') {
    return `${periodStart.format('YYYY-MM-DD')} – ${periodStart
      .add(6, 'day')
      .format('YYYY-MM-DD')}`
  }
  return periodStart.format(granularity === 'month' ? 'YYYY-MM' : 'YYYY-MM-DD')
}

export function buildUpstreamChannelTrendData(
  rows: UpstreamChannelStatisticsTrendItem[],
  metric: UpstreamChannelStatisticsMetric,
  granularity: UpstreamChannelStatisticsGranularity,
  range: { startTimestamp: number; endTimestamp: number }
): UpstreamChannelTrendData {
  const labels = buildUpstreamChannelLabels(rows)
  const seriesMetadata = new Map<number, UpstreamChannelTrendSeries>()
  const values = new Map<string, number>()

  for (const row of rows) {
    const seriesKey = String(row.channel_id)
    const seriesLabel = labels.get(row.channel_id) || `#${row.channel_id}`
    seriesMetadata.set(row.channel_id, {
      key: seriesKey,
      label: seriesLabel,
      channelId: row.channel_id,
      channelName: row.channel_name,
      baseUrl: row.base_url,
      provider: row.provider,
    })
    const period = getUpstreamChannelTrendPeriodKey(row.created_at, granularity)
    const valueKey = `${seriesKey}\u001f${period}`
    values.set(
      valueKey,
      (values.get(valueKey) || 0) +
        getUpstreamChannelStatisticsMetricValue(row, metric)
    )
  }

  const periodStarts: number[] = []
  let current = getUpstreamChannelTrendPeriodStart(
    range.startTimestamp,
    granularity
  )
  const end = getUpstreamChannelTrendPeriodStart(
    range.endTimestamp,
    granularity
  )
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

  const series = [...seriesMetadata.values()].sort(
    (left, right) =>
      left.label.localeCompare(right.label) || left.channelId - right.channelId
  )
  const data: UpstreamChannelTrendDatum[] = []
  for (const item of series) {
    for (const timestamp of periodStarts) {
      const period = getUpstreamChannelTrendPeriodKey(timestamp, granularity)
      data.push({
        period,
        periodLabel: getUpstreamChannelTrendPeriodLabel(timestamp, granularity),
        seriesKey: item.key,
        seriesLabel: item.label,
        channelId: item.channelId,
        channelName: item.channelName,
        baseUrl: item.baseUrl,
        provider: item.provider,
        value: values.get(`${item.key}\u001f${period}`) || 0,
      })
    }
  }

  return {
    data,
    periods: periodStarts.map((timestamp) =>
      getUpstreamChannelTrendPeriodKey(timestamp, granularity)
    ),
    series,
  }
}

export function filterUpstreamChannelTrendData(
  data: UpstreamChannelTrendData,
  hiddenSeriesKeys: Set<string>
): UpstreamChannelTrendData {
  return {
    ...data,
    data: data.data.filter((row) => !hiddenSeriesKeys.has(row.seriesKey)),
  }
}

export function toggleUpstreamChannelTrendSeriesVisibility(
  hiddenSeriesKeys: Set<string>,
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
