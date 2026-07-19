import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const IMPORT_MARKER = "import App from './App'\n"
const INSTALL_MARKER = 'installMobileViewportGuards()\n'
const SERVICE_WORKER_MARKER = `if ('serviceWorker' in navigator) {
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
const SERVICE_WORKER_REPLACEMENT = `if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    const scope = new URL(import.meta.env.BASE_URL, window.location.href).href
    void navigator.serviceWorker.getRegistration(scope)
      .then((registration) => registration?.unregister())
      .catch((error) => console.error('Service worker cleanup failed:', error))

    if ('caches' in window) {
      void window.caches.keys()
        .then((keys) => Promise.all(
          keys
            .filter((key) => key.startsWith('gpt-image-playground-'))
            .map((key) => window.caches.delete(key)),
        ))
        .catch((error) => console.error('Image playground cache cleanup failed:', error))
    }
  })
}
`
const INPUT_BAR_LAYOUT_HELPERS_MARKER = `function getMentionTagTextLength(el: Element) {
`
const INPUT_BAR_LAYOUT_HELPERS_REPLACEMENT = `const IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY = 'gpt-image-playground:layout'
const IMAGE_PLAYGROUND_LAYOUT_VERSION = 1
const MIN_RIGHT_PANEL_WIDTH = 320
const MAX_RIGHT_PANEL_WIDTH = 640
const DEFAULT_RIGHT_PANEL_WIDTH = 400
const RIGHT_LAYOUT_MIN_VIEWPORT_WIDTH = 900

type PlaygroundEditorPosition = 'bottom' | 'right'

type PlaygroundLayoutConfig = {
  version: 1
  editorPosition: PlaygroundEditorPosition
  rightPanelWidth: number
}

function clampRightPanelWidth(width: number) {
  return Math.min(MAX_RIGHT_PANEL_WIDTH, Math.max(MIN_RIGHT_PANEL_WIDTH, Math.round(width)))
}

function readPlaygroundLayout(): PlaygroundLayoutConfig {
  const fallback: PlaygroundLayoutConfig = {
    version: IMAGE_PLAYGROUND_LAYOUT_VERSION,
    editorPosition: 'bottom',
    rightPanelWidth: DEFAULT_RIGHT_PANEL_WIDTH,
  }

  try {
    const raw = window.localStorage.getItem(IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY)
    if (!raw) return fallback
    const value = JSON.parse(raw) as Partial<PlaygroundLayoutConfig>
    if (value.version !== IMAGE_PLAYGROUND_LAYOUT_VERSION) return fallback
    if (value.editorPosition !== 'bottom' && value.editorPosition !== 'right') return fallback
    const rightPanelWidth = value.rightPanelWidth
    if (typeof rightPanelWidth !== 'number'
      || !Number.isFinite(rightPanelWidth)
      || rightPanelWidth < MIN_RIGHT_PANEL_WIDTH
      || rightPanelWidth > MAX_RIGHT_PANEL_WIDTH) return fallback
    return {
      version: IMAGE_PLAYGROUND_LAYOUT_VERSION,
      editorPosition: value.editorPosition,
      rightPanelWidth: clampRightPanelWidth(rightPanelWidth),
    }
  } catch {
    return fallback
  }
}

function getMentionTagTextLength(el: Element) {
`
const INPUT_BAR_LAYOUT_STATE_MARKER = `  const showPromptExpand = promptExpanded || promptCanExpand
`
const INPUT_BAR_LAYOUT_STATE_REPLACEMENT = `  const showPromptExpand = promptExpanded || promptCanExpand
  const [playgroundLayout, setPlaygroundLayout] = useState<PlaygroundLayoutConfig>(() => readPlaygroundLayout())
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  const [isResizingRightPanel, setIsResizingRightPanel] = useState(false)
  const resizeStartRef = useRef<{ x: number; width: number } | null>(null)
  const effectiveEditorPosition: PlaygroundEditorPosition = viewportWidth < RIGHT_LAYOUT_MIN_VIEWPORT_WIDTH
    ? 'bottom'
    : playgroundLayout.editorPosition
  const isRightEditorLayout = effectiveEditorPosition === 'right'
  const editorPromptExpanded = promptExpanded || isRightEditorLayout

  const updatePlaygroundLayout = useCallback((patch: Partial<PlaygroundLayoutConfig>) => {
    setPlaygroundLayout((current) => {
      const next: PlaygroundLayoutConfig = {
        ...current,
        ...patch,
        version: IMAGE_PLAYGROUND_LAYOUT_VERSION,
      }
      try {
        window.localStorage.setItem(IMAGE_PLAYGROUND_LAYOUT_STORAGE_KEY, JSON.stringify(next))
      } catch {
        // Layout state remains usable when storage is unavailable.
      }
      return next
    })
  }, [])

  useEffect(() => {
    const updateViewportWidth = () => setViewportWidth(window.innerWidth)
    window.addEventListener('resize', updateViewportWidth)
    return () => window.removeEventListener('resize', updateViewportWidth)
  }, [])

  useEffect(() => {
    const root = document.documentElement
    const rightPadding = isRightEditorLayout
      ? String(playgroundLayout.rightPanelWidth + 24) + 'px'
      : '0px'
    const galleryBottomPadding = isRightEditorLayout ? '1.5rem' : '12rem'
    const agentBottomPadding = isRightEditorLayout
      ? '1.5rem'
      : 'calc(var(--input-bar-clearance, 12rem) + 1.5rem)'
    root.style.setProperty('--image-playground-gallery-content-padding-right', rightPadding)
    root.style.setProperty('--image-playground-gallery-content-padding-bottom', galleryBottomPadding)
    root.style.setProperty('--image-playground-agent-content-padding-right', rightPadding)
    root.style.setProperty('--image-playground-agent-content-padding-bottom', agentBottomPadding)
    root.style.setProperty('--image-playground-agent-scroll-bottom', agentBottomPadding)
    root.style.setProperty('--image-playground-right-editor-width', String(playgroundLayout.rightPanelWidth) + 'px')

    return () => {
      root.style.removeProperty('--image-playground-gallery-content-padding-right')
      root.style.removeProperty('--image-playground-gallery-content-padding-bottom')
      root.style.removeProperty('--image-playground-agent-content-padding-right')
      root.style.removeProperty('--image-playground-agent-content-padding-bottom')
      root.style.removeProperty('--image-playground-agent-scroll-bottom')
      root.style.removeProperty('--image-playground-right-editor-width')
    }
  }, [isRightEditorLayout, playgroundLayout.rightPanelWidth])

  useEffect(() => {
    if (!isResizingRightPanel) return

    const handlePointerMove = (event: PointerEvent) => {
      const start = resizeStartRef.current
      if (!start) return
      updatePlaygroundLayout({
        rightPanelWidth: clampRightPanelWidth(start.width + start.x - event.clientX),
      })
    }
    const handlePointerUp = () => {
      resizeStartRef.current = null
      setIsResizingRightPanel(false)
    }

    window.addEventListener('pointermove', handlePointerMove)
    window.addEventListener('pointerup', handlePointerUp)
    window.addEventListener('pointercancel', handlePointerUp)
    return () => {
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', handlePointerUp)
      window.removeEventListener('pointercancel', handlePointerUp)
    }
  }, [isResizingRightPanel, updatePlaygroundLayout])

  const handleRightPanelResizeStart = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!isRightEditorLayout) return
    event.preventDefault()
    resizeStartRef.current = {
      x: event.clientX,
      width: playgroundLayout.rightPanelWidth,
    }
    setIsResizingRightPanel(true)
  }

  const handleRightPanelResizeKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    const delta = event.key === 'ArrowLeft' ? 16 : -16
    updatePlaygroundLayout({
      rightPanelWidth: clampRightPanelWidth(playgroundLayout.rightPanelWidth + delta),
    })
  }
`
const INPUT_BAR_WRAPPER_MARKER = `      <div
        data-input-bar
        className={\`fixed bottom-4 sm:bottom-6 left-1/2 -translate-x-1/2 z-30 w-full max-w-4xl px-3 sm:px-4 transition-all duration-300\${promptExpanded ? ' flex flex-col' : ''}\`}
        style={promptExpanded ? { top: \`\${promptExpandedTop}px\`, transitionProperty: 'none' } : undefined}
      >
`
const INPUT_BAR_WRAPPER_REPLACEMENT = `      {isRightEditorLayout && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-valuemin={MIN_RIGHT_PANEL_WIDTH}
          aria-valuemax={MAX_RIGHT_PANEL_WIDTH}
          aria-valuenow={playgroundLayout.rightPanelWidth}
          aria-label="调整右侧编辑框宽度"
          tabIndex={0}
          onPointerDown={handleRightPanelResizeStart}
          onKeyDown={handleRightPanelResizeKeyDown}
          className="fixed top-14 bottom-0 z-40 w-2 -translate-x-1/2 cursor-col-resize touch-none bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400/70"
          style={{ right: 'var(--image-playground-right-editor-width)' }}
        />
      )}
      <div
        data-input-bar
        className={isRightEditorLayout
          ? 'fixed top-14 bottom-0 right-0 z-30 flex w-[var(--image-playground-right-editor-width)] flex-col px-3 py-4 sm:px-4'
          : 'fixed bottom-4 sm:bottom-6 left-1/2 -translate-x-1/2 z-30 w-full max-w-4xl px-3 sm:px-4 transition-all duration-300' + (promptExpanded ? ' flex flex-col' : '')}
        style={!isRightEditorLayout && promptExpanded
          ? { top: String(promptExpandedTop) + 'px', transitionProperty: 'none' }
          : undefined}
      >
`
const INPUT_BAR_CARD_MARKER = `        <div ref={cardRef} className={\`bg-white/70 dark:bg-gray-900/70 backdrop-blur-2xl border border-white/50 dark:border-white/[0.08] shadow-[0_8px_30px_rgb(0,0,0,0.08)] dark:shadow-[0_8px_30px_rgb(0,0,0,0.3)] rounded-2xl sm:rounded-3xl p-3 sm:p-4 ring-1 ring-black/5 dark:ring-white/10\${promptExpanded ? ' flex min-h-0 flex-1 flex-col' : ''}\`}>
`
const INPUT_BAR_CARD_REPLACEMENT = `        <div ref={cardRef} className={\`bg-white/70 dark:bg-gray-900/70 backdrop-blur-2xl border border-white/50 dark:border-white/[0.08] shadow-[0_8px_30px_rgb(0,0,0,0.08)] dark:shadow-[0_8px_30px_rgb(0,0,0,0.3)] rounded-2xl sm:rounded-3xl p-3 sm:p-4 ring-1 ring-black/5 dark:ring-white/10\${editorPromptExpanded ? ' flex min-h-0 flex-1 flex-col overflow-hidden' : ''}\`}>
`
const INPUT_BAR_REFERENCE_MARKER = `      <div ref={imagesRef}>
`
const INPUT_BAR_REFERENCE_REPLACEMENT = `      <div ref={imagesRef} className={isRightEditorLayout ? 'max-h-[35%] shrink-0 overflow-y-auto custom-scrollbar' : undefined}>
`
const INPUT_BAR_PROMPT_GRID_MARKER = `          <div className={\`relative grid\${promptExpanded ? ' min-h-0 flex-1' : ''}\`}>
`
const INPUT_BAR_PROMPT_GRID_REPLACEMENT = `          <div className={\`relative grid\${editorPromptExpanded ? ' min-h-0 flex-1' : ''}\`}>
`
const INPUT_BAR_PROMPT_EDITOR_MARKER = `              className={\`col-start-1 row-start-1 min-h-[42px] w-full overflow-hidden ios-rounded-scroll-fix whitespace-pre-wrap break-words rounded-2xl border border-gray-200/60 bg-white/50 pl-4 pr-10 py-3 text-sm leading-relaxed shadow-sm outline-none transition-[border-color,box-shadow] duration-200 focus:ring-1 focus:ring-blue-300/40 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-gray-100 dark:focus:ring-blue-500/30\${promptExpanded ? ' !h-full !overflow-y-auto' : ''}\`}
`
const INPUT_BAR_PROMPT_EDITOR_REPLACEMENT = `              className={\`col-start-1 row-start-1 min-h-[42px] w-full overflow-hidden ios-rounded-scroll-fix whitespace-pre-wrap break-words rounded-2xl border border-gray-200/60 bg-white/50 pl-4 pr-10 py-3 text-sm leading-relaxed shadow-sm outline-none transition-[border-color,box-shadow] duration-200 focus:ring-1 focus:ring-blue-300/40 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-gray-100 dark:focus:ring-blue-500/30\${editorPromptExpanded ? ' !h-full !overflow-y-auto' : ''}\`}
`
const INPUT_BAR_PROMPT_EXPAND_MARKER = `            {showPromptExpand && (
`
const INPUT_BAR_PROMPT_EXPAND_REPLACEMENT = `            {showPromptExpand && !isRightEditorLayout && (
`
const INPUT_BAR_HEIGHT_MARKER = `    const maxH = promptExpanded
      ? Math.max(el.parentElement?.clientHeight ?? 0, 80)
      : normalMaxH
`
const INPUT_BAR_HEIGHT_REPLACEMENT = `    const maxH = editorPromptExpanded
      ? Math.max(el.parentElement?.clientHeight ?? 0, 80)
      : normalMaxH
`
const INPUT_BAR_PARAMETERS_MARKER = `          <div className="mt-3">
`
const INPUT_BAR_PARAMETERS_REPLACEMENT = `          <div className={isRightEditorLayout ? 'mt-3 shrink-0' : 'mt-3'}>
`
const INPUT_BAR_DESKTOP_MARKER = `            <div className="hidden sm:flex items-end justify-between gap-3">
              {renderParams('grid-cols-6')}

              <div className="flex gap-2 flex-shrink-0 mb-0.5">
`
const INPUT_BAR_DESKTOP_REPLACEMENT = `            <div className={isRightEditorLayout ? 'hidden sm:flex shrink-0 flex-col gap-3' : 'hidden sm:flex items-end justify-between gap-3'}>
              {renderParams(isRightEditorLayout ? 'grid-cols-[repeat(auto-fit,minmax(136px,1fr))]' : 'grid-cols-6')}

              <div className={isRightEditorLayout ? 'flex shrink-0 items-center justify-end gap-2 border-t border-gray-200/60 pt-3 dark:border-white/[0.08]' : 'flex gap-2 flex-shrink-0 mb-0.5'}>
                {viewportWidth >= RIGHT_LAYOUT_MIN_VIEWPORT_WIDTH && (
                  <button
                    type="button"
                    onClick={() => {
                      if (!isRightEditorLayout) setPromptExpanded(false)
                      updatePlaygroundLayout({ editorPosition: isRightEditorLayout ? 'bottom' : 'right' })
                    }}
                    className="rounded-xl bg-gray-200 p-2.5 text-gray-500 shadow-sm transition-colors hover:bg-gray-300 hover:text-gray-700 dark:bg-white/[0.06] dark:text-gray-300 dark:hover:bg-white/[0.1]"
                    title={isRightEditorLayout ? '移到底部' : '移到右侧'}
                    aria-label={isRightEditorLayout ? '移到底部' : '移到右侧'}
                  >
                    <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                      {isRightEditorLayout ? (
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15h14M5 9h14M9 5l-4 4 4 4M15 19l4-4-4-4" />
                      ) : (
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 5v14M9 5v14M5 9l4-4 4 4M19 15l-4 4-4-4" />
                      )}
                    </svg>
                  </button>
                )}
`
const APP_GALLERY_MARKER = `        <main data-home-main data-drag-select-surface className="pb-48">
`
const APP_GALLERY_REPLACEMENT = `        <main
          data-home-main
          data-drag-select-surface
          className="pb-48"
          style={{
            paddingRight: 'var(--image-playground-gallery-content-padding-right, 0px)',
            paddingBottom: 'var(--image-playground-gallery-content-padding-bottom, 12rem)',
          }}
        >
`
const AGENT_WORKSPACE_MARKER = `          className="flex-1 space-y-4 overflow-visible pb-[calc(var(--input-bar-clearance,12rem)+1.5rem)] px-1 lg:pt-14 lg:px-4"
`
const AGENT_WORKSPACE_REPLACEMENT = `          className="flex-1 space-y-4 overflow-visible px-1 lg:pt-14 lg:px-4"
          style={{
            paddingRight: 'var(--image-playground-agent-content-padding-right, 0px)',
            paddingBottom: 'var(--image-playground-agent-content-padding-bottom, calc(var(--input-bar-clearance, 12rem) + 1.5rem))',
          }}
`
const AGENT_SCROLL_BUTTON_MARKER = `          className={\`fixed bottom-[calc(var(--input-bar-clearance,12rem)+1.5rem)] left-1/2 -translate-x-1/2 z-30 flex h-10 w-10 items-center justify-center rounded-full bg-white/90 backdrop-blur shadow-[0_2px_12px_rgba(0,0,0,0.1)] border border-gray-200/50 text-gray-500 transition-all duration-300 hover:bg-gray-50 hover:text-gray-800 dark:border-white/[0.08] dark:bg-gray-800/90 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200 \${
`
const AGENT_SCROLL_BUTTON_REPLACEMENT = `          className={\`fixed -translate-x-1/2 z-30 flex h-10 w-10 items-center justify-center rounded-full bg-white/90 backdrop-blur shadow-[0_2px_12px_rgba(0,0,0,0.1)] border border-gray-200/50 text-gray-500 transition-all duration-300 hover:bg-gray-50 hover:text-gray-800 dark:border-white/[0.08] dark:bg-gray-800/90 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200 \${
`
const AGENT_SCROLL_STYLE_MARKER = `          aria-label="滚动到底部"
`
const AGENT_SCROLL_STYLE_REPLACEMENT = `          style={{
            bottom: 'var(--image-playground-agent-scroll-bottom, calc(var(--input-bar-clearance, 12rem) + 1.5rem))',
            left: 'calc((100% - var(--image-playground-agent-content-padding-right, 0px)) / 2)',
          }}
          aria-label="滚动到底部"
`
const PERSISTENCE_MARKER = `export function getPersistedState(state: AppState) {
  const settings = normalizeSettings(state.settings)
`
const RESPONSE_OUTPUT_MERGE_MARKER = `function mergeResponseOutputItems(previous: ResponsesOutputItem[], next: ResponsesOutputItem[]) {
  const merged = [...previous]
  for (const item of next) {
    const index = item.id ? merged.findIndex((existing) => existing.id === item.id) : -1
    if (index >= 0) merged[index] = item
    else merged.push(item)
  }
  return merged
}
`
const RESPONSE_OUTPUT_MERGE_REPLACEMENT = `function mergeResponseOutputItems(previous: ResponsesOutputItem[], next: ResponsesOutputItem[]) {
  const merged = [...previous]
  for (const item of next) {
    let index = item.id ? merged.findIndex((existing) => existing.id === item.id) : -1
    const callId = item.call_id?.trim()
    if (index < 0 && callId && (item.type === 'function_call' || item.type === 'function_call_output')) {
      index = merged.findIndex((existing) => existing.type === item.type && existing.call_id?.trim() === callId)
    }
    if (index >= 0) merged[index] = item
    else merged.push(item)
  }
  return merged
}
`
const AGENT_IMAGE_FUNCTION_CALL_MARKER = `      if (imageFunctionCalls.length > 0) {
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
`
const AGENT_IMAGE_FUNCTION_CALL_REPLACEMENT = `      const customImageFunctionCalls: ResponsesOutputItem[] = []
      const customImageFunctionCallIndexById = new Map<string, number>()
      for (const fc of [...imageFunctionCalls, ...batchFunctionCalls]) {
        const callId = fc.call_id?.trim()
        if (!callId) {
          customImageFunctionCalls.push(fc)
          continue
        }

        const existingIndex = customImageFunctionCallIndexById.get(callId)
        if (existingIndex === undefined) {
          customImageFunctionCallIndexById.set(callId, customImageFunctionCalls.length)
          customImageFunctionCalls.push(fc)
        } else {
          customImageFunctionCalls[existingIndex] = fc
        }
      }

      const imageFunctionCallOutputs = await Promise.all(
        customImageFunctionCalls.map(async (fc) => ({
          type: 'function_call_output',
          call_id: fc.call_id,
          output: fc.name === 'generate_image_batch'
            ? await executeBatchFunctionCall(fc)
            : await executeSingleImageFunctionCall(fc),
        } satisfies ResponsesOutputItem)),
      )
      functionCallOutputs.push(...imageFunctionCallOutputs)
`

const AGENT_IMAGE_TASK_DURABLE_COMPLETION_MARKER = `      updateTaskInStore(taskId, {
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
`
const AGENT_IMAGE_TASK_DURABLE_COMPLETION_REPLACEMENT = `      updateTaskInStore(taskId, {
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
      const completedTask = useStore.getState().tasks.find((task) => task.id === taskId)
      if (completedTask) await putTask(completedTask)
      useStore.getState().setTaskStreamPreview(taskId)
`

const HYBRID_BATCH_TASK_COMPLETION_MARKER = `        // If not streaming and we have an image, complete the pre-created task.
        if (batchResult.image && !shouldStreamAssistantMessage) {
          await completeAgentImageTask({ ...batchResult.image, toolCallId: batchToolCallId }, batchResult.rawResponsePayload)
        }
`
const HYBRID_BATCH_TASK_COMPLETION_REPLACEMENT = `        // Hybrid image requests do not emit Agent image-tool completion callbacks,
        // so always complete their pre-created task card from the returned image.
        if (batchResult.image && (requestSettings.agentApiConfigMode === 'hybrid' || !shouldStreamAssistantMessage)) {
          await completeAgentImageTask({ ...batchResult.image, toolCallId: batchToolCallId }, batchResult.rawResponsePayload)
        }
`

const IN_PLACE_TASK_RETRY_MARKER = `/** 重试失败的任务：创建新任务并执行 */
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
`
const IN_PLACE_TASK_RETRY_REPLACEMENT = `/** 重试任务：复用原任务卡片和任务 ID，避免在画廊或 Agent 中创建不可见的新卡片 */
export async function retryTask(task: TaskRecord) {
  const state = useStore.getState()
  const currentTask = state.tasks.find((item) => item.id === task.id)
  if (!currentTask || currentTask.status === 'running') return

  const { settings } = state
  const activeProfile = getActiveApiProfile(settings)
  const normalizedParams = normalizeParamsForSettings(currentTask.params, settings, { hasInputImages: currentTask.inputImageIds.length > 0 })
  const shouldUseTransparentOutput = normalizedParams.output_format === 'png' && normalizedParams.transparent_output
  const taskParams = shouldUseTransparentOutput
    ? getTransparentRequestParams(normalizedParams)
    : { ...normalizedParams, transparent_output: false }
  const transparentMeta = taskParams.transparent_output
    ? createTransparentOutputMeta(currentTask.prompt.trim())
    : null
  const staleImageIds = uniqueIds([
    ...currentTask.outputImages,
    ...(currentTask.transparentOriginalImages ?? []),
    ...(currentTask.streamPartialImageIds ?? []),
  ])
  const retriedTask: TaskRecord = {
    ...currentTask,
    params: taskParams,
    apiProvider: activeProfile.provider,
    apiProfileId: activeProfile.id,
    apiProfileName: activeProfile.name,
    apiMode: activeProfile.apiMode,
    apiModel: activeProfile.model,
    falRequestId: undefined,
    falEndpoint: undefined,
    falRecoverable: false,
    customTaskId: undefined,
    customRecoverable: false,
    actualParams: undefined,
    actualParamsByImage: undefined,
    revisedPromptByImage: undefined,
    transparentOutput: transparentMeta?.transparentOutput,
    transparentPrompt: transparentMeta?.effectivePrompt,
    transparentOriginalImages: undefined,
    outputImages: [],
    outputErrors: undefined,
    streamPartialImageIds: undefined,
    rawImageUrls: undefined,
    rawResponsePayload: undefined,
    status: 'running',
    error: null,
    createdAt: Date.now(),
    finishedAt: null,
    elapsed: null,
  }

  clearOpenAIWatchdogTimer(currentTask.id)
  clearFalRecoveryTimer(currentTask.id)
  clearCustomRecoveryTimer(currentTask.id)
  state.setTaskStreamPreview(currentTask.id)
  state.setTasks(state.tasks.map((item) => item.id === currentTask.id ? retriedTask : item))
  await putTask(retriedTask)
  void deleteUnreferencedImageIds(staleImageIds)

  executeTask(currentTask.id)
}
`


const PERSISTENCE_REPLACEMENT = `export function getPersistedState(state: AppState) {
  const normalizedSettings = normalizeSettings(state.settings)
  const managedProfileIds = new Set(['new-api-managed', 'new-api-managed-agent'])
  const userProfiles = normalizedSettings.profiles.filter((profile) => !managedProfileIds.has(profile.id))
  const profiles = userProfiles.length > 0 ? userProfiles : DEFAULT_SETTINGS.profiles
  const profileIds = new Set(profiles.map((profile) => profile.id))
  const rememberedToolSettings = (() => {
    try {
      if (typeof window === 'undefined') return null
      const raw = window.localStorage.getItem('new-api:image-playground:tool-settings')
      if (!raw) return null
      return JSON.parse(raw) as Record<string, unknown>
    } catch {
      return null
    }
  })()
  const rememberedActiveProfileId = typeof rememberedToolSettings?.activeProfileId === 'string'
    && profileIds.has(rememberedToolSettings.activeProfileId)
    ? rememberedToolSettings.activeProfileId
    : null
  const activeProfileId = rememberedActiveProfileId
    ?? (!managedProfileIds.has(normalizedSettings.activeProfileId) && profileIds.has(normalizedSettings.activeProfileId)
      ? normalizedSettings.activeProfileId
      : profiles[0].id)
  const rememberedAgentApiConfigMode = rememberedToolSettings?.agentApiConfigMode
  const agentApiConfigMode = rememberedAgentApiConfigMode === 'native'
    || rememberedAgentApiConfigMode === 'hybrid'
    || rememberedAgentApiConfigMode === 'off'
    ? rememberedAgentApiConfigMode
    : (managedProfileIds.has(normalizedSettings.agentTextProfileId ?? '')
      || managedProfileIds.has(normalizedSettings.agentImageProfileId ?? '')
      ? 'off'
      : normalizedSettings.agentApiConfigMode)
  const rememberedAgentTextProfileId = typeof rememberedToolSettings?.agentTextProfileId === 'string'
    && profileIds.has(rememberedToolSettings.agentTextProfileId)
    ? rememberedToolSettings.agentTextProfileId
    : null
  const rememberedAgentImageProfileId = typeof rememberedToolSettings?.agentImageProfileId === 'string'
    && profileIds.has(rememberedToolSettings.agentImageProfileId)
    ? rememberedToolSettings.agentImageProfileId
    : null
  const settings = normalizeSettings({
    ...normalizedSettings,
    profiles,
    activeProfileId,
    agentApiConfigMode,
    agentTextProfileId: rememberedAgentTextProfileId
      ?? (!managedProfileIds.has(normalizedSettings.agentTextProfileId ?? '')
        ? normalizedSettings.agentTextProfileId
        : null),
    agentImageProfileId: rememberedAgentImageProfileId
      ?? (!managedProfileIds.has(normalizedSettings.agentImageProfileId ?? '')
        ? normalizedSettings.agentImageProfileId
        : activeProfileId),
  })
`

function replaceExactlyOnce(source, marker, replacement, markerName = 'entry') {
  const firstIndex = source.indexOf(marker)
  const lastIndex = source.lastIndexOf(marker)
  if (firstIndex === -1 || firstIndex !== lastIndex) {
    throw new Error(`upstream ${markerName} marker ${JSON.stringify(marker.trim())} did not match exactly once`)
  }
  return `${source.slice(0, firstIndex)}${replacement}${source.slice(firstIndex + marker.length)}`
}

export async function applyUpstreamPatch(upstreamRoot, options = {}) {
  const mainPath = path.join(upstreamRoot, 'src', 'main.tsx')
  const bridgePath = path.join(upstreamRoot, 'src', 'lib', 'newApiBridge.ts')
  const storePath = path.join(upstreamRoot, 'src', 'store.ts')
  const appPath = path.join(upstreamRoot, 'src', 'App.tsx')
  const inputBarPath = path.join(upstreamRoot, 'src', 'components', 'InputBar.tsx')
  const agentWorkspacePath = path.join(upstreamRoot, 'src', 'components', 'AgentWorkspace.tsx')
  const defaultBridgePath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'new-api-bridge.ts')
  const bridgeSource = options.bridgeSource ?? await readFile(defaultBridgePath, 'utf8')
  const mainSource = await readFile(mainPath, 'utf8')
  const storeSource = await readFile(storePath, 'utf8')
  const appSource = await readFile(appPath, 'utf8')
  const inputBarSource = await readFile(inputBarPath, 'utf8')
  const agentWorkspaceSource = await readFile(agentWorkspacePath, 'utf8')
  const withImport = replaceExactlyOnce(
    mainSource,
    IMPORT_MARKER,
    `${IMPORT_MARKER}import { installNewApiBridge } from './lib/newApiBridge'\n`,
  )
  const withBridge = replaceExactlyOnce(
    withImport,
    INSTALL_MARKER,
    `installNewApiBridge()\n\n${INSTALL_MARKER}`,
  )
  const patchedSource = replaceExactlyOnce(
    withBridge,
    SERVICE_WORKER_MARKER,
    SERVICE_WORKER_REPLACEMENT,
    'service worker',
  )
  const storeWithPersistence = replaceExactlyOnce(
    storeSource,
    PERSISTENCE_MARKER,
    PERSISTENCE_REPLACEMENT,
    'persistence',
  )
  const storeWithMergedResponseOutput = replaceExactlyOnce(
    storeWithPersistence,
    RESPONSE_OUTPUT_MERGE_MARKER,
    RESPONSE_OUTPUT_MERGE_REPLACEMENT,
    'Agent response output merge',
  )
  const storeWithAgentCalls = replaceExactlyOnce(
    storeWithMergedResponseOutput,
    AGENT_IMAGE_FUNCTION_CALL_MARKER,
    AGENT_IMAGE_FUNCTION_CALL_REPLACEMENT,
    'Agent image function calls',
  )
  const storeWithDurableAgentImageTaskCompletion = replaceExactlyOnce(
    storeWithAgentCalls,
    AGENT_IMAGE_TASK_DURABLE_COMPLETION_MARKER,
    AGENT_IMAGE_TASK_DURABLE_COMPLETION_REPLACEMENT,
    'Agent image task durable completion',
  )
  const storeWithHybridBatchTaskCompletion = replaceExactlyOnce(
    storeWithDurableAgentImageTaskCompletion,
    HYBRID_BATCH_TASK_COMPLETION_MARKER,
    HYBRID_BATCH_TASK_COMPLETION_REPLACEMENT,
    'hybrid Agent batch task completion',
  )
  const patchedStoreSource = replaceExactlyOnce(
    storeWithHybridBatchTaskCompletion,
    IN_PLACE_TASK_RETRY_MARKER,
    IN_PLACE_TASK_RETRY_REPLACEMENT,
    'task retry',
  )
  const inputBarWithLayoutHelpers = replaceExactlyOnce(
    inputBarSource,
    INPUT_BAR_LAYOUT_HELPERS_MARKER,
    INPUT_BAR_LAYOUT_HELPERS_REPLACEMENT,
    'InputBar layout helpers',
  )
  const inputBarWithLayoutState = replaceExactlyOnce(
    inputBarWithLayoutHelpers,
    INPUT_BAR_LAYOUT_STATE_MARKER,
    INPUT_BAR_LAYOUT_STATE_REPLACEMENT,
    'InputBar layout state',
  )
  const inputBarWithHeight = replaceExactlyOnce(
    inputBarWithLayoutState,
    INPUT_BAR_HEIGHT_MARKER,
    INPUT_BAR_HEIGHT_REPLACEMENT,
    'InputBar prompt sizing',
  )
  const inputBarWithReferenceImages = replaceExactlyOnce(
    inputBarWithHeight,
    INPUT_BAR_REFERENCE_MARKER,
    INPUT_BAR_REFERENCE_REPLACEMENT,
    'InputBar reference images',
  )
  const inputBarWithWrapper = replaceExactlyOnce(
    inputBarWithReferenceImages,
    INPUT_BAR_WRAPPER_MARKER,
    INPUT_BAR_WRAPPER_REPLACEMENT,
    'InputBar wrapper',
  )
  const inputBarWithCard = replaceExactlyOnce(
    inputBarWithWrapper,
    INPUT_BAR_CARD_MARKER,
    INPUT_BAR_CARD_REPLACEMENT,
    'InputBar panel',
  )
  const inputBarWithPromptGrid = replaceExactlyOnce(
    inputBarWithCard,
    INPUT_BAR_PROMPT_GRID_MARKER,
    INPUT_BAR_PROMPT_GRID_REPLACEMENT,
    'InputBar prompt grid',
  )
  const inputBarWithPromptEditor = replaceExactlyOnce(
    inputBarWithPromptGrid,
    INPUT_BAR_PROMPT_EDITOR_MARKER,
    INPUT_BAR_PROMPT_EDITOR_REPLACEMENT,
    'InputBar prompt editor',
  )
  const inputBarWithPromptExpand = replaceExactlyOnce(
    inputBarWithPromptEditor,
    INPUT_BAR_PROMPT_EXPAND_MARKER,
    INPUT_BAR_PROMPT_EXPAND_REPLACEMENT,
    'InputBar prompt expand',
  )
  const inputBarWithParameters = replaceExactlyOnce(
    inputBarWithPromptExpand,
    INPUT_BAR_PARAMETERS_MARKER,
    INPUT_BAR_PARAMETERS_REPLACEMENT,
    'InputBar parameter section',
  )
  const patchedInputBarSource = replaceExactlyOnce(
    inputBarWithParameters,
    INPUT_BAR_DESKTOP_MARKER,
    INPUT_BAR_DESKTOP_REPLACEMENT,
    'InputBar desktop controls',
  )
  const patchedAppSource = replaceExactlyOnce(
    appSource,
    APP_GALLERY_MARKER,
    APP_GALLERY_REPLACEMENT,
    'gallery right panel clearance',
  )
  const agentWorkspaceWithClearance = replaceExactlyOnce(
    agentWorkspaceSource,
    AGENT_WORKSPACE_MARKER,
    AGENT_WORKSPACE_REPLACEMENT,
    'Agent workspace right panel clearance',
  )
  const agentWorkspaceWithScrollButton = replaceExactlyOnce(
    agentWorkspaceWithClearance,
    AGENT_SCROLL_BUTTON_MARKER,
    AGENT_SCROLL_BUTTON_REPLACEMENT,
    'Agent scroll button clearance',
  )
  const patchedAgentWorkspaceSource = replaceExactlyOnce(
    agentWorkspaceWithScrollButton,
    AGENT_SCROLL_STYLE_MARKER,
    AGENT_SCROLL_STYLE_REPLACEMENT,
    'Agent scroll button position',
  )

  await writeFile(mainPath, patchedSource)
  await writeFile(storePath, patchedStoreSource)
  await writeFile(appPath, patchedAppSource)
  await writeFile(inputBarPath, patchedInputBarSource)
  await writeFile(agentWorkspacePath, patchedAgentWorkspaceSource)
  await writeFile(bridgePath, bridgeSource)
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const upstreamRoot = process.argv[2]
  if (!upstreamRoot) throw new Error('usage: node patch-upstream.mjs <upstream-root>')
  await applyUpstreamPatch(path.resolve(upstreamRoot))
}
