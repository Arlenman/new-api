import assert from 'node:assert/strict'
import { createHash, webcrypto } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import vm from 'node:vm'

const require = createRequire(import.meta.url)
const ts = require('../../web/default/node_modules/typescript/lib/typescript.js')
const sourceUrl = new URL('./new-api-sync.ts', import.meta.url)
const source = await readFile(sourceUrl, 'utf8')

function createLocalStorage(initial = {}) {
  const values = new Map(Object.entries(initial))
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null
    },
    setItem(key, value) {
      values.set(key, String(value))
    },
    removeItem(key) {
      values.delete(key)
    },
    value(key) {
      return values.get(key)
    },
  }
}

class RelativeRequest {
  constructor(input, init = {}) {
    this.url = typeof input === 'string' ? input : input.url
    this.headers = new Headers(init.headers ?? (typeof input === 'string' ? undefined : input.headers))
    this.credentials = init.credentials ?? (typeof input === 'string' ? undefined : input.credentials)
    this.method = init.method ?? (typeof input === 'string' ? 'GET' : input.method)
  }
}

class TestFileReader {
  result = null
  error = null
  onload = null
  onerror = null

  readAsDataURL(blob) {
    blob.arrayBuffer().then((buffer) => {
      this.result = `data:${blob.type || 'application/octet-stream'};base64,${Buffer.from(buffer).toString('base64')}`
      this.onload?.()
    }, (error) => {
      this.error = error
      this.onerror?.()
    })
  }
}

function jsonEnvelope(data) {
  return new Response(JSON.stringify({ success: true, data }), {
    headers: { 'Content-Type': 'application/json' },
  })
}

function loadSyncModule({ db = {}, storage = {}, globals = {} } = {}) {
  const output = ts.transpileModule(source, {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      esModuleInterop: true,
    },
    fileName: 'new-api-sync.ts',
  }).outputText
  const module = { exports: {} }
  const localStorage = globals.localStorage ?? createLocalStorage()
  const caches = globals.caches ?? {
    async open() {
      return {
        async delete() { return false },
        async match() { return undefined },
        async put() {},
      }
    },
  }
  const window = globals.window ?? {
    localStorage,
    caches,
    addEventListener() {},
    setInterval() { return 1 },
  }
  const document = globals.document ?? {
    visibilityState: 'visible',
    addEventListener() {},
  }
  const dbModule = {
    deleteAgentConversation: async () => {},
    deleteImage: async () => {},
    deleteImageThumbnail: async () => {},
    deleteTask: async () => {},
    getAllAgentConversations: async () => [],
    getAllImageIds: async () => [],
    getAllStoredImageThumbnailIds: async () => [],
    getAllTasks: async () => [],
    getImage: async () => undefined,
    getStoredImageThumbnail: async () => undefined,
    putAgentConversation: async () => {},
    putImage: async () => {},
    putImageThumbnail: async () => {},
    putTask: async () => {},
    ...db,
  }
  const storageModule = {
    getNewApiImagePlaygroundAssetCacheName: () => 'new-api-image-playground-assets-user-7',
    getNewApiImagePlaygroundMetadataKey: () => 'new-api-image-playground-metadata-user-7',
    getNewApiImagePlaygroundStorageKey: () => 'new-api-image-playground-state-user-7',
    getNewApiImagePlaygroundUserId: () => '7',
    NEW_API_IMAGE_PLAYGROUND_STORAGE_CHANGED_EVENT: 'new-api-image-playground-storage-changed',
    runWithoutNewApiImagePlaygroundSyncNotifications: async (callback) => callback(),
    ...storage,
  }

  vm.runInNewContext(output, {
    module,
    exports: module.exports,
    require: (id) => {
      if (id === './db') return dbModule
      if (id === './newApiStorage') return storageModule
      return {}
    },
    console,
    URLSearchParams,
    TextEncoder,
    crypto: webcrypto,
    atob,
    btoa,
    Blob,
    Headers,
    Request: RelativeRequest,
    Response,
    FileReader: TestFileReader,
    Event,
    StorageEvent: class StorageEvent {},
    setTimeout,
    clearTimeout,
    fetch: globals.fetch,
    caches,
    window,
    document,
  })
  return { module: module.exports, localStorage }
}

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex')
}

function remoteItem(overrides) {
  return {
    id: `remote-${overrides.kind}-${overrides.key}`,
    schema_version: 1,
    revision: 1,
    status: 'ready',
    payload: {},
    asset_ids: [],
    created_at: 1,
    updated_at: 1,
    deleted: false,
    ...overrides,
  }
}

test('does not reuse equal-length image or thumbnail data when SHA-256 content differs', async () => {
  const { module } = loadSyncModule()
  const originalDataUrl = 'data:image/png;base64,AA=='
  const changedDataUrl = 'data:image/png;base64,/w=='
  const metadataEntry = {
    deleted: false,
    encoded_length: originalDataUrl.length,
    content_sha256: sha256(Buffer.from([0])),
  }

  assert.equal(changedDataUrl.length, originalDataUrl.length)
  assert.equal(
    await module.canReuseImageSyncEntry('image', originalDataUrl, metadataEntry),
    true,
  )
  assert.equal(
    await module.canReuseImageSyncEntry('image', changedDataUrl, metadataEntry),
    false,
  )
  assert.equal(
    await module.canReuseImageSyncEntry('thumbnail', originalDataUrl, {
      ...metadataEntry,
      thumbnail_version: 1,
    }, 1),
    true,
  )
  assert.equal(
    await module.canReuseImageSyncEntry('thumbnail', changedDataUrl, {
      ...metadataEntry,
      thumbnail_version: 1,
    }, 1),
    false,
  )
})

test('does not reuse legacy image metadata without content SHA-256', async () => {
  const { module } = loadSyncModule()
  const dataUrl = 'data:image/png;base64,AA=='
  assert.equal(
    await module.canReuseImageSyncEntry('image', dataUrl, {
      deleted: false,
      encoded_length: dataUrl.length,
    }),
    false,
  )
})

test('keeps a cached asset when a deleted image shares it with another live image', async () => {
  const dataUrl = 'data:image/png;base64,AA=='
  const contentSha256 = sha256(Buffer.from([0]))
  const sharedAssetId = 'asset-shared-by-two-images'
  const metadataKey = 'new-api-image-playground-metadata-user-7'
  const localStorage = createLocalStorage({
    [metadataKey]: JSON.stringify({
      cursor: 10,
      entries: {
        ['image\u0000image-to-delete']: {
          revision: 1,
          hash: 'image-to-delete-hash',
          deleted: false,
          asset_id: sharedAssetId,
          encoded_length: dataUrl.length,
          content_sha256: contentSha256,
        },
        ['image\u0000image-to-keep']: {
          revision: 1,
          hash: 'image-to-keep-hash',
          deleted: false,
          asset_id: sharedAssetId,
          encoded_length: dataUrl.length,
          content_sha256: contentSha256,
        },
        ['state\u0000app']: {
          revision: 1,
          hash: sha256('{}'),
          deleted: false,
        },
      },
    }),
  })
  const deletedImages = []
  const cacheDeletes = []
  const caches = {
    async open(cacheName) {
      assert.equal(cacheName, 'new-api-image-playground-assets-user-7')
      return {
        async delete(request) {
          cacheDeletes.push(request.url)
          return true
        },
        async match() { return undefined },
        async put() {},
      }
    },
  }
  const images = new Map([
    ['image-to-delete', { id: 'image-to-delete', dataUrl, createdAt: 1, source: 'generated' }],
    ['image-to-keep', { id: 'image-to-keep', dataUrl, createdAt: 2, source: 'generated' }],
  ])
  const fetch = async (input) => {
    const url = typeof input === 'string' ? input : input.url
    if (url.startsWith('/api/user-tools/image-playground/changes?')) {
      return jsonEnvelope({
        items: [remoteItem({
          kind: 'image',
          key: 'image-to-delete',
          revision: 2,
          status: 'deleted',
          deleted: true,
        })],
        assets: [],
        next_cursor: 11,
        has_more: false,
      })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      getAllImageIds: async () => [...images.keys()],
      getImage: async (id) => images.get(id),
      deleteImage: async (id) => {
        deletedImages.push(id)
        images.delete(id)
      },
    },
    globals: { localStorage, caches, fetch },
  })

  const result = await module.initializeNewApiImagePlaygroundSync()

  assert.deepEqual(deletedImages, ['image-to-delete'])
  assert.equal(images.has('image-to-keep'), true)
  assert.deepEqual(cacheDeletes, [])
  assert.deepEqual(JSON.parse(localStorage.value(metadataKey)).entries['image\u0000image-to-keep'].asset_id, sharedAssetId)
  assert.equal(result.dataChanged, true)
})

test('keeps a cached asset when an image replacement leaves another live image referencing it', async () => {
  const oldDataUrl = 'data:image/png;base64,AA=='
  const oldContentSha256 = sha256(Buffer.from([0]))
  const sharedAssetId = 'asset-shared-before-replacement'
  const replacementAssetId = 'asset-after-replacement'
  const metadataKey = 'new-api-image-playground-metadata-user-7'
  const localStorage = createLocalStorage({
    [metadataKey]: JSON.stringify({
      cursor: 10,
      entries: {
        ['image\u0000image-to-replace']: {
          revision: 1,
          hash: 'image-to-replace-hash',
          deleted: false,
          asset_id: sharedAssetId,
          encoded_length: oldDataUrl.length,
          content_sha256: oldContentSha256,
        },
        ['image\u0000image-to-keep']: {
          revision: 1,
          hash: 'image-to-keep-hash',
          deleted: false,
          asset_id: sharedAssetId,
          encoded_length: oldDataUrl.length,
          content_sha256: oldContentSha256,
        },
        ['state\u0000app']: {
          revision: 1,
          hash: sha256('{}'),
          deleted: false,
        },
      },
    }),
  })
  const cacheDeletes = []
  const cachePuts = []
  const caches = {
    async open(cacheName) {
      assert.equal(cacheName, 'new-api-image-playground-assets-user-7')
      return {
        async delete(request) {
          cacheDeletes.push(request.url)
          return true
        },
        async match() { return undefined },
        async put(request) {
          cachePuts.push(request.url)
        },
      }
    },
  }
  const images = new Map([
    ['image-to-replace', { id: 'image-to-replace', dataUrl: oldDataUrl, createdAt: 1, source: 'generated' }],
    ['image-to-keep', { id: 'image-to-keep', dataUrl: oldDataUrl, createdAt: 2, source: 'generated' }],
  ])
  const fetch = async (input) => {
    const url = typeof input === 'string' ? input : input.url
    if (url.startsWith('/api/user-tools/image-playground/changes?')) {
      return jsonEnvelope({
        items: [remoteItem({
          kind: 'image',
          key: 'image-to-replace',
          revision: 2,
          payload: {
            id: 'image-to-replace',
            createdAt: 3,
            source: 'generated',
            content_sha256: sha256(Buffer.from([1])),
            content_type: 'image/png',
            encoded_length: 'data:image/png;base64,AQ=='.length,
            asset_id: replacementAssetId,
          },
          asset_ids: [replacementAssetId],
        })],
        assets: [],
        next_cursor: 11,
        has_more: false,
      })
    }
    if (url === `/api/user-tools/assets/${replacementAssetId}/content`) {
      return new Response(Uint8Array.from([1]), {
        headers: { 'Content-Type': 'image/png' },
      })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      getAllImageIds: async () => [...images.keys()],
      getImage: async (id) => images.get(id),
      putImage: async (image) => images.set(image.id, image),
    },
    globals: { localStorage, caches, fetch },
  })

  await module.initializeNewApiImagePlaygroundSync()

  assert.deepEqual(cacheDeletes, [])
  assert.deepEqual(cachePuts, [`/api/user-tools/assets/${replacementAssetId}/content`])
  assert.equal(images.get('image-to-replace').dataUrl, 'data:image/png;base64,AQ==')
  const entries = JSON.parse(localStorage.value(metadataKey)).entries
  assert.equal(entries['image\u0000image-to-replace'].asset_id, replacementAssetId)
  assert.equal(entries['image\u0000image-to-keep'].asset_id, sharedAssetId)
})

test('first sync restores remote image and task into an empty browser without uploading deleted mutations', async () => {
  const storedImages = []
  const storedTasks = []
  const syncBodies = []
  const requests = []
  const cachedAssets = []
  const assetId = 'asset-first-sync-image'
  const image = remoteItem({
    kind: 'image',
    key: 'remote-image',
    payload: {
      id: 'remote-image',
      createdAt: 100,
      source: 'generated',
      width: 1,
      height: 1,
      content_sha256: sha256(Buffer.from([0])),
      content_type: 'image/png',
      encoded_length: 'data:image/png;base64,AA=='.length,
      asset_id: assetId,
    },
    asset_ids: [assetId],
  })
  const task = remoteItem({
    kind: 'task',
    key: 'remote-task',
    status: 'completed',
    payload: {
      id: 'remote-task',
      status: 'completed',
      createdAt: 101,
      prompt: 'restored task',
    },
  })
  const caches = {
    async open(cacheName) {
      assert.equal(cacheName, 'new-api-image-playground-assets-user-7')
      return {
        async delete() { return false },
        async match() { return undefined },
        async put(request) {
          cachedAssets.push(request.url)
        },
      }
    },
  }
  const fetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    requests.push(url)
    if (url === '/api/user-tools/image-playground/sync') {
      syncBodies.push(JSON.parse(init.body))
      return jsonEnvelope({ results: [], cursor: 0 })
    }
    if (url.startsWith('/api/user-tools/image-playground/bootstrap?')) {
      return jsonEnvelope({
        items: [image, task],
        assets: [],
        cursor: 20,
        next_after_id: '',
        has_more: false,
      })
    }
    if (url === `/api/user-tools/assets/${assetId}/content`) {
      return new Response(Uint8Array.from([0]), {
        headers: { 'Content-Type': 'image/png' },
      })
    }
    if (url.startsWith('/api/user-tools/image-playground/changes?cursor=20')) {
      return jsonEnvelope({
        items: [],
        assets: [],
        next_cursor: 20,
        has_more: false,
      })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      putImage: async (value) => storedImages.push(value),
      putTask: async (value) => storedTasks.push(value),
    },
    globals: { caches, fetch },
  })

  const result = await module.initializeNewApiImagePlaygroundSync()

  assert.equal(result.dataChanged, true)
  assert.deepEqual(JSON.parse(JSON.stringify(storedImages)), [{
    id: 'remote-image',
    dataUrl: 'data:image/png;base64,AA==',
    createdAt: 100,
    source: 'generated',
    width: 1,
    height: 1,
  }])
  assert.deepEqual(JSON.parse(JSON.stringify(storedTasks)), [task.payload])
  assert.deepEqual(cachedAssets, [`/api/user-tools/assets/${assetId}/content`])
  assert.equal(requests.findIndex((url) => url.includes('/bootstrap?')) < requests.findIndex((url) => url.includes('/changes?cursor=20')), true)
  assert.equal(syncBodies.length, 1)
  assert.deepEqual(
    syncBodies[0].mutations.map(({ kind, key, deleted }) => ({ kind, key, deleted })),
    [{ kind: 'state', key: 'app', deleted: false }],
  )
  assert.equal(
    syncBodies[0].mutations.some((mutation) => mutation.deleted && (mutation.kind === 'image' || mutation.kind === 'task')),
    false,
  )
})


test('keeps browser-owned OpenAI running tasks local while syncing recoverable and terminal tasks', async () => {
  const storedTasks = []
  const syncBodies = []
  const localTasks = [
    { id: 'local-openai-running', status: 'running', apiProvider: 'openai', createdAt: 1 },
    { id: 'local-fal-running', status: 'running', apiProvider: 'fal', falRequestId: 'fal-1', createdAt: 2 },
    { id: 'local-custom-running', status: 'running', apiProvider: 'custom', customTaskId: 'custom-1', createdAt: 3 },
    { id: 'local-done', status: 'done', apiProvider: 'openai', createdAt: 4 },
  ]
  const remoteOpenAIRunning = remoteItem({
    kind: 'task',
    key: 'remote-openai-running',
    status: 'running',
    payload: { id: 'remote-openai-running', status: 'running', apiProvider: 'openai', createdAt: 5 },
  })
  const remoteFalRunning = remoteItem({
    kind: 'task',
    key: 'remote-fal-running',
    status: 'running',
    payload: { id: 'remote-fal-running', status: 'running', apiProvider: 'fal', falRequestId: 'fal-2', createdAt: 6 },
  })
  const remoteCustomRunning = remoteItem({
    kind: 'task',
    key: 'remote-custom-running',
    status: 'running',
    payload: { id: 'remote-custom-running', status: 'running', apiProvider: 'custom', customTaskId: 'custom-2', createdAt: 7 },
  })
  const remoteDone = remoteItem({
    kind: 'task',
    key: 'remote-done',
    status: 'done',
    payload: { id: 'remote-done', status: 'done', apiProvider: 'openai', createdAt: 8 },
  })
  const fetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    if (url === '/api/user-tools/image-playground/sync') {
      syncBodies.push(JSON.parse(init.body))
      return jsonEnvelope({ results: [], cursor: 0 })
    }
    if (url.startsWith('/api/user-tools/image-playground/bootstrap?')) {
      return jsonEnvelope({
        items: [remoteOpenAIRunning, remoteFalRunning, remoteCustomRunning, remoteDone],
        assets: [],
        cursor: 12,
        next_after_id: '',
        has_more: false,
      })
    }
    if (url.startsWith('/api/user-tools/image-playground/changes?cursor=12')) {
      return jsonEnvelope({ items: [], assets: [], next_cursor: 12, has_more: false })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      getAllTasks: async () => localTasks,
      putTask: async (task) => storedTasks.push(task),
    },
    globals: { fetch },
  })

  const result = await module.initializeNewApiImagePlaygroundSync()

  const taskMutations = syncBodies.flatMap((body) => body.mutations.filter((mutation) => mutation.kind === 'task'))
  assert.deepEqual(taskMutations.map((mutation) => mutation.key).sort(), [
    'local-custom-running',
    'local-done',
    'local-fal-running',
  ])
  assert.deepEqual(storedTasks.map((task) => task.id).sort(), [
    'remote-custom-running',
    'remote-done',
    'remote-fal-running',
  ])
  assert.equal(result.dataChanged, true)
})

test('performSync removes sensitive fields and credential-shaped values from the final sync request body', async () => {
  const metadataKey = 'new-api-image-playground-metadata-user-7'
  const storageKey = 'new-api-image-playground-state-user-7'
  const localStorage = createLocalStorage({
    [metadataKey]: JSON.stringify({ cursor: 12, entries: {} }),
    [storageKey]: JSON.stringify({
      version: 2,
      state: {
        params: {
          apiKey: 'sk-state-api-key-12345678',
          APIKey: 'sk-state-upper-api-key-12345678',
          baseUrl: 'https://state-base.example.test',
          baseURL: 'https://state-base-upper.example.test',
          apiUrl: 'https://state-api.example.test',
          authorization: 'Bearer state-authorization-12345678',
          accessToken: 'state-access-token-12345678',
          refreshToken: 'state-refresh-token-12345678',
          token: 'state-token-12345678',
          agentToken: 'state-agent-token-12345678',
          canvasAgentToken: 'state-canvas-agent-token-12345678',
          runtimeCredential: 'utrs_state-runtime-credential-12345678',
          credential: 'state-credential-12345678',
          password: 'state-password-12345678',
          secret: 'state-secret-12345678',
          webdavUrl: 'https://state-webdav.example.test',
          webdavUsername: 'state-webdav-user',
          webdavPassword: 'state-webdav-password-12345678',
          privateKey: 'state-private-key-12345678',
          documentationUrl: 'https://docs.example.test/image-playground',
          ordinaryText: 'Keep this ordinary state text unchanged.',
          token_count: 42,
          tokenCount: 43,
          jsonConfig: JSON.stringify({
            apiKey: 'sk-state-json-api-key-12345678',
            nested: {
              authorization: 'Basic c3RhdGUtanNvbjpzZWNyZXQ=',
              model: 'gpt-image-2',
              token_count: 44,
              documentationUrl: 'https://docs.example.test/json-config',
            },
          }),
        },
        prompt: 'Ordinary state prompt with https://images.example.test/reference.png',
      },
    }),
  })
  const tasks = [{
    id: 'sensitive-task',
    status: 'failed',
    createdAt: 100,
    apiKey: 'sk-task-api-key-12345678',
    nested: {
      APIKey: 'sk-task-upper-api-key-12345678',
      baseUrl: 'https://task-base.example.test',
      baseURL: 'https://task-base-upper.example.test',
      apiUrl: 'https://task-api.example.test',
      authorization: 'Bearer task-authorization-12345678',
      accessToken: 'task-access-token-12345678',
      refreshToken: 'task-refresh-token-12345678',
      token: 'task-token-12345678',
      agentToken: 'task-agent-token-12345678',
      canvasAgentToken: 'task-canvas-agent-token-12345678',
      runtimeCredential: 'utrs_task-runtime-credential-12345678',
      credential: 'task-credential-12345678',
      password: 'task-password-12345678',
      secret: 'task-secret-12345678',
      webdavUrl: 'https://task-webdav.example.test',
      webdavUsername: 'task-webdav-user',
      webdavPassword: 'task-webdav-password-12345678',
      privateKey: 'task-private-key-12345678',
    },
    error: 'Upstream exposed sk-task-error-12345678, utrs_task-error-12345678, Bearer task-error-bearer-12345678 and Basic dGFzazpzZWNyZXQ=',
    rawJson: JSON.stringify({
      apiKey: 'sk-task-json-api-key-12345678',
      nested: {
        token: 'task-json-token-12345678',
        prompt: 'Keep JSON task text.',
        token_count: 45,
      },
    }),
    documentationUrl: 'https://docs.example.test/tasks',
    ordinaryText: 'Keep this ordinary task text unchanged.',
    token_count: 46,
    tokenCount: 47,
  }]
  const conversations = [{
    id: 'sensitive-conversation',
    createdAt: 101,
    messages: [
      {
        role: 'user',
        content: 'Credential dump: sk-conversation-message-12345678 and utrs_conversation-message-12345678',
      },
      {
        role: 'assistant',
        content: 'Bearer conversation-bearer-12345678',
      },
      {
        role: 'tool',
        content: JSON.stringify({
          canvasAgentToken: 'conversation-json-agent-token-12345678',
          privateKey: 'conversation-json-private-key-12345678',
          result: 'Keep JSON conversation result.',
          token_count: 48,
        }),
      },
    ],
    config: {
      apiKey: 'sk-conversation-api-key-12345678',
      baseURL: 'https://conversation-base.example.test',
      webdavPassword: 'conversation-webdav-password-12345678',
    },
    documentationUrl: 'https://docs.example.test/conversations',
    ordinaryText: 'Keep this ordinary conversation text unchanged.',
    token_count: 49,
  }]
  const syncBodies = []
  const fetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    if (url === '/api/user-tools/image-playground/sync') {
      syncBodies.push(JSON.parse(init.body))
      return jsonEnvelope({ results: [], cursor: 12 })
    }
    if (url.startsWith('/api/user-tools/image-playground/changes?cursor=12')) {
      return jsonEnvelope({
        items: [],
        assets: [],
        next_cursor: 12,
        has_more: false,
      })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      getAllTasks: async () => tasks,
      getAllAgentConversations: async () => conversations,
    },
    globals: { localStorage, fetch },
  })

  await module.initializeNewApiImagePlaygroundSync()

  assert.equal(syncBodies.length, 1)
  const body = syncBodies[0]
  const payloadByKind = Object.fromEntries(
    body.mutations.map((mutation) => [mutation.kind, mutation.payload]),
  )
  assert.deepEqual(
    body.mutations.map((mutation) => mutation.kind).sort(),
    ['agent-conversation', 'state', 'task'],
  )

  const sensitiveKeys = new Set([
    'apikey',
    'baseurl',
    'apiurl',
    'authorization',
    'accesstoken',
    'refreshtoken',
    'token',
    'agenttoken',
    'canvasagenttoken',
    'runtimecredential',
    'credential',
    'password',
    'secret',
    'webdavurl',
    'webdavusername',
    'webdavpassword',
    'privatekey',
  ])
  const inspectKeys = (value) => {
    if (Array.isArray(value)) {
      value.forEach(inspectKeys)
      return
    }
    if (!value || typeof value !== 'object') return
    for (const [key, child] of Object.entries(value)) {
      assert.equal(
        sensitiveKeys.has(key.toLowerCase().replace(/[^a-z0-9]/g, '')),
        false,
        `sensitive field reached /sync: ${key}`,
      )
      inspectKeys(child)
    }
  }
  inspectKeys(body)

  const serializedBody = JSON.stringify(body)
  for (const secret of [
    'sk-state-api-key-12345678',
    'sk-state-upper-api-key-12345678',
    'state-access-token-12345678',
    'utrs_state-runtime-credential-12345678',
    'sk-state-json-api-key-12345678',
    'c3RhdGUtanNvbjpzZWNyZXQ=',
    'sk-task-api-key-12345678',
    'sk-task-upper-api-key-12345678',
    'task-token-12345678',
    'utrs_task-runtime-credential-12345678',
    'sk-task-error-12345678',
    'utrs_task-error-12345678',
    'task-error-bearer-12345678',
    'dGFzazpzZWNyZXQ=',
    'sk-task-json-api-key-12345678',
    'task-json-token-12345678',
    'sk-conversation-message-12345678',
    'utrs_conversation-message-12345678',
    'conversation-bearer-12345678',
    'conversation-json-agent-token-12345678',
    'conversation-json-private-key-12345678',
    'sk-conversation-api-key-12345678',
    'conversation-webdav-password-12345678',
  ]) {
    assert.equal(serializedBody.includes(secret), false, `sensitive value reached /sync: ${secret}`)
  }
  assert.doesNotMatch(serializedBody, /\bsk-[A-Za-z0-9_-]{8,}\b/)
  assert.doesNotMatch(serializedBody, /\butrs_[A-Za-z0-9._-]{8,}\b/)
  assert.doesNotMatch(serializedBody, /\b(?:Bearer|Basic)\s+\S+/i)

  assert.equal(payloadByKind.task.documentationUrl, 'https://docs.example.test/tasks')
  assert.equal(payloadByKind.task.ordinaryText, 'Keep this ordinary task text unchanged.')
  assert.equal(payloadByKind.task.token_count, 46)
  assert.equal(payloadByKind.task.tokenCount, 47)
  assert.deepEqual(JSON.parse(payloadByKind.task.rawJson), {
    nested: {
      prompt: 'Keep JSON task text.',
      token_count: 45,
    },
  })

  assert.equal(payloadByKind['agent-conversation'].documentationUrl, 'https://docs.example.test/conversations')
  assert.equal(payloadByKind['agent-conversation'].ordinaryText, 'Keep this ordinary conversation text unchanged.')
  assert.equal(payloadByKind['agent-conversation'].token_count, 49)
  assert.deepEqual(JSON.parse(payloadByKind['agent-conversation'].messages[2].content), {
    result: 'Keep JSON conversation result.',
    token_count: 48,
  })

  assert.equal(payloadByKind.state.params.documentationUrl, 'https://docs.example.test/image-playground')
  assert.equal(payloadByKind.state.params.ordinaryText, 'Keep this ordinary state text unchanged.')
  assert.equal(payloadByKind.state.params.token_count, 42)
  assert.equal(payloadByKind.state.params.tokenCount, 43)
  assert.deepEqual(JSON.parse(payloadByKind.state.params.jsonConfig), {
    nested: {
      model: 'gpt-image-2',
      token_count: 44,
      documentationUrl: 'https://docs.example.test/json-config',
    },
  })
  assert.equal(payloadByKind.state.prompt, 'Ordinary state prompt with https://images.example.test/reference.png')
})

test('restores a locally evicted image from its server asset without uploading a tombstone', async () => {
  const dataUrl = 'data:image/png;base64,AA=='
  const assetId = 'asset-restored-after-local-eviction'
  const metadataKey = 'new-api-image-playground-metadata-user-7'
  const localStorage = createLocalStorage({
    [metadataKey]: JSON.stringify({
      cursor: 10,
      entries: {
        ['image\u0000evicted-image']: {
          revision: 3,
          hash: 'server-image-hash',
          deleted: false,
          asset_id: assetId,
          encoded_length: dataUrl.length,
          content_sha256: sha256(Buffer.from([0])),
        },
        ['state\u0000app']: {
          revision: 1,
          hash: sha256('{}'),
          deleted: false,
        },
      },
    }),
  })
  const images = new Map()
  const syncBodies = []
  const requests = []
  const fetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    requests.push(url)
    if (url === `/api/user-tools/assets/${assetId}/content`) {
      return new Response(Uint8Array.from([0]), {
        headers: { 'Content-Type': 'image/png' },
      })
    }
    if (url === '/api/user-tools/image-playground/sync') {
      syncBodies.push(JSON.parse(init.body))
      return jsonEnvelope({ results: [], cursor: 10 })
    }
    if (url.startsWith('/api/user-tools/image-playground/changes?cursor=10')) {
      return jsonEnvelope({
        items: [],
        assets: [],
        next_cursor: 10,
        has_more: false,
      })
    }
    throw new Error(`Unexpected request: ${url}`)
  }
  const { module } = loadSyncModule({
    db: {
      getAllImageIds: async () => [...images.keys()],
      getImage: async (id) => images.get(id),
      putImage: async (image) => images.set(image.id, image),
    },
    globals: { localStorage, fetch },
  })

  await module.initializeNewApiImagePlaygroundSync()

  assert.equal(images.get('evicted-image').dataUrl, dataUrl)
  assert.equal(syncBodies.length, 0)
  assert.deepEqual(requests, [
    `/api/user-tools/assets/${assetId}/content`,
    '/api/user-tools/image-playground/changes?cursor=10&limit=500',
  ])
})

test('checkpoints successful uploads so a later 429 retry does not re-upload earlier images', async () => {
  const metadataKey = 'new-api-image-playground-metadata-user-7'
  const localStorage = createLocalStorage()
  const images = new Map([
    ['image-a', { id: 'image-a', dataUrl: 'data:image/png;base64,AA==', createdAt: 1, source: 'generated' }],
    ['image-b', { id: 'image-b', dataUrl: 'data:image/png;base64,AQ==', createdAt: 2, source: 'generated' }],
  ])
  const db = {
    getAllImageIds: async () => [...images.keys()],
    getImage: async (id) => images.get(id),
  }
  const firstUploadNames = []
  const firstFetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    if (url === '/api/user-tools/assets/uploads') {
      firstUploadNames.push(new Headers(init.headers).get('X-File-Name'))
      if (firstUploadNames.length === 2) return new Response('', { status: 429 })
      return jsonEnvelope({
        id: 'asset-image-a',
        sha256: new Headers(init.headers).get('X-Content-SHA256'),
        filename: firstUploadNames[0],
        content_type: new Headers(init.headers).get('Content-Type'),
        size_bytes: init.body.size,
        created_at: 1,
        updated_at: 1,
      })
    }
    throw new Error(`Unexpected request during first sync: ${url}`)
  }
  const first = loadSyncModule({ db, globals: { localStorage, fetch: firstFetch } })

  await first.module.initializeNewApiImagePlaygroundSync()

  assert.deepEqual(firstUploadNames, ['image-a.png', 'image-b.png'])
  const checkpoint = JSON.parse(localStorage.value(metadataKey))
    .entries['image\u0000image-a']
  assert.equal(checkpoint.pending_asset_id, 'asset-image-a')
  assert.equal(checkpoint.pending_content_sha256, sha256(Buffer.from([0])))

  const secondUploadNames = []
  const syncBodies = []
  const secondFetch = async (input, init = {}) => {
    const url = typeof input === 'string' ? input : input.url
    if (url === '/api/user-tools/assets/uploads') {
      const filename = new Headers(init.headers).get('X-File-Name')
      secondUploadNames.push(filename)
      return jsonEnvelope({
        id: 'asset-image-b',
        sha256: new Headers(init.headers).get('X-Content-SHA256'),
        filename,
        content_type: new Headers(init.headers).get('Content-Type'),
        size_bytes: init.body.size,
        created_at: 2,
        updated_at: 2,
      })
    }
    if (url === '/api/user-tools/image-playground/sync') {
      const body = JSON.parse(init.body)
      syncBodies.push(body)
      return jsonEnvelope({
        results: body.mutations.map((mutation) => ({
          client_mutation_id: mutation.client_mutation_id,
          kind: mutation.kind,
          key: mutation.key,
          result: 'applied',
          item: remoteItem({
            kind: mutation.kind,
            key: mutation.key,
            revision: 1,
            status: mutation.status,
            payload: mutation.payload,
            asset_ids: mutation.asset_ids,
            deleted: mutation.deleted,
          }),
        })),
        cursor: 1,
      })
    }
    if (url.startsWith('/api/user-tools/image-playground/bootstrap?')) {
      return jsonEnvelope({
        items: [],
        assets: [],
        cursor: 1,
        next_after_id: '',
        has_more: false,
      })
    }
    if (url.startsWith('/api/user-tools/image-playground/changes?cursor=1')) {
      return jsonEnvelope({
        items: [],
        assets: [],
        next_cursor: 1,
        has_more: false,
      })
    }
    throw new Error(`Unexpected request during resumed sync: ${url}`)
  }
  const second = loadSyncModule({ db, globals: { localStorage, fetch: secondFetch } })

  await second.module.initializeNewApiImagePlaygroundSync()

  assert.deepEqual(secondUploadNames, ['image-b.png'])
  assert.equal(syncBodies.length, 1)
  const imageMutations = syncBodies[0].mutations.filter((mutation) => mutation.kind === 'image')
  assert.deepEqual(imageMutations.map((mutation) => mutation.asset_ids[0]), [
    'asset-image-a',
    'asset-image-b',
  ])
  const entries = JSON.parse(localStorage.value(metadataKey)).entries
  assert.equal(entries['image\u0000image-a'].pending_asset_id, undefined)
  assert.equal(entries['image\u0000image-a'].asset_id, 'asset-image-a')
  assert.equal(entries['image\u0000image-b'].asset_id, 'asset-image-b')
})
