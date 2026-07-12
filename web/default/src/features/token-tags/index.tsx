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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getTokenTagOptions,
  getTokenTagQuotaDates,
} from '@/features/dashboard/api'
import type { TokenTagQuotaDataItem } from '@/features/dashboard/types'
import { formatNumber, formatQuota } from '@/lib/format'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SectionPageLayout } from '@/components/layout'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  TOKEN_TAGS_CONTENT_CLASS,
  TOKEN_TAGS_FIXED_CONTENT,
  buildTokenTagOptionNames,
  formatTokenTagLastUsedAt,
  groupTokenTagRows,
  sortTokenTagRows,
  type TokenTagSortKey,
  type TokenTagSortState,
} from './lib'
import { ArrowDown, ArrowUp, ArrowUpDown, RotateCcw, Search } from 'lucide-react'

const ALL_TAGS_VALUE = '__all__'

function getDefaultRange() {
  const end = new Date()
  const start = new Date(end.getTime() - 30 * 24 * 3600 * 1000)
  return { start, end }
}

function toSeconds(date: Date) {
  return Math.floor(date.getTime() / 1000)
}

function sumRows(
  rows: TokenTagQuotaDataItem[],
  key: keyof Pick<TokenTagQuotaDataItem, 'quota' | 'token_used' | 'count'>
) {
  return rows.reduce((total, row) => total + (row[key] || 0), 0)
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
  const Icon = active ? (sort.direction === 'desc' ? ArrowDown : ArrowUp) : ArrowUpDown
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
  const defaultRange = useMemo(() => getDefaultRange(), [])
  const [startTime, setStartTime] = useState(defaultRange.start)
  const [endTime, setEndTime] = useState(defaultRange.end)
  const [usernameDraft, setUsernameDraft] = useState('')
  const [tokenTagDraft, setTokenTagDraft] = useState('')
  const [appliedFilters, setAppliedFilters] = useState({
    startTime: defaultRange.start,
    endTime: defaultRange.end,
    username: '',
    tokenTag: '',
  })
  const [stableRows, setStableRows] = useState<TokenTagQuotaDataItem[]>([])
  const [tagSort, setTagSort] = useState<TokenTagSortState>({
    key: 'quota',
    direction: 'desc',
  })
  const [keySort, setKeySort] = useState<TokenTagSortState>({
    key: 'quota',
    direction: 'desc',
  })

  const query = useQuery({
    queryKey: [
      'token-tag-quota-data',
      appliedFilters.startTime.getTime(),
      appliedFilters.endTime.getTime(),
      appliedFilters.username,
      appliedFilters.tokenTag,
      isAdmin,
    ],
    queryFn: () =>
      getTokenTagQuotaDates(
        {
          start_timestamp: toSeconds(appliedFilters.startTime),
          end_timestamp: toSeconds(appliedFilters.endTime),
          ...(isAdmin && appliedFilters.username
            ? { username: appliedFilters.username }
            : {}),
          ...(appliedFilters.tokenTag
            ? { token_tag: appliedFilters.tokenTag }
            : {}),
        },
        isAdmin
      ),
  })

  const optionUsername = isAdmin ? usernameDraft.trim() : ''
  const tagOptionsQuery = useQuery({
    queryKey: ['token-tag-options', isAdmin, optionUsername],
    queryFn: () =>
      getTokenTagOptions(isAdmin && optionUsername ? { username: optionUsername } : {}),
    staleTime: 60_000,
  })

  useEffect(() => {
    if (query.data?.success) {
      setStableRows(query.data.data || [])
    }
  }, [query.data])

  const rows = stableRows
  const totalQuota = sumRows(rows, 'quota')
  const totalTokens = sumRows(rows, 'token_used')
  const totalRequests = sumRows(rows, 'count')

  const tagOptions = useMemo(() => {
    return buildTokenTagOptionNames(
      tagOptionsQuery.data?.data,
      rows,
      tokenTagDraft
    )
  }, [tagOptionsQuery.data?.data, rows, tokenTagDraft])

  const groupedRows = useMemo(() => {
    return sortTokenTagRows(groupTokenTagRows(rows), tagSort)
  }, [rows, tagSort])

  const sortedRows = useMemo(() => {
    return sortTokenTagRows(rows, keySort)
  }, [rows, keySort])

  const queryErrorMessage = query.isError
    ? getErrorMessage(query.error) || t('Failed to load data')
    : query.data && !query.data.success
      ? query.data.message || t('Failed to load data')
      : ''
  const isInitialLoading = query.isLoading && rows.length === 0

  const handleApply = () => {
    setAppliedFilters({
      startTime,
      endTime,
      username: isAdmin ? usernameDraft.trim() : '',
      tokenTag: tokenTagDraft,
    })
  }

  const handleReset = () => {
    const range = getDefaultRange()
    setStartTime(range.start)
    setEndTime(range.end)
    setUsernameDraft('')
    setTokenTagDraft('')
    setAppliedFilters({
      startTime: range.start,
      endTime: range.end,
      username: '',
      tokenTag: '',
    })
  }

  return (
    <SectionPageLayout fixedContent={TOKEN_TAGS_FIXED_CONTENT}>
      <SectionPageLayout.Title>{t('Key Tag Analytics')}</SectionPageLayout.Title>
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
              <>
                <Input
                  className='w-48'
                  value={usernameDraft}
                  onChange={(event) => setUsernameDraft(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') handleApply()
                  }}
                  placeholder={t('Username')}
                />
              </>
            )}
            <Select
              value={tokenTagDraft || ALL_TAGS_VALUE}
              onValueChange={(value) =>
                setTokenTagDraft(!value || value === ALL_TAGS_VALUE ? '' : value)
              }
            >
              <SelectTrigger className='w-48'>
                <SelectValue>{tokenTagDraft || t('All Tags')}</SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value={ALL_TAGS_VALUE}>{t('All Tags')}</SelectItem>
                  {tagOptions.map((tag) => (
                    <SelectItem key={tag} value={tag}>
                      {tag}
                    </SelectItem>
                  ))}
                  {tagOptions.length === 0 && (
                    <SelectItem value='__no_tags__' disabled>
                      {t('No tags')}
                    </SelectItem>
                  )}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button type='button' onClick={handleApply}>
              <Search className='size-4' />
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
                {formatQuota(totalQuota)}
              </CardContent>
            </Card>
            <Card size='sm'>
              <CardHeader>
                <CardTitle>{t('Tokens')}</CardTitle>
              </CardHeader>
              <CardContent className='text-2xl font-semibold'>
                {formatNumber(totalTokens)}
              </CardContent>
            </Card>
            <Card size='sm'>
              <CardHeader>
                <CardTitle>{t('Requests')}</CardTitle>
              </CardHeader>
              <CardContent className='text-2xl font-semibold'>
                {formatNumber(totalRequests)}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('Tag Ranking')}</CardTitle>
            </CardHeader>
            <CardContent>
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
                      onSort={(key) => setTagSort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Tokens')}
                      sortKey='token_used'
                      sort={tagSort}
                      onSort={(key) => setTagSort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Requests')}
                      sortKey='count'
                      sort={tagSort}
                      onSort={(key) => setTagSort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Last Used At')}
                      sortKey='last_used_at'
                      sort={tagSort}
                      onSort={(key) => setTagSort((current) => getNextSortState(current, key))}
                    />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {groupedRows.map((row, index) => (
                    <TableRow key={`${row.username || 'self'}-${row.tag_name}`}>
                      <TableCell>{index + 1}</TableCell>
                      {isAdmin && <TableCell>{row.username || '-'}</TableCell>}
                      <TableCell className='font-medium'>{row.tag_name || t('No tags')}</TableCell>
                      <TableCell className='text-right'>{formatQuota(row.quota || 0)}</TableCell>
                      <TableCell className='text-right'>{formatNumber(row.token_used)}</TableCell>
                      <TableCell className='text-right'>{formatNumber(row.count)}</TableCell>
                      <TableCell>{formatTokenTagLastUsedAt(row.last_used_at)}</TableCell>
                    </TableRow>
                  ))}
                  {!isInitialLoading && groupedRows.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={isAdmin ? 7 : 6} className='text-muted-foreground h-24 text-center'>
                        {t('No data')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Key Ranking')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-16'>{t('No.')}</TableHead>
                    {isAdmin && <TableHead>{t('User')}</TableHead>}
                    <TableHead>{t('Key Tag')}</TableHead>
                    <TableHead>{t('API Key')}</TableHead>
                    <SortableHead
                      label={t('Cost')}
                      sortKey='quota'
                      sort={keySort}
                      onSort={(key) => setKeySort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Tokens')}
                      sortKey='token_used'
                      sort={keySort}
                      onSort={(key) => setKeySort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Requests')}
                      sortKey='count'
                      sort={keySort}
                      onSort={(key) => setKeySort((current) => getNextSortState(current, key))}
                      className='text-right'
                    />
                    <SortableHead
                      label={t('Last Used At')}
                      sortKey='last_used_at'
                      sort={keySort}
                      onSort={(key) => setKeySort((current) => getNextSortState(current, key))}
                    />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sortedRows.map((row, index) => (
                    <TableRow key={`${row.tag_id}-${row.token_id}-${row.username || ''}`}>
                      <TableCell>{index + 1}</TableCell>
                      {isAdmin && <TableCell>{row.username || '-'}</TableCell>}
                      <TableCell>{row.tag_name || t('No tags')}</TableCell>
                      <TableCell>{row.token_name || `#${row.token_id}`}</TableCell>
                      <TableCell className='text-right'>{formatQuota(row.quota || 0)}</TableCell>
                      <TableCell className='text-right'>{formatNumber(row.token_used)}</TableCell>
                      <TableCell className='text-right'>{formatNumber(row.count)}</TableCell>
                      <TableCell>{formatTokenTagLastUsedAt(row.last_used_at)}</TableCell>
                    </TableRow>
                  ))}
                  {!isInitialLoading && sortedRows.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={isAdmin ? 8 : 7} className='text-muted-foreground h-24 text-center'>
                        {t('No data')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
