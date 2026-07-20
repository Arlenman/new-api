import type { AgentConversation, StoredImage, StoredImageThumbnail, TaskRecord } from '../types'
import {
  deleteAgentConversation,
  deleteImage,
  deleteImageThumbnail,
  deleteTask,
  getAllAgentConversations,
  getAllImageIds,
  getAllStoredImageThumbnailIds,
  getAllTasks,
  getImage,
  getStoredImageThumbnail,
  putAgentConversation,
  putImage,
  putImageThumbnail,
  putTask,
} from './db'
import {
  getNewApiImagePlaygroundAssetCacheName,
  getNewApiImagePlaygroundMetadataKey,
  getNewApiImagePlaygroundStorageKey,
  getNewApiImagePlaygroundUserId,
  NEW_API_IMAGE_PLAYGROUND_STORAGE_CHANGED_EVENT,
  runWithoutNewApiImagePlaygroundSyncNotifications,
} from './newApiStorage'

const TOOL = 'image-playground'
const SCHEMA_VERSION = 1
const SYNC_DEBOUNCE_MS = 750
const SYNC_INTERVAL_MS = 30_000
const MAX_SYNC_PAGES = 10_000
const BOOTSTRAP_PAGE_SIZE = 100

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data: T
}

interface RemoteItem {
  id: string
  kind: string
  key: string
  schema_version: number
  revision: number
  status: string
  payload: unknown
  asset_ids: string[]
  created_at: number
  updated_at: number
  deleted: boolean
}

interface RemoteAsset {
  id: string
  sha256: string
  filename: string
  content_type: string
  size_bytes: number
  created_at: number
  updated_at: number
}

interface BootstrapBatch {
  items: RemoteItem[]
  assets: RemoteAsset[]
  cursor: number
  next_after_id: string
  has_more: boolean
}

interface ChangeBatch {
  items: RemoteItem[]
  assets: RemoteAsset[]
  next_cursor: number
  has_more: boolean
}

interface MutationResult {
  client_mutation_id: string
  kind: string
  key: string
  result: 'applied' | 'conflict' | 'error'
  message?: string
  item?: RemoteItem
}

interface SyncResponse {
  results: MutationResult[]
  cursor: number
}

interface SyncEntry {
  revision: number
  hash: string
  deleted: boolean
  asset_id?: string
  encoded_length?: number
  content_sha256?: string
  thumbnail_version?: number
}

interface SyncMetadata {
  cursor: number
  entries: Record<string, SyncEntry>
}

interface Mutation {
  client_mutation_id: string
  kind: string
  key: string
  schema_version: number
  base_revision: number
  status: string
  payload: unknown
  asset_ids: string[]
  created_at: number
  deleted: boolean
}

interface LocalItem {
  kind: string
  key: string
  status: string
  payload: unknown
  createdAt: number
  hash: string
  encodedLength?: number
  contentSha256?: string
  thumbnailVersion?: number
  asset?: {
    blob: Blob
    sha256: string
    filename: string
  }
}

export interface ImagePlaygroundSyncResult {
  stateChanged: boolean
  dataChanged: boolean
}

type RemoteAppliedHandler = (result: ImagePlaygroundSyncResult) => void | Promise<void>

let initializationPromise: Promise<ImagePlaygroundSyncResult> | null = null
let syncPromise: Promise<ImagePlaygroundSyncResult> | null = null
let syncTimer: ReturnType<typeof setTimeout> | null = null
let installed = false
let remoteAppliedHandler: RemoteAppliedHandler | null = null

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

const sensitiveSyncFieldNames = new Set([
  'apikey',
  'baseurl',
  'apiurl',
  'authorization',
  'accesstoken',
  'refreshtoken',
  'token',
  'idtoken',
  'authtoken',
  'agenttoken',
  'canvasagenttoken',
  'runtimecredential',
  'credential',
  'secret',
  'clientsecret',
  'password',
  'passphrase',
  'privatekey',
  'webdavurl',
  'webdavusername',
  'webdavpassword',
  'webdavtoken',
])

const credentialValuePatterns = [
  /\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+\/=-]{8,}/gi,
  /\butrs_[A-Za-z0-9._-]{8,}\b/g,
  /\bsk-[A-Za-z0-9_-]{8,}\b/g,
]

function normalizedFieldName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]/g, '')
}

function redactSensitiveDiagnosticText(value: string): string {
  let sanitized = value
  for (const pattern of credentialValuePatterns) {
    sanitized = sanitized.replace(pattern, '[REDACTED]')
  }
  return sanitized
    .replace(/((?:api[-_ ]?key|base[-_ ]?url|api[-_ ]?url|authorization|access[-_ ]?token|refresh[-_ ]?token|agent[-_ ]?token|canvas[-_ ]?agent[-_ ]?token|runtime[-_ ]?credential|credential|password|secret|webdav[-_ ]?(?:url|username|password|token)|private[-_ ]?key)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)/gi, '$1[REDACTED]')
    .replace(/([?&](?:api[-_]?key|base[-_]?url|api[-_]?url|access[-_]?token|refresh[-_]?token|auth(?:orization)?|token|agent[-_]?token|runtime[-_]?credential|credential|password|secret|private[-_]?key|webdav[-_]?(?:url|username|password|token))=)[^&#\s]*/gi, '$1[REDACTED]')
}

function decodeJsonContainer(value: string): { value: unknown; encoded: boolean } {
  const trimmed = value.trim()
  if (!(
    (trimmed.startsWith('{') && trimmed.endsWith('}')) ||
    (trimmed.startsWith('[') && trimmed.endsWith(']'))
  )) {
    return { value, encoded: false }
  }
  try {
    return { value: JSON.parse(value), encoded: true }
  } catch {
    return { value, encoded: false }
  }
}

function sanitizeStructuredSyncData(value: unknown, redactDiagnostics = false): unknown {
  if (typeof value === 'string') {
    const decoded = decodeJsonContainer(value)
    if (decoded.encoded) {
      return JSON.stringify(sanitizeStructuredSyncData(decoded.value, redactDiagnostics))
    }
    return redactSensitiveDiagnosticText(value)
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeStructuredSyncData(item, redactDiagnostics))
  }
  if (!isRecord(value)) return value

  const sanitized: Record<string, unknown> = {}
  for (const [key, fieldValue] of Object.entries(value)) {
    const normalizedKey = normalizedFieldName(key)
    if (sensitiveSyncFieldNames.has(normalizedKey)) continue
    if (redactDiagnostics && (normalizedKey === 'rawimageurls' || normalizedKey === 'rawresponsepayload')) continue
    sanitized[key] = sanitizeStructuredSyncData(fieldValue, redactDiagnostics)
  }
  return sanitized
}

function sanitizePersistedInputImages(value: unknown): unknown {
  if (!Array.isArray(value)) return value
  return value.map((image) => isRecord(image) && typeof image.id === 'string'
    ? { id: image.id, dataUrl: '' }
    : image)
}

function sanitizePersistedInputDraft(value: unknown): unknown {
  if (!isRecord(value)) return value
  return {
    ...value,
    inputImages: sanitizePersistedInputImages(value.inputImages),
    maskDraft: null,
  }
}

export function stableStringify(value: unknown): string {
  if (value === undefined || typeof value === 'function' || typeof value === 'symbol') return 'null'
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null'
  if (Array.isArray(value)) return `[${value.map((item) => stableStringify(item)).join(',')}]`
  const record = value as Record<string, unknown>
  return `{${Object.keys(record).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(record[key])}`).join(',')}}`
}

async function sha256Hex(value: Blob | string): Promise<string> {
  const bytes = typeof value === 'string'
    ? new TextEncoder().encode(value)
    : new Uint8Array(await value.arrayBuffer())
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function hashPayload(value: unknown): Promise<string> {
  return sha256Hex(stableStringify(value))
}

function entryKey(kind: string, key: string) {
  return `${kind}\u0000${key}`
}

function readMetadata(): SyncMetadata {
  const key = getNewApiImagePlaygroundMetadataKey()
  if (!key) return { cursor: 0, entries: {} }
  try {
    const value = JSON.parse(window.localStorage.getItem(key) ?? '') as Partial<SyncMetadata>
    return {
      cursor: typeof value.cursor === 'number' && value.cursor >= 0 ? value.cursor : 0,
      entries: isRecord(value.entries) ? value.entries as Record<string, SyncEntry> : {},
    }
  } catch {
    return { cursor: 0, entries: {} }
  }
}

function writeMetadata(metadata: SyncMetadata) {
  const key = getNewApiImagePlaygroundMetadataKey()
  if (!key) return
  window.localStorage.setItem(key, JSON.stringify(metadata))
}

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const userId = getNewApiImagePlaygroundUserId()
  if (!userId) throw new Error('New API user context is unavailable')
  const headers = new Headers(init?.headers)
  headers.set('New-Api-User', userId)
  if (init?.body && !(init.body instanceof Blob) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: 'same-origin',
  })
  if (!response.ok) throw new Error(`New API sync request failed (${response.status})`)
  const envelope = await response.json() as ApiEnvelope<T>
  if (!envelope.success) throw new Error(envelope.message || 'New API sync request failed')
  return envelope.data
}

function readSafePersistedState(): Record<string, unknown> {
  const key = getNewApiImagePlaygroundStorageKey()
  try {
    const wrapper = JSON.parse(window.localStorage.getItem(key) ?? '') as Record<string, unknown>
    const state = isRecord(wrapper.state) ? wrapper.state : {}
    const safeKeys = [
      'params',
      'prompt',
      'inputImages',
      'appMode',
      'galleryInputDraft',
      'activeAgentConversationId',
      'agentInputDrafts',
      'agentSidebarCollapsed',
      'agentAssetTab',
      'agentAssetPanelCollapsed',
      'favoriteCollections',
      'defaultFavoriteCollectionId',
      'supportPromptDismissed',
      'supportPromptOpen',
      'supportPromptSkippedForImportedData',
    ]
    const safeState: Record<string, unknown> = {}
    for (const safeKey of safeKeys) {
      if (safeKey in state) safeState[safeKey] = state[safeKey]
    }
    if ('inputImages' in safeState) {
      safeState.inputImages = sanitizePersistedInputImages(safeState.inputImages)
    }
    if ('galleryInputDraft' in safeState) {
      safeState.galleryInputDraft = sanitizePersistedInputDraft(safeState.galleryInputDraft)
    }
    if (isRecord(safeState.agentInputDrafts)) {
      safeState.agentInputDrafts = Object.fromEntries(
        Object.entries(safeState.agentInputDrafts).map(([conversationId, draft]) => [
          conversationId,
          sanitizePersistedInputDraft(draft),
        ]),
      )
    }
    return sanitizeStructuredSyncData(safeState) as Record<string, unknown>
  } catch {
    return {}
  }
}

function writeSafePersistedState(value: unknown): boolean {
  if (!isRecord(value)) return false
  const key = getNewApiImagePlaygroundStorageKey()
  let wrapper: Record<string, unknown> = { state: {}, version: 2 }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) ?? '') as Record<string, unknown>
    if (isRecord(parsed)) wrapper = parsed
  } catch {
    // Replace malformed local state while retaining the upstream persistence version.
  }
  const currentState = isRecord(wrapper.state) ? wrapper.state : {}
  const nextState = { ...currentState, ...value }
  const before = stableStringify(currentState)
  const after = stableStringify(nextState)
  if (before === after) return false
  window.localStorage.setItem(key, JSON.stringify({ ...wrapper, state: nextState }))
  return true
}

function dataUrlToBlob(dataUrl: string): Blob {
  const match = /^data:([^;,]+)?(;base64)?,(.*)$/s.exec(dataUrl)
  if (!match) throw new Error('Invalid image data URL')
  const contentType = match[1] || 'application/octet-stream'
  const encoded = match[3] || ''
  if (match[2]) {
    const binary = atob(encoded)
    const bytes = new Uint8Array(binary.length)
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
    return new Blob([bytes], { type: contentType })
  }
  return new Blob([decodeURIComponent(encoded)], { type: contentType })
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(blob)
  })
}

function extensionForContentType(contentType: string) {
  const subtype = contentType.split('/')[1]?.split(';')[0]?.toLowerCase() || 'bin'
  if (subtype === 'jpeg') return 'jpg'
  if (subtype === 'svg+xml') return 'svg'
  return subtype.replace(/[^a-z0-9]+/g, '') || 'bin'
}

export async function canReuseImageSyncEntry(
  kind: 'image' | 'thumbnail',
  dataUrl: string,
  metadataEntry: SyncEntry | undefined,
  thumbnailVersion?: number,
): Promise<boolean> {
  if (!metadataEntry || metadataEntry.deleted) return false
  if (metadataEntry.encoded_length !== dataUrl.length) return false
  if (kind === 'thumbnail' && metadataEntry.thumbnail_version !== thumbnailVersion) return false
  if (!metadataEntry.content_sha256) return false
  return (await sha256Hex(dataUrlToBlob(dataUrl))) === metadataEntry.content_sha256
}

async function createImageItem(kind: 'image' | 'thumbnail', key: string, record: StoredImage | StoredImageThumbnail): Promise<LocalItem> {
  const dataUrl = kind === 'image'
    ? (record as StoredImage).dataUrl
    : (record as StoredImageThumbnail).thumbnailDataUrl
  const blob = dataUrlToBlob(dataUrl)
  const contentSha256 = await sha256Hex(blob)
  const payload = kind === 'image'
    ? {
        id: key,
        createdAt: (record as StoredImage).createdAt,
        source: (record as StoredImage).source,
        width: record.width,
        height: record.height,
        content_sha256: contentSha256,
        content_type: blob.type || 'application/octet-stream',
        encoded_length: dataUrl.length,
      }
    : {
        id: key,
        width: record.width,
        height: record.height,
        thumbnailVersion: (record as StoredImageThumbnail).thumbnailVersion,
        content_sha256: contentSha256,
        content_type: blob.type || 'application/octet-stream',
        encoded_length: dataUrl.length,
      }
  return {
    kind,
    key,
    status: 'ready',
    payload,
    createdAt: kind === 'image' ? (record as StoredImage).createdAt ?? Date.now() : Date.now(),
    hash: await hashPayload(payload),
    encodedLength: dataUrl.length,
    contentSha256,
    thumbnailVersion: kind === 'thumbnail' ? (record as StoredImageThumbnail).thumbnailVersion : undefined,
    asset: {
      blob,
      sha256: contentSha256,
      filename: `${key}.${extensionForContentType(blob.type)}`,
    },
  }
}

async function collectLocalItems(metadata: SyncMetadata): Promise<Map<string, LocalItem>> {
  const items = new Map<string, LocalItem>()
  const [tasks, conversations, imageIds, thumbnailIds] = await Promise.all([
    getAllTasks(),
    getAllAgentConversations(),
    getAllImageIds(),
    getAllStoredImageThumbnailIds(),
  ])

  for (const task of tasks) {
    const payload = sanitizeStructuredSyncData(task, true)
    const item: LocalItem = {
      kind: 'task',
      key: task.id,
      status: task.status,
      payload,
      createdAt: task.createdAt,
      hash: await hashPayload(payload),
    }
    items.set(entryKey(item.kind, item.key), item)
  }
  for (const conversation of conversations) {
    const payload = sanitizeStructuredSyncData(conversation)
    const item: LocalItem = {
      kind: 'agent-conversation',
      key: conversation.id,
      status: 'active',
      payload,
      createdAt: conversation.createdAt,
      hash: await hashPayload(payload),
    }
    items.set(entryKey(item.kind, item.key), item)
  }

  for (const imageId of imageIds) {
    const metadataEntry = metadata.entries[entryKey('image', imageId)]
    const image = await getImage(imageId)
    if (image && await canReuseImageSyncEntry('image', image.dataUrl, metadataEntry)) {
      items.set(entryKey('image', imageId), {
        kind: 'image',
        key: imageId,
        status: 'ready',
        payload: null,
        createdAt: 0,
        hash: metadataEntry.hash,
      })
      continue
    }
    if (image) {
      const item = await createImageItem('image', imageId, image)
      items.set(entryKey(item.kind, item.key), item)
    }
  }

  for (const imageId of thumbnailIds) {
    const metadataEntry = metadata.entries[entryKey('thumbnail', imageId)]
    const thumbnail = await getStoredImageThumbnail(imageId)
    if (thumbnail && await canReuseImageSyncEntry('thumbnail', thumbnail.thumbnailDataUrl, metadataEntry, thumbnail.thumbnailVersion)) {
      items.set(entryKey('thumbnail', imageId), {
        kind: 'thumbnail',
        key: imageId,
        status: 'ready',
        payload: null,
        createdAt: 0,
        hash: metadataEntry.hash,
      })
      continue
    }
    if (thumbnail) {
      const item = await createImageItem('thumbnail', imageId, thumbnail)
      items.set(entryKey(item.kind, item.key), item)
    }
  }

  const state = readSafePersistedState()
  const stateItem: LocalItem = {
    kind: 'state',
    key: 'app',
    status: 'active',
    payload: state,
    createdAt: 0,
    hash: await hashPayload(state),
  }
  items.set(entryKey(stateItem.kind, stateItem.key), stateItem)
  return items
}

async function uploadAsset(asset: NonNullable<LocalItem['asset']>): Promise<RemoteAsset> {
  return apiRequest<RemoteAsset>('/api/user-tools/assets/uploads', {
    method: 'POST',
    headers: {
      'Content-Type': asset.blob.type || 'application/octet-stream',
      'X-File-Name': asset.filename,
      'X-Content-SHA256': asset.sha256,
    },
    body: asset.blob,
  })
}

async function buildMutations(localItems: Map<string, LocalItem>, metadata: SyncMetadata): Promise<Mutation[]> {
  const mutations: Mutation[] = []
  for (const [key, item] of localItems) {
    const previous = metadata.entries[key]
    if (previous && !previous.deleted && previous.hash === item.hash) continue

    let payload = item.payload
    let assetIds: string[] = []
    if (item.asset) {
      const asset = await uploadAsset(item.asset)
      payload = { ...(isRecord(payload) ? payload : {}), asset_id: asset.id }
      assetIds = [asset.id]
    }
    const baseRevision = previous?.revision ?? 0
    mutations.push({
      client_mutation_id: `ip_${await sha256Hex(stableStringify({
        kind: item.kind,
        key: item.key,
        baseRevision,
        hash: item.hash,
        deleted: false,
      }))}`,
      kind: item.kind,
      key: item.key,
      schema_version: SCHEMA_VERSION,
      base_revision: baseRevision,
      status: item.status,
      payload,
      asset_ids: assetIds,
      created_at: item.createdAt,
      deleted: false,
    })
  }

  for (const [key, previous] of Object.entries(metadata.entries)) {
    if (previous.deleted || localItems.has(key)) continue
    const separatorIndex = key.indexOf('\u0000')
    if (separatorIndex < 0) continue
    mutations.push({
      client_mutation_id: `ip_${await sha256Hex(stableStringify({
        kind: key.slice(0, separatorIndex),
        key: key.slice(separatorIndex + 1),
        baseRevision: previous.revision,
        hash: '',
        deleted: true,
      }))}`,
      kind: key.slice(0, separatorIndex),
      key: key.slice(separatorIndex + 1),
      schema_version: SCHEMA_VERSION,
      base_revision: previous.revision,
      status: 'deleted',
      payload: {},
      asset_ids: [],
      created_at: 0,
      deleted: true,
    })
  }
  return mutations
}

async function streamRemoteItems(
  cursor: number,
  applyBatch: (items: RemoteItem[]) => Promise<void>,
): Promise<number> {
  let nextCursor = cursor
  if (nextCursor === 0) {
    let afterId = ''
    let snapshotCursor: number | null = null
    const seenAfterIds = new Set<string>()
    let completed = false

    for (let page = 0; page < MAX_SYNC_PAGES; page += 1) {
      const query = new URLSearchParams({ limit: String(BOOTSTRAP_PAGE_SIZE) })
      if (afterId) query.set('after_id', afterId)
      if (snapshotCursor !== null) query.set('snapshot_cursor', String(snapshotCursor))
      const batch = await apiRequest<BootstrapBatch>(`/api/user-tools/${TOOL}/bootstrap?${query}`)
      if (snapshotCursor === null) snapshotCursor = batch.cursor
      else if (batch.cursor !== snapshotCursor) throw new Error('New API bootstrap snapshot changed during pagination')

      await applyBatch(batch.items)
      if (!batch.has_more) {
        nextCursor = snapshotCursor
        completed = true
        break
      }
      if (!batch.next_after_id || batch.next_after_id === afterId || seenAfterIds.has(batch.next_after_id)) {
        throw new Error('New API bootstrap pagination did not advance')
      }
      seenAfterIds.add(batch.next_after_id)
      afterId = batch.next_after_id
    }
    if (!completed) throw new Error('New API bootstrap has too many pages')
  }

  for (let page = 0; page < MAX_SYNC_PAGES; page += 1) {
    const batch = await apiRequest<ChangeBatch>(`/api/user-tools/${TOOL}/changes?cursor=${nextCursor}&limit=500`)
    await applyBatch(batch.items)
    if (!batch.has_more) return batch.next_cursor
    if (batch.next_cursor <= nextCursor) throw new Error('New API change pagination did not advance')
    nextCursor = batch.next_cursor
  }
  throw new Error('New API sync change backlog is too large')
}

function createAssetRequest(assetId: string): Request {
  const userId = getNewApiImagePlaygroundUserId()
  if (!userId) throw new Error('New API user context is unavailable')
  return new Request(`/api/user-tools/assets/${encodeURIComponent(assetId)}/content`, {
    headers: { 'New-Api-User': userId },
    credentials: 'same-origin',
  })
}

async function deleteCachedAsset(assetId: string | undefined): Promise<void> {
  const cacheName = getNewApiImagePlaygroundAssetCacheName()
  if (!assetId || !cacheName || !('caches' in window)) return
  const cache = await caches.open(cacheName)
  await cache.delete(createAssetRequest(assetId))
}

async function deleteCachedAssetIfUnreferenced(
  metadata: SyncMetadata,
  currentKey: string,
  assetId: string | undefined,
): Promise<void> {
  if (!assetId) return
  const hasOtherLiveReference = Object.entries(metadata.entries).some(([key, entry]) => (
    key !== currentKey && !entry.deleted && entry.asset_id === assetId
  ))
  if (!hasOtherLiveReference) await deleteCachedAsset(assetId)
}

async function downloadAsset(assetId: string): Promise<Blob> {
  const request = createAssetRequest(assetId)
  const cacheName = getNewApiImagePlaygroundAssetCacheName()
  const cache = cacheName && 'caches' in window ? await caches.open(cacheName) : null
  const cached = await cache?.match(request)
  if (cached) return cached.blob()

  const response = await fetch(request)
  if (!response.ok) throw new Error(`New API asset download failed (${response.status})`)
  if (cache) await cache.put(request, response.clone())
  return response.blob()
}

async function applyRemoteItem(
  item: RemoteItem,
  metadata: SyncMetadata,
  previousAssetId?: string,
): Promise<ImagePlaygroundSyncResult> {
  const result = { stateChanged: false, dataChanged: false }
  const currentKey = entryKey(item.kind, item.key)
  if (item.kind === 'state') {
    if (!item.deleted) result.stateChanged = writeSafePersistedState(item.payload)
    return result
  }

  result.dataChanged = true
  if (item.deleted) {
    if (item.kind === 'task') await deleteTask(item.key)
    else if (item.kind === 'agent-conversation') await deleteAgentConversation(item.key)
    else if (item.kind === 'image') await deleteImage(item.key)
    else if (item.kind === 'thumbnail') await deleteImageThumbnail(item.key)
    if (item.kind === 'image' || item.kind === 'thumbnail') {
      await deleteCachedAssetIfUnreferenced(metadata, currentKey, previousAssetId)
      for (const assetId of item.asset_ids) {
        await deleteCachedAssetIfUnreferenced(metadata, currentKey, assetId)
      }
    }
    return result
  }

  if (item.kind === 'task' && isRecord(item.payload)) {
    await putTask(item.payload as unknown as TaskRecord)
  } else if (item.kind === 'agent-conversation' && isRecord(item.payload)) {
    await putAgentConversation(item.payload as unknown as AgentConversation)
  } else if ((item.kind === 'image' || item.kind === 'thumbnail') && isRecord(item.payload)) {
    const assetId = typeof item.payload.asset_id === 'string' ? item.payload.asset_id : item.asset_ids[0]
    if (!assetId) throw new Error(`Missing asset for ${item.kind}/${item.key}`)
    if (previousAssetId && previousAssetId !== assetId) {
      await deleteCachedAssetIfUnreferenced(metadata, currentKey, previousAssetId)
    }
    const dataUrl = await blobToDataUrl(await downloadAsset(assetId))
    if (item.kind === 'image') {
      await putImage({
        id: item.key,
        dataUrl,
        createdAt: typeof item.payload.createdAt === 'number' ? item.payload.createdAt : undefined,
        source: item.payload.source === 'upload' || item.payload.source === 'generated' || item.payload.source === 'mask'
          ? item.payload.source
          : undefined,
        width: typeof item.payload.width === 'number' ? item.payload.width : undefined,
        height: typeof item.payload.height === 'number' ? item.payload.height : undefined,
      })
    } else {
      await putImageThumbnail({
        id: item.key,
        thumbnailDataUrl: dataUrl,
        width: typeof item.payload.width === 'number' ? item.payload.width : undefined,
        height: typeof item.payload.height === 'number' ? item.payload.height : undefined,
        thumbnailVersion: typeof item.payload.thumbnailVersion === 'number' ? item.payload.thumbnailVersion : undefined,
      })
    }
  }
  return result
}

async function remoteItemHash(item: RemoteItem): Promise<string> {
  if (item.deleted) return ''
  if ((item.kind === 'image' || item.kind === 'thumbnail') && isRecord(item.payload)) {
    const { asset_id: _assetId, ...payload } = item.payload
    return hashPayload(payload)
  }
  return hashPayload(item.payload)
}

async function applyRemoteItemAndUpdateMetadata(
  item: RemoteItem,
  metadata: SyncMetadata,
  applied: ImagePlaygroundSyncResult,
  force: boolean,
): Promise<void> {
  const key = entryKey(item.kind, item.key)
  const known = metadata.entries[key]
  if (!force && known && known.revision >= item.revision) return

  const itemResult = await applyRemoteItem(item, metadata, known?.asset_id)
  applied.stateChanged ||= itemResult.stateChanged
  applied.dataChanged ||= itemResult.dataChanged
  metadata.entries[key] = {
    revision: item.revision,
    hash: await remoteItemHash(item),
    deleted: item.deleted,
    asset_id: item.asset_ids[0],
    encoded_length: isRecord(item.payload) && typeof item.payload.encoded_length === 'number' ? item.payload.encoded_length : undefined,
    content_sha256: isRecord(item.payload) && typeof item.payload.content_sha256 === 'string' ? item.payload.content_sha256 : undefined,
    thumbnail_version: isRecord(item.payload) && typeof item.payload.thumbnailVersion === 'number' ? item.payload.thumbnailVersion : undefined,
  }
}

async function performSync(): Promise<ImagePlaygroundSyncResult> {
  const userId = getNewApiImagePlaygroundUserId()
  if (!userId) return { stateChanged: false, dataChanged: false }

  const metadata = readMetadata()
  const localItems = await collectLocalItems(metadata)
  const mutations = await buildMutations(localItems, metadata)
  const applied = { stateChanged: false, dataChanged: false }

  if (mutations.length > 0) {
    for (let offset = 0; offset < mutations.length; offset += 500) {
      const mutationBatch = mutations.slice(offset, offset + 500)
      const response = await apiRequest<SyncResponse>(`/api/user-tools/${TOOL}/sync`, {
        method: 'POST',
        body: JSON.stringify({ mutations: mutationBatch }),
      })
      for (const mutationResult of response.results) {
        if (mutationResult.result === 'error') {
          console.error(
            `New API image playground mutation failed (${mutationResult.kind}/${mutationResult.key}):`,
            mutationResult.message || 'unknown error',
          )
          continue
        }
        const localKey = entryKey(mutationResult.kind, mutationResult.key)
        const localItem = localItems.get(localKey)
        if (mutationResult.result === 'applied' && mutationResult.item && localItem) {
          metadata.entries[localKey] = {
            revision: mutationResult.item.revision,
            hash: localItem.hash,
            deleted: mutationResult.item.deleted,
            asset_id: mutationResult.item.asset_ids[0],
            encoded_length: localItem.encodedLength,
            content_sha256: localItem.contentSha256,
            thumbnail_version: localItem.thumbnailVersion,
          }
        } else if (mutationResult.result === 'conflict' && mutationResult.item) {
          await runWithoutNewApiImagePlaygroundSyncNotifications(async () => {
            await applyRemoteItemAndUpdateMetadata(mutationResult.item!, metadata, applied, true)
          })
        }
      }
      writeMetadata(metadata)
    }
  }

  await runWithoutNewApiImagePlaygroundSyncNotifications(async () => {
    metadata.cursor = await streamRemoteItems(metadata.cursor, async (items) => {
      for (const item of items) {
        await applyRemoteItemAndUpdateMetadata(item, metadata, applied, false)
      }
      writeMetadata(metadata)
    })
  })

  writeMetadata(metadata)
  return applied
}

async function runSync(notify = true): Promise<ImagePlaygroundSyncResult> {
  if (syncPromise) return syncPromise
  syncPromise = performSync()
    .then(async (result) => {
      if (notify && (result.stateChanged || result.dataChanged)) {
        await remoteAppliedHandler?.(result)
      }
      return result
    })
    .finally(() => {
      syncPromise = null
    })
  return syncPromise
}

function scheduleSync() {
  if (syncTimer) clearTimeout(syncTimer)
  syncTimer = setTimeout(() => {
    syncTimer = null
    void runSync().catch((error) => console.error('New API image playground sync failed:', error))
  }, SYNC_DEBOUNCE_MS)
}

function installSyncTriggers() {
  if (installed) return
  installed = true
  window.addEventListener(NEW_API_IMAGE_PLAYGROUND_STORAGE_CHANGED_EVENT, scheduleSync)
  window.addEventListener('online', scheduleSync)
  window.addEventListener('focus', scheduleSync)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') scheduleSync()
  })
  window.setInterval(() => {
    if (document.visibilityState === 'visible') scheduleSync()
  }, SYNC_INTERVAL_MS)
}

export function initializeNewApiImagePlaygroundSync(
  onRemoteApplied?: RemoteAppliedHandler,
): Promise<ImagePlaygroundSyncResult> {
  if (onRemoteApplied) remoteAppliedHandler = onRemoteApplied
  if (!getNewApiImagePlaygroundUserId()) {
    return Promise.resolve({ stateChanged: false, dataChanged: false })
  }
  installSyncTriggers()
  initializationPromise ??= runSync(false).catch((error) => {
    initializationPromise = null
    console.error('New API image playground initial sync failed:', error)
    return { stateChanged: false, dataChanged: false }
  })
  return initializationPromise
}
