import type { AgentApiConfigMode, ApiMode, ApiProfile, AppSettings } from '../types'
import {
  createDefaultOpenAIProfile,
  DEFAULT_IMAGES_MODEL,
  DEFAULT_RESPONSES_MODEL,
} from './apiProfiles'
import { useStore } from '../store'

const MANAGED_IMAGE_PROFILE_ID = 'new-api-managed'
const MANAGED_AGENT_PROFILE_ID = 'new-api-managed-agent'
const MANAGED_PROFILE_IDS = new Set([
  MANAGED_IMAGE_PROFILE_ID,
  MANAGED_AGENT_PROFILE_ID,
])
const TOOL_PROFILE_STORAGE_KEY = 'new-api:image-playground:tool-profile-id'
const TOOL_SETTINGS_STORAGE_KEY = 'new-api:image-playground:tool-settings'
const MESSAGE_SOURCE = 'new-api'
const CHILD_SOURCE = 'gpt-image-playground'

type BridgeMode = 'new-api' | 'tool'

interface ConfigureMessage {
  source: typeof MESSAGE_SOURCE
  type: 'new-api:image-playground:configure'
  mode: BridgeMode
  apiUrl?: string
  apiKey?: string
  apiMode?: ApiMode
  profileName?: string
}

interface ToolSettingsSnapshot {
  activeProfileId: string
  agentApiConfigMode: AgentApiConfigMode
  agentTextProfileId: string | null
  agentImageProfileId: string | null
}

interface ManagedConfiguration {
  apiUrl: string
  apiKey: string
  profileName: string
}

let activeConfigureMessage: ConfigureMessage | null = null

function isConfigureMessage(value: unknown): value is ConfigureMessage {
  if (!value || typeof value !== 'object') return false
  const message = value as Record<string, unknown>
  return message.source === MESSAGE_SOURCE &&
    message.type === 'new-api:image-playground:configure' &&
    (message.mode === 'new-api' || message.mode === 'tool')
}

function isManagedProfile(profile: ApiProfile) {
  return MANAGED_PROFILE_IDS.has(profile.id)
}

function getManagedConfiguration(message: ConfigureMessage): ManagedConfiguration | null {
  if (typeof message.apiUrl !== 'string' || typeof message.apiKey !== 'string') return null
  let apiUrl = message.apiUrl.trim().replace(/\/+$/, '')
  const apiKey = message.apiKey.trim()
  if (!apiUrl || !apiKey) return null

  let parsedUrl: URL
  try {
    parsedUrl = new URL(apiUrl, window.location.origin)
  } catch {
    return null
  }
  if (parsedUrl.protocol !== 'http:' && parsedUrl.protocol !== 'https:') return null
  if (message.mode === 'new-api') {
    const normalizedPathname = parsedUrl.pathname.replace(/\/+$/, '') || '/'
    if (
      parsedUrl.origin !== window.location.origin ||
      normalizedPathname !== '/pg' ||
      parsedUrl.search !== '' ||
      parsedUrl.hash !== '' ||
      !apiKey.startsWith('utrs_')
    ) {
      return null
    }
    apiUrl = `${parsedUrl.origin}/pg`
  }

  return {
    apiUrl,
    apiKey,
    profileName: message.profileName?.trim()
      || (message.mode === 'new-api' ? 'New API' : 'Custom API'),
  }
}

function managedProfilesMatch(settings: AppSettings, message: ConfigureMessage) {
  const configuration = getManagedConfiguration(message)
  if (!configuration) return false

  const imageProfile = settings.profiles.find((profile) => profile.id === MANAGED_IMAGE_PROFILE_ID)
  if (
    !imageProfile ||
    imageProfile.name !== configuration.profileName ||
    imageProfile.provider !== 'openai' ||
    imageProfile.baseUrl !== configuration.apiUrl ||
    imageProfile.apiKey !== configuration.apiKey ||
    imageProfile.apiMode !== 'images' ||
    !imageProfile.model ||
    imageProfile.codexCli ||
    imageProfile.apiProxy ||
    imageProfile.streamImages !== (message.mode === 'new-api')
  ) {
    return false
  }

  const agentProfile = settings.profiles.find((profile) => profile.id === MANAGED_AGENT_PROFILE_ID)
  if (message.mode === 'tool') {
    if (agentProfile) return false
    if (!MANAGED_PROFILE_IDS.has(settings.activeProfileId)) return true
    return settings.activeProfileId === MANAGED_IMAGE_PROFILE_ID &&
      settings.agentApiConfigMode === 'off' &&
      settings.agentTextProfileId === null &&
      settings.agentImageProfileId === MANAGED_IMAGE_PROFILE_ID
  }

  if (
    !agentProfile ||
    agentProfile.name !== `${configuration.profileName} · Agent` ||
    agentProfile.provider !== 'openai' ||
    agentProfile.baseUrl !== configuration.apiUrl ||
    agentProfile.apiKey !== configuration.apiKey ||
    agentProfile.apiMode !== 'responses' ||
    !agentProfile.model ||
    agentProfile.codexCli ||
    agentProfile.apiProxy ||
    !agentProfile.streamImages
  ) {
    return false
  }

  if (!MANAGED_PROFILE_IDS.has(settings.activeProfileId)) return true
  return settings.activeProfileId === MANAGED_IMAGE_PROFILE_ID &&
    settings.agentApiConfigMode === 'hybrid' &&
    settings.agentTextProfileId === MANAGED_AGENT_PROFILE_ID &&
    settings.agentImageProfileId === MANAGED_IMAGE_PROFILE_ID
}

function postToParent(type: 'ready' | 'configured', mode?: BridgeMode) {
  window.parent.postMessage({
    source: CHILD_SOURCE,
    type: `new-api:image-playground:${type}`,
    ...(mode ? { mode } : {}),
  }, window.location.origin)
}

function readToolSettingsSnapshot(): ToolSettingsSnapshot | null {
  try {
    const raw = window.localStorage.getItem(TOOL_SETTINGS_STORAGE_KEY)
    if (!raw) return null
    const value = JSON.parse(raw) as Record<string, unknown>
    const agentApiConfigMode = value.agentApiConfigMode
    if (agentApiConfigMode !== 'off' && agentApiConfigMode !== 'native' && agentApiConfigMode !== 'hybrid') {
      return null
    }
    if (typeof value.activeProfileId !== 'string') return null
    return {
      activeProfileId: value.activeProfileId,
      agentApiConfigMode,
      agentTextProfileId: typeof value.agentTextProfileId === 'string' ? value.agentTextProfileId : null,
      agentImageProfileId: typeof value.agentImageProfileId === 'string' ? value.agentImageProfileId : null,
    }
  } catch {
    return null
  }
}

function rememberToolSettings(settings: AppSettings) {
  const snapshot: ToolSettingsSnapshot = {
    activeProfileId: settings.activeProfileId,
    agentApiConfigMode: settings.agentApiConfigMode,
    agentTextProfileId: settings.agentTextProfileId ?? null,
    agentImageProfileId: settings.agentImageProfileId ?? null,
  }
  try {
    window.localStorage.setItem(TOOL_SETTINGS_STORAGE_KEY, JSON.stringify(snapshot))
    window.localStorage.setItem(TOOL_PROFILE_STORAGE_KEY, settings.activeProfileId)
  } catch {
    // The bridge remains usable when browser storage is disabled.
  }
}

function clearToolSettingsSnapshot() {
  try {
    window.localStorage.removeItem(TOOL_SETTINGS_STORAGE_KEY)
    window.localStorage.removeItem(TOOL_PROFILE_STORAGE_KEY)
  } catch {
    // The bridge remains usable when browser storage is disabled.
  }
}

function removeManagedProfiles(preferredProfileId?: string | null) {
  const state = useStore.getState()
  const snapshot = readToolSettingsSnapshot()
  const profiles = state.settings.profiles.filter((profile) => !isManagedProfile(profile))

  if (profiles.length === 0) profiles.push(createDefaultOpenAIProfile())

  const rememberedProfileId = preferredProfileId
    ?? (!MANAGED_PROFILE_IDS.has(state.settings.activeProfileId) ? state.settings.activeProfileId : null)
    ?? snapshot?.activeProfileId
    ?? window.localStorage.getItem(TOOL_PROFILE_STORAGE_KEY)
  const activeProfile = profiles.find((profile) => profile.id === rememberedProfileId)
    ?? profiles.find((profile) => profile.id === state.settings.activeProfileId)
    ?? profiles[0]
  const hasProfile = (profileId: string | null | undefined) =>
    Boolean(profileId && profiles.some((profile) => profile.id === profileId))
  const currentAgentUsesManagedProfile =
    MANAGED_PROFILE_IDS.has(state.settings.agentTextProfileId ?? '') ||
    MANAGED_PROFILE_IDS.has(state.settings.agentImageProfileId ?? '')
  const currentAgentTextProfileId = hasProfile(state.settings.agentTextProfileId)
    ? state.settings.agentTextProfileId
    : null
  const currentAgentImageProfileId = hasProfile(state.settings.agentImageProfileId)
    ? state.settings.agentImageProfileId
    : null

  state.setSettings({
    profiles,
    activeProfileId: activeProfile.id,
    agentApiConfigMode: currentAgentUsesManagedProfile
      ? (snapshot?.agentApiConfigMode ?? 'off')
      : state.settings.agentApiConfigMode,
    agentTextProfileId: currentAgentTextProfileId
      ?? (hasProfile(snapshot?.agentTextProfileId) ? snapshot?.agentTextProfileId : null),
    agentImageProfileId: currentAgentImageProfileId
      ?? (hasProfile(snapshot?.agentImageProfileId) ? snapshot?.agentImageProfileId : activeProfile.id),
  })
  clearToolSettingsSnapshot()
}

function restoreToolProfile() {
  removeManagedProfiles()
}

function applyManagedProfiles(message: ConfigureMessage) {
  const configuration = getManagedConfiguration(message)
  if (!configuration) return false

  const state = useStore.getState()
  const hadManagedProfiles = state.settings.profiles.some(isManagedProfile)
  const shouldActivateManagedProfile = !hadManagedProfiles || MANAGED_PROFILE_IDS.has(state.settings.activeProfileId)
  if (!hadManagedProfiles && !readToolSettingsSnapshot()) {
    rememberToolSettings(state.settings)
  }

  const existingImageProfile = state.settings.profiles.find((profile) => profile.id === MANAGED_IMAGE_PROFILE_ID)
  const managedImageProfile: ApiProfile = {
    ...(existingImageProfile ?? createDefaultOpenAIProfile()),
    id: MANAGED_IMAGE_PROFILE_ID,
    name: configuration.profileName,
    provider: 'openai',
    baseUrl: configuration.apiUrl,
    apiKey: configuration.apiKey,
    model: existingImageProfile?.model || DEFAULT_IMAGES_MODEL,
    apiMode: 'images',
    codexCli: false,
    apiProxy: false,
    streamImages: message.mode === 'new-api',
  }
  const userProfiles = state.settings.profiles.filter((profile) => !isManagedProfile(profile))

  if (message.mode === 'tool') {
    const nextSettings: Partial<AppSettings> = {
      profiles: [...userProfiles, managedImageProfile],
    }
    if (shouldActivateManagedProfile) {
      Object.assign(nextSettings, {
        activeProfileId: MANAGED_IMAGE_PROFILE_ID,
        agentApiConfigMode: 'off',
        agentTextProfileId: null,
        agentImageProfileId: MANAGED_IMAGE_PROFILE_ID,
      })
    } else if (state.settings.agentTextProfileId === MANAGED_AGENT_PROFILE_ID) {
      Object.assign(nextSettings, {
        agentApiConfigMode: 'off',
        agentTextProfileId: null,
      })
    }
    state.setSettings(nextSettings)
    return true
  }

  const existingAgentProfile = state.settings.profiles.find((profile) => profile.id === MANAGED_AGENT_PROFILE_ID)
  const managedAgentProfile: ApiProfile = {
    ...(existingAgentProfile ?? createDefaultOpenAIProfile({ apiMode: 'responses' })),
    id: MANAGED_AGENT_PROFILE_ID,
    name: `${configuration.profileName} · Agent`,
    provider: 'openai',
    baseUrl: configuration.apiUrl,
    apiKey: configuration.apiKey,
    model: existingAgentProfile?.model || DEFAULT_RESPONSES_MODEL,
    apiMode: 'responses',
    codexCli: false,
    apiProxy: false,
    streamImages: true,
  }

  const nextSettings: Partial<AppSettings> = {
    profiles: [...userProfiles, managedImageProfile, managedAgentProfile],
  }
  if (shouldActivateManagedProfile) {
    Object.assign(nextSettings, {
      activeProfileId: MANAGED_IMAGE_PROFILE_ID,
      agentApiConfigMode: 'hybrid',
      agentTextProfileId: MANAGED_AGENT_PROFILE_ID,
      agentImageProfileId: MANAGED_IMAGE_PROFILE_ID,
    })
  }
  state.setSettings(nextSettings)
  return true
}

export function installNewApiBridge() {
  if (window.parent === window) return

  const profiles = useStore.getState().settings.profiles
  if (profiles.some(isManagedProfile)) removeManagedProfiles()

  useStore.persist.onFinishHydration(() => {
    if (activeConfigureMessage && !managedProfilesMatch(useStore.getState().settings, activeConfigureMessage)) {
      applyManagedProfiles(activeConfigureMessage)
    }
  })

  let repairingManagedProfiles = false
  useStore.subscribe((state) => {
    if (
      !activeConfigureMessage ||
      repairingManagedProfiles ||
      managedProfilesMatch(state.settings, activeConfigureMessage)
    ) {
      return
    }

    repairingManagedProfiles = true
    try {
      applyManagedProfiles(activeConfigureMessage)
    } finally {
      repairingManagedProfiles = false
    }
  })

  window.addEventListener('message', (event) => {
    if (event.origin !== window.location.origin || event.source !== window.parent) return
    const message = event.data as Record<string, unknown> | null
    if (message?.source === MESSAGE_SOURCE && message.type === 'new-api:image-playground:probe') {
      postToParent('ready')
      return
    }
    if (!isConfigureMessage(event.data)) return

    if (event.data.mode === 'tool' && !event.data.apiUrl && !event.data.apiKey) {
      activeConfigureMessage = null
      restoreToolProfile()
      postToParent('configured', 'tool')
      return
    }
    const previousConfigureMessage = activeConfigureMessage
    activeConfigureMessage = {
      source: MESSAGE_SOURCE,
      type: 'new-api:image-playground:configure',
      mode: event.data.mode,
      apiUrl: event.data.apiUrl,
      apiKey: event.data.apiKey,
      apiMode: event.data.apiMode,
      profileName: event.data.profileName,
    }
    if (!applyManagedProfiles(activeConfigureMessage)) {
      activeConfigureMessage = previousConfigureMessage
      return
    }
    postToParent('configured', event.data.mode)
  })

  postToParent('ready')
}
