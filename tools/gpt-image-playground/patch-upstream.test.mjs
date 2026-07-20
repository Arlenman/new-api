import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { applyUpstreamPatch } from './patch-upstream.mjs'

const DB_SOURCE = `import type { AgentConversation, TaskRecord, StoredImage, StoredImageThumbnail } from '../types'

const DB_NAME = 'gpt-image-playground'
const DB_VERSION = 3
const STORE_TASKS = 'tasks'
const STORE_IMAGES = 'images'
const STORE_THUMBNAILS = 'thumbnails'
const STORE_AGENT_CONVERSATIONS = 'agentConversations'

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function dbTransaction<T>(
  storeName: string,
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction(storeName, mode)
        const store = tx.objectStore(storeName)
        const req = fn(store)
        req.onsuccess = () => resolve(req.result)
        req.onerror = () => reject(req.error)
      }),
  )
}

export function putAgentConversation(conversation: AgentConversation): Promise<IDBValidKey> {
  return dbTransaction(STORE_AGENT_CONVERSATIONS, 'readwrite', (s) => s.put(conversation))
}

export function replaceAgentConversations(conversations: AgentConversation[]): Promise<undefined> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_AGENT_CONVERSATIONS, 'readwrite')
        const store = tx.objectStore(STORE_AGENT_CONVERSATIONS)
        store.clear()
        for (const conversation of conversations) store.put(conversation)
        tx.oncomplete = () => resolve(undefined)
        tx.onerror = () => reject(tx.error)
        tx.onabort = () => reject(tx.error)
      }),
  )
}

export function getStoredImageThumbnail(id: string): Promise<StoredImageThumbnail | undefined> {
  return dbTransaction(STORE_THUMBNAILS, 'readonly', (s) => s.get(id))
}

export function putImageThumbnail(thumbnail: StoredImageThumbnail): Promise<IDBValidKey> {
  return dbTransaction(STORE_THUMBNAILS, 'readwrite', (s) => s.put(thumbnail))
}

export function deleteImage(id: string): Promise<undefined> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction([STORE_IMAGES, STORE_THUMBNAILS], 'readwrite')
        tx.objectStore(STORE_IMAGES).delete(id)
        tx.objectStore(STORE_THUMBNAILS).delete(id)
        tx.oncomplete = () => resolve(undefined)
        tx.onerror = () => reject(tx.error)
      }),
  )
}

export function clearImages(): Promise<undefined> {
  return openDB().then(
    (db) =>
      new Promise((resolve, reject) => {
        const tx = db.transaction([STORE_IMAGES, STORE_THUMBNAILS], 'readwrite')
        tx.objectStore(STORE_IMAGES).clear()
        tx.objectStore(STORE_THUMBNAILS).clear()
        tx.oncomplete = () => resolve(undefined)
        tx.onerror = () => reject(tx.error)
      }),
  )
}
`

const MAIN_SOURCE = `import 'core-js/actual/array/at'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import 'streamdown/styles.css'
import 'katex/dist/katex.min.css'
import './index.css'
import { installMobileViewportGuards } from './lib/viewport'

installMobileViewportGuards()

if ('serviceWorker' in navigator) {
  if (import.meta.env.PROD) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register(\`\${import.meta.env.BASE_URL}sw.js\`).catch((error) => {
        console.error('Service worker registration failed:', error)
      })
    })
  } else {
    navigator.serviceWorker.getRegistrations().then((registrations) => {
      registrations.forEach((registration) => registration.unregister())
    })
  }
}
`

const STORE_SOURCE = `import { create } from 'zustand'
import { persist } from 'zustand/middleware'

function normalizeSettings(settings: unknown) {
  return settings
}

export function getPersistedState(state: AppState) {
  const settings = normalizeSettings(state.settings)
  return {
    settings,
    params: state.params,
  }
}

const useStore = create(
  persist(() => ({}), {
      name: 'gpt-image-playground',
  }),
)

function mergeResponseOutputItems(previous: ResponsesOutputItem[], next: ResponsesOutputItem[]) {
  const merged = [...previous]
  for (const item of next) {
    const index = item.id ? merged.findIndex((existing) => existing.id === item.id) : -1
    if (index >= 0) merged[index] = item
    else merged.push(item)
  }
  return merged
}

async function executeAgentFunctionCalls() {
      if (imageFunctionCalls.length > 0) {
        for (const fc of imageFunctionCalls) {
          const output = await executeSingleImageFunctionCall(fc)
          functionCallOutputs.push({
            type: 'function_call_output',
            call_id: fc.call_id,
            output,
          })
        }
      }

      if (batchFunctionCalls.length > 0) {
        for (const fc of batchFunctionCalls) {
          const output = await executeBatchFunctionCall(fc)
          functionCallOutputs.push({
            type: 'function_call_output',
            call_id: fc.call_id,
            output,
          })
        }
      }
}

async function completeAgentImageTask(image: AgentApiResultImage, rawResponsePayload?: string) {
      updateTaskInStore(taskId, {
        prompt: image.revisedPrompt ?? latestTask?.prompt ?? '',
        outputImages: [stored.id],
        actualParams,
        actualParamsByImage: { [stored.id]: actualParams },
        revisedPromptByImage: image.revisedPrompt ? { [stored.id]: image.revisedPrompt } : undefined,
        rawResponsePayload,
        status: 'done',
        error: null,
        finishedAt: Date.now(),
        elapsed: Date.now() - (latestTask?.createdAt ?? startedAt),
        agentToolAction: image.action,
      })
      useStore.getState().setTaskStreamPreview(taskId)
}

async function completeHybridBatchTask() {
        // If not streaming and we have an image, complete the pre-created task.
        if (batchResult.image && !shouldStreamAssistantMessage) {
          await completeAgentImageTask({ ...batchResult.image, toolCallId: batchToolCallId }, batchResult.rawResponsePayload)
        }
}

/** 重试失败的任务：创建新任务并执行 */
export async function retryTask(task: TaskRecord) {
  const { settings } = useStore.getState()
  const activeProfile = getActiveApiProfile(settings)
  const normalizedParams = normalizeParamsForSettings(task.params, settings, { hasInputImages: task.inputImageIds.length > 0 })
  const shouldUseTransparentOutput = normalizedParams.output_format === 'png' && normalizedParams.transparent_output
  const taskParams = shouldUseTransparentOutput
    ? getTransparentRequestParams(normalizedParams)
    : { ...normalizedParams, transparent_output: false }
  const transparentMeta = taskParams.transparent_output
    ? createTransparentOutputMeta(task.prompt.trim())
    : null
  const taskId = genId()
  const newTask: TaskRecord = {
    id: taskId,
    prompt: task.prompt,
    params: taskParams,
    apiProvider: activeProfile.provider,
    apiProfileId: activeProfile.id,
    apiProfileName: activeProfile.name,
    apiMode: activeProfile.apiMode,
    apiModel: activeProfile.model,
    inputImageIds: [...task.inputImageIds],
    maskTargetImageId: task.maskTargetImageId ?? null,
    maskImageId: task.maskImageId ?? null,
    transparentOutput: transparentMeta?.transparentOutput,
    transparentPrompt: transparentMeta?.effectivePrompt,
    outputImages: [],
    status: 'running',
    error: null,
    createdAt: Date.now(),
    finishedAt: null,
    elapsed: null,
  }

  const latestTasks = useStore.getState().tasks
  useStore.getState().setTasks([newTask, ...latestTasks])
  await putTask(newTask)

  executeTask(taskId)
}

export async function initStore() {
  const legacyAgentConversations = normalizeAgentConversations(useStore.getState().agentConversations)
  const storedTasks = await getAllTasks()
  const { tasks: markedTasks, interruptedTasks } = markInterruptedOpenAIRunningTasks(storedTasks)
  return { legacyAgentConversations, markedTasks, interruptedTasks }
}
`

const APP_SOURCE = `export default function App() {
  return (
        <main data-home-main data-drag-select-surface className="pb-48">
          <div>gallery</div>
        </main>
  )
}
`

const INPUT_BAR_SOURCE = `function getMentionTagTextLength(el: Element) {
  return el.textContent?.length ?? 0
}

  const showPromptExpand = promptExpanded || promptCanExpand

    const maxH = promptExpanded
      ? Math.max(el.parentElement?.clientHeight ?? 0, 80)
      : normalMaxH

      <div
        data-input-bar
        className={§fixed bottom-4 sm:bottom-6 left-1/2 -translate-x-1/2 z-30 w-full max-w-4xl px-3 sm:px-4 transition-all duration-300¤{promptExpanded ? ' flex flex-col' : ''}§}
        style={promptExpanded ? { top: §¤{promptExpandedTop}px§, transitionProperty: 'none' } : undefined}
      >
        <div ref={cardRef} className={§bg-white/70 dark:bg-gray-900/70 backdrop-blur-2xl border border-white/50 dark:border-white/[0.08] shadow-[0_8px_30px_rgb(0,0,0,0.08)] dark:shadow-[0_8px_30px_rgb(0,0,0,0.3)] rounded-2xl sm:rounded-3xl p-3 sm:p-4 ring-1 ring-black/5 dark:ring-white/10¤{promptExpanded ? ' flex min-h-0 flex-1 flex-col' : ''}§}>
      <div ref={imagesRef}>
          <div className={§relative grid¤{promptExpanded ? ' min-h-0 flex-1' : ''}§}>
              className={§col-start-1 row-start-1 min-h-[42px] w-full overflow-hidden ios-rounded-scroll-fix whitespace-pre-wrap break-words rounded-2xl border border-gray-200/60 bg-white/50 pl-4 pr-10 py-3 text-sm leading-relaxed shadow-sm outline-none transition-[border-color,box-shadow] duration-200 focus:ring-1 focus:ring-blue-300/40 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-gray-100 dark:focus:ring-blue-500/30¤{promptExpanded ? ' !h-full !overflow-y-auto' : ''}§}
            {showPromptExpand && (
          <div className="mt-3">
            <div className="hidden sm:flex items-end justify-between gap-3">
              {renderParams('grid-cols-6')}

              <div className="flex gap-2 flex-shrink-0 mb-0.5">
`.replaceAll('§', '`').replaceAll('¤', '$')

const AGENT_WORKSPACE_SOURCE = `export default function AgentWorkspace() {
  return (
    <>
          <div
          className="flex-1 space-y-4 overflow-visible pb-[calc(var(--input-bar-clearance,12rem)+1.5rem)] px-1 lg:pt-14 lg:px-4"
          />
          <button
          className={§fixed bottom-[calc(var(--input-bar-clearance,12rem)+1.5rem)] left-1/2 -translate-x-1/2 z-30 flex h-10 w-10 items-center justify-center rounded-full bg-white/90 backdrop-blur shadow-[0_2px_12px_rgba(0,0,0,0.1)] border border-gray-200/50 text-gray-500 transition-all duration-300 hover:bg-gray-50 hover:text-gray-800 dark:border-white/[0.08] dark:bg-gray-800/90 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200 ¤{
          }§}
          aria-label="滚动到底部"
          />
    </>
  )
}
`.replaceAll('§', '`').replaceAll('¤', '$')

const SETTINGS_MODAL_SOURCE = `export default function SettingsModal() {
  const showSettings = useStore((s) => s.showSettings)
  const settings = useStore((s) => s.settings)
  const reusedTaskApiProfileId = useStore((s) => s.reusedTaskApiProfileId)
  const apiProxyAvailable = true
  const apiProxyLocked = false
  const [draft, setDraft] = useState<AppSettings>(normalizeSettings(settings))
  const [timeoutInput, setTimeoutInput] = useState(String(getActiveApiProfile(settings).timeout))
  const [agentMaxToolRoundsInput, setAgentMaxToolRoundsInput] = useState(String(settings.agentMaxToolRounds))
  const [activeTab] = useState<SettingsTab>('api')
  const activeProfile = draft.profiles.find((profile) => profile.id === draft.activeProfileId) ?? draft.profiles[0] ?? getActiveApiProfile(draft)

  const wasSettingsOpenRef = useRef(false)

  useEffect(() => {
    if (!showSettings) {
      wasSettingsOpenRef.current = false
      return
    }
    if (wasSettingsOpenRef.current) return

    wasSettingsOpenRef.current = true
    const normalizedSettings = normalizeSettings(settings)
    const displaySettings = normalizedSettings.reuseTaskApiProfileTemporarily && reusedTaskApiProfileId && normalizedSettings.profiles.some((profile) => profile.id === reusedTaskApiProfileId)
      ? normalizeSettings({ ...normalizedSettings, activeProfileId: reusedTaskApiProfileId })
      : normalizedSettings
    const nextDraft = normalizeSettings({
      ...displaySettings,
      profiles: displaySettings.profiles.map((profile) => ({
        ...profile,
        apiProxy: isProfileApiProxyEligible(displaySettings, profile) && apiProxyAvailable
          ? (apiProxyLocked || profile.apiProxy)
          : false,
      })),
    })
    setDraft(nextDraft)
    setTimeoutInput(String(getActiveApiProfile(nextDraft).timeout))
    setAgentMaxToolRoundsInput(String(nextDraft.agentMaxToolRounds))
  }, [apiProxyAvailable, apiProxyLocked, showSettings, settings, reusedTaskApiProfileId])

  const createNewProfile = () => {}
  const switchProfile = (id: string) => {}
  const confirmCopyProfileImportUrl = (profile: ApiProfile) => {
    setShowProfileMenu(false)
  }
  const duplicateActiveProfile = () => {
    if (defaultConfigOnly) return
  }
  const deleteProfile = (id: string) => {
    if (draft.profiles.length <= 1) return
  }

  return (
    <>
            {activeTab === 'agent' && (
              <AgentSettingsTab
                draft={draft}
                agentMaxToolRoundsInput={agentMaxToolRoundsInput}
              />
            )}

            {activeTab === 'api' && (
              <div className="space-y-4">
                <span className="block text-sm text-gray-600 dark:text-gray-300">当前配置</span>
                <input value={activeProfile.name} />
                <input value={activeProfile.baseUrl} />
                <input value={activeProfile.apiKey} />
                <div className="flex shrink-0 items-center gap-1">
                  <button
                    type="button"
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      confirmCopyProfileImportUrl(profile)
                    }}
                    className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-gray-400 opacity-60 transition-all hover:bg-gray-100 hover:text-gray-600 hover:opacity-100 dark:hover:bg-white/[0.08] dark:hover:text-gray-200"
                    aria-label={\`复制导入配置「\${profile.name}」的 URL\`}
                    title="复制导入 URL"
                  >
                    <LinkIcon className="h-3.5 w-3.5" />
                  </button>
                  {draft.profiles.length > 1 && (
                    <button
                      type="button"
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        setConfirmDialog({
                          title: '删除配置',
                          message: \`确定要删除配置「\${profile.name}」吗？\`,
                          action: () => deleteProfile(profile.id)
                        })
                      }}
                      className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-gray-400 opacity-60 transition-all hover:bg-red-50 hover:text-red-500 hover:opacity-100 dark:hover:bg-red-500/10"
                      aria-label="删除配置"
                    >
                      <TrashIcon className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
            </div>
            )}
${'            '}
            {activeTab === 'data' && (
              <div className="space-y-4">data</div>
            )}
    </>
  )
}
`


const AGENT_SETTINGS_SOURCE = `interface AgentSettingsTabProps {
  draft: AppSettings
  agentMaxToolRoundsInput: string
  updateAgentApiConfigMode: (mode: AgentApiConfigMode) => void
}

export default function AgentSettingsTab({
  draft,
  agentMaxToolRoundsInput,
  updateAgentApiConfigMode,
}: AgentSettingsTabProps) {
  return (
    <div className="space-y-4">
      <div className="block">
        <span>使用独立的 API 配置</span>
        <Select
          value={draft.agentApiConfigMode}
          onChange={(value) => updateAgentApiConfigMode(value as AgentApiConfigMode)}
          options={[{ label: '关闭', value: 'off' }]}
        />
      </div>

      {draft.agentApiConfigMode !== 'off' && (
        <div>
          <Select value={draft.agentTextProfileId ?? ''} options={[]} />
          <Select value={draft.agentImageProfileId ?? ''} options={[]} />
        </div>
      )}
      <label className="block">
        <span>最大工具调用轮数</span>
        <input value={agentMaxToolRoundsInput} />
      </label>
    </div>
  )
}
`

async function createFixture(
  mainSource = MAIN_SOURCE,
  storeSource = STORE_SOURCE,
  appSource = APP_SOURCE,
  inputBarSource = INPUT_BAR_SOURCE,
  agentWorkspaceSource = AGENT_WORKSPACE_SOURCE,
  dbSource = DB_SOURCE,
  settingsModalSource = SETTINGS_MODAL_SOURCE,
  agentSettingsSource = AGENT_SETTINGS_SOURCE,
) {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-patch-'))
  await mkdir(path.join(root, 'src', 'components', 'settings'), { recursive: true })
  await mkdir(path.join(root, 'src', 'lib'), { recursive: true })
  await writeFile(path.join(root, 'src', 'main.tsx'), mainSource)
  await writeFile(path.join(root, 'src', 'lib', 'db.ts'), dbSource)
  await writeFile(path.join(root, 'src', 'store.ts'), storeSource)
  await writeFile(path.join(root, 'src', 'App.tsx'), appSource)
  await writeFile(path.join(root, 'src', 'components', 'InputBar.tsx'), inputBarSource)
  await writeFile(path.join(root, 'src', 'components', 'AgentWorkspace.tsx'), agentWorkspaceSource)
  await writeFile(path.join(root, 'src', 'components', 'SettingsModal.tsx'), settingsModalSource)
  await writeFile(path.join(root, 'src', 'components', 'settings', 'AgentSettingsTab.tsx'), agentSettingsSource)
  return root
}

test('injects the New API bridge through the validated upstream entry markers', async () => {
  const root = await createFixture()

  await applyUpstreamPatch(root, { bridgeSource: 'export const bridgeFixture = true\n' })

  const mainSource = await readFile(path.join(root, 'src', 'main.tsx'), 'utf8')
  const bridgeSource = await readFile(path.join(root, 'src', 'lib', 'newApiBridge.ts'), 'utf8')
  const storageSource = await readFile(path.join(root, 'src', 'lib', 'newApiStorage.ts'), 'utf8')
  const syncSource = await readFile(path.join(root, 'src', 'lib', 'newApiSync.ts'), 'utf8')
  const dbSource = await readFile(path.join(root, 'src', 'lib', 'db.ts'), 'utf8')
  const storeSource = await readFile(path.join(root, 'src', 'store.ts'), 'utf8')
  const appSource = await readFile(path.join(root, 'src', 'App.tsx'), 'utf8')
  const inputBarSource = await readFile(path.join(root, 'src', 'components', 'InputBar.tsx'), 'utf8')
  const agentWorkspaceSource = await readFile(path.join(root, 'src', 'components', 'AgentWorkspace.tsx'), 'utf8')
  const settingsModalSource = await readFile(path.join(root, 'src', 'components', 'SettingsModal.tsx'), 'utf8')
  const agentSettingsSource = await readFile(path.join(root, 'src', 'components', 'settings', 'AgentSettingsTab.tsx'), 'utf8')
  assert.match(mainSource, /import \{ installNewApiBridge \} from '\.\/lib\/newApiBridge'/)
  assert.match(mainSource, /installNewApiBridge\(\)\n\ninstallMobileViewportGuards\(\)/)
  assert.match(mainSource, /navigator\.serviceWorker\.getRegistration\(scope\)/)
  assert.match(mainSource, /key\.startsWith\('gpt-image-playground-'\)/)
  assert.doesNotMatch(mainSource, /serviceWorker\.register/)
  assert.equal(bridgeSource, 'export const bridgeFixture = true\n')
  assert.match(storageSource, /getNewApiImagePlaygroundDatabaseName/)
  assert.doesNotMatch(storageSource, /new_api_user/)
  assert.match(storageSource, /normalizeInjectedUserId\(window\.__NEW_API_USER_ID__\)/)
  assert.match(storageSource, /New API authenticated user identity is required/)
  assert.doesNotMatch(storageSource, /getItem\(['"]uid['"]\)/)
  assert.match(storageSource, /LEGACY_MIGRATION_OWNER_KEY/)
  assert.match(storageSource, /claimLegacyMigrationOwner\(storage, userId\) !== userId/)
  assert.match(syncSource, /initializeNewApiImagePlaygroundSync/)
  assert.match(syncSource, /sensitiveSyncFieldNames/)
  assert.match(syncSource, /normalizedKey === 'rawimageurls' \|\| normalizedKey === 'rawresponsepayload'/)
  assert.match(syncSource, /maskDraft: null/)
  assert.match(syncSource, /deleteCachedAsset/)
  assert.match(syncSource, /mutationResult\.result === 'conflict' && mutationResult\.item/)
  assert.match(syncSource, /applyRemoteItemAndUpdateMetadata/)
  assert.match(syncSource, /await canReuseImageSyncEntry\('image', image\.dataUrl, metadataEntry\)/)
  assert.match(syncSource, /await canReuseImageSyncEntry\('thumbnail', thumbnail\.thumbnailDataUrl, metadataEntry, thumbnail\.thumbnailVersion\)/)
  assert.match(syncSource, /content_sha256: localItem\.contentSha256/)
  assert.match(dbSource, /const DB_NAME = getNewApiImagePlaygroundDatabaseName\(\)/)
  assert.match(dbSource, /await ensureLegacyImagePlaygroundIndexedDBMigration/)
  assert.match(dbSource, /export function deleteAgentConversation/)
  assert.match(dbSource, /export function getAllStoredImageThumbnailIds/)
  assert.match(dbSource, /notifyNewApiImagePlaygroundStorageChanged\(\)/)
  assert.match(storeSource, /getNewApiImagePlaygroundStorageKey/)
  assert.match(storeSource, /initializeNewApiImagePlaygroundSync/)
  assert.match(storeSource, /name: getNewApiImagePlaygroundStorageKey\(\)/)
  assert.match(storeSource, /async function refreshNewApiImagePlaygroundStore/)
  assert.match(storeSource, /const \{ tasks: markedTasks, interruptedTasks \} = markInterruptedOpenAIRunningTasks\(storedTasks\)/)
  assert.doesNotMatch(storeSource, /getNewApiImagePlaygroundUserId/)
  assert.match(settingsModalSource, /const managedProfileIds = new Set\(\[/)
  assert.match(settingsModalSource, /'new-api-managed'/)
  assert.match(settingsModalSource, /'new-api-managed-agent'/)
  assert.match(settingsModalSource, /if \(wasSettingsOpenRef\.current && !managedProfileActive\) return/)
  assert.match(settingsModalSource, /const managedProfile = draft\.profiles\.find\(\(profile\) => profile\.id === 'new-api-managed'\)/)
  assert.match(settingsModalSource, /const managedModeActive = Boolean\(managedProfile\) && managedProfileActive/)
  assert.match(settingsModalSource, /新增第三方配置/)
  assert.match(settingsModalSource, /onClick=\{createNewProfile\}/)
  assert.match(settingsModalSource, /userProfiles\.map\(\(profile\) =>/)
  assert.match(settingsModalSource, /onClick=\{\(\) => switchProfile\(profile\.id\)\}/)
  assert.match(settingsModalSource, /if \(defaultConfigOnly \|\| managedProfileIds\.has\(activeProfile\.id\)\) return/)
  assert.match(settingsModalSource, /if \(managedProfileIds\.has\(id\) \|\| draft\.profiles\.length <= 1\) return/)
  assert.match(settingsModalSource, /if \(managedProfileIds\.has\(profile\.id\)\) return/)
  assert.match(settingsModalSource, /当前由 New API 托管/)
  assert.match(settingsModalSource, /页面顶部的 New API 密钥下拉/)
  assert.match(settingsModalSource, /Images、Responses 与 Agent 使用同一个 New API 托管密钥/)
  assert.match(settingsModalSource, /通过同源 <code[^>]*>\/pg<\/code> 请求/)
  assert.match(settingsModalSource, /managedProfileName=\{managedModeActive \? \(managedProfile\?\.name \?\? 'New API'\) : null\}/)
  assert.match(agentSettingsSource, /managedProfileName: string \| null/)
  assert.match(agentSettingsSource, /Agent 连接由 New API 托管/)
  assert.match(agentSettingsSource, /Responses 对话与 Images 生图均使用宿主在页面顶部选中的 New API 密钥/)
  assert.match(agentSettingsSource, /此处不接受 API URL 或 API Key，也不会覆盖宿主管理的连接配置/)
  const managedAgentPanel = agentSettingsSource.match(/\{managedProfileName \? \(([\s\S]*?)\n\s*\) : \(/)?.[1]
  assert.ok(managedAgentPanel)
  assert.doesNotMatch(managedAgentPanel, /<input|<Select|updateAgentApiConfigMode|commitSettings|OpenAI|默认 OpenAI|apiKey|baseUrl/)
  const managedApiPanel = settingsModalSource.match(/managedModeActive \? \(([\s\S]*?)\n\s*\) : \(/)?.[1]
  assert.ok(managedApiPanel)
  assert.doesNotMatch(managedApiPanel, /activeProfile\.apiKey|<input|OpenAI/)
  const customApiPanel = settingsModalSource.match(/managedModeActive \? \([\s\S]*?\n\s*\) : \(\n([\s\S]*?)\n\s*\)\n\s*\)}/)?.[1]
  assert.ok(customApiPanel)
  assert.match(customApiPanel, /activeProfile\.baseUrl/)
  assert.match(customApiPanel, /activeProfile\.apiKey/)
  assert.match(storeSource, /new Set\(\['new-api-managed', 'new-api-managed-agent'\]\)/)
  assert.match(storeSource, /localStorage\.getItem\('new-api:image-playground:tool-settings'\)/)
  assert.match(storeSource, /const settings = normalizeSettings\(\{[\s\S]*profiles,[\s\S]*activeProfileId,[\s\S]*agentApiConfigMode,/)
  assert.doesNotMatch(storeSource, /const settings = normalizeSettings\(state\.settings\)/)
  assert.match(storeSource, /let index = item\.id \? merged\.findIndex\(\(existing\) => existing\.id === item\.id\) : -1/)
  assert.match(storeSource, /const callId = item\.call_id\?\.trim\(\)/)
  assert.match(storeSource, /existing\.type === item\.type && existing\.call_id\?\.trim\(\) === callId/)
  assert.doesNotMatch(storeSource, /const index = item\.id \? merged\.findIndex/)
  assert.match(storeSource, /const customImageFunctionCallIndexById = new Map<string, number>\(\)/)
  assert.match(storeSource, /const callId = fc\.call_id\?\.trim\(\)/)
  assert.match(storeSource, /customImageFunctionCalls\[existingIndex\] = fc/)
  assert.match(storeSource, /const imageFunctionCallOutputs = await Promise\.all\(/)
  assert.match(storeSource, /customImageFunctionCalls\.map\(async \(fc\) =>/)
  assert.match(storeSource, /fc\.name === 'generate_image_batch'/)
  assert.match(storeSource, /\? await executeBatchFunctionCall\(fc\)/)
  assert.match(storeSource, /: await executeSingleImageFunctionCall\(fc\)/)
  assert.doesNotMatch(storeSource, /for \(const fc of imageFunctionCalls\)/)
  assert.doesNotMatch(storeSource, /for \(const fc of batchFunctionCalls\)/)
  assert.match(storeSource, /requestSettings\.agentApiConfigMode === 'hybrid' \|\| !shouldStreamAssistantMessage/)
  assert.match(storeSource, /await completeAgentImageTask\(\{ \.\.\.batchResult\.image, toolCallId: batchToolCallId \}, batchResult\.rawResponsePayload\)/)
  assert.doesNotMatch(storeSource, /batchResult\.image && !shouldStreamAssistantMessage/)
  assert.match(storeSource, /const completedTask = useStore\.getState\(\)\.tasks\.find\(\(task\) => task\.id === taskId\)/)
  assert.match(storeSource, /if \(!completedTask\) await putTask|if \(completedTask\) await putTask\(completedTask\)/)
  assert.match(storeSource, /const currentTask = state\.tasks\.find\(\(item\) => item\.id === task\.id\)/)
  assert.match(storeSource, /if \(!currentTask \|\| currentTask\.status === 'running'\) return/)
  assert.match(storeSource, /const retriedTask: TaskRecord = \{[\s\S]*\.\.\.currentTask,[\s\S]*status: 'running'/)
  assert.match(storeSource, /state\.setTasks\(state\.tasks\.map\(\(item\) => item\.id === currentTask\.id \? retriedTask : item\)\)/)
  assert.match(storeSource, /executeTask\(currentTask\.id\)/)
  assert.match(storeSource, /clearFalRecoveryTimer\(currentTask\.id\)/)
  assert.match(storeSource, /clearCustomRecoveryTimer\(currentTask\.id\)/)
  assert.match(storeSource, /deleteUnreferencedImageIds\(staleImageIds\)/)
  assert.doesNotMatch(storeSource, /const taskId = genId\(\)/)
  assert.doesNotMatch(storeSource, /setTasks\(\[newTask, \.\.\.latestTasks\]\)/)
  assert.match(inputBarSource, /const IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY = 'gpt-image-playground:layout'/)
  assert.match(inputBarSource, /const IMAGE_PLAYGROUND_LAYOUT_VERSION = 1/)
  assert.match(inputBarSource, /const MIN_RIGHT_PANEL_WIDTH = 320/)
  assert.match(inputBarSource, /const MAX_RIGHT_PANEL_WIDTH = 640/)
  assert.match(inputBarSource, /const DEFAULT_RIGHT_PANEL_WIDTH = 400/)
  assert.match(inputBarSource, /const RIGHT_LAYOUT_MIN_VIEWPORT_WIDTH = 900/)
  assert.match(inputBarSource, /type PlaygroundEditorPosition = 'bottom' \| 'right'/)
  assert.match(inputBarSource, /editorPosition: 'bottom'/)
  assert.match(inputBarSource, /window\.localStorage\.setItem\(IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY, JSON\.stringify\(next\)\)/)
  assert.match(inputBarSource, /viewportWidth < RIGHT_LAYOUT_MIN_VIEWPORT_WIDTH[\s\S]*\? 'bottom'[\s\S]*: playgroundLayout\.editorPosition/)
  assert.match(inputBarSource, /role="separator"/)
  assert.match(inputBarSource, /aria-orientation="vertical"/)
  assert.match(inputBarSource, /aria-valuemin=\{MIN_RIGHT_PANEL_WIDTH\}/)
  assert.match(inputBarSource, /aria-valuemax=\{MAX_RIGHT_PANEL_WIDTH\}/)
  assert.match(inputBarSource, /aria-valuenow=\{playgroundLayout\.rightPanelWidth\}/)
  assert.match(inputBarSource, /onPointerDown=\{handleRightPanelResizeStart\}/)
  assert.match(inputBarSource, /className="fixed top-14 bottom-0 z-40/)
  assert.match(inputBarSource, /'fixed top-14 bottom-0 right-0 z-30/)
  assert.match(inputBarSource, /window\.addEventListener\('pointermove', handlePointerMove\)/)
  assert.match(inputBarSource, /window\.addEventListener\('pointercancel', handlePointerUp\)/)
  assert.match(inputBarSource, /event\.key !== 'ArrowLeft' && event\.key !== 'ArrowRight'/)
  assert.match(inputBarSource, /event\.key === 'ArrowLeft' \? 16 : -16/)
  assert.match(inputBarSource, /max-h-\[35%\] shrink-0 overflow-y-auto custom-scrollbar/)
  assert.match(inputBarSource, /grid-cols-\[repeat\(auto-fit,minmax\(136px,1fr\)\)\]/)
  assert.match(inputBarSource, /editorPosition: isRightEditorLayout \? 'bottom' : 'right'/)
  assert.match(inputBarSource, /title=\{isRightEditorLayout \? '移到底部' : '移到右侧'\}/)
  assert.match(inputBarSource, /--image-playground-gallery-content-padding-right/)
  assert.match(inputBarSource, /--image-playground-gallery-content-padding-bottom/)
  assert.match(inputBarSource, /--image-playground-agent-content-padding-right/)
  assert.match(appSource, /paddingRight: 'var\(--image-playground-gallery-content-padding-right, 0px\)'/)
  assert.match(appSource, /paddingBottom: 'var\(--image-playground-gallery-content-padding-bottom, 12rem\)'/)
  assert.match(agentWorkspaceSource, /paddingRight: 'var\(--image-playground-agent-content-padding-right, 0px\)'/)
  assert.match(agentWorkspaceSource, /paddingBottom: 'var\(--image-playground-agent-content-padding-bottom/)
  assert.match(agentWorkspaceSource, /bottom: 'var\(--image-playground-agent-scroll-bottom/)
  assert.match(agentWorkspaceSource, /left: 'calc\(\(100% - var\(--image-playground-agent-content-padding-right, 0px\)\) \/ 2\)'/)
  assert.equal(inputBarSource.match(/const IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY =/g)?.length, 1)
  assert.equal(inputBarSource.match(/role="separator"/g)?.length, 1)
  assert.equal(appSource.match(/--image-playground-gallery-content-padding-right/g)?.length, 1)
  assert.equal(agentWorkspaceSource.match(/--image-playground-agent-content-padding-right/g)?.length, 2)
})

test('fails closed when the upstream InputBar layout marker no longer matches', async () => {
  const root = await createFixture(
    MAIN_SOURCE,
    STORE_SOURCE,
    APP_SOURCE,
    INPUT_BAR_SOURCE.replace(
      'function getMentionTagTextLength(el: Element) {\n',
      'function getMentionTextLength(el: Element) {\n',
    ),
  )

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream InputBar layout helpers marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream InputBar layout marker matches more than once', async () => {
  const marker = 'function getMentionTagTextLength(el: Element) {\n'
  const root = await createFixture(
    MAIN_SOURCE,
    STORE_SOURCE,
    APP_SOURCE,
    INPUT_BAR_SOURCE.replace(marker, marker + marker),
  )

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream InputBar layout helpers marker .* did not match exactly once/,
  )
})

test('fails closed when the validated upstream entry markers no longer match', async () => {
  const root = await createFixture(MAIN_SOURCE.replace(
    'installMobileViewportGuards()\n',
    'startApplication()\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream entry marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream service worker marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE.replace(
    "if ('serviceWorker' in navigator) {",
    "if ('serviceWorker' in globalThis.navigator) {",
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream service worker marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream persistence marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '  const settings = normalizeSettings(state.settings)\n',
    '  const settings = migrateSettings(state.settings)\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream persistence marker .* did not match exactly once/,
  )
})


test('fails closed when the upstream Agent response output merge marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '  const merged = [...previous]\n',
    '  const merged = previous.slice()\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream Agent response output merge marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream Agent image execution marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '        for (const fc of imageFunctionCalls) {\n',
    '        for (const call of imageFunctionCalls) {\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream Agent image function calls marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream Agent image task completion marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '      useStore.getState().setTaskStreamPreview(taskId)\n',
    '      clearTaskStreamPreview(taskId)\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream Agent image task durable completion marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream hybrid Agent batch completion marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '        if (batchResult.image && !shouldStreamAssistantMessage) {\n',
    '        if (batchResult.image) {\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream hybrid Agent batch task completion marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream task retry marker no longer matches', async () => {
  const root = await createFixture(MAIN_SOURCE, STORE_SOURCE.replace(
    '/** 重试失败的任务：创建新任务并执行 */',
    '/** retry a task */',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream task retry marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream SettingsModal managed-mode marker no longer matches', async () => {
  const root = await createFixture(
    MAIN_SOURCE,
    STORE_SOURCE,
    APP_SOURCE,
    INPUT_BAR_SOURCE,
    AGENT_WORKSPACE_SOURCE,
    DB_SOURCE,
    SETTINGS_MODAL_SOURCE.replace(
      '    if (wasSettingsOpenRef.current) return\n',
      '    if (wasSettingsOpenRef.current && showSettings) return\n',
    ),
  )

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream SettingsModal managed hydration marker .* did not match exactly once/,
  )
})

test('fails closed when the upstream AgentSettings managed-mode marker no longer matches', async () => {
  const root = await createFixture()
  const agentSettingsPath = path.join(root, 'src', 'components', 'settings', 'AgentSettingsTab.tsx')
  const source = await readFile(agentSettingsPath, 'utf8')
  await writeFile(agentSettingsPath, source.replace(
    'export default function AgentSettingsTab({\n  draft,\n',
    'export default function AgentSettingsTab({\n  settingsDraft,\n',
  ))

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: 'export {}\n' }),
    /upstream AgentSettings managed property destructuring marker .* did not match exactly once/,
  )
})

test('managed New API profiles enable hybrid Agent mode without changing tool-only streaming', async () => {
  const root = await createFixture()

  await applyUpstreamPatch(root)

  const bridgeSource = await readFile(path.join(root, 'src', 'lib', 'newApiBridge.ts'), 'utf8')
  assert.match(bridgeSource, /const MANAGED_IMAGE_PROFILE_ID = 'new-api-managed'/)
  assert.match(bridgeSource, /const MANAGED_AGENT_PROFILE_ID = 'new-api-managed-agent'/)
  assert.match(bridgeSource, /model: existingAgentProfile\?\.model \|\| DEFAULT_RESPONSES_MODEL/)
  assert.match(bridgeSource, /apiMode: 'responses'/)
  assert.match(bridgeSource, /agentApiConfigMode: 'hybrid'/)
  assert.match(bridgeSource, /agentTextProfileId: MANAGED_AGENT_PROFILE_ID/)
  assert.match(bridgeSource, /agentImageProfileId: MANAGED_IMAGE_PROFILE_ID/)
  assert.match(bridgeSource, /streamImages: message\.mode === 'new-api'/)
  assert.match(bridgeSource, /streamImages: true/)
  assert.match(bridgeSource, /useStore\.persist\.onFinishHydration/)
  assert.match(bridgeSource, /managedProfilesMatch\(useStore\.getState\(\)\.settings, activeConfigureMessage\)/)
  assert.match(bridgeSource, /useStore\.subscribe\(\(state\) =>/)
  assert.match(bridgeSource, /const previousConfigureMessage = activeConfigureMessage/)
  assert.match(bridgeSource, /activeConfigureMessage = previousConfigureMessage/)
})

test('default bridge removes both managed profiles before restoring tool settings', async () => {
  const root = await createFixture()

  await applyUpstreamPatch(root)

  const bridgeSource = await readFile(path.join(root, 'src', 'lib', 'newApiBridge.ts'), 'utf8')
  assert.match(bridgeSource, /function removeManagedProfiles/)
  assert.match(bridgeSource, /profiles\.filter\(\(profile\) => !isManagedProfile\(profile\)\)/)
  assert.match(bridgeSource, /rememberToolSettings\(state\.settings\)/)
  assert.match(bridgeSource, /agentApiConfigMode: currentAgentUsesManagedProfile/)
  assert.match(bridgeSource, /\? \(snapshot\?\.agentApiConfigMode \?\? 'off'\)/)
  assert.match(bridgeSource, /if \(profiles\.some\(isManagedProfile\)\) removeManagedProfiles\(\)/)
})
