const LEGACY_DATABASE_NAME = 'gpt-image-playground'
const STORAGE_NAMESPACE = 'new-api-user'
const MIGRATION_VERSION = 1
const LEGACY_MIGRATION_OWNER_KEY = `new-api:image-playground:legacy-migration-owner:v${MIGRATION_VERSION}`
const LEGACY_MIGRATION_MARKER_PATTERN = /^new-api:image-playground:(?:local-storage|indexed-db)-migration:v\d+:(\d+)(?::|$)/

export const NEW_API_IMAGE_PLAYGROUND_STORAGE_CHANGED_EVENT = 'new-api:image-playground:storage-changed'

let syncNotificationSuppression = 0
const indexedDBMigrationPromises = new Map<string, Promise<void>>()

function getLocalStorage(): Storage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

declare global {
  interface Window {
    __NEW_API_USER_ID__?: number | string
  }
}

function normalizeInjectedUserId(value: unknown): string | null {
  if (typeof value === 'string' && !/^\d+$/.test(value)) return null
  if (typeof value !== 'string' && typeof value !== 'number') return null

  const numericValue = Number(value)
  if (!Number.isSafeInteger(numericValue) || numericValue <= 0) return null
  return String(numericValue)
}

function isValidUserId(value: string | null | undefined): value is string {
  return Boolean(value && /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0)
}

const injectedUserId = typeof window === 'undefined'
  ? null
  : normalizeInjectedUserId(window.__NEW_API_USER_ID__)

function requireInjectedUserId(): string {
  if (!injectedUserId) throw new Error('New API authenticated user identity is required')
  return injectedUserId
}

function claimLegacyMigrationOwner(storage: Storage, userId: string): string | null {
  const savedOwner = storage.getItem(LEGACY_MIGRATION_OWNER_KEY)?.trim()
  if (isValidUserId(savedOwner)) return savedOwner

  let migratedOwner: string | null = null
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (!key || storage.getItem(key) !== 'done') continue
    const markerMatch = LEGACY_MIGRATION_MARKER_PATTERN.exec(key)
    if (markerMatch && isValidUserId(markerMatch[1])) {
      migratedOwner = markerMatch[1]
      break
    }
  }

  storage.setItem(LEGACY_MIGRATION_OWNER_KEY, migratedOwner ?? userId)
  const claimedOwner = storage.getItem(LEGACY_MIGRATION_OWNER_KEY)?.trim()
  return isValidUserId(claimedOwner) ? claimedOwner : null
}

export function getNewApiImagePlaygroundUserId(): string | null {
  return injectedUserId
}

export function getNewApiImagePlaygroundDatabaseName(): string {
  const userId = requireInjectedUserId()
  return `${LEGACY_DATABASE_NAME}:${STORAGE_NAMESPACE}:${userId}`
}

export function getNewApiImagePlaygroundStorageKey(baseKey = LEGACY_DATABASE_NAME): string {
  const userId = requireInjectedUserId()

  const storage = getLocalStorage()
  const namespacedKey = `${baseKey}:${STORAGE_NAMESPACE}:${userId}`
  const markerKey = `new-api:image-playground:local-storage-migration:v${MIGRATION_VERSION}:${userId}:${baseKey}`
  if (storage && storage.getItem(markerKey) !== 'done') {
    const migrationOwner = claimLegacyMigrationOwner(storage, userId)
    if (migrationOwner === userId && storage.getItem(namespacedKey) == null) {
      const legacyValue = storage.getItem(baseKey)
      if (legacyValue != null) storage.setItem(namespacedKey, legacyValue)
    }
    storage.setItem(markerKey, 'done')
  }
  return namespacedKey
}

export function getNewApiImagePlaygroundMetadataKey(): string | null {
  const userId = getNewApiImagePlaygroundUserId()
  return userId ? `new-api:image-playground:sync:v1:${userId}` : null
}

export function getNewApiImagePlaygroundAssetCacheName(): string | null {
  const userId = getNewApiImagePlaygroundUserId()
  return userId ? `new-api-image-playground-assets-v1-user-${userId}` : null
}

export function notifyNewApiImagePlaygroundStorageChanged() {
  if (syncNotificationSuppression > 0 || typeof window === 'undefined') return
  window.dispatchEvent(new Event(NEW_API_IMAGE_PLAYGROUND_STORAGE_CHANGED_EVENT))
}

export async function runWithoutNewApiImagePlaygroundSyncNotifications<T>(fn: () => Promise<T>): Promise<T> {
  syncNotificationSuppression += 1
  try {
    return await fn()
  } finally {
    syncNotificationSuppression -= 1
  }
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function transactionComplete(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error ?? new Error('IndexedDB transaction aborted'))
  })
}

function openDatabase(name: string, version: number, storeNames: string[]): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(name, version)
    request.onupgradeneeded = () => {
      const database = request.result
      for (const storeName of storeNames) {
        if (!database.objectStoreNames.contains(storeName)) {
          database.createObjectStore(storeName, { keyPath: 'id' })
        }
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

async function legacyDatabaseExists(): Promise<boolean> {
  const factory = indexedDB as IDBFactory & {
    databases?: () => Promise<Array<{ name?: string }>>
  }
  if (!factory.databases) return true
  const databases = await factory.databases()
  return databases.some((database) => database.name === LEGACY_DATABASE_NAME)
}

function openLegacyDatabase(): Promise<IDBDatabase | null> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(LEGACY_DATABASE_NAME)
    let createdEmptyDatabase = false
    request.onupgradeneeded = () => {
      createdEmptyDatabase = true
    }
    request.onsuccess = () => {
      if (!createdEmptyDatabase) {
        resolve(request.result)
        return
      }
      request.result.close()
      const deletion = indexedDB.deleteDatabase(LEGACY_DATABASE_NAME)
      deletion.onsuccess = () => resolve(null)
      deletion.onerror = () => reject(deletion.error)
      deletion.onblocked = () => resolve(null)
    }
    request.onerror = () => reject(request.error)
  })
}

async function copyMissingStoreRecords(source: IDBDatabase, target: IDBDatabase, storeName: string) {
  if (!source.objectStoreNames.contains(storeName) || !target.objectStoreNames.contains(storeName)) return

  const sourceTransaction = source.transaction(storeName, 'readonly')
  const sourceCompletion = transactionComplete(sourceTransaction)
  const sourceStore = sourceTransaction.objectStore(storeName)
  const records = await requestResult(sourceStore.getAll())
  await sourceCompletion
  if (records.length === 0) return

  const targetTransaction = target.transaction(storeName, 'readwrite')
  const targetCompletion = transactionComplete(targetTransaction)
  const targetStore = targetTransaction.objectStore(storeName)
  const existingKeys = new Set((await requestResult(targetStore.getAllKeys())).map(String))
  for (const record of records) {
    if (!record || typeof record !== 'object') continue
    const key = (record as { id?: IDBValidKey }).id
    if (key == null || existingKeys.has(String(key))) continue
    targetStore.put(record)
  }
  await targetCompletion
}

export function ensureLegacyImagePlaygroundIndexedDBMigration(
  databaseName: string,
  databaseVersion: number,
  storeNames: string[],
): Promise<void> {
  if (databaseName === LEGACY_DATABASE_NAME || typeof indexedDB === 'undefined') return Promise.resolve()

  const existingPromise = indexedDBMigrationPromises.get(databaseName)
  if (existingPromise) return existingPromise

  const migrationPromise = (async () => {
    const userId = getNewApiImagePlaygroundUserId()
    const storage = getLocalStorage()
    const markerKey = userId
      ? `new-api:image-playground:indexed-db-migration:v${MIGRATION_VERSION}:${userId}`
      : null
    if (markerKey && storage?.getItem(markerKey) === 'done') return
    if (!userId || !storage || claimLegacyMigrationOwner(storage, userId) !== userId) {
      if (markerKey) storage?.setItem(markerKey, 'done')
      return
    }

    const target = await openDatabase(databaseName, databaseVersion, storeNames)
    try {
      if (await legacyDatabaseExists()) {
        const source = await openLegacyDatabase()
        if (source) {
          try {
            for (const storeName of storeNames) {
              await copyMissingStoreRecords(source, target, storeName)
            }
          } finally {
            source.close()
          }
        }
      }
      if (markerKey) storage?.setItem(markerKey, 'done')
    } finally {
      target.close()
    }
  })().catch((error) => {
    indexedDBMigrationPromises.delete(databaseName)
    throw error
  })

  indexedDBMigrationPromises.set(databaseName, migrationPromise)
  return migrationPromise
}
