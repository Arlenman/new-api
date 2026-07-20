import localforage from "localforage";
import { nanoid } from "nanoid";

import {
  hydrateAssistantImages,
  hydrateCanvasImages,
} from "@/lib/canvas/canvas-generation-helpers";
import {
  ensureLegacyInfiniteCanvasStorageMigration,
  getNewApiInfiniteCanvasAssetCacheName,
  getNewApiInfiniteCanvasMetadataKey,
  getNewApiInfiniteCanvasPluginDatabaseName,
  getNewApiInfiniteCanvasUserId,
  listNewApiInfiniteCanvasPluginStoreNames,
  namespacedLocalForageName,
  namespacedStorageKey,
  NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT,
  NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT,
  runWithoutNewApiInfiniteCanvasSyncNotifications,
} from "@/lib/new-api-storage";
import {
  deleteStoredMedia,
  getMediaBlob,
  resolveMediaUrl,
  setMediaBlob,
} from "@/services/file-storage";
import {
  deleteStoredImages,
  getImageBlob,
  resolveImageUrl,
  setImageBlob,
} from "@/services/image-storage";
import {
  useCanvasStore,
  type CanvasProject,
} from "@/stores/canvas/use-canvas-store";
import { usePluginStore } from "@/stores/canvas/use-plugin-store";
import { useAgentStore } from "@/stores/use-agent-store";
import { useAssetStore, type Asset } from "@/stores/use-asset-store";
import {
  CANVAS_SIDE_PANEL_DEFAULT_WIDTH,
  CANVAS_SIDE_PANEL_MAX_WIDTH,
  CANVAS_SIDE_PANEL_MIN_WIDTH,
  useCanvasSidePanelStore,
} from "@/stores/use-canvas-side-panel-store";
import { useConfigStore } from "@/stores/use-config-store";
import { usePromptSourceStore } from "@/stores/use-prompt-source-store";
import { useThemeStore } from "@/stores/use-theme-store";

const TOOL = "infinite-canvas";
const SCHEMA_VERSION = 1;
const SYNC_DEBOUNCE_MS = 750;
const SYNC_INTERVAL_MS = 30_000;
const MAX_SYNC_PAGES = 10_000;
const BOOTSTRAP_PAGE_SIZE = 100;
const MAX_BATCH_SIZE = 500;
const IMAGE_LOG_STORE = "image_generation_logs";
const VIDEO_LOG_STORE = "video_generation_logs";
const PLUGIN_RECORD_KIND = "plugin-record";
const SETTING_KIND = "setting";
const APP_STATE_STORE = "app_state";
const MAX_ITEM_KEY_LENGTH = 255;
const LOCAL_STORAGE_SETTING_KEYS = [
  "infinite-canvas:theme_store",
  "infinite-canvas:ai_config_store",
  "infinite-canvas:prompt_source_store",
  "canvas-side-panel-width",
  "canvas-side-panel-open",
  "canvas-agent-panel-width",
  "canvas-image-quick-tools-v6",
] as const;
const LOCAL_FORAGE_SETTING_KEYS = ["infinite-canvas:plugin_store"] as const;
const SETTING_STORAGE = new Map<
  string,
  "local-storage" | "localforage-app-state"
>([
  ...LOCAL_STORAGE_SETTING_KEYS.map((key) => [key, "local-storage"] as const),
  ...LOCAL_FORAGE_SETTING_KEYS.map(
    (key) => [key, "localforage-app-state"] as const,
  ),
]);
const SENSITIVE_FIELD_NAMES = new Set([
  "apikey",
  "authorization",
  "runtimecredential",
  "token",
  "accesstoken",
  "refreshtoken",
  "idtoken",
  "authtoken",
  "agenttoken",
  "canvasagenttoken",
  "secret",
  "clientsecret",
  "password",
  "passphrase",
  "privatekey",
  "webdavpassword",
  "webdavtoken",
  "webdavusername",
  "webdavurl",
  "canvasagenturl",
]);

interface ApiEnvelope<T> {
  success: boolean;
  message?: string;
  data: T;
}

interface RemoteItem {
  id: string;
  kind: string;
  key: string;
  schema_version: number;
  revision: number;
  status: string;
  payload: unknown;
  asset_ids: string[];
  created_at: number;
  updated_at: number;
  deleted: boolean;
}

interface RemoteAsset {
  id: string;
  sha256: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: number;
  updated_at: number;
}

interface BootstrapBatch {
  items: RemoteItem[];
  assets: RemoteAsset[];
  cursor: number;
  next_after_id: string;
  has_more: boolean;
}

interface ChangeBatch {
  items: RemoteItem[];
  assets: RemoteAsset[];
  next_cursor: number;
  has_more: boolean;
}

interface MutationResult {
  client_mutation_id: string;
  kind: string;
  key: string;
  result: "applied" | "conflict" | "error";
  message?: string;
  item?: RemoteItem;
}

interface SyncResponse {
  results: MutationResult[];
  cursor: number;
}

interface SyncEntry {
  revision: number;
  hash: string;
  deleted: boolean;
  asset_id?: string;
  size_bytes?: number;
  content_type?: string;
  content_sha256?: string;
  plugin_id?: string;
  record_key?: string;
}

interface SyncMetadata {
  cursor: number;
  entries: Record<string, SyncEntry>;
}

interface Mutation {
  client_mutation_id: string;
  kind: string;
  key: string;
  schema_version: number;
  base_revision: number;
  status: string;
  payload: unknown;
  asset_ids: string[];
  created_at: number;
  deleted: boolean;
}

interface LocalItem {
  kind: string;
  key: string;
  status: string;
  payload: unknown;
  localValue?: unknown;
  createdAt: number;
  hash: string;
  asset?: {
    blob: Blob;
    sha256: string;
    filename: string;
  };
  pluginRecord?: {
    pluginId: string;
    recordKey: string;
  };
}

export interface InfiniteCanvasSyncResult {
  dataChanged: boolean;
  conflictCopies: number;
}

type RemoteAppliedHandler = (
  result: InfiniteCanvasSyncResult,
) => void | Promise<void>;

let initializationPromise: Promise<InfiniteCanvasSyncResult> | null = null;
let syncPromise: Promise<InfiniteCanvasSyncResult> | null = null;
let syncTimer: ReturnType<typeof setTimeout> | null = null;
let installed = false;
let remoteAppliedHandler: RemoteAppliedHandler | null = null;

type HydratableStore = {
  getState: () => { hydrated: boolean };
  persist: {
    hasHydrated: () => boolean;
    onFinishHydration: (callback: () => void) => () => void;
  };
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function waitForStoreHydration(
  store: HydratableStore,
  label: string,
): Promise<void> {
  if (store.persist.hasHydrated() || store.getState().hydrated)
    return Promise.resolve();
  return new Promise((resolve, reject) => {
    let settled = false;
    let timeout = 0;
    let unsubscribe = () => {};
    const finish = (error?: Error) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timeout);
      unsubscribe();
      if (error) reject(error);
      else resolve();
    };
    unsubscribe = store.persist.onFinishHydration(() => finish());
    timeout = window.setTimeout(
      () => finish(new Error(`${label} persistence hydration timed out`)),
      15_000,
    );
    if (store.persist.hasHydrated() || store.getState().hydrated) finish();
  });
}

function waitForPersistedStores() {
  return Promise.all([
    waitForStoreHydration(
      useCanvasStore as unknown as HydratableStore,
      "Canvas store",
    ),
    waitForStoreHydration(
      useAssetStore as unknown as HydratableStore,
      "Asset store",
    ),
  ]).then(() => undefined);
}

export function stableStringify(value: unknown): string {
  if (
    value === undefined ||
    typeof value === "function" ||
    typeof value === "symbol"
  )
    return "null";
  if (value === null || typeof value !== "object")
    return JSON.stringify(value) ?? "null";
  if (Array.isArray(value))
    return `[${value.map((item) => stableStringify(item)).join(",")}]`;
  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableStringify(record[key])}`)
    .join(",")}}`;
}

async function sha256Hex(value: Blob | string): Promise<string> {
  const bytes =
    typeof value === "string"
      ? new TextEncoder().encode(value)
      : new Uint8Array(await value.arrayBuffer());
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

async function hashPayload(value: unknown) {
  return sha256Hex(stableStringify(value));
}

function base64UrlUtf8(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

export async function pluginRecordItemKey(
  pluginId: string,
  recordKey: string,
): Promise<string> {
  const identity = stableStringify([pluginId, recordKey]);
  const encoded = `plugin:${base64UrlUtf8(identity)}`;
  if (encoded.length <= MAX_ITEM_KEY_LENGTH) return encoded;
  return `plugin-sha256:${await sha256Hex(identity)}`;
}

function entryKey(kind: string, key: string) {
  return `${kind}\u0000${key}`;
}

function readMetadata(): SyncMetadata {
  const key = getNewApiInfiniteCanvasMetadataKey();
  if (!key) return { cursor: 0, entries: {} };
  try {
    const value = JSON.parse(
      window.localStorage.getItem(key) ?? "",
    ) as Partial<SyncMetadata>;
    return {
      cursor:
        typeof value.cursor === "number" && value.cursor >= 0
          ? value.cursor
          : 0,
      entries: isRecord(value.entries)
        ? (value.entries as Record<string, SyncEntry>)
        : {},
    };
  } catch {
    return { cursor: 0, entries: {} };
  }
}

function writeMetadata(metadata: SyncMetadata) {
  const key = getNewApiInfiniteCanvasMetadataKey();
  if (key) window.localStorage.setItem(key, JSON.stringify(metadata));
}

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const userId = getNewApiInfiniteCanvasUserId();
  if (!userId) throw new Error("New API user context is unavailable");
  const headers = new Headers(init?.headers);
  headers.set("New-Api-User", userId);
  if (
    init?.body &&
    !(init.body instanceof Blob) &&
    !headers.has("Content-Type")
  )
    headers.set("Content-Type", "application/json");
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });
  if (!response.ok)
    throw new Error(`New API sync request failed (${response.status})`);
  const envelope = (await response.json()) as ApiEnvelope<T>;
  if (!envelope.success)
    throw new Error(envelope.message || "New API sync request failed");
  return envelope.data;
}

function normalizeFieldName(key: string) {
  return key.replace(/[^A-Za-z0-9]/g, "").toLowerCase();
}

export function isSensitiveSyncField(
  key: string,
  ancestors: string[] = [],
): boolean {
  const normalized = normalizeFieldName(key);
  if (SENSITIVE_FIELD_NAMES.has(normalized)) return true;
  if (
    normalized.endsWith("apikey") ||
    normalized.endsWith("authorization") ||
    normalized.endsWith("runtimecredential")
  )
    return true;
  if (
    normalized.endsWith("token") ||
    normalized.endsWith("secret") ||
    normalized.endsWith("secretkey")
  )
    return true;
  if (
    normalized.endsWith("password") ||
    normalized.endsWith("passphrase") ||
    normalized.endsWith("privatekey")
  )
    return true;
  if (
    normalized.endsWith("apiurl") ||
    normalized.endsWith("baseurl") ||
    normalized === "canvasagenturl"
  )
    return true;

  const inWebdavConfig = ancestors.some((ancestor) =>
    normalizeFieldName(ancestor).includes("webdav"),
  );
  return (
    inWebdavConfig &&
    [
      "url",
      "username",
      "user",
      "password",
      "token",
      "secret",
      "apikey",
      "authorization",
    ].includes(normalized)
  );
}

const CREDENTIAL_VALUE_PATTERNS = [
  /^\s*(?:Bearer|Basic)\s+\S+/i,
  /\butrs_[A-Za-z0-9._-]{8,}\b/,
  /\bsk-[A-Za-z0-9_-]{8,}\b/,
  /\b(?:gh[pousr]|github_pat)_[A-Za-z0-9_]{8,}\b/i,
  /\bAKIA[0-9A-Z]{16}\b/,
  /\bAIza[0-9A-Za-z_-]{20,}\b/,
  /\bxox[baprs]-[A-Za-z0-9-]{8,}\b/i,
  /[?&](?:api[_-]?key|access[_-]?token|auth(?:orization)?|token|key)=[^&#\s]+/i,
];

function decodeJsonContainer(value: unknown): { value: unknown; encoded: boolean } {
  if (typeof value !== "string") return { value, encoded: false };
  const trimmed = value.trim();
  if (
    !((trimmed.startsWith("{") && trimmed.endsWith("}")) ||
      (trimmed.startsWith("[") && trimmed.endsWith("]")))
  )
    return { value, encoded: false };
  try {
    return { value: JSON.parse(value), encoded: true };
  } catch {
    return { value, encoded: false };
  }
}

function containsCredentialValue(value: string): boolean {
  return CREDENTIAL_VALUE_PATTERNS.some((pattern) => pattern.test(value));
}

export function scrubForSync(
  value: unknown,
  key = "",
  ancestors: string[] = [],
): unknown {
  if (key && isSensitiveSyncField(key, ancestors)) return undefined;
  if (typeof value === "string") {
    const decoded = decodeJsonContainer(value);
    if (decoded.encoded)
      return JSON.stringify(scrubForSync(decoded.value, key, ancestors));
    if (containsCredentialValue(value)) return "";
    if (
      (key === "dataUrl" ||
        key === "url" ||
        key === "coverUrl" ||
        key === "content") &&
      /^(?:data:|blob:)/.test(value)
    )
      return "";
    return value;
  }
  if (Array.isArray(value))
    return value.map(
      (item) => scrubForSync(item, key, ancestors) ?? null,
    );
  if (!isRecord(value)) return value;

  const tag = Object.prototype.toString.call(value);
  if (tag === "[object Date]")
    return Date.prototype.toISOString.call(value);
  if (tag !== "[object Object]") return null;

  const clean: Record<string, unknown> = {};
  const childAncestors = key ? [...ancestors, key] : ancestors;
  for (const [childKey, childValue] of Object.entries(value)) {
    if (isSensitiveSyncField(childKey, childAncestors)) continue;
    const scrubbed = scrubForSync(childValue, childKey, childAncestors);
    if (scrubbed !== undefined) clean[childKey] = scrubbed;
  }
  return clean;
}

export function isSafePluginBlobContentType(contentType: string): boolean {
  return /^(?:image|video|audio)\//i.test(contentType);
}

function isSafePluginBlob(value: Blob): boolean {
  return isSafePluginBlobContentType(value.type);
}

function containsUnsupportedPluginValue(
  value: unknown,
  seen = new WeakSet<object>(),
): boolean {
  if (value === null || typeof value !== "object") return false;
  if (value instanceof Blob) return true;
  if (seen.has(value)) return true;
  seen.add(value);
  if (Array.isArray(value))
    return value.some((item) => containsUnsupportedPluginValue(item, seen));
  if (Object.prototype.toString.call(value) !== "[object Object]") return true;
  return Object.values(value).some((item) =>
    containsUnsupportedPluginValue(item, seen),
  );
}

export function canSyncPluginRecord(
  pluginId: string,
  recordKey: string,
  value: unknown,
): boolean {
  if (!pluginId || !recordKey) return false;
  if (isSensitiveSyncField(recordKey, [pluginId])) return false;
  if (value instanceof Blob) return isSafePluginBlob(value);
  if (containsUnsupportedPluginValue(value)) return false;
  const scrubbed = scrubForSync(value, recordKey, [pluginId]);
  return !(typeof value === "string" && value.length > 0 && scrubbed === "");
}

function arrayRecordIdentity(value: unknown): string | null {
  if (!isRecord(value)) return null;
  for (const key of ["id", "key"]) {
    const identity = value[key];
    if (typeof identity === "string" && identity) return `${key}:${identity}`;
  }
  return null;
}

export function mergeWithLocalSensitiveFields(
  remoteValue: unknown,
  localValue: unknown,
  ancestors: string[] = [],
): unknown {
  if (Array.isArray(remoteValue)) {
    if (!Array.isArray(localValue)) return remoteValue;
    const localByIdentity = new Map<string, unknown>();
    for (const item of localValue) {
      const identity = arrayRecordIdentity(item);
      if (identity) localByIdentity.set(identity, item);
    }
    return remoteValue.map((item, index) => {
      const identity = arrayRecordIdentity(item);
      const localItem = identity
        ? localByIdentity.get(identity)
        : localValue[index];
      return mergeWithLocalSensitiveFields(item, localItem, ancestors);
    });
  }
  if (!isRecord(remoteValue)) return remoteValue;

  const localRecord = isRecord(localValue) ? localValue : {};
  const merged: Record<string, unknown> = {};
  for (const [childKey, childValue] of Object.entries(remoteValue)) {
    if (isSensitiveSyncField(childKey, ancestors)) {
      if (Object.prototype.hasOwnProperty.call(localRecord, childKey))
        merged[childKey] = localRecord[childKey];
      continue;
    }
    merged[childKey] = mergeWithLocalSensitiveFields(
      childValue,
      localRecord[childKey],
      [...ancestors, childKey],
    );
  }
  for (const [childKey, childValue] of Object.entries(localRecord)) {
    if (
      !Object.prototype.hasOwnProperty.call(remoteValue, childKey) &&
      isSensitiveSyncField(childKey, ancestors)
    ) {
      merged[childKey] = childValue;
    }
  }
  return merged;
}

export function mergePluginRecordValue(
  pluginId: string,
  recordKey: string,
  remoteValue: unknown,
  localValue: unknown,
): unknown {
  const decodedRemote = decodeJsonContainer(remoteValue);
  const decodedLocal = decodeJsonContainer(localValue);
  const safeRemote = scrubForSync(decodedRemote.value, recordKey, [pluginId]);
  const merged = mergeWithLocalSensitiveFields(
    safeRemote,
    decodedLocal.value,
    [pluginId, recordKey],
  );
  return decodedRemote.encoded ? JSON.stringify(merged) : merged;
}

async function materializeEmbeddedMedia(
  value: unknown,
): Promise<{ value: unknown; changed: boolean }> {
  if (Array.isArray(value)) {
    let changed = false;
    const items = await Promise.all(
      value.map(async (item) => {
        const migrated = await materializeEmbeddedMedia(item);
        changed ||= migrated.changed;
        return migrated.value;
      }),
    );
    return { value: changed ? items : value, changed };
  }
  if (!isRecord(value)) return { value, changed: false };

  const result: Record<string, unknown> = { ...value };
  let changed = false;
  let storageKey =
    typeof result.storageKey === "string" ? result.storageKey : "";
  if (!storageKey) {
    const candidateKey = ["dataUrl", "content", "url"].find(
      (name) =>
        typeof result[name] === "string" &&
        /^(?:data:|blob:)/.test(String(result[name])),
    );
    if (candidateKey) {
      try {
        const blob = await (await fetch(String(result[candidateKey]))).blob();
        const image = blob.type.startsWith("image/");
        storageKey = `${image ? "image" : "file"}:${nanoid()}`;
        result.storageKey = storageKey;
        result[candidateKey] = image
          ? await setImageBlob(storageKey, blob)
          : await setMediaBlob(storageKey, blob);
        changed = true;
      } catch {
        // Keep unreadable legacy values locally; scrubForSync prevents large inline payloads from leaving the browser.
      }
    }
  }

  for (const [childKey, childValue] of Object.entries(result)) {
    if (childKey === "storageKey") continue;
    const migrated = await materializeEmbeddedMedia(childValue);
    if (migrated.changed) {
      result[childKey] = migrated.value;
      changed = true;
    }
  }
  return { value: changed ? result : value, changed };
}

async function readStoreEntries(
  storeName: string,
): Promise<Array<{ key: string; value: unknown }>> {
  const store = localforage.createInstance({
    name: namespacedLocalForageName("infinite-canvas"),
    storeName,
  });
  const entries: Array<{ key: string; value: unknown }> = [];
  await store.iterate((value, key) => {
    entries.push({ key, value });
  });
  return entries;
}

type StoredSetting = {
  encoding: "json" | "text" | "structured";
  value: unknown;
};

function decodeStoredSetting(value: unknown): StoredSetting {
  if (typeof value !== "string") return { encoding: "structured", value };
  try {
    return { encoding: "json", value: JSON.parse(value) };
  } catch {
    return { encoding: "text", value };
  }
}

function encodeStoredSetting(setting: StoredSetting): unknown {
  if (setting.encoding === "structured") return setting.value;
  if (setting.encoding === "text")
    return typeof setting.value === "string"
      ? setting.value
      : String(setting.value ?? "");
  return JSON.stringify(setting.value);
}

function appStateStore() {
  return localforage.createInstance({
    name: namespacedLocalForageName("infinite-canvas"),
    storeName: APP_STATE_STORE,
  });
}

async function readStoredSetting(key: string): Promise<unknown | null> {
  const storage = SETTING_STORAGE.get(key);
  if (storage === "local-storage")
    return window.localStorage.getItem(namespacedStorageKey(key));
  if (storage === "localforage-app-state")
    return appStateStore().getItem(namespacedStorageKey(key));
  return null;
}

async function writeStoredSetting(key: string, value: unknown) {
  const storage = SETTING_STORAGE.get(key);
  if (storage === "local-storage") {
    window.localStorage.setItem(namespacedStorageKey(key), String(value));
    return;
  }
  if (storage === "localforage-app-state")
    await appStateStore().setItem(namespacedStorageKey(key), value);
}

async function removeStoredSetting(key: string) {
  const storage = SETTING_STORAGE.get(key);
  if (storage === "local-storage") {
    window.localStorage.removeItem(namespacedStorageKey(key));
    return;
  }
  if (storage === "localforage-app-state")
    await appStateStore().removeItem(namespacedStorageKey(key));
}

async function makeSettingItem(
  key: string,
  storedValue: unknown,
): Promise<LocalItem> {
  const decoded = decodeStoredSetting(storedValue);
  const payload = {
    encoding: decoded.encoding,
    value: scrubForSync(decoded.value),
  };
  return {
    kind: SETTING_KIND,
    key,
    status: "ready",
    payload,
    localValue: decoded.value,
    createdAt: 0,
    hash: await hashPayload(payload),
  };
}

function timestamp(value: unknown) {
  if (typeof value === "number" && Number.isFinite(value) && value >= 0)
    return Math.floor(value);
  if (typeof value === "string") {
    const parsed = Date.parse(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function statusOf(value: unknown, fallback = "ready") {
  if (!isRecord(value)) return fallback;
  const valueStatus = value.status;
  return typeof valueStatus === "string" && valueStatus.length <= 32
    ? valueStatus
    : fallback;
}

async function makeJsonItem(
  kind: string,
  key: string,
  value: unknown,
  createdAt: number,
): Promise<LocalItem> {
  const payload = scrubForSync(value);
  return {
    kind,
    key,
    status: statusOf(value),
    payload,
    localValue: value,
    createdAt,
    hash: await hashPayload(payload),
  };
}

async function makePluginRecordItem(
  pluginId: string,
  recordKey: string,
  value: unknown,
): Promise<LocalItem | null> {
  if (!canSyncPluginRecord(pluginId, recordKey, value)) return null;
  const key = await pluginRecordItemKey(pluginId, recordKey);
  const pluginRecord = { pluginId, recordKey };
  if (value instanceof Blob) {
    const sha256 = await sha256Hex(value);
    const payload = {
      plugin_id: pluginId,
      record_key: recordKey,
      value_type: "blob",
      content_type: value.type || "application/octet-stream",
      size_bytes: value.size,
      content_sha256: sha256,
    };
    return {
      kind: PLUGIN_RECORD_KIND,
      key,
      status: "ready",
      payload,
      localValue: value,
      createdAt: 0,
      hash: await hashPayload(payload),
      asset: {
        blob: value,
        sha256,
        filename:
          `plugin_${pluginId}_${recordKey}`
            .replace(/[^A-Za-z0-9._-]+/g, "_")
            .slice(0, 220) || "plugin-record.bin",
      },
      pluginRecord,
    };
  }

  const payload = {
    plugin_id: pluginId,
    record_key: recordKey,
    value: scrubForSync(value, recordKey, [pluginId]),
  };
  return {
    kind: PLUGIN_RECORD_KIND,
    key,
    status: "ready",
    payload,
    localValue: value,
    createdAt: 0,
    hash: await hashPayload(payload),
    pluginRecord,
  };
}

export async function makeBlobItem(
  storageKey: string,
  blob: Blob,
): Promise<LocalItem> {
  const sha256 = await sha256Hex(blob);
  const payload = {
    storageKey,
    family: storageKey.startsWith("image:") ? "image" : "media",
    content_type: blob.type || "application/octet-stream",
    size_bytes: blob.size,
    content_sha256: sha256,
  };
  return {
    kind: "blob",
    key: storageKey,
    status: "ready",
    payload,
    createdAt: 0,
    hash: await hashPayload(payload),
    asset: {
      blob,
      sha256,
      filename: `${storageKey.replace(/[^A-Za-z0-9._-]+/g, "_")}.${blob.type.split("/")[1]?.replace(/[^A-Za-z0-9]+/g, "") || "bin"}`,
    },
  };
}

async function collectLocalItems(
  metadata: SyncMetadata,
): Promise<Map<string, LocalItem>> {
  await ensureLegacyInfiniteCanvasStorageMigration();
  const items = new Map<string, LocalItem>();

  const originalProjects = useCanvasStore.getState().projects;
  const migratedProjects: CanvasProject[] = [];
  let projectsChanged = false;
  for (const project of originalProjects) {
    const migrated = await materializeEmbeddedMedia(project);
    projectsChanged ||= migrated.changed;
    migratedProjects.push(migrated.value as CanvasProject);
  }
  if (projectsChanged)
    useCanvasStore.getState().replaceProjects(migratedProjects);

  const originalAssets = useAssetStore.getState().assets;
  const migratedAssets: Asset[] = [];
  let assetsChanged = false;
  for (const asset of originalAssets) {
    const migrated = await materializeEmbeddedMedia(asset);
    assetsChanged ||= migrated.changed;
    migratedAssets.push(migrated.value as Asset);
  }
  if (assetsChanged) useAssetStore.getState().replaceAssets(migratedAssets);

  for (const project of migratedProjects) {
    const item = await makeJsonItem(
      "canvas-project",
      project.id,
      project,
      timestamp(project.createdAt),
    );
    items.set(entryKey(item.kind, item.key), item);
  }
  for (const asset of migratedAssets) {
    const item = await makeJsonItem(
      "asset",
      asset.id,
      asset,
      timestamp(asset.createdAt),
    );
    items.set(entryKey(item.kind, item.key), item);
  }

  for (const { key, value } of await readStoreEntries(IMAGE_LOG_STORE)) {
    const item = await makeJsonItem(
      "image-generation-log",
      key,
      value,
      timestamp(isRecord(value) ? value.createdAt : 0),
    );
    items.set(entryKey(item.kind, item.key), item);
  }
  for (const { key, value } of await readStoreEntries(VIDEO_LOG_STORE)) {
    const item = await makeJsonItem(
      "video-generation-log",
      key,
      value,
      timestamp(isRecord(value) ? value.createdAt : 0),
    );
    items.set(entryKey(item.kind, item.key), item);
  }

  for (const key of SETTING_STORAGE.keys()) {
    const storedValue = await readStoredSetting(key);
    if (storedValue == null) continue;
    const item = await makeSettingItem(key, storedValue);
    items.set(entryKey(item.kind, item.key), item);
  }

  const pluginDatabaseName = getNewApiInfiniteCanvasPluginDatabaseName();
  for (const pluginId of await listNewApiInfiniteCanvasPluginStoreNames(
    pluginDatabaseName,
  )) {
    const store = localforage.createInstance({
      name: pluginDatabaseName,
      storeName: pluginId,
    });
    const records: Array<{ key: string; value: unknown }> = [];
    await store.iterate((value, key) => {
      records.push({ key, value });
    });
    for (const record of records) {
      const item = await makePluginRecordItem(
        pluginId,
        record.key,
        record.value,
      );
      if (item) items.set(entryKey(item.kind, item.key), item);
    }
  }

  for (const { key, value } of await readStoreEntries("image_files")) {
    if (!(value instanceof Blob)) continue;
    const item = await makeBlobItem(key, value);
    items.set(entryKey(item.kind, item.key), item);
  }
  for (const { key, value } of await readStoreEntries("media_files")) {
    if (!(value instanceof Blob)) continue;
    const item = await makeBlobItem(key, value);
    items.set(entryKey(item.kind, item.key), item);
  }
  return items;
}

async function uploadAsset(
  asset: NonNullable<LocalItem["asset"]>,
): Promise<RemoteAsset> {
  return apiRequest<RemoteAsset>("/api/user-tools/assets/uploads", {
    method: "POST",
    headers: {
      "Content-Type": asset.blob.type || "application/octet-stream",
      "X-File-Name": asset.filename,
      "X-Content-SHA256": asset.sha256,
    },
    body: asset.blob,
  });
}

async function buildMutations(
  localItems: Map<string, LocalItem>,
  metadata: SyncMetadata,
): Promise<Mutation[]> {
  const mutations: Mutation[] = [];
  for (const [metadataKey, item] of localItems) {
    const previous = metadata.entries[metadataKey];
    if (previous && !previous.deleted && previous.hash === item.hash) continue;

    let payload = item.payload;
    let assetIds: string[] = [];
    if (item.asset) {
      const asset = await uploadAsset(item.asset);
      payload = { ...(isRecord(payload) ? payload : {}), asset_id: asset.id };
      assetIds = [asset.id];
    }
    const baseRevision = previous?.revision ?? 0;
    mutations.push({
      client_mutation_id: `ic_${await sha256Hex(stableStringify({ kind: item.kind, key: item.key, baseRevision, hash: item.hash, deleted: false }))}`,
      kind: item.kind,
      key: item.key,
      schema_version: SCHEMA_VERSION,
      base_revision: baseRevision,
      status: item.status,
      payload,
      asset_ids: assetIds,
      created_at: item.createdAt,
      deleted: false,
    });
  }

  for (const [metadataKey, previous] of Object.entries(metadata.entries)) {
    if (previous.deleted || localItems.has(metadataKey)) continue;
    const separator = metadataKey.indexOf("\u0000");
    if (separator < 0) continue;
    const kind = metadataKey.slice(0, separator);
    const key = metadataKey.slice(separator + 1);
    const payload =
      kind === PLUGIN_RECORD_KIND && previous.plugin_id && previous.record_key
        ? { plugin_id: previous.plugin_id, record_key: previous.record_key }
        : {};
    mutations.push({
      client_mutation_id: `ic_${await sha256Hex(stableStringify({ kind, key, baseRevision: previous.revision, hash: "", deleted: true }))}`,
      kind,
      key,
      schema_version: SCHEMA_VERSION,
      base_revision: previous.revision,
      status: "deleted",
      payload,
      asset_ids: [],
      created_at: 0,
      deleted: true,
    });
  }
  return mutations;
}

async function streamRemoteItems(
  cursor: number,
  applyBatch: (items: RemoteItem[]) => Promise<void>,
): Promise<number> {
  let nextCursor = cursor;
  if (nextCursor === 0) {
    let afterId = "";
    let snapshotCursor: number | null = null;
    const seenAfterIds = new Set<string>();
    let completed = false;

    for (let page = 0; page < MAX_SYNC_PAGES; page += 1) {
      const query = new URLSearchParams({ limit: String(BOOTSTRAP_PAGE_SIZE) });
      if (afterId) query.set("after_id", afterId);
      if (snapshotCursor !== null)
        query.set("snapshot_cursor", String(snapshotCursor));
      const batch = await apiRequest<BootstrapBatch>(
        `/api/user-tools/${TOOL}/bootstrap?${query}`,
      );
      if (snapshotCursor === null) snapshotCursor = batch.cursor;
      else if (batch.cursor !== snapshotCursor)
        throw new Error("New API bootstrap snapshot changed during pagination");

      await applyBatch(batch.items);
      if (!batch.has_more) {
        nextCursor = snapshotCursor;
        completed = true;
        break;
      }
      if (
        !batch.next_after_id ||
        batch.next_after_id === afterId ||
        seenAfterIds.has(batch.next_after_id)
      ) {
        throw new Error("New API bootstrap pagination did not advance");
      }
      seenAfterIds.add(batch.next_after_id);
      afterId = batch.next_after_id;
    }
    if (!completed) throw new Error("New API bootstrap has too many pages");
  }

  for (let page = 0; page < MAX_SYNC_PAGES; page += 1) {
    const batch = await apiRequest<ChangeBatch>(
      `/api/user-tools/${TOOL}/changes?cursor=${nextCursor}&limit=${MAX_BATCH_SIZE}`,
    );
    await applyBatch(batch.items);
    if (!batch.has_more) return batch.next_cursor;
    if (batch.next_cursor <= nextCursor)
      throw new Error("New API change pagination did not advance");
    nextCursor = batch.next_cursor;
  }
  throw new Error("New API sync change backlog is too large");
}

async function downloadAsset(assetId: string): Promise<Blob> {
  const userId = getNewApiInfiniteCanvasUserId();
  if (!userId) throw new Error("New API user context is unavailable");
  const request = new Request(
    `/api/user-tools/assets/${encodeURIComponent(assetId)}/content`,
    {
      headers: { "New-Api-User": userId },
      credentials: "same-origin",
    },
  );
  const cacheName = getNewApiInfiniteCanvasAssetCacheName();
  const cache =
    cacheName && "caches" in window ? await caches.open(cacheName) : null;
  const cached = await cache?.match(request);
  if (cached) return cached.blob();

  const response = await fetch(request);
  if (!response.ok)
    throw new Error(`New API asset download failed (${response.status})`);
  if (cache) await cache.put(request, response.clone());
  return response.blob();
}

async function hydrateAsset(asset: Asset): Promise<Asset> {
  if (asset.kind === "image" && asset.data.storageKey) {
    const url = await resolveImageUrl(
      asset.data.storageKey,
      asset.data.dataUrl,
    );
    return {
      ...asset,
      coverUrl: asset.coverUrl || url,
      data: { ...asset.data, dataUrl: url },
    };
  }
  if (asset.kind === "video" && asset.data.storageKey) {
    return {
      ...asset,
      data: {
        ...asset.data,
        url: await resolveMediaUrl(asset.data.storageKey, asset.data.url),
      },
    };
  }
  return asset;
}

function pluginRecordIdentity(
  item: RemoteItem,
  known?: SyncEntry,
): { pluginId: string; recordKey: string } | null {
  const payload = isRecord(item.payload) ? item.payload : {};
  const pluginId =
    typeof payload.plugin_id === "string"
      ? payload.plugin_id
      : known?.plugin_id;
  const recordKey =
    typeof payload.record_key === "string"
      ? payload.record_key
      : known?.record_key;
  return pluginId && recordKey ? { pluginId, recordKey } : null;
}

function remoteStoredSetting(payload: unknown): StoredSetting | null {
  if (
    !isRecord(payload) ||
    !Object.prototype.hasOwnProperty.call(payload, "value")
  )
    return null;
  if (
    payload.encoding !== "json" &&
    payload.encoding !== "text" &&
    payload.encoding !== "structured"
  )
    return null;
  return { encoding: payload.encoding, value: payload.value };
}

async function applyRemoteItem(
  item: RemoteItem,
  known?: SyncEntry,
): Promise<boolean> {
  if (item.kind === "blob") {
    if (item.deleted) {
      if (item.key.startsWith("image:"))
        await deleteStoredImages([item.key]);
      else await deleteStoredMedia([item.key]);
      return true;
    }
    const assetId =
      isRecord(item.payload) && typeof item.payload.asset_id === "string"
        ? item.payload.asset_id
        : item.asset_ids[0];
    if (!assetId) throw new Error(`Missing asset for blob/${item.key}`);
    const blob = await downloadAsset(assetId);
    if (item.key.startsWith("image:")) await setImageBlob(item.key, blob);
    else await setMediaBlob(item.key, blob);
    return true;
  }

  if (item.kind === SETTING_KIND) {
    if (!SETTING_STORAGE.has(item.key)) return false;
    if (item.deleted) {
      await removeStoredSetting(item.key);
      return true;
    }

    const remoteSetting = remoteStoredSetting(item.payload);
    if (!remoteSetting)
      throw new Error(`Invalid setting payload for ${item.key}`);
    const currentStoredValue = await readStoredSetting(item.key);
    const currentValue =
      currentStoredValue == null
        ? undefined
        : decodeStoredSetting(currentStoredValue).value;
    const safeRemoteValue = scrubForSync(remoteSetting.value);
    const mergedValue = mergeWithLocalSensitiveFields(
      safeRemoteValue,
      currentValue,
    );
    await writeStoredSetting(
      item.key,
      encodeStoredSetting({ ...remoteSetting, value: mergedValue }),
    );
    return true;
  }

  if (item.kind === PLUGIN_RECORD_KIND) {
    const identity = pluginRecordIdentity(item, known);
    if (!identity)
      throw new Error(`Missing plugin identity for ${item.kind}/${item.key}`);
    if (isSensitiveSyncField(identity.recordKey, [identity.pluginId]))
      return false;
    const store = localforage.createInstance({
      name: getNewApiInfiniteCanvasPluginDatabaseName(),
      storeName: identity.pluginId,
    });
    if (item.deleted) {
      await store.removeItem(identity.recordKey);
      return true;
    }
    if (!isRecord(item.payload))
      throw new Error(`Invalid plugin record payload for ${item.key}`);
    if (item.payload.value_type === "blob") {
      const contentType =
        typeof item.payload.content_type === "string"
          ? item.payload.content_type
          : "";
      if (!isSafePluginBlobContentType(contentType)) return false;
      const assetId =
        typeof item.payload.asset_id === "string"
          ? item.payload.asset_id
          : item.asset_ids[0];
      if (!assetId)
        throw new Error(`Missing asset for ${item.kind}/${item.key}`);
      await store.setItem(identity.recordKey, await downloadAsset(assetId));
    } else {
      if (
        !canSyncPluginRecord(
          identity.pluginId,
          identity.recordKey,
          item.payload.value,
        )
      )
        return false;
      const localValue = await store.getItem(identity.recordKey);
      await store.setItem(
        identity.recordKey,
        mergePluginRecordValue(
          identity.pluginId,
          identity.recordKey,
          item.payload.value,
          localValue,
        ),
      );
    }
    return true;
  }

  if (item.kind === "canvas-project") {
    const projects = useCanvasStore.getState().projects;
    const next = projects.filter((project) => project.id !== item.key);
    if (!item.deleted && isRecord(item.payload)) {
      const project = item.payload as unknown as CanvasProject;
      next.unshift({
        ...project,
        nodes: await hydrateCanvasImages(project.nodes || []),
        chatSessions: await hydrateAssistantImages(project.chatSessions || []),
      });
    }
    useCanvasStore.getState().replaceProjects(next);
    return true;
  }

  if (item.kind === "asset") {
    const assets = useAssetStore.getState().assets;
    const next = assets.filter((asset) => asset.id !== item.key);
    if (!item.deleted && isRecord(item.payload))
      next.unshift(await hydrateAsset(item.payload as unknown as Asset));
    useAssetStore.getState().replaceAssets(next);
    return true;
  }

  if (
    item.kind === "image-generation-log" ||
    item.kind === "video-generation-log"
  ) {
    const storeName =
      item.kind === "image-generation-log" ? IMAGE_LOG_STORE : VIDEO_LOG_STORE;
    const store = localforage.createInstance({
      name: namespacedLocalForageName("infinite-canvas"),
      storeName,
    });
    if (item.deleted) await store.removeItem(item.key);
    else await store.setItem(item.key, item.payload);
    window.dispatchEvent(
      new Event(NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT),
    );
    return true;
  }
  return false;
}

type PersistStoreForRefresh = {
  getState: () => Record<string, unknown>;
  setState: (state: Record<string, unknown>) => void;
  persist: { rehydrate: () => Promise<void> | void };
};

async function rehydratePersistStorePreservingSensitiveFields(
  store: PersistStoreForRefresh,
) {
  const localState = store.getState();
  await Promise.resolve(store.persist.rehydrate());
  const restoredState = store.getState();
  store.setState(
    mergeWithLocalSensitiveFields(restoredState, localState) as Record<
      string,
      unknown
    >,
  );
}

function readLocalStorageSettingValue(key: string): unknown {
  const storedValue = window.localStorage.getItem(namespacedStorageKey(key));
  return storedValue == null
    ? undefined
    : decodeStoredSetting(storedValue).value;
}

async function refreshRestoredSettings(
  changedKeys: Set<string>,
  restoredKeys: Set<string>,
) {
  const persistStores = new Map<string, PersistStoreForRefresh>([
    [
      "infinite-canvas:theme_store",
      useThemeStore as unknown as PersistStoreForRefresh,
    ],
    [
      "infinite-canvas:ai_config_store",
      useConfigStore as unknown as PersistStoreForRefresh,
    ],
    [
      "infinite-canvas:prompt_source_store",
      usePromptSourceStore as unknown as PersistStoreForRefresh,
    ],
    [
      "infinite-canvas:plugin_store",
      usePluginStore as unknown as PersistStoreForRefresh,
    ],
  ]);
  await Promise.all(
    [...restoredKeys].map((key) => {
      const store = persistStores.get(key);
      return store
        ? rehydratePersistStorePreservingSensitiveFields(store)
        : Promise.resolve();
    }),
  );

  if (
    changedKeys.has("canvas-side-panel-width") ||
    changedKeys.has("canvas-side-panel-open")
  ) {
    const storedWidth = Number(
      readLocalStorageSettingValue("canvas-side-panel-width"),
    );
    const width =
      Number.isFinite(storedWidth) && storedWidth > 0
        ? Math.min(
            CANVAS_SIDE_PANEL_MAX_WIDTH,
            Math.max(CANVAS_SIDE_PANEL_MIN_WIDTH, storedWidth),
          )
        : CANVAS_SIDE_PANEL_DEFAULT_WIDTH;
    const storedOpen = readLocalStorageSettingValue("canvas-side-panel-open");
    const panelOpen =
      storedOpen === undefined ||
      (storedOpen !== 0 && storedOpen !== "0" && storedOpen !== false);
    useCanvasSidePanelStore.setState({
      width,
      panelOpen,
      panelMounted: panelOpen,
      panelClosing: false,
    });
  }

  if (changedKeys.has("canvas-agent-panel-width")) {
    const storedWidth = Number(
      readLocalStorageSettingValue("canvas-agent-panel-width"),
    );
    useAgentStore.setState({
      width:
        Number.isFinite(storedWidth) && storedWidth > 0 ? storedWidth : 440,
    });
  }

  for (const key of changedKeys) {
    if (SETTING_STORAGE.get(key) !== "local-storage") continue;
    try {
      const storageKey = namespacedStorageKey(key);
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: storageKey,
          newValue: window.localStorage.getItem(storageKey),
          storageArea: window.localStorage,
        }),
      );
    } catch {
      // Some embedded browser engines do not expose a constructible StorageEvent.
    }
  }
}

async function remoteItemHash(item: RemoteItem): Promise<string> {
  if (item.deleted) return "";
  if (item.kind === "blob" && isRecord(item.payload)) {
    const { asset_id: _assetId, ...payload } = item.payload;
    return hashPayload(payload);
  }
  return hashPayload(item.payload);
}

function createProjectConflictCopy(item: LocalItem) {
  if (item.kind !== "canvas-project" || !isRecord(item.localValue))
    return false;
  const project = item.localValue as unknown as CanvasProject;
  const now = new Date().toISOString();
  const copy: CanvasProject = {
    ...project,
    id: nanoid(),
    title: `${project.title || "未命名画布"}（冲突副本 ${new Date().toLocaleString()}）`,
    createdAt: now,
    updatedAt: now,
  };
  useCanvasStore
    .getState()
    .replaceProjects([
      copy,
      ...useCanvasStore
        .getState()
        .projects.filter((existing) => existing.id !== copy.id),
    ]);
  return true;
}

async function performSync(): Promise<InfiniteCanvasSyncResult> {
  if (!getNewApiInfiniteCanvasUserId())
    return { dataChanged: false, conflictCopies: 0 };
  await waitForPersistedStores();

  const metadata = readMetadata();
  const localItems = await collectLocalItems(metadata);
  const mutations = await buildMutations(localItems, metadata);
  let conflictCopies = 0;

  for (let offset = 0; offset < mutations.length; offset += MAX_BATCH_SIZE) {
    const response = await apiRequest<SyncResponse>(
      `/api/user-tools/${TOOL}/sync`,
      {
        method: "POST",
        body: JSON.stringify({
          mutations: mutations.slice(offset, offset + MAX_BATCH_SIZE),
        }),
      },
    );
    for (const mutationResult of response.results) {
      if (mutationResult.result === "error") {
        console.error(
          `New API Infinite Canvas mutation failed (${mutationResult.kind}/${mutationResult.key}):`,
          mutationResult.message || "unknown error",
        );
        continue;
      }
      const localKey = entryKey(mutationResult.kind, mutationResult.key);
      const localItem = localItems.get(localKey);
      if (
        mutationResult.result === "conflict" &&
        localItem &&
        createProjectConflictCopy(localItem)
      )
        conflictCopies += 1;
      if (
        mutationResult.result === "applied" &&
        mutationResult.item &&
        localItem
      ) {
        metadata.entries[localKey] = {
          revision: mutationResult.item.revision,
          hash: localItem.hash,
          deleted: mutationResult.item.deleted,
          asset_id: mutationResult.item.asset_ids[0],
          size_bytes: localItem.asset?.blob.size,
          content_type: localItem.asset?.blob.type,
          content_sha256: localItem.asset?.sha256,
          plugin_id: localItem.pluginRecord?.pluginId,
          record_key: localItem.pluginRecord?.recordKey,
        };
      }
    }
    writeMetadata(metadata);
  }

  let dataChanged = false;
  let blobChanged = false;
  const changedSettingKeys = new Set<string>();
  const restoredSettingKeys = new Set<string>();
  await runWithoutNewApiInfiniteCanvasSyncNotifications(async () => {
    metadata.cursor = await streamRemoteItems(
      metadata.cursor,
      async (items) => {
        const orderedItems = [...items].sort(
          (left, right) =>
            Number(right.kind === "blob") - Number(left.kind === "blob"),
        );
        for (const item of orderedItems) {
          const key = entryKey(item.kind, item.key);
          const known = metadata.entries[key];
          if (known && known.revision >= item.revision) continue;
          const itemChanged = await applyRemoteItem(item, known);
          dataChanged ||= itemChanged;
          blobChanged ||= item.kind === "blob";
          if (itemChanged && item.kind === SETTING_KIND) {
            changedSettingKeys.add(item.key);
            if (!item.deleted) restoredSettingKeys.add(item.key);
          }
          const identity =
            item.kind === PLUGIN_RECORD_KIND
              ? pluginRecordIdentity(item, known)
              : null;
          metadata.entries[key] = {
            revision: item.revision,
            hash: await remoteItemHash(item),
            deleted: item.deleted,
            asset_id: item.asset_ids[0],
            size_bytes:
              isRecord(item.payload) &&
              typeof item.payload.size_bytes === "number"
                ? item.payload.size_bytes
                : undefined,
            content_type:
              isRecord(item.payload) &&
              typeof item.payload.content_type === "string"
                ? item.payload.content_type
                : undefined,
            content_sha256:
              isRecord(item.payload) &&
              typeof item.payload.content_sha256 === "string"
                ? item.payload.content_sha256
                : undefined,
            plugin_id: identity?.pluginId,
            record_key: identity?.recordKey,
          };
        }
        writeMetadata(metadata);
      },
    );

    if (blobChanged) {
      const projects = await Promise.all(
        useCanvasStore.getState().projects.map(async (project) => ({
          ...project,
          nodes: await hydrateCanvasImages(project.nodes || []),
          chatSessions: await hydrateAssistantImages(
            project.chatSessions || [],
          ),
        })),
      );
      useCanvasStore.getState().replaceProjects(projects);
      useAssetStore
        .getState()
        .replaceAssets(
          await Promise.all(useAssetStore.getState().assets.map(hydrateAsset)),
        );
    }
    if (changedSettingKeys.size > 0)
      await refreshRestoredSettings(changedSettingKeys, restoredSettingKeys);
  });

  writeMetadata(metadata);
  return { dataChanged, conflictCopies };
}

async function runSync(notify = true): Promise<InfiniteCanvasSyncResult> {
  if (syncPromise) return syncPromise;
  syncPromise = performSync()
    .then(async (result) => {
      if (notify && (result.dataChanged || result.conflictCopies > 0))
        await remoteAppliedHandler?.(result);
      return result;
    })
    .finally(() => {
      syncPromise = null;
    });
  return syncPromise;
}

function scheduleSync() {
  if (syncTimer) clearTimeout(syncTimer);
  syncTimer = setTimeout(() => {
    syncTimer = null;
    void runSync().catch((error) =>
      console.error("New API Infinite Canvas sync failed:", error),
    );
  }, SYNC_DEBOUNCE_MS);
}

function installSyncTriggers() {
  if (installed) return;
  installed = true;
  window.addEventListener(
    NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT,
    scheduleSync,
  );
  window.addEventListener("online", scheduleSync);
  window.addEventListener("focus", scheduleSync);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") scheduleSync();
  });
  window.setInterval(() => {
    if (document.visibilityState === "visible") scheduleSync();
  }, SYNC_INTERVAL_MS);
}

export function initializeNewApiInfiniteCanvasSync(
  onRemoteApplied?: RemoteAppliedHandler,
): Promise<InfiniteCanvasSyncResult> {
  if (onRemoteApplied) remoteAppliedHandler = onRemoteApplied;
  if (!getNewApiInfiniteCanvasUserId())
    return Promise.resolve({ dataChanged: false, conflictCopies: 0 });
  installSyncTriggers();
  initializationPromise ??= ensureLegacyInfiniteCanvasStorageMigration()
    .then(waitForPersistedStores)
    .then(() => runSync(false))
    .catch((error) => {
      initializationPromise = null;
      console.error("New API Infinite Canvas initial sync failed:", error);
      return { dataChanged: false, conflictCopies: 0 };
    });
  return initializationPromise;
}
