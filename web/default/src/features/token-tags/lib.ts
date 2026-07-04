import type {
  TokenTagOptionItem,
  TokenTagQuotaDataItem,
} from '../dashboard/types'
import dayjs from 'dayjs'

export type TokenTagSortKey = 'quota' | 'token_used' | 'count' | 'last_used_at'
export type SortDirection = 'asc' | 'desc'

export interface TokenTagSortState {
  key: TokenTagSortKey
  direction: SortDirection
}

export function groupTokenTagRows(
  rows: TokenTagQuotaDataItem[]
): TokenTagQuotaDataItem[] {
  const byTag = new Map<string, TokenTagQuotaDataItem>()
  for (const row of rows) {
    const key = `${row.username || ''}\u001f${row.tag_name}`
    const current = byTag.get(key)
    if (!current) {
      byTag.set(key, { ...row })
      continue
    }
    current.quota = (current.quota || 0) + (row.quota || 0)
    current.token_used = (current.token_used || 0) + (row.token_used || 0)
    current.count = (current.count || 0) + (row.count || 0)
    current.last_used_at = Math.max(current.last_used_at || 0, row.last_used_at || 0)
    current.token_name = ''
    current.token_id = 0
  }
  return sortTokenTagRows(Array.from(byTag.values()), {
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
  selectedTag?: string
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
  if (selectedTag) {
    names.add(selectedTag)
  }
  return Array.from(names).sort((a, b) => a.localeCompare(b))
}

export function formatTokenTagLastUsedAt(timestamp?: number): string {
  if (!timestamp || timestamp === -1 || timestamp === 0) {
    return '-'
  }
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm:ss')
}
