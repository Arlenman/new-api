import localforage from "localforage";

const LEGACY_DATABASE_NAME = "infinite-canvas";
const LEGACY_PLUGIN_DATABASE_NAME = "infinite-canvas-plugins";
const MIGRATION_VERSION = 3;
const LEGACY_STORAGE_CLAIM_KEY = `new-api:infinite-canvas:legacy-storage-claim:v${MIGRATION_VERSION}`;
const MIGRATED_STORE_NAMES = [
    "app_state",
    "image_files",
    "media_files",
    "image_generation_logs",
    "video_generation_logs",
    "prompt_cache",
] as const;
const LEGACY_LOCAL_STORAGE_KEYS = [
    "infinite-canvas:ai_config_store",
    "infinite-canvas:prompt_source_store",
    "infinite-canvas:theme_store",
    "infinite-canvas:asset_store",
    "infinite-canvas:plugin_store",
    "infinite-canvas:canvas_store",
    "canvas-agent-panel-width",
    "canvas-agent-url",
    "canvas-agent-token",
    "canvas-side-panel-width",
    "canvas-side-panel-open",
    "canvas-image-quick-tools-v6",
] as const;

export const NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT = "new-api:infinite-canvas:storage-changed";
export const NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT = "new-api:infinite-canvas:remote-logs-changed";

let syncNotificationSuppression = 0;
let migrationPromise: Promise<void> | null = null;

function getLocalStorage(): Storage | null {
    try {
        return typeof window === "undefined" ? null : window.localStorage;
    } catch {
        return null;
    }
}

declare global {
    interface Window {
        __NEW_API_USER_ID__?: number | string;
    }
}

function normalizeInjectedUserId(value: unknown): string | null {
    if (typeof value === "string" && !/^[0-9]+$/.test(value)) return null;
    if (typeof value !== "string" && typeof value !== "number") return null;

    const numericValue = Number(value);
    if (!Number.isSafeInteger(numericValue) || numericValue <= 0) return null;
    return String(numericValue);
}

function readNamespace() {
    if (typeof window === "undefined") return "default";
    return normalizeInjectedUserId(window.__NEW_API_USER_ID__) ?? "default";
}

export const NEW_API_STORAGE_NAMESPACE = readNamespace();

export function getNewApiInfiniteCanvasUserId(): string | null {
    return NEW_API_STORAGE_NAMESPACE === "default" ? null : NEW_API_STORAGE_NAMESPACE;
}

function copyLegacyLocalStorageValue(storage: Storage, key: string, userId: string) {
    const namespacedKey = `${key}:${userId}`;
    if (storage.getItem(namespacedKey) != null) return;

    const legacyValue = storage.getItem(key);
    if (legacyValue != null) storage.setItem(namespacedKey, legacyValue);
}

export function namespacedStorageKey(key: string) {
    const userId = getNewApiInfiniteCanvasUserId();
    if (!userId) return key;

    const storage = getLocalStorage();
    if (storage?.getItem(LEGACY_STORAGE_CLAIM_KEY) === userId) {
        copyLegacyLocalStorageValue(storage, key, userId);
    }
    return `${key}:${userId}`;
}

export function namespacedLocalForageName(name: string) {
    const userId = getNewApiInfiniteCanvasUserId();
    return userId ? `${name}-${userId}` : name;
}

export function getNewApiInfiniteCanvasPluginDatabaseName() {
    return namespacedLocalForageName(LEGACY_PLUGIN_DATABASE_NAME);
}

export function getNewApiInfiniteCanvasMetadataKey(): string | null {
    const userId = getNewApiInfiniteCanvasUserId();
    return userId ? `new-api:infinite-canvas:sync:v1:${userId}` : null;
}

export function getNewApiInfiniteCanvasAssetCacheName(): string | null {
    const userId = getNewApiInfiniteCanvasUserId();
    return userId ? `new-api-infinite-canvas-assets-v1-user-${userId}` : null;
}

export function configureNewApiLocalForage() {
    localforage.config({
        name: namespacedLocalForageName(LEGACY_DATABASE_NAME),
        storeName: "app_state",
    });
}

async function copyMissingStoreRecords(
    storeName: string,
    sourceName: string,
    targetName: string,
    userId: string,
    namespaceAppStateKeys = false,
) {
    const source = localforage.createInstance({ name: sourceName, storeName });
    const target = localforage.createInstance({ name: targetName, storeName });
    const keys = await source.keys();
    for (const key of keys) {
        let targetKey = key;
        if (namespaceAppStateKeys) {
            if (/:\d+$/.test(key)) continue;
            targetKey = `${key}:${userId}`;
        }
        if (sourceName === targetName && targetKey === key) continue;
        if (await target.getItem(targetKey) != null) continue;

        const value = await source.getItem(key);
        if (value != null) await target.setItem(targetKey, value);
    }
}

async function databaseExists(databaseName: string): Promise<boolean> {
    if (typeof indexedDB === "undefined") return false;
    const factory = indexedDB as IDBFactory & { databases?: () => Promise<Array<{ name?: string }>> };
    if (!factory.databases) return true;
    const databases = await factory.databases();
    return databases.some((database) => database.name === databaseName);
}

export async function listNewApiInfiniteCanvasPluginStoreNames(databaseName = getNewApiInfiniteCanvasPluginDatabaseName()): Promise<string[]> {
    if (typeof indexedDB === "undefined" || !(await databaseExists(databaseName))) return [];

    return new Promise((resolve, reject) => {
        const request = indexedDB.open(databaseName);
        let createdEmptyDatabase = false;
        request.onupgradeneeded = () => {
            createdEmptyDatabase = true;
        };
        request.onsuccess = () => {
            const database = request.result;
            const storeNames = Array.from(database.objectStoreNames);
            database.close();
            if (!createdEmptyDatabase) {
                resolve(storeNames);
                return;
            }
            const deletion = indexedDB.deleteDatabase(databaseName);
            deletion.onsuccess = () => resolve([]);
            deletion.onerror = () => reject(deletion.error);
            deletion.onblocked = () => resolve([]);
        };
        request.onerror = () => reject(request.error);
    });
}

async function copyMissingPluginStoreRecords(userId: string) {
    const targetName = getNewApiInfiniteCanvasPluginDatabaseName();
    if (targetName === LEGACY_PLUGIN_DATABASE_NAME) return;
    const storeNames = await listNewApiInfiniteCanvasPluginStoreNames(LEGACY_PLUGIN_DATABASE_NAME);
    for (const storeName of storeNames) {
        await copyMissingStoreRecords(storeName, LEGACY_PLUGIN_DATABASE_NAME, targetName, userId);
    }
}

async function migrateClaimedLegacyStorage(userId: string, storage: Storage) {
    const completedKey = `new-api:infinite-canvas:legacy-storage-migration:v${MIGRATION_VERSION}:${userId}`;
    if (storage.getItem(completedKey) === "done") return;

    for (const key of LEGACY_LOCAL_STORAGE_KEYS) {
        copyLegacyLocalStorageValue(storage, key, userId);
    }

    const targetName = namespacedLocalForageName(LEGACY_DATABASE_NAME);
    await copyMissingStoreRecords("app_state", targetName, targetName, userId, true);
    for (const storeName of MIGRATED_STORE_NAMES) {
        await copyMissingStoreRecords(
            storeName,
            LEGACY_DATABASE_NAME,
            targetName,
            userId,
            storeName === "app_state",
        );
    }
    await copyMissingPluginStoreRecords(userId);
    storage.setItem(completedKey, "done");
}

async function claimAndMigrateLegacyStorage(userId: string, storage: Storage) {
    const claimedUserId = storage.getItem(LEGACY_STORAGE_CLAIM_KEY);
    if (claimedUserId && claimedUserId !== userId) return;
    if (!claimedUserId) storage.setItem(LEGACY_STORAGE_CLAIM_KEY, userId);
    if (storage.getItem(LEGACY_STORAGE_CLAIM_KEY) !== userId) return;

    await migrateClaimedLegacyStorage(userId, storage);
}

export function ensureLegacyInfiniteCanvasStorageMigration(): Promise<void> {
    const userId = getNewApiInfiniteCanvasUserId();
    const storage = getLocalStorage();
    if (!userId || !storage || typeof window === "undefined") return Promise.resolve();
    if (migrationPromise) return migrationPromise;

    migrationPromise = (async () => {
        if (typeof navigator !== "undefined" && navigator.locks) {
            await navigator.locks.request(LEGACY_STORAGE_CLAIM_KEY, async () => {
                await claimAndMigrateLegacyStorage(userId, storage);
            });
            return;
        }
        await claimAndMigrateLegacyStorage(userId, storage);
    })().catch((error) => {
        migrationPromise = null;
        throw error;
    });
    return migrationPromise;
}

export function notifyNewApiInfiniteCanvasStorageChanged() {
    if (syncNotificationSuppression > 0 || typeof window === "undefined") return;
    window.dispatchEvent(new Event(NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT));
}

export async function runWithoutNewApiInfiniteCanvasSyncNotifications<T>(fn: () => Promise<T>): Promise<T> {
    syncNotificationSuppression += 1;
    try {
        return await fn();
    } finally {
        syncNotificationSuppression -= 1;
    }
}
