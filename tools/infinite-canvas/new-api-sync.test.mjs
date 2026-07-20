import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import test from "node:test";
import vm from "node:vm";

const require = createRequire(import.meta.url);
const ts = require("../../web/default/node_modules/typescript/lib/typescript.js");
const sourceUrl = new URL("./new-api-sync.ts", import.meta.url);
const source = await readFile(sourceUrl, "utf8");

function loadSyncExports({ modules = {}, globals = {} } = {}) {
  const output = ts.transpileModule(source, {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      esModuleInterop: true,
    },
    fileName: "new-api-sync.ts",
  }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(output, {
    module,
    exports: module.exports,
    require: (id) => modules[id] ?? {},
    console,
    URLSearchParams,
    TextEncoder,
    crypto,
    btoa,
    Blob,
    Headers,
    Request,
    Response,
    Event,
    StorageEvent: class StorageEvent {},
    setTimeout,
    clearTimeout,
    ...globals,
  });
  return module.exports;
}

const sync = loadSyncExports();
const plain = (value) => JSON.parse(JSON.stringify(value));

test("declares every account-level setting and keeps plugin_store in namespaced app_state", () => {
  for (const key of [
    "infinite-canvas:theme_store",
    "infinite-canvas:ai_config_store",
    "infinite-canvas:prompt_source_store",
    "infinite-canvas:plugin_store",
    "canvas-side-panel-width",
    "canvas-side-panel-open",
    "canvas-agent-panel-width",
    "canvas-image-quick-tools-v6",
  ]) {
    assert.match(
      source,
      new RegExp(JSON.stringify(key).replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    );
  }
  assert.match(source, /const SETTING_KIND = "setting"/);
  assert.match(source, /const APP_STATE_STORE = "app_state"/);
  assert.match(
    source,
    /LOCAL_FORAGE_SETTING_KEYS = \["infinite-canvas:plugin_store"\]/,
  );
  assert.match(
    source,
    /localforage\.createInstance\(\{\s*name:\s*namespacedLocalForageName\("infinite-canvas"\),\s*storeName:\s*APP_STATE_STORE,?\s*\}\)/,
  );
  assert.match(
    source,
    /const item = await makeSettingItem\(key, storedValue\)/,
  );
  assert.match(source, /await removeStoredSetting\(item\.key\)/);
});

test("scrubForSync removes credentials and endpoints without removing ordinary plugin URLs", () => {
  const scrubbed = plain(
    sync.scrubForSync({
      apiKey: "api-key",
      runtimeCredential: "utrs_runtime",
      Authorization: "Bearer token",
      access_token: "access-token",
      clientSecret: "client-secret",
      password: "password",
      private_key: "private-key",
      imageApiUrl: "https://api.example.test",
      baseUrl: "https://base.example.test",
      canvasAgentUrl: "http://127.0.0.1:17371",
      canvasAgentToken: "agent-token",
      webdav: {
        url: "https://dav.example.test",
        username: "dav-user",
        password: "dav-password",
        token: "dav-token",
        directory: "infinite-canvas",
        lastSyncedAt: "2026-07-19T00:00:00Z",
      },
      plugin: {
        id: "demo",
        url: "https://plugins.example.test/demo.js",
        source: "export default {};",
      },
      model: "gpt-image-2",
    }),
  );

  assert.deepEqual(scrubbed, {
    webdav: {
      directory: "infinite-canvas",
      lastSyncedAt: "2026-07-19T00:00:00Z",
    },
    plugin: {
      id: "demo",
      url: "https://plugins.example.test/demo.js",
      source: "export default {};",
    },
    model: "gpt-image-2",
  });
});

test("mergeWithLocalSensitiveFields restores local secrets recursively and matches array records by id", () => {
  const remote = {
    state: {
      config: {
        quality: "hd",
        channels: [
          { id: "channel-b", name: "远端 B", models: ["b-model"] },
          { id: "channel-a", name: "远端 A", models: ["a-model"] },
        ],
      },
      webdav: {
        directory: "remote-directory",
        lastSyncedAt: "remote-time",
      },
      localOnly: "remote-value",
    },
    version: 1,
  };
  const local = {
    state: {
      config: {
        quality: "local-quality",
        apiKey: "local-top-level-key",
        baseUrl: "https://local-base.example.test",
        channels: [
          {
            id: "channel-a",
            name: "本地 A",
            apiKey: "local-a-key",
            baseUrl: "https://a.example.test",
          },
          {
            id: "channel-b",
            name: "本地 B",
            apiKey: "local-b-key",
            baseUrl: "https://b.example.test",
          },
        ],
      },
      webdav: {
        url: "https://dav.local.test",
        username: "local-user",
        password: "local-password",
        token: "local-token",
        directory: "local-directory",
      },
      localOnly: "local-value",
    },
    version: 0,
  };

  const merged = plain(sync.mergeWithLocalSensitiveFields(remote, local));
  assert.equal(merged.state.config.quality, "hd");
  assert.equal(merged.state.config.apiKey, "local-top-level-key");
  assert.equal(merged.state.config.baseUrl, "https://local-base.example.test");
  assert.equal(merged.state.config.channels[0].id, "channel-b");
  assert.equal(merged.state.config.channels[0].name, "远端 B");
  assert.equal(merged.state.config.channels[0].apiKey, "local-b-key");
  assert.equal(
    merged.state.config.channels[0].baseUrl,
    "https://b.example.test",
  );
  assert.equal(merged.state.config.channels[1].apiKey, "local-a-key");
  assert.equal(merged.state.webdav.directory, "remote-directory");
  assert.equal(merged.state.webdav.url, "https://dav.local.test");
  assert.equal(merged.state.webdav.username, "local-user");
  assert.equal(merged.state.webdav.password, "local-password");
  assert.equal(merged.state.webdav.token, "local-token");
  assert.equal(merged.state.localOnly, "remote-value");
  assert.equal(merged.version, 1);
});

test("remote setting restoration rehydrates persist stores and refreshes direct Zustand stores", () => {
  for (const store of [
    "useThemeStore",
    "useConfigStore",
    "usePromptSourceStore",
    "usePluginStore",
  ]) {
    assert.ok(source.includes(`${store} as unknown as PersistStoreForRefresh`));
  }
  assert.match(source, /store\.persist\.rehydrate\(\)/);
  assert.match(
    source,
    /mergeWithLocalSensitiveFields\(restoredState, localState\)/,
  );
  assert.match(source, /useCanvasSidePanelStore\.setState\(/);
  assert.match(source, /useAgentStore\.setState\(/);
  assert.match(source, /new StorageEvent\("storage"/);
  assert.doesNotMatch(source, /dataChanged \|\|= await applyRemoteItem/);
  assert.match(
    source,
    /const itemChanged = await applyRemoteItem\(item, known\)/,
  );
});

test("scrubForSync serializes Date values without weakening record sanitization", () => {
  const result = sync.scrubForSync({
    createdAt: new Date("2026-07-20T00:00:00.000Z"),
  });

  assert.equal(result.createdAt, "2026-07-20T00:00:00.000Z");
});

test("scrubForSync sanitizes JSON-encoded secrets and credential-shaped strings", () => {
  assert.equal(
    sync.scrubForSync(
      '{"apiKey":"sk-test-secret-value","model":"gpt-image-2","nested":{"access_token":"token-value","enabled":true}}',
      "config",
      ["plugin-demo"],
    ),
    '{"model":"gpt-image-2","nested":{"enabled":true}}',
  );
  assert.equal(
    sync.scrubForSync("Bearer should-never-leave-this-browser", "value"),
    "",
  );
  assert.equal(
    sync.scrubForSync(
      "https://example.test/v1?api_key=should-never-leave",
      "endpoint",
    ),
    "",
  );
});

test("plugin sync policy rejects sensitive identities, secret values, and non-media blobs", () => {
  assert.equal(
    sync.canSyncPluginRecord("demo-plugin", "apiKey", "secret"),
    false,
  );
  assert.equal(
    sync.canSyncPluginRecord(
      "demo-plugin",
      "history",
      "Bearer should-never-leave-this-browser",
    ),
    false,
  );
  assert.equal(
    sync.canSyncPluginRecord(
      "demo-plugin",
      "binary-state",
      new Blob(["{\"apiKey\":\"secret\"}"], { type: "application/json" }),
    ),
    false,
  );
  assert.equal(
    sync.canSyncPluginRecord(
      "demo-plugin",
      "preview",
      new Blob(["safe image bytes"], { type: "image/png" }),
    ),
    true,
  );
  assert.equal(
    sync.canSyncPluginRecord("demo-plugin", "state", new Map([["key", "value"]])),
    false,
  );
  assert.equal(
    sync.canSyncPluginRecord("demo-plugin", "history", { prompt: "hello" }),
    true,
  );
});

test("blob sync descriptor changes when equal-size equal-type content changes", async () => {
  const first = await sync.makeBlobItem(
    "image:same-size",
    new Blob(["abc"], { type: "image/png" }),
  );
  const second = await sync.makeBlobItem(
    "image:same-size",
    new Blob(["xyz"], { type: "image/png" }),
  );

  assert.notEqual(first.asset.sha256, second.asset.sha256);
  assert.notEqual(first.hash, second.hash);
  assert.doesNotMatch(source, /previous\.size_bytes === value\.size/);
});

test("plugin remote merge preserves local secrets while applying remote non-sensitive values", () => {
  assert.equal(
    sync.mergePluginRecordValue(
      "demo-plugin",
      "config",
      '{"model":"remote-model","apiKey":"remote-secret"}',
      '{"model":"local-model","apiKey":"local-secret","baseUrl":"https://local.example.test"}',
    ),
    '{"model":"remote-model","apiKey":"local-secret","baseUrl":"https://local.example.test"}',
  );
  assert.equal(sync.isSafePluginBlobContentType("image/png"), true);
  assert.equal(sync.isSafePluginBlobContentType("application/json"), false);
  assert.match(source, /mergePluginRecordValue\(/);
});


test("first sync restores a remote canvas into an empty browser without uploading a deletion", async () => {
  const localStorageValues = new Map();
  const localStorage = {
    getItem: (key) => localStorageValues.get(key) ?? null,
    setItem: (key, value) => localStorageValues.set(key, String(value)),
    removeItem: (key) => localStorageValues.delete(key),
  };
  const emptyLocalForageStore = {
    getItem: async () => null,
    setItem: async () => undefined,
    removeItem: async () => undefined,
    iterate: async () => undefined,
  };
  const canvasState = {
    hydrated: true,
    projects: [],
    replaceProjects(projects) {
      this.projects = projects;
    },
  };
  const assetState = {
    hydrated: true,
    assets: [],
    replaceAssets(assets) {
      this.assets = assets;
    },
  };
  const hydratedStore = (state) => ({
    getState: () => state,
    persist: {
      hasHydrated: () => true,
      onFinishHydration: () => () => undefined,
    },
  });
  const remoteProject = {
    id: "remote-project",
    title: "服务端已有画布",
    createdAt: "2026-07-19T00:00:00.000Z",
    updatedAt: "2026-07-19T00:00:00.000Z",
    nodes: [],
    chatSessions: [],
  };
  const remoteItem = {
    id: "item-remote-project",
    kind: "canvas-project",
    key: remoteProject.id,
    schema_version: 1,
    revision: 4,
    status: "ready",
    payload: remoteProject,
    asset_ids: [],
    created_at: 0,
    updated_at: 11,
    deleted: false,
  };
  const syncUploads = [];
  const requestedPaths = [];
  const fetch = async (path, init = {}) => {
    requestedPaths.push(String(path));
    if (String(path).includes("/sync")) {
      syncUploads.push(JSON.parse(init.body));
      return Response.json({
        success: true,
        data: { results: [], cursor: 11 },
      });
    }
    if (String(path).includes("/bootstrap?")) {
      return Response.json({
        success: true,
        data: {
          items: [remoteItem],
          assets: [],
          cursor: 11,
          next_after_id: "",
          has_more: false,
        },
      });
    }
    if (String(path).includes("/changes?cursor=11")) {
      return Response.json({
        success: true,
        data: {
          items: [],
          assets: [],
          next_cursor: 11,
          has_more: false,
        },
      });
    }
    throw new Error(`Unexpected sync request: ${path}`);
  };
  const window = {
    localStorage,
    clearTimeout,
    setTimeout,
    setInterval: () => 1,
    addEventListener: () => undefined,
    dispatchEvent: () => true,
  };
  const document = {
    visibilityState: "visible",
    addEventListener: () => undefined,
  };
  const syncModule = loadSyncExports({
    modules: {
      localforage: {
        createInstance: () => emptyLocalForageStore,
      },
      nanoid: { nanoid: () => "conflict-copy" },
      "@/lib/canvas/canvas-generation-helpers": {
        hydrateAssistantImages: async (sessions) => sessions,
        hydrateCanvasImages: async (nodes) => nodes,
      },
      "@/lib/new-api-storage": {
        ensureLegacyInfiniteCanvasStorageMigration: async () => undefined,
        getNewApiInfiniteCanvasAssetCacheName: () => "new-api:test-assets:7",
        getNewApiInfiniteCanvasMetadataKey: () => "new-api:test-sync:7",
        getNewApiInfiniteCanvasPluginDatabaseName: () => "plugins:7",
        getNewApiInfiniteCanvasUserId: () => "7",
        listNewApiInfiniteCanvasPluginStoreNames: async () => [],
        namespacedLocalForageName: (name) => `${name}:7`,
        namespacedStorageKey: (key) => `${key}:7`,
        NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT:
          "new-api:test-remote-logs",
        NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT:
          "new-api:test-storage-changed",
        runWithoutNewApiInfiniteCanvasSyncNotifications: async (fn) => fn(),
      },
      "@/services/file-storage": {
        getMediaBlob: async () => null,
        resolveMediaUrl: async (value) => value,
        setMediaBlob: async () => undefined,
      },
      "@/services/image-storage": {
        getImageBlob: async () => null,
        resolveImageUrl: async (value) => value,
        setImageBlob: async () => undefined,
      },
      "@/stores/canvas/use-canvas-store": {
        useCanvasStore: hydratedStore(canvasState),
      },
      "@/stores/canvas/use-plugin-store": { usePluginStore: {} },
      "@/stores/use-agent-store": { useAgentStore: {} },
      "@/stores/use-asset-store": {
        useAssetStore: hydratedStore(assetState),
      },
      "@/stores/use-canvas-side-panel-store": {
        CANVAS_SIDE_PANEL_DEFAULT_WIDTH: 360,
        CANVAS_SIDE_PANEL_MAX_WIDTH: 600,
        CANVAS_SIDE_PANEL_MIN_WIDTH: 280,
        useCanvasSidePanelStore: {},
      },
      "@/stores/use-config-store": { useConfigStore: {} },
      "@/stores/use-prompt-source-store": { usePromptSourceStore: {} },
      "@/stores/use-theme-store": { useThemeStore: {} },
    },
    globals: { fetch, window, document },
  });

  const result = await syncModule.initializeNewApiInfiniteCanvasSync();

  assert.equal(result.dataChanged, true);
  assert.deepEqual(plain(canvasState.projects), [remoteProject]);
  assert.deepEqual(syncUploads, []);
  assert.equal(
    requestedPaths.some((path) => path.includes("/bootstrap?")),
    true,
  );
  assert.equal(
    requestedPaths.some((path) => path.includes("/changes?cursor=11")),
    true,
  );
});


test("remote blob deletions remove image and media blobs without evicting the user asset cache", async () => {
  const imageKey = "image:remote-image";
  const mediaKey = "media:remote-video";
  const imageAssetId = "asset-image";
  const mediaAssetId = "asset-media";
  const imageBlob = new Blob(["image-bytes"], { type: "image/png" });
  const mediaBlob = new Blob(["video-bytes"], { type: "video/mp4" });
  const stores = new Map();
  const storeFor = (storeName) => {
    const values = stores.get(storeName) ?? new Map();
    stores.set(storeName, values);
    return {
      getItem: async (key) => values.get(key) ?? null,
      setItem: async (key, value) => values.set(key, value),
      removeItem: async (key) => values.delete(key),
      iterate: async (callback) => {
        for (const [key, value] of values) callback(value, key);
      },
    };
  };
  const metadataKey = "new-api:test-sync:7";
  const localStorageValues = new Map([
    [metadataKey, JSON.stringify({ cursor: 20, entries: {} })],
  ]);
  const localStorage = {
    getItem: (key) => localStorageValues.get(key) ?? null,
    setItem: (key, value) => localStorageValues.set(key, String(value)),
    removeItem: (key) => localStorageValues.delete(key),
  };
  const deletedImages = [];
  const deletedMedia = [];
  const cachedAssets = new Map();
  const openedCacheNames = [];
  let cacheDeleteCount = 0;
  const caches = {
    open: async (name) => {
      openedCacheNames.push(name);
      return {
        match: async (request) => cachedAssets.get(request.url),
        put: async (request, response) =>
          cachedAssets.set(request.url, response),
        delete: async (request) => {
          cacheDeleteCount += 1;
          return cachedAssets.delete(request.url);
        },
      };
    },
  };
  class TestRequest extends Request {
    constructor(input, init) {
      super(
        typeof input === "string"
          ? new URL(input, "http://new-api.test").href
          : input,
        init,
      );
    }
  }
  let projectRefreshes = 0;
  let assetRefreshes = 0;
  const canvasState = {
    hydrated: true,
    projects: [],
    replaceProjects(projects) {
      projectRefreshes += 1;
      this.projects = projects;
    },
  };
  const assetState = {
    hydrated: true,
    assets: [],
    replaceAssets(assets) {
      assetRefreshes += 1;
      this.assets = assets;
    },
  };
  const hydratedStore = (state) => ({
    getState: () => state,
    persist: {
      hasHydrated: () => true,
      onFinishHydration: () => () => undefined,
    },
  });
  const readyItem = (key, assetId, blob, family) => ({
    id: `ready-${key}`,
    kind: "blob",
    key,
    schema_version: 1,
    revision: 1,
    status: "ready",
    payload: {
      storageKey: key,
      family,
      content_type: blob.type,
      size_bytes: blob.size,
      content_sha256: `sha256-${assetId}`,
      asset_id: assetId,
    },
    asset_ids: [assetId],
    created_at: 0,
    updated_at: 20,
    deleted: false,
  });
  const deletedItem = (key) => ({
    id: `deleted-${key}`,
    kind: "blob",
    key,
    schema_version: 1,
    revision: 2,
    status: "deleted",
    payload: {},
    asset_ids: [],
    created_at: 0,
    updated_at: 21,
    deleted: true,
  });
  const syncUploads = [];
  const fetch = async (input, init = {}) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/sync")) {
      syncUploads.push(JSON.parse(init.body));
      return Response.json({
        success: true,
        data: { results: [], cursor: 20 },
      });
    }
    if (url.includes("/changes?cursor=20")) {
      return Response.json({
        success: true,
        data: {
          items: [
            readyItem(imageKey, imageAssetId, imageBlob, "image"),
            readyItem(mediaKey, mediaAssetId, mediaBlob, "media"),
            deletedItem(imageKey),
            deletedItem(mediaKey),
          ],
          assets: [],
          next_cursor: 21,
          has_more: false,
        },
      });
    }
    if (url.includes(`/assets/${imageAssetId}/content`))
      return new Response(imageBlob);
    if (url.includes(`/assets/${mediaAssetId}/content`))
      return new Response(mediaBlob);
    throw new Error(`Unexpected sync request: ${url}`);
  };
  const window = {
    localStorage,
    caches,
    clearTimeout,
    setTimeout,
    setInterval: () => 1,
    addEventListener: () => undefined,
    dispatchEvent: () => true,
  };
  const document = {
    visibilityState: "visible",
    addEventListener: () => undefined,
  };
  const syncModule = loadSyncExports({
    modules: {
      localforage: {
        createInstance: ({ storeName }) => storeFor(storeName),
      },
      nanoid: { nanoid: () => "conflict-copy" },
      "@/lib/canvas/canvas-generation-helpers": {
        hydrateAssistantImages: async (sessions) => sessions,
        hydrateCanvasImages: async (nodes) => nodes,
      },
      "@/lib/new-api-storage": {
        ensureLegacyInfiniteCanvasStorageMigration: async () => undefined,
        getNewApiInfiniteCanvasAssetCacheName: () => "new-api:test-assets:7",
        getNewApiInfiniteCanvasMetadataKey: () => metadataKey,
        getNewApiInfiniteCanvasPluginDatabaseName: () => "plugins:7",
        getNewApiInfiniteCanvasUserId: () => "7",
        listNewApiInfiniteCanvasPluginStoreNames: async () => [],
        namespacedLocalForageName: (name) => `${name}:7`,
        namespacedStorageKey: (key) => `${key}:7`,
        NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT:
          "new-api:test-remote-logs",
        NEW_API_INFINITE_CANVAS_STORAGE_CHANGED_EVENT:
          "new-api:test-storage-changed",
        runWithoutNewApiInfiniteCanvasSyncNotifications: async (fn) => fn(),
      },
      "@/services/file-storage": {
        deleteStoredMedia: async (keys) => {
          const uniqueKeys = [...new Set(keys)];
          deletedMedia.push(...uniqueKeys);
          await Promise.all(
            uniqueKeys.map((key) => storeFor("media_files").removeItem(key)),
          );
        },
        getMediaBlob: async (key) => storeFor("media_files").getItem(key),
        resolveMediaUrl: async (value) => value,
        setMediaBlob: async (key, blob) =>
          storeFor("media_files").setItem(key, blob),
      },
      "@/services/image-storage": {
        deleteStoredImages: async (keys) => {
          const uniqueKeys = [...new Set(keys)];
          deletedImages.push(...uniqueKeys);
          await Promise.all(
            uniqueKeys.map((key) => storeFor("image_files").removeItem(key)),
          );
        },
        getImageBlob: async (key) => storeFor("image_files").getItem(key),
        resolveImageUrl: async (value) => value,
        setImageBlob: async (key, blob) =>
          storeFor("image_files").setItem(key, blob),
      },
      "@/stores/canvas/use-canvas-store": {
        useCanvasStore: hydratedStore(canvasState),
      },
      "@/stores/canvas/use-plugin-store": { usePluginStore: {} },
      "@/stores/use-agent-store": { useAgentStore: {} },
      "@/stores/use-asset-store": {
        useAssetStore: hydratedStore(assetState),
      },
      "@/stores/use-canvas-side-panel-store": {
        CANVAS_SIDE_PANEL_DEFAULT_WIDTH: 360,
        CANVAS_SIDE_PANEL_MAX_WIDTH: 600,
        CANVAS_SIDE_PANEL_MIN_WIDTH: 280,
        useCanvasSidePanelStore: {},
      },
      "@/stores/use-config-store": { useConfigStore: {} },
      "@/stores/use-prompt-source-store": { usePromptSourceStore: {} },
      "@/stores/use-theme-store": { useThemeStore: {} },
    },
    globals: { caches, fetch, Request: TestRequest, window, document },
  });

  const result = await syncModule.initializeNewApiInfiniteCanvasSync();

  assert.equal(result.dataChanged, true);
  assert.deepEqual(syncUploads, []);
  assert.deepEqual(deletedImages, [imageKey]);
  assert.deepEqual(deletedMedia, [mediaKey]);
  assert.equal(stores.get("image_files").has(imageKey), false);
  assert.equal(stores.get("media_files").has(mediaKey), false);
  assert.equal(projectRefreshes, 1);
  assert.equal(assetRefreshes, 1);
  assert.deepEqual(openedCacheNames, [
    "new-api:test-assets:7",
    "new-api:test-assets:7",
  ]);
  assert.equal(cacheDeleteCount, 0);
  assert.equal(cachedAssets.size, 2);
  assert.equal(
    await cachedAssets
      .get(`http://new-api.test/api/user-tools/assets/${imageAssetId}/content`)
      .clone()
      .text(),
    "image-bytes",
  );
  assert.equal(
    await cachedAssets
      .get(`http://new-api.test/api/user-tools/assets/${mediaAssetId}/content`)
      .clone()
      .text(),
    "video-bytes",
  );
});
