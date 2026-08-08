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
import { useQuery } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  BarChart3,
  LineChart,
  List,
  Loader2,
  PieChart,
  RotateCcw,
  Search,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { formatNumber, formatQuota } from '@/lib/format'

import { getManagedUpstreamChannelStatistics } from '../api'
import {
  buildUpstreamChannelRankingData,
  buildUpstreamChannelStatisticsSearchParams,
  buildUpstreamChannelTrendData,
  formatUpstreamChannelStatisticsLastUsedAt,
  getNextUpstreamChannelStatisticsSortState,
  getUpstreamChannelStatisticsTodayRange,
  sortUpstreamChannelStatisticsRows,
  type UpstreamChannelStatisticsGranularity,
  type UpstreamChannelStatisticsMetric,
  type UpstreamChannelStatisticsSortKey,
  type UpstreamChannelStatisticsSortState,
  type UpstreamChannelStatisticsView,
} from '../statistics-lib'
import type {
  UpstreamChannelStatisticsItem,
  UpstreamChannelStatisticsSummary,
  UpstreamChannelStatisticsTrendItem,
} from '../types'
import { UpstreamChannelDistributionChart } from './upstream-channel-distribution-chart'
import { UpstreamChannelRankingChart } from './upstream-channel-ranking-chart'
import { UpstreamChannelTrendChart } from './upstream-channel-trend-chart'

const EMPTY_SUMMARY: UpstreamChannelStatisticsSummary = {
  quota: 0,
  token_used: 0,
  count: 0,
}

interface StableStatisticsData {
  rows: UpstreamChannelStatisticsItem[]
  summary: UpstreamChannelStatisticsSummary
  trend: UpstreamChannelStatisticsTrendItem[]
  startTimestamp: number
  endTimestamp: number
}

function toSeconds(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

function getErrorMessage(error: unknown): string {
  return error instanceof Error && error.message ? error.message : ''
}

function SortableHead({
  label,
  sortKey,
  sort,
  onSort,
  className,
}: {
  label: string
  sortKey: UpstreamChannelStatisticsSortKey
  sort: UpstreamChannelStatisticsSortState
  onSort: (key: UpstreamChannelStatisticsSortKey) => void
  className?: string
}) {
  const active = sort.key === sortKey
  let Icon = ArrowUpDown
  if (active) Icon = sort.direction === 'desc' ? ArrowDown : ArrowUp

  return (
    <TableHead className={className}>
      <Button
        type='button'
        variant='ghost'
        size='sm'
        className='ml-auto h-7 px-1.5'
        onClick={() => onSort(sortKey)}
      >
        {label}
        <Icon className='size-3.5' />
      </Button>
    </TableHead>
  )
}

export function UpstreamChannelStatistics() {
  const { t } = useTranslation()
  const defaultRange = useMemo(
    () => getUpstreamChannelStatisticsTodayRange(),
    []
  )
  const [startTime, setStartTime] = useState(defaultRange.start)
  const [endTime, setEndTime] = useState(defaultRange.end)
  const [appliedRange, setAppliedRange] = useState(defaultRange)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [viewMode, setViewMode] =
    useState<UpstreamChannelStatisticsView>('table')
  const [metric, setMetric] = useState<UpstreamChannelStatisticsMetric>('quota')
  const [granularity, setGranularity] =
    useState<UpstreamChannelStatisticsGranularity>('day')
  const [sort, setSort] = useState<UpstreamChannelStatisticsSortState>({
    key: 'quota',
    direction: 'desc',
  })
  const [stableData, setStableData] = useState<StableStatisticsData>({
    rows: [],
    summary: EMPTY_SUMMARY,
    trend: [],
    startTimestamp: toSeconds(defaultRange.start),
    endTimestamp: toSeconds(defaultRange.end),
  })

  const startTimestamp = toSeconds(appliedRange.start)
  const endTimestamp = toSeconds(appliedRange.end)
  const query = useQuery({
    queryKey: [
      'managed-upstream-channel-statistics',
      startTimestamp,
      endTimestamp,
      refreshTrigger,
    ],
    queryFn: () =>
      getManagedUpstreamChannelStatistics(
        buildUpstreamChannelStatisticsSearchParams({
          startTimestamp,
          endTimestamp,
        })
      ),
  })

  useEffect(() => {
    if (!query.data?.success) return
    setStableData({
      rows: query.data.data || [],
      summary: query.data.summary || EMPTY_SUMMARY,
      trend: query.data.trend || [],
      startTimestamp,
      endTimestamp,
    })
  }, [endTimestamp, query.data, startTimestamp])

  const sortedRows = useMemo(
    () => sortUpstreamChannelStatisticsRows(stableData.rows, sort),
    [sort, stableData.rows]
  )
  const rankingData = useMemo(
    () => buildUpstreamChannelRankingData(stableData.rows, metric),
    [metric, stableData.rows]
  )
  const trendData = useMemo(
    () =>
      buildUpstreamChannelTrendData(stableData.trend, metric, granularity, {
        startTimestamp: stableData.startTimestamp,
        endTimestamp: stableData.endTimestamp,
      }),
    [
      granularity,
      metric,
      stableData.endTimestamp,
      stableData.startTimestamp,
      stableData.trend,
    ]
  )

  let queryErrorMessage = ''
  if (query.isError) {
    queryErrorMessage =
      getErrorMessage(query.error) || t('Failed to load channel statistics')
  } else if (query.data && !query.data.success) {
    queryErrorMessage =
      query.data.message || t('Failed to load channel statistics')
  }

  const applyRange = () => {
    setAppliedRange({ start: startTime, end: endTime })
    setRefreshTrigger((current) => current + 1)
  }

  const resetRange = () => {
    const range = getUpstreamChannelStatisticsTodayRange()
    setStartTime(range.start)
    setEndTime(range.end)
    setAppliedRange(range)
    setRefreshTrigger((current) => current + 1)
  }

  const setSortKey = (key: UpstreamChannelStatisticsSortKey) => {
    setSort((current) =>
      getNextUpstreamChannelStatisticsSortState(current, key)
    )
  }

  const metricSelect = (
    <div className='flex items-center gap-2'>
      <span className='text-muted-foreground text-sm'>{t('Usage Metric')}</span>
      <Select
        value={metric}
        onValueChange={(value) =>
          setMetric(value as UpstreamChannelStatisticsMetric)
        }
      >
        <SelectTrigger className='w-40'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            <SelectItem value='quota'>{t('Cost Consumption')}</SelectItem>
            <SelectItem value='token_used'>{t('Token Count')}</SelectItem>
            <SelectItem value='count'>{t('Request Count')}</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
    </div>
  )

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <div className='min-w-[280px]'>
          <CompactDateTimeRangePicker
            start={startTime}
            end={endTime}
            onChange={({ start, end }) => {
              if (start) setStartTime(start)
              if (end) setEndTime(end)
            }}
          />
        </div>
        <Button type='button' disabled={query.isFetching} onClick={applyRange}>
          {query.isFetching ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <Search className='size-4' />
          )}
          {t('View')}
        </Button>
        <Button type='button' variant='outline' onClick={resetRange}>
          <RotateCcw className='size-4' />
          {t('Reset')}
        </Button>
      </div>

      {queryErrorMessage && (
        <div className='text-destructive text-sm'>{queryErrorMessage}</div>
      )}

      <div className='grid gap-3 md:grid-cols-3'>
        <Card size='sm'>
          <CardHeader>
            <CardTitle>{t('Cost')}</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold tabular-nums'>
            {formatQuota(stableData.summary.quota)}
          </CardContent>
        </Card>
        <Card size='sm'>
          <CardHeader>
            <CardTitle>{t('Tokens')}</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold tabular-nums'>
            {formatNumber(stableData.summary.token_used)}
          </CardContent>
        </Card>
        <Card size='sm'>
          <CardHeader>
            <CardTitle>{t('Requests')}</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold tabular-nums'>
            {formatNumber(stableData.summary.count)}
          </CardContent>
        </Card>
      </div>

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='bg-muted inline-flex rounded-md p-1'>
          <Button
            type='button'
            size='sm'
            variant={viewMode === 'table' ? 'secondary' : 'ghost'}
            onClick={() => setViewMode('table')}
          >
            <List className='size-4' />
            {t('Data List')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant={viewMode === 'bar' ? 'secondary' : 'ghost'}
            onClick={() => setViewMode('bar')}
          >
            <BarChart3 className='size-4' />
            {t('Bar Chart')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant={viewMode === 'pie' ? 'secondary' : 'ghost'}
            onClick={() => setViewMode('pie')}
          >
            <PieChart className='size-4' />
            {t('Pie Chart')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant={viewMode === 'trend' ? 'secondary' : 'ghost'}
            onClick={() => setViewMode('trend')}
          >
            <LineChart className='size-4' />
            {t('Trend Chart')}
          </Button>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          {(viewMode === 'bar' || viewMode === 'pie' || viewMode === 'trend') &&
            metricSelect}
          {viewMode === 'trend' && (
            <Select
              value={granularity}
              onValueChange={(value) =>
                setGranularity(value as UpstreamChannelStatisticsGranularity)
              }
            >
              <SelectTrigger className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='day'>{t('Daily')}</SelectItem>
                  <SelectItem value='week'>{t('Weekly')}</SelectItem>
                  <SelectItem value='month'>{t('Monthly')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          )}
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>
            {viewMode === 'table' && t('Data List')}
            {viewMode === 'bar' && t('Channel Ranking')}
            {viewMode === 'pie' && t('Pie Chart')}
            {viewMode === 'trend' && t('Channel Usage Trend')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {viewMode === 'bar' && (
            <UpstreamChannelRankingChart data={rankingData} metric={metric} />
          )}
          {viewMode === 'pie' && (
            <UpstreamChannelDistributionChart
              data={rankingData}
              metric={metric}
            />
          )}
          {viewMode === 'trend' && (
            <UpstreamChannelTrendChart data={trendData} metric={metric} />
          )}
          {viewMode === 'table' && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-16'>{t('No.')}</TableHead>
                  <TableHead>{t('Channel')}</TableHead>
                  <TableHead>{t('Provider')}</TableHead>
                  <TableHead>{t('Base URL')}</TableHead>
                  <SortableHead
                    label={t('Cost')}
                    sortKey='quota'
                    sort={sort}
                    onSort={setSortKey}
                    className='text-right'
                  />
                  <SortableHead
                    label={t('Tokens')}
                    sortKey='token_used'
                    sort={sort}
                    onSort={setSortKey}
                    className='text-right'
                  />
                  <SortableHead
                    label={t('Requests')}
                    sortKey='count'
                    sort={sort}
                    onSort={setSortKey}
                    className='text-right'
                  />
                  <SortableHead
                    label={t('Last Used At')}
                    sortKey='last_used_at'
                    sort={sort}
                    onSort={setSortKey}
                  />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRows.map((row, index) => (
                  <TableRow key={row.channel_id}>
                    <TableCell>{index + 1}</TableCell>
                    <TableCell className='font-medium'>
                      {row.channel_name || `#${row.channel_id}`}
                    </TableCell>
                    <TableCell>
                      <Badge variant='outline'>{row.provider}</Badge>
                    </TableCell>
                    <TableCell className='max-w-72 truncate font-mono text-xs'>
                      {row.base_url || '-'}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatQuota(row.quota)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatNumber(row.token_used)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatNumber(row.count)}
                    </TableCell>
                    <TableCell>
                      {formatUpstreamChannelStatisticsLastUsedAt(
                        row.last_used_at
                      )}
                    </TableCell>
                  </TableRow>
                ))}
                {!query.isLoading && sortedRows.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground h-24 text-center'
                    >
                      {t('No data')}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
