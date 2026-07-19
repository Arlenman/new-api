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

  const rememberedProfileId = snapshot?.activeProfileId
    ?? preferredProfileId
    ?? window.localStorage.getItem(TOOL_PROFILE_STORAGE_KEY)
  const activeProfile = profiles.find((profile) => profile.id === rememberedProfileId)
    ?? profiles.find((profile) => profile.id === state.settings.activeProfileId)
    ?? profiles[0]
  const hasProfile = (profileId: string | null | undefined) =>
    Boolean(profileId && profiles.some((profile) => profile.id === profileId))
  const currentAgentUsesManagedProfile =
    MANAGED_PROFILE_IDS.has(state.settings.agentTextProfileId ?? '') ||
    MANAGED_PROFILE_IDS.has(state.settings.agentImageProfileId ?? '')

  state.setSettings({
    profiles,
    activeProfileId: activeProfile.id,
    agentApiConfigMode: snapshot?.agentApiConfigMode
      ?? (currentAgentUsesManagedProfile ? 'off' : state.settings.agentApiConfigMode),
    agentTextProfileId: hasProfile(snapshot?.agentTextProfileId)
      ? snapshot?.agentTextProfileId
      : (hasProfile(state.settings.agentTextProfileId) ? state.settings.agentTextProfileId : null),
    agentImageProfileId: hasProfile(snapshot?.agentImageProfileId)
      ? snapshot?.agentImageProfileId
      : (hasProfile(state.settings.agentImageProfileId) ? state.settings.agentImageProfileId : activeProfile.id),
  })
  clearToolSettingsSnapshot()
}

function restoreToolProfile() {
  removeManagedProfiles()
}

function applyManagedProfiles(message: ConfigureMessage) {
  if (typeof message.apiUrl !== 'string' || typeof message.apiKey !== 'string') return false
  const apiUrl = message.apiUrl.trim().replace(/\/+$/, '')
  const apiKey = message.apiKey.trim()
  if (!apiUrl || !apiKey) return false

  let parsedUrl: URL
  try {
    parsedUrl = new URL(apiUrl, window.location.origin)
  } catch {
    return false
  }
  if (parsedUrl.protocol !== 'http:' && parsedUrl.protocol !== 'https:') return false
  if (message.mode === 'new-api' && parsedUrl.origin !== window.location.origin) return false

  const state = useStore.getState()
  if (!state.settings.profiles.some(isManagedProfile)) rememberToolSettings(state.settings)

  const existingImageProfile = state.settings.profiles.find((profile) => profile.id === MANAGED_IMAGE_PROFILE_ID)
  const managedImageProfile: ApiProfile = {
    ...(existingImageProfile ?? createDefaultOpenAIProfile()),
    id: MANAGED_IMAGE_PROFILE_ID,
    name: message.profileName?.trim() || (message.mode === 'new-api' ? 'New API' : 'Custom API'),
    provider: 'openai',
    baseUrl: apiUrl,
    apiKey,
    model: existingImageProfile?.model || DEFAULT_IMAGES_MODEL,
    apiMode: 'images',
    codexCli: false,
    apiProxy: false,
    streamImages: message.mode === 'new-api',
  }
  const userProfiles = state.settings.profiles.filter((profile) => !isManagedProfile(profile))

  if (message.mode === 'tool') {
    state.setSettings({
      profiles: [...userProfiles, managedImageProfile],
      activeProfileId: MANAGED_IMAGE_PROFILE_ID,
      agentApiConfigMode: 'off',
      agentTextProfileId: null,
      agentImageProfileId: MANAGED_IMAGE_PROFILE_ID,
    })
    return true
  }

  const existingAgentProfile = state.settings.profiles.find((profile) => profile.id === MANAGED_AGENT_PROFILE_ID)
  const managedAgentProfile: ApiProfile = {
    ...(existingAgentProfile ?? createDefaultOpenAIProfile({ apiMode: 'responses' })),
    id: MANAGED_AGENT_PROFILE_ID,
    name: 'New API Agent',
    provider: 'openai',
    baseUrl: apiUrl,
    apiKey,
    model: existingAgentProfile?.model || DEFAULT_RESPONSES_MODEL,
    apiMode: 'responses',
    codexCli: false,
    apiProxy: false,
    streamImages: true,
  }

  state.setSettings({
    profiles: [...userProfiles, managedImageProfile, managedAgentProfile],
    activeProfileId: MANAGED_IMAGE_PROFILE_ID,
    agentApiConfigMode: 'hybrid',
    agentTextProfileId: MANAGED_AGENT_PROFILE_ID,
    agentImageProfileId: MANAGED_IMAGE_PROFILE_ID,
  })
  return true
}

export function installNewApiBridge() {
  if (window.parent === window) return

  const profiles = useStore.getState().settings.profiles
  if (profiles.some(isManagedProfile)) removeManagedProfiles()

  window.addEventListener('message', (event) => {
    if (event.origin !== window.location.origin || event.source !== window.parent) return
    const message = event.data as Record<string, unknown> | null
    if (message?.source === MESSAGE_SOURCE && message.type === 'new-api:image-playground:probe') {
      postToParent('ready')
      return
    }
    if (!isConfigureMessage(event.data)) return

    if (event.data.mode === 'tool' && !event.data.apiUrl && !event.data.apiKey) {
      restoreToolProfile()
      postToParent('configured', 'tool')
      return
    }
    if (applyManagedProfiles(event.data)) postToParent('configured', event.data.mode)
  })

  postToParent('ready')
}
