import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import vm from 'node:vm'

const require = createRequire(import.meta.url)
const ts = require('../../web/default/node_modules/typescript/lib/typescript.js')
const source = await readFile(new URL('./new-api-bridge.ts', import.meta.url), 'utf8')

function createStorage() {
  const values = new Map()
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
    serializedValues() {
      return JSON.stringify([...values.entries()])
    },
  }
}

function createDefaultProfile() {
  return {
    id: 'default',
    name: 'Default',
    provider: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    model: 'gpt-image-1',
    apiMode: 'images',
    codexCli: false,
    apiProxy: false,
    streamImages: false,
  }
}

function createDefaultSettings() {
  return {
    profiles: [createDefaultProfile()],
    activeProfileId: 'default',
    agentApiConfigMode: 'off',
    agentTextProfileId: null,
    agentImageProfileId: 'default',
  }
}

function loadBridge() {
  const listeners = new Map()
  const hydrationListeners = new Set()
  const storeListeners = new Set()
  let afterNextSetSettings = null
  const parent = {
    messages: [],
    postMessage(message, origin) {
      this.messages.push({ message, origin })
    },
  }
  const localStorage = createStorage()
  const window = {
    parent,
    location: { origin: 'https://new-api.example.com' },
    localStorage,
    addEventListener(type, listener) {
      listeners.set(type, listener)
    },
  }
  let state = {
    settings: createDefaultSettings(),
    setSettings(patch) {
      state.settings = { ...state.settings, ...patch }
      for (const listener of storeListeners) listener(state)
      const callback = afterNextSetSettings
      afterNextSetSettings = null
      callback?.()
    },
  }
  const useStore = {
    getState() {
      return state
    },
    persist: {
      onFinishHydration(listener) {
        hydrationListeners.add(listener)
        return () => hydrationListeners.delete(listener)
      },
    },
    subscribe(listener) {
      storeListeners.add(listener)
      return () => storeListeners.delete(listener)
    },
  }

  const output = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
    fileName: 'new-api-bridge.ts',
  }).outputText
  const module = { exports: {} }
  const context = vm.createContext({
    module,
    exports: module.exports,
    URL,
    window,
  })
  const wrapper = vm.runInContext(`(function (require, module, exports) { ${output}\n})`, context)
  wrapper((specifier) => {
    if (specifier === '../store') return { useStore }
    if (specifier === './apiProfiles') {
      return {
        createDefaultOpenAIProfile(options = {}) {
          return { ...createDefaultProfile(), ...options }
        },
        DEFAULT_IMAGES_MODEL: 'gpt-image-1',
        DEFAULT_RESPONSES_MODEL: 'gpt-5.4',
      }
    }
    throw new Error(`Unexpected import: ${specifier}`)
  }, module, module.exports)

  return {
    bridge: module.exports,
    dispatch(data) {
      listeners.get('message')?.({
        origin: window.location.origin,
        source: parent,
        data,
      })
    },
    finishHydration(settings = createDefaultSettings()) {
      state.settings = settings
      for (const listener of storeListeners) listener(state)
      for (const listener of hydrationListeners) listener(state)
    },
    replaceSettings(settings) {
      state.settings = settings
      for (const listener of storeListeners) listener(state)
    },
    runAfterNextSetSettings(callback) {
      afterNextSetSettings = callback
    },
    getSettings() {
      return state.settings
    },
    hydrationListeners,
    localStorage,
    parent,
  }
}

function newApiConfiguration(overrides = {}) {
  return {
    source: 'new-api',
    type: 'new-api:image-playground:configure',
    mode: 'new-api',
    apiUrl: 'https://new-api.example.com/pg',
    apiKey: 'utrs_runtime-session',
    apiMode: 'images',
    profileName: 'New API · test2',
    ...overrides,
  }
}

function assertManagedNewApiSettings(settings) {
  assert.equal(settings.activeProfileId, 'new-api-managed')
  assert.equal(settings.agentApiConfigMode, 'hybrid')
  assert.equal(settings.agentTextProfileId, 'new-api-managed-agent')
  assert.equal(settings.agentImageProfileId, 'new-api-managed')

  const imageProfile = settings.profiles.find((profile) => profile.id === 'new-api-managed')
  const agentProfile = settings.profiles.find((profile) => profile.id === 'new-api-managed-agent')
  assert.deepEqual(imageProfile && {
    name: imageProfile.name,
    provider: imageProfile.provider,
    baseUrl: imageProfile.baseUrl,
    apiMode: imageProfile.apiMode,
    apiKey: imageProfile.apiKey,
    streamImages: imageProfile.streamImages,
  }, {
    name: 'New API · test2',
    provider: 'openai',
    baseUrl: 'https://new-api.example.com/pg',
    apiMode: 'images',
    apiKey: 'utrs_runtime-session',
    streamImages: true,
  })
  assert.deepEqual(agentProfile && {
    name: agentProfile.name,
    provider: agentProfile.provider,
    baseUrl: agentProfile.baseUrl,
    apiMode: agentProfile.apiMode,
    apiKey: agentProfile.apiKey,
    streamImages: agentProfile.streamImages,
  }, {
    name: 'New API · test2 · Agent',
    provider: 'openai',
    baseUrl: 'https://new-api.example.com/pg',
    apiMode: 'responses',
    apiKey: 'utrs_runtime-session',
    streamImages: true,
  })
}

test('reapplies managed Images and Responses profiles after every persisted-state hydration', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()

  assert.equal(harness.hydrationListeners.size, 1)
  harness.dispatch(newApiConfiguration())
  assertManagedNewApiSettings(harness.getSettings())

  harness.finishHydration()
  assertManagedNewApiSettings(harness.getSettings())
  assert.equal(harness.localStorage.serializedValues().includes('utrs_runtime-session'), false)
})

test('keeps the managed profile active when hydration finishes during configure application', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.runAfterNextSetSettings(() => harness.finishHydration())

  harness.dispatch(newApiConfiguration())

  assertManagedNewApiSettings(harness.getSettings())
})

test('repairs managed settings overwritten by another store refresh while New API mode is active', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration())

  harness.replaceSettings(createDefaultSettings())

  assertManagedNewApiSettings(harness.getSettings())
})

test('updates both managed profiles when the host refreshes the runtime credential and label', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration())

  harness.dispatch(newApiConfiguration({
    apiKey: 'utrs_refreshed-session',
    profileName: 'New API · test2 · A组',
  }))

  const settings = harness.getSettings()
  const imageProfile = settings.profiles.find((profile) => profile.id === 'new-api-managed')
  const agentProfile = settings.profiles.find((profile) => profile.id === 'new-api-managed-agent')
  assert.deepEqual(imageProfile && {
    name: imageProfile.name,
    apiMode: imageProfile.apiMode,
    apiKey: imageProfile.apiKey,
  }, {
    name: 'New API · test2 · A组',
    apiMode: 'images',
    apiKey: 'utrs_refreshed-session',
  })
  assert.deepEqual(agentProfile && {
    name: agentProfile.name,
    apiMode: agentProfile.apiMode,
    apiKey: agentProfile.apiKey,
  }, {
    name: 'New API · test2 · A组 · Agent',
    apiMode: 'responses',
    apiKey: 'utrs_refreshed-session',
  })
  assert.equal(harness.localStorage.serializedValues().includes('utrs_refreshed-session'), false)
})

test('keeps user-created third-party profiles editable and selected beside read-only managed profiles', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration())

  const customProfile = {
    ...createDefaultProfile(),
    id: 'custom-third-party',
    name: '第三方图片服务',
    baseUrl: 'https://third-party.example.com/v1',
    apiKey: 'sk-local-test-only',
    model: 'third-party-image-model',
  }
  const customAgentProfile = {
    ...createDefaultProfile(),
    id: 'custom-third-party-agent',
    name: '第三方 Agent 服务',
    baseUrl: 'https://third-party-agent.example.com/v1',
    apiKey: 'sk-local-agent-test-only',
    model: 'third-party-agent-model',
    apiMode: 'responses',
  }
  const settingsWithCustomProfiles = structuredClone(harness.getSettings())
  settingsWithCustomProfiles.profiles.push(customProfile, customAgentProfile)
  settingsWithCustomProfiles.activeProfileId = customProfile.id
  settingsWithCustomProfiles.agentApiConfigMode = 'native'
  settingsWithCustomProfiles.agentTextProfileId = customAgentProfile.id
  settingsWithCustomProfiles.agentImageProfileId = customProfile.id
  harness.replaceSettings(settingsWithCustomProfiles)

  const editedSettings = structuredClone(harness.getSettings())
  const editedCustomProfile = editedSettings.profiles.find((profile) => profile.id === customProfile.id)
  editedCustomProfile.name = '第三方图片服务（已编辑）'
  editedCustomProfile.baseUrl = 'https://edited-third-party.example.com/v1'
  editedCustomProfile.apiKey = 'sk-local-edited-test-only'
  harness.replaceSettings(editedSettings)
  harness.dispatch(newApiConfiguration({
    apiKey: 'utrs_refreshed-session',
    profileName: 'New API · refreshed',
  }))

  const settings = harness.getSettings()
  assert.equal(settings.activeProfileId, customProfile.id)
  assert.equal(settings.agentApiConfigMode, 'native')
  assert.equal(settings.agentTextProfileId, customAgentProfile.id)
  assert.equal(settings.agentImageProfileId, customProfile.id)
  assert.deepEqual(settings.profiles.find((profile) => profile.id === customProfile.id), editedCustomProfile)
  assert.deepEqual(settings.profiles.find((profile) => profile.id === customAgentProfile.id), customAgentProfile)
  assert.equal(
    settings.profiles.find((profile) => profile.id === 'new-api-managed')?.apiKey,
    'utrs_refreshed-session',
  )
  assert.equal(
    settings.profiles.find((profile) => profile.id === 'new-api-managed-agent')?.apiKey,
    'utrs_refreshed-session',
  )

  harness.replaceSettings({
    ...settings,
    profiles: settings.profiles.filter((profile) => profile.id !== customProfile.id),
    activeProfileId: 'new-api-managed',
  })
  const managedSettings = harness.getSettings()
  assert.equal(managedSettings.activeProfileId, 'new-api-managed')
  assert.equal(managedSettings.agentApiConfigMode, 'hybrid')
  assert.equal(managedSettings.agentTextProfileId, 'new-api-managed-agent')
  assert.equal(managedSettings.agentImageProfileId, 'new-api-managed')
  assert.equal(
    managedSettings.profiles.find((profile) => profile.id === 'new-api-managed')?.apiKey,
    'utrs_refreshed-session',
  )
})

test('normalizes the managed New API endpoint to the exact same-origin /pg route', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration({ apiUrl: 'https://new-api.example.com/pg/' }))

  assertManagedNewApiSettings(harness.getSettings())
})

test('rejects persistent keys and every New API endpoint outside the restricted /pg route', () => {
  const invalidConfigurations = [
    { apiKey: 'sk-persistent-user-key' },
    { apiUrl: 'https://new-api.example.com/v1' },
    { apiUrl: 'https://new-api.example.com/pg?route=v1' },
    { apiUrl: 'https://new-api.example.com/pg#fragment' },
    { apiUrl: 'https://attacker.example.com/pg' },
  ]

  for (const overrides of invalidConfigurations) {
    const harness = loadBridge()
    harness.bridge.installNewApiBridge()
    harness.dispatch(newApiConfiguration(overrides))
    harness.finishHydration()

    assert.equal(
      harness.getSettings().profiles.some((profile) => profile.id.startsWith('new-api-managed')),
      false
    )
    assert.equal(
      harness.parent.messages.some(({ message }) => message.type === 'new-api:image-playground:configured'),
      false
    )
  }
})

test('clears the in-memory managed configuration when switching back to an unmanaged tool profile', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration())

  harness.dispatch({
    source: 'new-api',
    type: 'new-api:image-playground:configure',
    mode: 'tool',
  })
  harness.finishHydration()

  assert.equal(
    harness.getSettings().profiles.some((profile) => profile.id.startsWith('new-api-managed')),
    false
  )
})

test('does not retain an invalid configuration for a later hydration callback', () => {
  const harness = loadBridge()
  harness.bridge.installNewApiBridge()
  harness.dispatch(newApiConfiguration({ apiKey: '' }))
  harness.finishHydration()

  assert.equal(
    harness.getSettings().profiles.some((profile) => profile.id.startsWith('new-api-managed')),
    false
  )
  assert.equal(
    harness.parent.messages.some(({ message }) => message.type === 'new-api:image-playground:configured'),
    false
  )
})
