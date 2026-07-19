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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  BarChart3,
  List,
  Loader2,
  RotateCcw,
  Search,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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
import {
  getTokenTagOptions,
  getTokenTagQuotaDates,
} from '@/features/dashboard/api'
import type {
  TokenTagQuotaDataItem,
  TokenTagQuotaSummary,
} from '@/features/dashboard/types'
import { fetchApiKeyIPLocations } from '@/features/keys/api'
import { RecordedIPsCell } from '@/features/keys/components/api-keys-cells'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { useIsAdmin } from '@/hooks/use-admin'
import { getChartColor } from '@/lib/colors'
import { formatNumber, formatQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { RankingChart } from './components/ranking-chart'
import {
  TagMultiSelect,
  type TagMultiSelectOption,
} from './components/tag-multi-select'
import {
  NO_TAG_FILTER_VALUE,
  TOKEN_TAGS_CONTENT_CLASS,
  TOKEN_TAGS_FIXED_CONTENT,
  buildKeyRankingChartData,
  buildTagRankingChartData,
  buildTokenKeyRows,
  buildTokenTagOptionNames,
  buildTokenTagSearchParams,
  formatTokenTagLastUsedAt,
  getTodayRange,
  groupTokenTagRows,
  sortTokenTagRows,
  type TokenTagRankingMetric,
  type TokenTagSortKey,
  type TokenTagSortState,
} from './lib'

type ViewMode = 'table' | 'chart'

const TOKEN_IP_LOCATION_BATCH_SIZE = 50

const EMPTY_SUMMARY: TokenTagQuotaSummary = {
  quota: 0,
  token_used: 0,
  count: 0,
}

function toSeconds(date: Date) {
  return Math.floor(date.getTime() / 1000)
}

function getErrorMessage(error: unknown) {
  if (error instanceof Error && error.message) {
    return error.message
  }
  return ''
}

function getNextSortState(
  current: TokenTagSortState,
  key: TokenTagSortKey
): TokenTagSortState {
  if (current.key !== key) {
    return { key, direction: 'desc' }
  }
  return {
    key,
    direction: current.direction === 'desc' ? 'asc' : 'desc',
  }
}

function SortableHead({
  label,
  sortKey,
  sort,
  onSort,
  className,
}: {
  label: string
  sortKey: TokenTagSortKey
  sort: TokenTagSortState
  onSort: (key: TokenTagSortKey) => void
  className?: string
}) {
  const active = sort.key === sortKey
  let Icon = ArrowUpDown
  if (active) {
    Icon = sort.direction === 'desc' ? ArrowDown : ArrowUp
  }
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

export function TokenTagsDashboard() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const role = useAuthStore((state) => state.auth.user?.role)
  const isRoot = role === ROLE.SUPER_ADMIN
  const queryClient = useQueryClient()
  const defaultRange = useMemo(() => getTodayRange(), [])
  const [startTime, setStartTime] = useState(defaultRange.start)
  const [endTime, setEndTime] = useState(defaultRange.end)
  const [usernameDraft, setUsernameDraft] = useState('')
  const [includedTagsDraft, setIncludedTagsDraft] = useState<string[]>([])
  const [excludedTagsDraft, setExcludedTagsDraft] = useState<string[]>([])
  const [appliedFilters, setAppliedFilters] = useState({
    startTime: defaultRange.start,
    endTime: defaultRange.end,
    username: '',
    includedTags: [] as string[],
    excludedTags: [] as string[],
  })
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [stableRows, setStableRows] = useState<TokenTagQuotaDataItem[]>([])
  const [stableSummary, setStableSummary] =
    useState<TokenTagQuotaSummary>(EMPTY_SUMMARY)
  const [viewMode, setViewMode] = useState<ViewMode>('table')
  const [metric, setMetric] = useState<TokenTagRankingMetric>('quota')
  const [tagSort, setTagSort] = useState<TokenTagSortState>({
    key: 'quota',
    direction: 'desc',
  })
  const [keySort, setKeySort] = useState<TokenTagSortState>({
    key: 'quota',
    direction: 'desc',
  })
  const [loadingIP, setLoadingIP] = useState<string>()

  const query = useQuery({
    queryKey: [
      'token-tag-quota-data',
      appliedFilters.startTime.getTime(),
      appliedFilters.endTime.getTime(),
      appliedFilters.username,
      appliedFilters.includedTags,
      appliedFilters.excludedTags,
      isAdmin,
      isRoot,
      refreshTrigger,
    ],
    queryFn: () =>
      getTokenTagQuotaDates(
        buildTokenTagSearchParams({
          startTimestamp: toSeconds(appliedFilters.startTime),
          endTimestamp: toSeconds(appliedFilters.endTime),
          username:
            isAdmin && appliedFilters.username
              ? appliedFilters.username
              : undefined,
          includedTags: appliedFilters.includedTags,
          excludedTags: appliedFilters.excludedTags,
        }),
        isAdmin
      ),
  })

  const optionUsername = isAdmin ? usernameDraft.trim() : ''
  const tagOptionsQuery = useQuery({
    queryKey: ['token-tag-options', isAdmin, optionUsername],
    queryFn: () =>
      getTokenTagOptions(
        isAdmin && optionUsername ? { username: optionUsername } : {}
      ),
    staleTime: 60_000,
  })

  useEffect(() => {
    if (query.data?.success) {
      setStableRows(query.data.data || [])
      setStableSummary(query.data.summary || EMPTY_SUMMARY)
    }
  }, [query.data])

  const rows = stableRows
  const optionRows = useMemo(() => {
    if (!isAdmin || !optionUsername) {
      return rows
    }
    return rows.filter((row) => row.username === optionUsername)
  }, [isAdmin, optionUsername, rows])
  const tagOptions = useMemo<TagMultiSelectOption[]>(() => {
    const names = buildTokenTagOptionNames(
      tagOptionsQuery.data?.data,
      optionRows,
      [...includedTagsDraft, ...excludedTagsDraft].filter(
        (tag) => tag !== NO_TAG_FILTER_VALUE
      )
    )
    return [
      { value: NO_TAG_FILTER_VALUE, label: t('No tags') },
      ...names.map((name) => ({ value: name, label: name })),
    ]
  }, [
    excludedTagsDraft,
    includedTagsDraft,
    optionRows,
    tagOptionsQuery.data?.data,
    t,
  ])
  const groupedRows = useMemo(
    () => sortTokenTagRows(groupTokenTagRows(rows), tagSort),
    [rows, tagSort]
  )
  const keyRows = useMemo(() => buildTokenKeyRows(rows), [rows])
  const sortedKeyRows = useMemo(
    () => sortTokenTagRows(keyRows, keySort),
    [keyRows, keySort]
  )
  const pendingIPLocations = useMemo(() => {
    if (!isRoot) return []
    const items = new Map<string, { token_id: number; ip: string }>()
    for (const row of keyRows) {
      for (const item of row.ips || []) {
        if (item.private || item.country_code || item.region || item.city) {
          continue
        }
        const key = `${row.token_id}:${item.ip}`
        items.set(key, { token_id: row.token_id, ip: item.ip })
      }
    }
    return [...items.values()]
  }, [isRoot, keyRows])

  const fetchIPLocations = async (
    items: Array<{ token_id: number; ip: string }>,
    loadingKey: string
  ) => {
    setLoadingIP(loadingKey)
    try {
      let failureMessage = ''
      for (
        let start = 0;
        start < items.length;
        start += TOKEN_IP_LOCATION_BATCH_SIZE
      ) {
        const result = await fetchApiKeyIPLocations(
          items.slice(start, start + TOKEN_IP_LOCATION_BATCH_SIZE)
        )
        if (!result.success) {
          failureMessage = result.message || t('Failed to fetch IP location')
          break
        }
        const failure = (result.data || []).find((item) => !item.success)
        if (!failureMessage && failure) {
          failureMessage = failure.message || t('Failed to fetch IP location')
        }
      }
      if (failureMessage) {
        toast.error(failureMessage)
      } else {
        toast.success(t('Location fetched'))
      }
    } catch {
      toast.error(t('Failed to fetch IP location'))
    } finally {
      await queryClient.invalidateQueries({
        queryKey: ['token-tag-quota-data'],
      })
      setLoadingIP(undefined)
    }
  }

  const handleFetchIPLocation = (tokenId: number, ip: string) => {
    void fetchIPLocations([{ token_id: tokenId, ip }], `${tokenId}:${ip}`)
  }

  const handleFetchAllIPLocations = () => {
    if (pendingIPLocations.length === 0) return
    void fetchIPLocations(pendingIPLocations, 'batch')
  }

  const chartOptions = useMemo(
    () => ({
      isAdmin,
      noTagLabel: t('No tags'),
      unknownModelLabel: t('Unknown Model'),
    }),
    [isAdmin, t]
  )
  const tagChart = useMemo(
    () => buildTagRankingChartData(rows, metric, chartOptions),
    [chartOptions, metric, rows]
  )
  const keyChart = useMemo(
    () => buildKeyRankingChartData(rows, metric, chartOptions),
    [chartOptions, metric, rows]
  )
  const modelColorMap = useMemo(() => {
    const models = [...new Set([...tagChart.models, ...keyChart.models])].sort(
      (a, b) => a.localeCompare(b)
    )
    return new Map(models.map((model, index) => [model, getChartColor(index)]))
  }, [keyChart.models, tagChart.models])

  let queryErrorMessage = ''
  if (query.isError) {
    queryErrorMessage = getErrorMessage(query.error) || t('Failed to load data')
  } else if (query.data && !query.data.success) {
    queryErrorMessage = query.data.message || t('Failed to load data')
  }
  const isInitialLoading = query.isLoading && rows.length === 0
  let keyTableColumnCount = 7
  if (isAdmin) keyTableColumnCount += 1
  if (isRoot) keyTableColumnCount += 1

  const handleApply = () => {
    setAppliedFilters({
      startTime,
      endTime,
      username: isAdmin ? usernameDraft.trim() : '',
      includedTags: [...includedTagsDraft].sort((a, b) => a.localeCompare(b)),
      excludedTags: [...excludedTagsDraft].sort((a, b) => a.localeCompare(b)),
    })
    setRefreshTrigger((current) => current + 1)
  }

  const handleReset = () => {
    const range = getTodayRange()
    setStartTime(range.start)
    setEndTime(range.end)
    setUsernameDraft('')
    setIncludedTagsDraft([])
    setExcludedTagsDraft([])
    setAppliedFilters({
      startTime: range.start,
      endTime: range.end,
      username: '',
      includedTags: [],
      excludedTags: [],
    })
    setRefreshTrigger((current) => current + 1)
  }

  return (
    <SectionPageLayout fixedContent={TOKEN_TAGS_FIXED_CONTENT}>
      <SectionPageLayout.Title>
        {t('Key Tag Analytics')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className={TOKEN_TAGS_CONTENT_CLASS}>
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
            {isAdmin && (
              <Input
                className='w-48'
                value={usernameDraft}
                onChange={(event) => setUsernameDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') handleApply()
                }}
                placeholder={t('Username')}
              />
            )}
            <TagMultiSelect
              label={t('Include Tags')}
              emptyLabel={t('All Tags')}
              options={tagOptions}
              selected={includedTagsDraft}
              onChange={setIncludedTagsDraft}
            />
            <TagMultiSelect
              label={t('Exclude Tags')}
              emptyLabel={t('Do not exclude tags')}
              options={tagOptions}
              selected={excludedTagsDraft}
              onChange={setExcludedTagsDraft}
            />
            <Button
              type='button'
              disabled={query.isFetching}
              onClick={handleApply}
            >
              {query.isFetching ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <Search className='size-4' />
              )}
              {t('View')}
            </Button>
            <Button type='button' variant='outline' onClick={handleReset}>
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
              <CardContent className='text-2xl font-semibold'>
                {formatQuota(stableSummary.quota)}
              </CardContent>
            </Card>
            <Card size='sm'>
              <CardHeader>
                <CardTitle>{t('Tokens')}</CardTitle>
              </CardHeader>
              <CardContent className='text-2xl font-semibold'>
                {formatNumber(stableSummary.token_used)}
              </CardContent>
            </Card>
            <Card size='sm'>
              <CardHeader>
                <CardTitle>{t('Requests')}</CardTitle>
              </CardHeader>
              <CardContent className='text-2xl font-semibold'>
                {formatNumber(stableSummary.count)}
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
                {t('List')}
              </Button>
              <Button
                type='button'
                size='sm'
                variant={viewMode === 'chart' ? 'secondary' : 'ghost'}
                onClick={() => setViewMode('chart')}
              >
                <BarChart3 className='size-4' />
                {t('Bar Chart')}
              </Button>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              {isRoot && pendingIPLocations.length > 0 && (
                <>
                  <span className='text-muted-foreground text-xs'>
                    {t('{{count}} IP(s) waiting for location', {
                      count: pendingIPLocations.length,
                    })}
                  </span>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    disabled={loadingIP === 'batch'}
                    onClick={handleFetchAllIPLocations}
                  >
                    {loadingIP === 'batch' && (
                      <Loader2 className='animate-spin' />
                    )}
                    {t('Fetch locations')}
                  </Button>
                </>
              )}
              {viewMode === 'chart' && (
                <div className='flex items-center gap-2'>
                  <span className='text-muted-foreground text-sm'>
                    {t('Ranking Metric')}
                  </span>
                  <Select
                    value={metric}
                    onValueChange={(value) =>
                      setMetric(value as TokenTagRankingMetric)
                    }
                  >
                    <SelectTrigger className='w-40'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='quota'>
                          {t('Cost Consumption')}
                        </SelectItem>
                        <SelectItem value='token_used'>
                          {t('Token Count')}
                        </SelectItem>
                        <SelectItem value='count'>
                          {t('Request Count')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('Tag Ranking')}</CardTitle>
            </CardHeader>
            <CardContent>
              {viewMode === 'chart' ? (
                <RankingChart
                  data={tagChart}
                  metric={metric}
                  colorMap={modelColorMap}
                  kind='tag'
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-16'>{t('No.')}</TableHead>
                      {isAdmin && <TableHead>{t('User')}</TableHead>}
                      <TableHead>{t('Key Tag')}</TableHead>
                      <SortableHead
                        label={t('Cost')}
                        sortKey='quota'
                        sort={tagSort}
                        onSort={(key) =>
                          setTagSort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Tokens')}
                        sortKey='token_used'
                        sort={tagSort}
                        onSort={(key) =>
                          setTagSort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Requests')}
                        sortKey='count'
                        sort={tagSort}
                        onSort={(key) =>
                          setTagSort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Last Used At')}
                        sortKey='last_used_at'
                        sort={tagSort}
                        onSort={(key) =>
                          setTagSort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                      />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {groupedRows.map((row, index) => (
                      <TableRow
                        key={`${row.user_id || row.username || 'self'}-${row.tag_id}-${row.tag_name}`}
                      >
                        <TableCell>{index + 1}</TableCell>
                        {isAdmin && (
                          <TableCell>{row.username || '-'}</TableCell>
                        )}
                        <TableCell className='font-medium'>
                          {row.tag_name || t('No tags')}
                        </TableCell>
                        <TableCell className='text-right'>
                          {formatQuota(row.quota || 0)}
                        </TableCell>
                        <TableCell className='text-right'>
                          {formatNumber(row.token_used)}
                        </TableCell>
                        <TableCell className='text-right'>
                          {formatNumber(row.count)}
                        </TableCell>
                        <TableCell>
                          {formatTokenTagLastUsedAt(row.last_used_at)}
                        </TableCell>
                      </TableRow>
                    ))}
                    {!isInitialLoading && groupedRows.length === 0 && (
                      <TableRow>
                        <TableCell
                          colSpan={isAdmin ? 7 : 6}
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

          <Card>
            <CardHeader>
              <CardTitle>{t('Key Ranking')}</CardTitle>
            </CardHeader>
            <CardContent>
              {viewMode === 'chart' ? (
                <RankingChart
                  data={keyChart}
                  metric={metric}
                  colorMap={modelColorMap}
                  kind='key'
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-16'>{t('No.')}</TableHead>
                      {isAdmin && <TableHead>{t('User')}</TableHead>}
                      <TableHead>{t('Key Tag')}</TableHead>
                      <TableHead>{t('API Key')}</TableHead>
                      {isRoot && <TableHead>{t('IP')}</TableHead>}
                      <SortableHead
                        label={t('Cost')}
                        sortKey='quota'
                        sort={keySort}
                        onSort={(key) =>
                          setKeySort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Tokens')}
                        sortKey='token_used'
                        sort={keySort}
                        onSort={(key) =>
                          setKeySort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Requests')}
                        sortKey='count'
                        sort={keySort}
                        onSort={(key) =>
                          setKeySort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                        className='text-right'
                      />
                      <SortableHead
                        label={t('Last Used At')}
                        sortKey='last_used_at'
                        sort={keySort}
                        onSort={(key) =>
                          setKeySort((current) =>
                            getNextSortState(current, key)
                          )
                        }
                      />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sortedKeyRows.map((row, index) => (
                      <TableRow
                        key={`${row.user_id || row.username || 'self'}-${row.tag_id}-${row.token_id}`}
                      >
                        <TableCell>{index + 1}</TableCell>
                        {isAdmin && (
                          <TableCell>{row.username || '-'}</TableCell>
                        )}
                        <TableCell>{row.tag_name || t('No tags')}</TableCell>
                        <TableCell>
                          {row.token_name || `#${row.token_id}`}
                        </TableCell>
                        {isRoot && (
                          <TableCell>
                            <RecordedIPsCell
                              tokenId={row.token_id}
                              ips={row.ips}
                              onFetchLocation={handleFetchIPLocation}
                              loadingIP={loadingIP}
                            />
                          </TableCell>
                        )}
                        <TableCell className='text-right'>
                          {formatQuota(row.quota || 0)}
                        </TableCell>
                        <TableCell className='text-right'>
                          {formatNumber(row.token_used)}
                        </TableCell>
                        <TableCell className='text-right'>
                          {formatNumber(row.count)}
                        </TableCell>
                        <TableCell>
                          {formatTokenTagLastUsedAt(row.last_used_at)}
                        </TableCell>
                      </TableRow>
                    ))}
                    {!isInitialLoading && sortedKeyRows.length === 0 && (
                      <TableRow>
                        <TableCell
                          colSpan={keyTableColumnCount}
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
