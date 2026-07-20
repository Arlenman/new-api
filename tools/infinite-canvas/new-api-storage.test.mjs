import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { stripTypeScriptTypes } from "node:module";
import test from "node:test";

const storageSource = await readFile(new URL("./new-api-storage.ts", import.meta.url), "utf8");
let moduleSequence = 0;

function createStorage(seed = {}) {
  const values = new Map(Object.entries(seed));
  return {
    get length() {
      return values.size;
    },
    clear() {
      values.clear();
    },
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    key(index) {
      return [...values.keys()][index] ?? null;
    },
    removeItem(key) {
      values.delete(key);
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
  };
}

function createLocalForage(seed = {}) {
  const stores = new Map();
  const storeId = (name, storeName) => `${name}/${storeName}`;
  const getStore = (name, storeName) => {
    const id = storeId(name, storeName);
    if (!stores.has(id)) stores.set(id, new Map());
    return stores.get(id);
  };

  for (const [id, records] of Object.entries(seed)) {
    stores.set(id, new Map(Object.entries(records)));
  }

  return {
    config() {},
    createInstance({ name, storeName }) {
      const records = getStore(name, storeName);
      return {
        async getItem(key) {
          return records.has(key) ? records.get(key) : null;
        },
        async keys() {
          return [...records.keys()];
        },
        async removeItem(key) {
          records.delete(key);
        },
        async setItem(key, value) {
          records.set(key, value);
          return value;
        },
      };
    },
    read(name, storeName, key) {
      return stores.get(storeId(name, storeName))?.get(key);
    },
  };
}

async function loadStorageModule(userId, localStorage, localforage, search = `?new_api_user=${userId}`) {
  const transformed = stripTypeScriptTypes(
    storageSource.replace(
      'import localforage from "localforage";',
      "const localforage = globalThis.__newApiLocalForage;",
    ),
    { mode: "transform" },
  );

  globalThis.__newApiLocalForage = localforage;
  globalThis.window = {
    __NEW_API_USER_ID__: userId,
    location: { search },
    localStorage,
    sessionStorage: createStorage(),
    dispatchEvent() {},
  };
  Object.defineProperty(globalThis, "indexedDB", {
    configurable: true,
    value: undefined,
  });

  moduleSequence += 1;
  const sourceUrl = `new-api-storage-test-${moduleSequence}.mjs`;
  return import(
    `data:text/javascript;base64,${Buffer.from(`${transformed}\n//# sourceURL=${sourceUrl}`).toString("base64")}`
  );
}

function restoreGlobals(t) {
  const previousWindow = globalThis.window;
  const previousLocalForage = globalThis.__newApiLocalForage;
  const indexedDbDescriptor = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
  t.after(() => {
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
    if (previousLocalForage === undefined) delete globalThis.__newApiLocalForage;
    else globalThis.__newApiLocalForage = previousLocalForage;
    if (indexedDbDescriptor) Object.defineProperty(globalThis, "indexedDB", indexedDbDescriptor);
    else delete globalThis.indexedDB;
  });
}

test("URL parameters cannot override the server-injected user identity", async (t) => {
  restoreGlobals(t);
  const localStorage = createStorage();
  const localforage = createLocalForage();
  const storage = await loadStorageModule(
    "101",
    localStorage,
    localforage,
    "?new_api_user=202",
  );

  assert.equal(storage.getNewApiInfiniteCanvasUserId(), "101");
  assert.equal(storage.namespacedStorageKey("infinite-canvas:test"), "infinite-canvas:test:101");
});

test("legacy localStorage and IndexedDB are globally claimed only by the first authenticated user", async (t) => {
  restoreGlobals(t);
  const localStorage = createStorage({
    "infinite-canvas:theme_store": "legacy-theme",
    "infinite-canvas:prompt_source_store": "legacy-prompts",
  });
  const localforage = createLocalForage({
    "infinite-canvas/app_state": {
      "infinite-canvas:canvas_store": "legacy-canvas",
    },
    "infinite-canvas/image_files": {
      "image-1": { type: "image/png", bytes: "legacy-image" },
    },
  });

  const userA = await loadStorageModule("101", localStorage, localforage);
  await userA.ensureLegacyInfiniteCanvasStorageMigration();

  assert.equal(
    localStorage.getItem("new-api:infinite-canvas:legacy-storage-claim:v3"),
    "101",
  );
  assert.equal(
    localStorage.getItem("infinite-canvas:theme_store:101"),
    "legacy-theme",
  );
  assert.equal(
    localforage.read(
      "infinite-canvas-101",
      "app_state",
      "infinite-canvas:canvas_store:101",
    ),
    "legacy-canvas",
  );
  assert.deepEqual(
    localforage.read("infinite-canvas-101", "image_files", "image-1"),
    { type: "image/png", bytes: "legacy-image" },
  );

  const userB = await loadStorageModule("202", localStorage, localforage);
  await userB.ensureLegacyInfiniteCanvasStorageMigration();

  assert.equal(
    localStorage.getItem("new-api:infinite-canvas:legacy-storage-claim:v3"),
    "101",
  );
  assert.equal(localStorage.getItem("infinite-canvas:theme_store:202"), null);
  assert.equal(
    localforage.read(
      "infinite-canvas-202",
      "app_state",
      "infinite-canvas:canvas_store:202",
    ),
    undefined,
  );
  assert.equal(
    localforage.read("infinite-canvas-202", "image_files", "image-1"),
    undefined,
  );

  assert.equal(localStorage.getItem("infinite-canvas:theme_store"), "legacy-theme");
  assert.equal(
    localforage.read(
      "infinite-canvas",
      "app_state",
      "infinite-canvas:canvas_store",
    ),
    "legacy-canvas",
  );
});

test("app_state migration maps legacy Zustand keys and never overwrites current user data", async (t) => {
  restoreGlobals(t);
  const localStorage = createStorage({
    "infinite-canvas:theme_store": "legacy-theme",
    "infinite-canvas:theme_store:101": "current-theme",
  });
  const localforage = createLocalForage({
    "infinite-canvas/app_state": {
      "infinite-canvas:canvas_store": "legacy-canvas",
      "infinite-canvas:asset_store": "legacy-assets",
      "infinite-canvas:plugin_store": "legacy-plugins",
    },
    "infinite-canvas-101/app_state": {
      "infinite-canvas:asset_store": "v2-user-assets",
      "infinite-canvas:plugin_store:101": "current-user-plugins",
    },
  });

  const storage = await loadStorageModule("101", localStorage, localforage);
  await storage.ensureLegacyInfiniteCanvasStorageMigration();

  assert.equal(
    localforage.read(
      "infinite-canvas-101",
      "app_state",
      "infinite-canvas:canvas_store:101",
    ),
    "legacy-canvas",
  );
  assert.equal(
    localforage.read(
      "infinite-canvas-101",
      "app_state",
      "infinite-canvas:asset_store:101",
    ),
    "v2-user-assets",
  );
  assert.equal(
    localforage.read(
      "infinite-canvas-101",
      "app_state",
      "infinite-canvas:plugin_store:101",
    ),
    "current-user-plugins",
  );
  assert.equal(
    localforage.read(
      "infinite-canvas-101",
      "app_state",
      "infinite-canvas:asset_store",
    ),
    "v2-user-assets",
  );
  assert.equal(
    localStorage.getItem("infinite-canvas:theme_store:101"),
    "current-theme",
  );
});
