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
export type ModuleAccess = { enabled: boolean; requireAuth: boolean }

export type HeaderNavModule = 'rankings' | 'pricing'

export type HeaderNavModules = {
  home: boolean
  console: boolean
  pricing: ModuleAccess
  rankings: ModuleAccess
  docs: boolean
  about: boolean
  [key: string]: boolean | ModuleAccess
}

export type CustomNavPlacement = 'top' | 'sidebar' | 'both'

export type CustomNavMenu = {
  id: string
  title: string
  url: string
  enabled: boolean
  placement: CustomNavPlacement
  openInNewTab: boolean
  requireAuth: boolean
}

const DEFAULT_HEADER_NAV_MODULES: HeaderNavModules = {
  home: true,
  console: true,
  pricing: { enabled: true, requireAuth: false },
  rankings: { enabled: true, requireAuth: false },
  docs: true,
  about: true,
}

const DEFAULTS: Record<HeaderNavModule, ModuleAccess> = {
  pricing: DEFAULT_HEADER_NAV_MODULES.pricing,
  rankings: DEFAULT_HEADER_NAV_MODULES.rankings,
}

function cloneHeaderNavDefaults(): HeaderNavModules {
  return {
    ...DEFAULT_HEADER_NAV_MODULES,
    pricing: { ...DEFAULT_HEADER_NAV_MODULES.pricing },
    rankings: { ...DEFAULT_HEADER_NAV_MODULES.rankings },
  }
}

export function parseHeaderNavBoolean(
  raw: unknown,
  fallback: boolean
): boolean {
  if (typeof raw === 'boolean') return raw
  if (typeof raw === 'number') {
    if (raw === 1) return true
    if (raw === 0) return false
    return fallback
  }
  if (typeof raw === 'string') {
    const normalized = raw.trim().toLowerCase()
    if (normalized === 'true' || normalized === '1') return true
    if (normalized === 'false' || normalized === '0') return false
  }
  return fallback
}

function parseAccess(raw: unknown, fallback: ModuleAccess): ModuleAccess {
  if (
    typeof raw === 'boolean' ||
    typeof raw === 'number' ||
    typeof raw === 'string'
  ) {
    return {
      enabled: parseHeaderNavBoolean(raw, fallback.enabled),
      requireAuth: fallback.requireAuth,
    }
  }
  if (raw && typeof raw === 'object') {
    const r = raw as Record<string, unknown>
    return {
      enabled: parseHeaderNavBoolean(r.enabled, fallback.enabled),
      requireAuth: parseHeaderNavBoolean(r.requireAuth, fallback.requireAuth),
    }
  }
  return { ...fallback }
}

function parseHeaderNavRecord(raw: unknown): Record<string, unknown> | null {
  if (!raw || String(raw).trim() === '') return null
  if (raw && typeof raw === 'object') return raw as Record<string, unknown>

  try {
    return JSON.parse(String(raw)) as Record<string, unknown>
  } catch {
    return null
  }
}

function parseCustomNavArray(raw: unknown): unknown[] {
  if (!raw || String(raw).trim() === '') return []
  if (Array.isArray(raw)) return raw

  try {
    const parsed = JSON.parse(String(raw)) as unknown
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function isSafeCustomNavURL(url: string): boolean {
  if (url.startsWith('/') && !url.startsWith('//')) return true

  try {
    const parsed = new URL(url)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function isCustomNavPlacement(value: unknown): value is CustomNavPlacement {
  return value === 'top' || value === 'sidebar' || value === 'both'
}

export function parseCustomNavMenus(raw: unknown): CustomNavMenu[] {
  const parsed = parseCustomNavArray(raw)
  const seenIds = new Set<string>()
  const result: CustomNavMenu[] = []

  parsed.forEach((item) => {
    if (!item || typeof item !== 'object') return

    const record = item as Record<string, unknown>
    const id = typeof record.id === 'string' ? record.id.trim() : ''
    const title = typeof record.title === 'string' ? record.title.trim() : ''
    const url = typeof record.url === 'string' ? record.url.trim() : ''
    const placement = record.placement

    if (!id || seenIds.has(id)) return
    if (!title || !url || !isSafeCustomNavURL(url)) return
    if (!isCustomNavPlacement(placement)) return

    seenIds.add(id)
    result.push({
      id,
      title,
      url,
      enabled: parseHeaderNavBoolean(record.enabled, true),
      placement,
      openInNewTab: parseHeaderNavBoolean(record.openInNewTab, false),
      requireAuth: parseHeaderNavBoolean(record.requireAuth, false),
    })
  })

  return result
}

export function getCustomNavMenusForPlacement(
  raw: unknown,
  placement: Exclude<CustomNavPlacement, 'both'>,
  isAuthenticated: boolean
): CustomNavMenu[] {
  return getEnabledCustomNavMenusForPlacement(raw, placement).filter((item) => {
    if (item.requireAuth && !isAuthenticated) return false
    return true
  })
}

export function getEnabledCustomNavMenusForPlacement(
  raw: unknown,
  placement: Exclude<CustomNavPlacement, 'both'>
): CustomNavMenu[] {
  return parseCustomNavMenus(raw).filter((item) => {
    if (!item.enabled) return false
    return item.placement === placement || item.placement === 'both'
  })
}

export function parseCustomNavMenusFromStatus(
  status: Record<string, unknown> | null
): CustomNavMenu[] {
  return parseCustomNavMenus(status?.CustomNavMenus)
}

export function parseHeaderNavModules(raw: unknown): HeaderNavModules {
  const result = cloneHeaderNavDefaults()
  const parsed = parseHeaderNavRecord(raw)
  if (!parsed) return result

  Object.entries(parsed).forEach(([key, value]) => {
    if (key === 'pricing') {
      result.pricing = parseAccess(value, result.pricing)
      return
    }
    if (key === 'rankings') {
      result.rankings = parseAccess(value, result.rankings)
      return
    }

    const fallback = result[key]
    if (
      typeof fallback === 'boolean' ||
      typeof value === 'boolean' ||
      typeof value === 'number' ||
      typeof value === 'string'
    ) {
      result[key] = parseHeaderNavBoolean(
        value,
        typeof fallback === 'boolean' ? fallback : true
      )
    }
  })

  return result
}

export function parseHeaderNavModulesFromStatus(
  status: Record<string, unknown> | null
): HeaderNavModules {
  return parseHeaderNavModules(status?.HeaderNavModules)
}

function getCachedStatus(): Record<string, unknown> | null {
  try {
    if (typeof window === 'undefined') return null
    const raw = window.localStorage.getItem('status')
    return raw ? (JSON.parse(raw) as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function cacheStatus(status: Record<string, unknown> | null): void {
  try {
    if (typeof window !== 'undefined' && status) {
      window.localStorage.setItem('status', JSON.stringify(status))
    }
  } catch {
    /* empty */
  }
}

export function getModuleAccessFromStatus(
  status: Record<string, unknown> | null,
  module: HeaderNavModule
): ModuleAccess {
  return parseHeaderNavModulesFromStatus(status)[module] ?? DEFAULTS[module]
}

export function getModuleAccess(module: HeaderNavModule): ModuleAccess {
  return getModuleAccessFromStatus(getCachedStatus(), module)
}

export async function getFreshModuleAccess(
  module: HeaderNavModule
): Promise<ModuleAccess> {
  try {
    const { getStatus } = await import('@/lib/api')
    const status = (await getStatus()) as Record<string, unknown> | null
    cacheStatus(status)
    return getModuleAccessFromStatus(status, module)
  } catch {
    return { enabled: false, requireAuth: true }
  }
}

export function isSidebarModuleEnabled(
  section: string,
  module: string
): boolean {
  const status = getCachedStatus()
  if (!status) return true

  const raw = status.SidebarModulesAdmin
  if (!raw || String(raw).trim() === '') return true

  try {
    const parsed = JSON.parse(String(raw)) as Record<
      string,
      Record<string, boolean>
    >
    const sectionConfig = parsed[section]
    if (!sectionConfig) return true
    if (sectionConfig.enabled === false) return false
    if (sectionConfig[module] === false) return false
    return true
  } catch {
    return true
  }
}
