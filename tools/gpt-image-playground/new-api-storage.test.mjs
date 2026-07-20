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

async function loadStorageModule({ injectedUserId, localStorage, search = "" }) {
  const transformed = stripTypeScriptTypes(storageSource, { mode: "transform" });
  globalThis.window = {
    __NEW_API_USER_ID__: injectedUserId,
    location: { search },
    localStorage,
    dispatchEvent() {},
  };
  Object.defineProperty(globalThis, "indexedDB", {
    configurable: true,
    value: undefined,
  });

  moduleSequence += 1;
  const sourceUrl = `new-api-image-storage-test-${moduleSequence}.mjs`;
  return import(
    `data:text/javascript;base64,${Buffer.from(`${transformed}\n//# sourceURL=${sourceUrl}`).toString("base64")}`
  );
}

function restoreGlobals(t) {
  const previousWindow = globalThis.window;
  const indexedDbDescriptor = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
  t.after(() => {
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
    if (indexedDbDescriptor) Object.defineProperty(globalThis, "indexedDB", indexedDbDescriptor);
    else delete globalThis.indexedDB;
  });
}

test("server-injected identity owns every image playground browser namespace", async (t) => {
  restoreGlobals(t);
  const localStorage = createStorage({
    uid: "202",
    "gpt-image-playground": "legacy-state",
  });
  const storage = await loadStorageModule({
    injectedUserId: 101,
    localStorage,
    search: "?new_api_user=303",
  });

  assert.equal(storage.getNewApiImagePlaygroundUserId(), "101");
  assert.equal(
    storage.getNewApiImagePlaygroundDatabaseName(),
    "gpt-image-playground:new-api-user:101",
  );
  assert.equal(
    storage.getNewApiImagePlaygroundStorageKey(),
    "gpt-image-playground:new-api-user:101",
  );
  assert.equal(
    storage.getNewApiImagePlaygroundMetadataKey(),
    "new-api:image-playground:sync:v1:101",
  );
  assert.equal(
    storage.getNewApiImagePlaygroundAssetCacheName(),
    "new-api-image-playground-assets-v1-user-101",
  );
  assert.equal(
    localStorage.getItem("gpt-image-playground:new-api-user:101"),
    "legacy-state",
  );
  assert.equal(localStorage.getItem("gpt-image-playground:new-api-user:202"), null);
});

test("missing or invalid server identity fails closed instead of trusting localStorage uid", async (t) => {
  restoreGlobals(t);
  const localStorage = createStorage({
    uid: "202",
    "gpt-image-playground": "legacy-state",
  });
  const storage = await loadStorageModule({
    injectedUserId: undefined,
    localStorage,
    search: "?new_api_user=303",
  });

  assert.equal(storage.getNewApiImagePlaygroundUserId(), null);
  assert.throws(
    () => storage.getNewApiImagePlaygroundDatabaseName(),
    /authenticated user identity/i,
  );
  assert.throws(
    () => storage.getNewApiImagePlaygroundStorageKey(),
    /authenticated user identity/i,
  );
  assert.equal(storage.getNewApiImagePlaygroundMetadataKey(), null);
  assert.equal(storage.getNewApiImagePlaygroundAssetCacheName(), null);
  assert.equal(localStorage.getItem("gpt-image-playground:new-api-user:202"), null);
});
