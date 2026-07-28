import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { applyUpstreamPatch } from "./patch-upstream.mjs";

const FIXTURE_FILES = {
  "web/index.html":
    'var s = JSON.parse(localStorage.getItem("infinite-canvas:theme_store") || "{}");\n',
  "web/src/main.tsx": `import React from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";

import { AppProviders } from "@/components/layout/app-providers";
import { initAnalytics } from "@/lib/analytics";
import { router } from "@/router";

initAnalytics();

document.body.style.fontFamily = '"SF Pro Display","SF Pro Text","PingFang SC","Microsoft YaHei","Helvetica Neue",sans-serif';

createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
        <AppProviders>
            <RouterProvider router={router} />
        </AppProviders>
    </React.StrictMode>,
);
`,
  "web/src/router.tsx": `export const router = createBrowserRouter([
    { path: "*", element: <NotFound /> },
]);
`,
  "web/src/stores/use-config-store.ts": `import { nanoid } from "nanoid";
export const CONFIG_STORE_KEY = "infinite-canvas:ai_config_store";
partialize: (state) => ({ config: state.config, webdav: state.webdav }),
export function resolveModelRequestConfig(config: AiConfig, value: string) {
    const channel = resolveModelChannel(config, value);
    return {
        ...config,
        model: modelOptionName(value || config.model),
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
    };
}
`,
  "web/src/lib/localforage-storage.ts": `import localforage from "localforage";
import type { StateStorage } from "zustand/middleware";

localforage.config({
    name: "infinite-canvas",
    storeName: "app_state",
});

export const localForageStorage: StateStorage = {
    getItem: async (name) => {
        if (typeof window === "undefined") return null;
        try {
            return (await localforage.getItem<string>(name)) || null;
        } catch {
            return window.localStorage.getItem(name);
        }
    },
    setItem: async (name, value) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.setItem(name, value);
        } catch {
            window.localStorage.setItem(name, value);
        }
    },
    removeItem: async (name) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.removeItem(name);
        } catch {
            window.localStorage.removeItem(name);
        }
    },
};
`,
  "web/src/stores/use-prompt-source-store.ts": `import { persist } from "zustand/middleware";
const PROMPT_SOURCE_STORE_KEY = "infinite-canvas:prompt_source_store_v2";
`,
  "web/src/stores/use-theme-store.ts": `import { persist } from "zustand/middleware";
{ name: "infinite-canvas:theme_store" }
`,
  "web/src/stores/use-asset-store.ts": `import { localForageStorage } from "@/lib/localforage-storage";
const ASSET_STORE_KEY = "infinite-canvas:asset_store";
`,
  "web/src/stores/canvas/use-plugin-store.ts": `import { localForageStorage } from "@/lib/localforage-storage";
name: "infinite-canvas:plugin_store",
`,
  "web/src/stores/canvas/use-canvas-store.ts": `import { localForageStorage } from "@/lib/localforage-storage";
const CANVAS_STORE_KEY = "infinite-canvas:canvas_store";
`,
  "web/src/stores/use-agent-store.ts": `import type { CanvasAgentOp, CanvasAgentSnapshot } from "@/lib/canvas/canvas-agent-ops";
const CONNECT_TIMEOUT_MS = 6000;
Number(localStorage.getItem("canvas-agent-panel-width"))
localStorage.getItem("canvas-agent-url")
localStorage.getItem("canvas-agent-token")
localStorage.setItem("canvas-agent-url", endpoint);
        localStorage.setItem("canvas-agent-token", token);
`,
  "web/src/stores/use-canvas-side-panel-store.ts": `import { create } from "zustand";
const WIDTH_KEY = "canvas-side-panel-width";
const OPEN_KEY = "canvas-side-panel-open";
localStorage.getItem(WIDTH_KEY)
`,
  "web/src/components/agent/agent-panel.tsx": `import { canvasThemes } from "@/lib/canvas-theme";
localStorage.setItem("canvas-agent-panel-width", String(nextWidth));
`,
  "web/src/components/canvas/canvas-local-agent-panel.tsx": `import { randomId } from "@/lib/utils";
localStorage.setItem("canvas-agent-url", endpoint);
        localStorage.setItem("canvas-agent-token", token);
`,
  "web/src/components/canvas/canvas-side-panel.tsx": `CANVAS_SIDE_PANEL_MOTION_MS,
    useCanvasSidePanelStore,
localStorage.setItem("canvas-side-panel-width", String(nextWidth));
`,
  "web/src/components/layout/app-config-modal.tsx": `const modelGroups: ModelGroup[] = [
    { capability: "image", modelKey: "imageModel", defaultLabel: "默认生图模型" },
    { capability: "video", modelKey: "videoModel", defaultLabel: "默认视频模型" },
    { capability: "text", modelKey: "textModel", defaultLabel: "默认文本模型" },
    { capability: "audio", modelKey: "audioModel", defaultLabel: "默认音频模型" },
];

const webdavDomainKeys: AppSyncDomainKey[] = ["canvas", "assets", "image-workbench", "video-workbench"];
const webdavDomainLabels: Record<AppSyncDomainKey, string> = {
    canvas: "画布",
};

export function AppConfigPanel() {
    const config = useConfigStore((state) => state.config);
    const webdavReady = Boolean(webdav.url.trim());
    const editingChannel = config.channels.find((channel) => channel.id === editingChannelId) || null;
    useEffect(() => setActiveTab(initialTab), [initialTab]);

    return (
        <Tabs
            items={[
                {
                    key: "channels",
                    label: "渠道",
                    children: (
                            <div>
                                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                                    <div className="text-xs text-stone-500">每个渠道选择一个协议并拉取模型，为每个模型指定能力（生图/视频/文本/音频），并可自定义调用脚本。</div>
                                    <Button type="primary" icon={<Plus className="size-4" />} onClick={addChannel}>
                                        新增渠道
                                    </Button>
                                </div>
                                <div className="space-y-2">
                                    {config.channels.map((channel) => (
                                        <div key={channel.id}>
                                            <div className="flex shrink-0 gap-2">
                                                <Button size="small" icon={<Pencil className="size-3.5" />} onClick={() => setEditingChannelId(channel.id)}>
                                                    编辑
                                                </Button>
                                                <Button size="small" danger icon={<Trash2 className="size-3.5" />} onClick={() => deleteChannel(channel.id)} />
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                    ),
                },
            ]}
        />
    );
}
`,
  "web/src/components/canvas/canvas-node-hover-toolbar.tsx": `import { useCopyText } from "@/hooks/use-copy-text";
import { IMAGE_QUICK_TOOLS_STORAGE_KEY, buildImageToolbarTools, defaultImageQuickToolIds, readImageQuickToolsConfig, type ImageQuickToolId } from "./canvas-image-toolbar-tools";
window.localStorage.getItem(IMAGE_QUICK_TOOLS_STORAGE_KEY)
window.localStorage.removeItem(IMAGE_QUICK_TOOLS_STORAGE_KEY)
window.localStorage.setItem(IMAGE_QUICK_TOOLS_STORAGE_KEY, JSON.stringify(config));
`,
  "web/src/lib/canvas/canvas-event-bus.ts": `import type { PluginStorage } from "@/types/canvas-plugin";
localforage.createInstance({ name: "infinite-canvas-plugins", storeName: pluginId })
        set: async (key, value) => {
            await store!.setItem(key, value);
        },
        remove: async (key) => {
            await store!.removeItem(key);
        },
`,
  "web/src/pages/image/index.tsx": `import localforage from "localforage";
name: "infinite-canvas"
    useEffect(() => {
        void refreshLogs();
    }, []);
`,
  "web/src/pages/video/index.tsx": `import localforage from "localforage";
name: "infinite-canvas"
    useEffect(() => {
        void refreshLogs();
    }, []);
`,
  "web/src/services/image-storage.ts": `import { nanoid } from "nanoid";
name: "infinite-canvas"
`,
  "web/src/services/file-storage.ts": `import { nanoid } from "nanoid";
name: "infinite-canvas"
`,
  "web/src/services/api/prompts.ts": `import localforage from "localforage";
name: "infinite-canvas"
`,
  "web/src/services/app-sync.ts": `import localforage from "localforage";
const imageLogStore = localforage.createInstance({ name: "infinite-canvas", storeName: "image_generation_logs" });
const videoLogStore = localforage.createInstance({ name: "infinite-canvas", storeName: "video_generation_logs" });
`,
  "web/src/lib/canvas/canvas-generation-helpers.ts": `import { resolveImageUrl, uploadImage } from "@/services/image-storage";
import { resolveMediaUrl } from "@/services/file-storage";
import { imageMetadata } from "@/lib/canvas/canvas-node-factory";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

export async function hydrateCanvasImages(nodes: CanvasNodeData[]) {
    return Promise.all(
        nodes.map(async (node) => {
            const content = node.metadata?.content;
            if ((node.type === CanvasNodeType.Video || node.type === CanvasNodeType.Audio) && node.metadata?.storageKey) return { ...node, metadata: { ...node.metadata, content: await resolveMediaUrl(node.metadata.storageKey, content) } };
            if (node.type !== CanvasNodeType.Image || !content) return node;
            if (node.metadata?.storageKey) return { ...node, metadata: { ...node.metadata, content: await resolveImageUrl(node.metadata.storageKey, content) } };
            if (!content.startsWith("data:image/")) return node;
            return { ...node, metadata: { ...node.metadata, ...imageMetadata(await uploadImage(content)) } };
        }),
    );
}
`,
  "web/src/pages/canvas/index.tsx": "import { useEffect, useRef } from \"react\";\nimport { useNavigate, useSearchParams } from \"react-router-dom\";\nimport { App, Button } from \"antd\";\nimport { Download, FileUp, Plus } from \"lucide-react\";\n\nimport { readZip } from \"@/lib/zip\";\nimport { setMediaBlob } from \"@/services/file-storage\";\nimport { setImageBlob } from \"@/services/image-storage\";\nimport { CanvasDeleteProjectsDialog } from \"@/components/canvas/canvas-delete-projects-dialog\";\nimport { CanvasProjectCard } from \"@/components/canvas/canvas-project-card\";\nimport type { CanvasExportFile } from \"@/types/canvas-export\";\nimport { useCanvasStore } from \"@/stores/canvas/use-canvas-store\";\nimport { useCanvasUiStore } from \"@/stores/canvas/use-canvas-ui-store\";\nimport { exportCanvasProjects } from \"@/lib/canvas/canvas-export\";\n\nexport default function CanvasPage() {\n    const { message } = App.useApp();\n    const navigate = useNavigate();\n    const [searchParams] = useSearchParams();\n    const inputRef = useRef<HTMLInputElement>(null);\n    const autoOpenRef = useRef(false);\n    const hydrated = useCanvasStore((state) => state.hydrated);\n    const projects = useCanvasStore((state) => state.projects);\n    const createProject = useCanvasStore((state) => state.createProject);\n    const importProject = useCanvasStore((state) => state.importProject);\n    const selectedIds = useCanvasUiStore((state) => state.selectedProjectIds);\n    const setDeleteIds = useCanvasUiStore((state) => state.setDeleteProjectIds);\n\n    const mode = searchParams.get(\"mode\");\n    const agentMode = mode === \"new\" || mode === \"recent\" || mode === \"choose\";\n    const agentQuery = agentMode ? `?${searchParams.toString()}` : \"\";\n    const enterProject = (id: string) => {\n        navigate(`/canvas/${id}${agentQuery}`);\n    };\n    const createAndEnter = () => enterProject(createProject(`无限画布 ${projects.length + 1}`));\n    const importCanvas = async (file?: File) => {\n        if (!file) return;\n        try {\n            const zip = await readZip(file);\n            const projectFile = zip.get(\"projects.json\");\n            if (!projectFile) throw new Error(\"missing projects.json\");\n            const data = JSON.parse(await projectFile.text()) as CanvasExportFile;\n            await Promise.all(\n                data.projects.flatMap((project) =>\n                    project.files.map(async (item) => {\n                        const blob = zip.get(item.path);\n                        if (!blob) return;\n                        const typedBlob = blob.type ? blob : blob.slice(0, blob.size, item.mimeType);\n                        await (item.storageKey.startsWith(\"image:\") ? setImageBlob(item.storageKey, typedBlob) : setMediaBlob(item.storageKey, typedBlob));\n                    }),\n                ),\n            );\n            data.projects.forEach((item) => importProject(item.project));\n            message.success(`已导入 ${data.projects.length} 个画布`);\n        } catch {\n            message.error(\"导入失败，请选择有效的画布压缩包\");\n        } finally {\n            if (inputRef.current) inputRef.current.value = \"\";\n        }\n    };\n\n    useEffect(() => {\n        if (!hydrated || autoOpenRef.current || (mode !== \"new\" && mode !== \"recent\")) return;\n        autoOpenRef.current = true;\n        enterProject(mode === \"new\" ? createProject(`无限画布 ${projects.length + 1}`) : projects[0]?.id || createProject(`无限画布 ${projects.length + 1}`));\n    }, [createProject, hydrated, mode, projects]);\n\n    if (hydrated && (mode === \"new\" || mode === \"recent\")) return <main className=\"flex h-full items-center justify-center bg-background text-sm text-stone-500\">正在打开画布...</main>;\n\n    return (\n        <main className=\"h-full overflow-auto bg-background text-stone-950 dark:text-stone-100\">\n            <div className=\"mx-auto flex w-full max-w-6xl flex-col gap-8 px-6 py-10\">\n                <header className=\"flex flex-wrap items-end justify-between gap-4 border-b border-stone-200 pb-6 dark:border-stone-800\">\n                    <div>\n                        <p className=\"text-xs text-stone-500\">画布库</p>\n                        <h1 className=\"mt-3 text-3xl font-semibold\">无限画布</h1>\n                    </div>\n                    <div className=\"flex items-center gap-2\">\n                        {selectedIds.length ? (\n                            <>\n                                <Button disabled={!hydrated} icon={<Download className=\"size-4\" />} onClick={() => void exportCanvasProjects(projects.filter((project) => selectedIds.includes(project.id)), `无限画布-${selectedIds.length}个项目`)}>\n                                    导出选中\n                                </Button>\n                                <Button disabled={!hydrated} onClick={() => setDeleteIds(selectedIds)}>\n                                    删除选中\n                                </Button>\n                            </>\n                        ) : null}\n                        {projects.length ? (\n                            <Button disabled={!hydrated} onClick={() => setDeleteIds(projects.map((project) => project.id))}>\n                                删除全部\n                            </Button>\n                        ) : null}\n                        <Button disabled={!hydrated} icon={<FileUp className=\"size-4\" />} onClick={() => inputRef.current?.click()}>\n                            导入画布\n                        </Button>\n                        <Button disabled={!hydrated} type=\"primary\" icon={<Plus className=\"size-4\" />} onClick={createAndEnter}>\n                            新建画布\n                        </Button>\n                    </div>\n                </header>\n\n                {!hydrated ? (\n                    <section className=\"flex min-h-[360px] items-center justify-center border-y border-stone-200 text-sm text-stone-500 dark:border-stone-800\">正在加载画布...</section>\n                ) : projects.length ? (\n                    <div className=\"grid gap-5 sm:grid-cols-2 xl:grid-cols-3\">\n                        {projects.map((project) => (\n                            <CanvasProjectCard key={project.id} project={project} />\n                        ))}\n                    </div>\n                ) : (\n                    <section className=\"flex min-h-[360px] flex-col items-center justify-center border-y border-stone-200 text-center dark:border-stone-800\">\n                        <h2 className=\"text-xl font-medium\">还没有画布</h2>\n                        <p className=\"mt-3 text-sm text-stone-500\">新建一个画布后，就可以独立保存节点、连线和画布外观。</p>\n                        <Button type=\"primary\" className=\"mt-6\" icon={<Plus className=\"size-4\" />} onClick={createAndEnter}>\n                            新建画布\n                        </Button>\n                    </section>\n                )}\n            </div>\n\n            <input ref={inputRef} type=\"file\" accept=\"application/zip,.zip\" className=\"hidden\" onChange={(event) => void importCanvas(event.target.files?.[0])} />\n            <CanvasDeleteProjectsDialog />\n        </main>\n    );\n}\n",
  "web/src/components/canvas/canvas-project-card.tsx": "import { Check, Download, Pencil, Trash2, X } from \"lucide-react\";\nimport { useNavigate, useSearchParams } from \"react-router-dom\";\nimport { Button, Input } from \"antd\";\n\nimport { useCanvasStore, type CanvasProject } from \"@/stores/canvas/use-canvas-store\";\nimport { useCanvasUiStore } from \"@/stores/canvas/use-canvas-ui-store\";\nimport { exportCanvasProjects } from \"@/lib/canvas/canvas-export\";\n\nexport function CanvasProjectCard({ project }: { project: CanvasProject }) {\n    const navigate = useNavigate();\n    const [searchParams] = useSearchParams();\n    const renameProject = useCanvasStore((state) => state.renameProject);\n    const selectedIds = useCanvasUiStore((state) => state.selectedProjectIds);\n    const editingId = useCanvasUiStore((state) => state.editingProjectId);\n    const editingTitle = useCanvasUiStore((state) => state.editingProjectTitle);\n    const startEditing = useCanvasUiStore((state) => state.startEditingProject);\n    const setEditingTitle = useCanvasUiStore((state) => state.setEditingProjectTitle);\n    const stopEditing = useCanvasUiStore((state) => state.stopEditingProject);\n    const toggleSelected = useCanvasUiStore((state) => state.toggleSelectedProjectId);\n    const setDeleteIds = useCanvasUiStore((state) => state.setDeleteProjectIds);\n    const editing = editingId === project.id;\n    const selected = selectedIds.includes(project.id);\n    const open = () => navigate(`/canvas/${project.id}${searchParams.toString() ? `?${searchParams.toString()}` : \"\"}`);\n    const saveTitle = () => {\n        renameProject(project.id, editingTitle);\n        stopEditing();\n    };\n\n    return (\n        <article className=\"group flex min-h-44 cursor-pointer flex-col justify-between rounded-2xl bg-[#f1eee8] p-5 transition hover:bg-[#ebe6dc] dark:bg-white/5 dark:hover:bg-white/10\" onClick={() => !editing && open()}>\n            <div className=\"flex items-start gap-3\">\n                <input\n                    type=\"checkbox\"\n                    checked={selected}\n                    onClick={(event) => event.stopPropagation()}\n                    onChange={(event) => toggleSelected(project.id, event.target.checked)}\n                    className=\"mt-1 size-4 accent-stone-950 dark:accent-stone-100\"\n                    aria-label={`选择 ${project.title}`}\n                />\n                {editing ? (\n                    <Input className=\"min-w-0\" value={editingTitle} onClick={(event) => event.stopPropagation()} onChange={(event) => setEditingTitle(event.target.value)} onKeyDown={(event) => event.key === \"Enter\" && saveTitle()} autoFocus />\n                ) : (\n                    <button\n                        type=\"button\"\n                        className=\"min-w-0 cursor-pointer text-left\"\n                        onClick={(event) => {\n                            event.stopPropagation();\n                            open();\n                        }}\n                    >\n                        <h2 className=\"truncate text-xl font-semibold\">{project.title}</h2>\n                        <p className=\"mt-3 text-sm leading-6 text-stone-600 dark:text-stone-400\">\n                            {project.nodes.length} 个节点 · {project.connections.length} 条连线\n                        </p>\n                    </button>\n                )}\n            </div>\n            <div className=\"mt-8 flex items-end justify-between gap-3\">\n                <p className=\"text-xs text-stone-500\">更新于 {new Date(project.updatedAt).toLocaleString(\"zh-CN\", { month: \"2-digit\", day: \"2-digit\", hour: \"2-digit\", minute: \"2-digit\" })}</p>\n                <div className=\"flex items-center gap-1\" onClick={(event) => event.stopPropagation()}>\n                    {editing ? (\n                        <>\n                            <Button type=\"text\" size=\"small\" shape=\"circle\" icon={<Check className=\"size-4\" />} onClick={saveTitle} aria-label=\"保存名称\" />\n                            <Button type=\"text\" size=\"small\" shape=\"circle\" icon={<X className=\"size-4\" />} onClick={stopEditing} aria-label=\"取消重命名\" />\n                        </>\n                    ) : (\n                        <>\n                            <Button type=\"text\" size=\"small\" shape=\"circle\" icon={<Download className=\"size-4\" />} onClick={() => void exportCanvasProjects([project], project.title || \"无限画布\")} aria-label=\"导出\" />\n                            <Button type=\"text\" size=\"small\" shape=\"circle\" icon={<Pencil className=\"size-4\" />} onClick={() => startEditing(project.id, project.title)} aria-label=\"重命名\" />\n                            <Button type=\"text\" size=\"small\" shape=\"circle\" icon={<Trash2 className=\"size-4\" />} onClick={() => setDeleteIds([project.id])} aria-label=\"删除\" />\n                        </>\n                    )}\n                </div>\n            </div>\n        </article>\n    );\n}\n",
  "web/src/stores/canvas/use-canvas-ui-store.ts": "import { create } from \"zustand\";\n\ntype CanvasUiStore = {\n    editingProjectId: string | null;\n    editingProjectTitle: string;\n    selectedProjectIds: string[];\n    deleteProjectIds: string[];\n    startEditingProject: (id: string, title: string) => void;\n    setEditingProjectTitle: (title: string) => void;\n    stopEditingProject: () => void;\n    toggleSelectedProjectId: (id: string, selected: boolean) => void;\n    setDeleteProjectIds: (ids: string[]) => void;\n    removeSelectedProjectIds: (ids: string[]) => void;\n};\n\nexport const useCanvasUiStore = create<CanvasUiStore>((set) => ({\n    editingProjectId: null,\n    editingProjectTitle: \"\",\n    selectedProjectIds: [],\n    deleteProjectIds: [],\n    startEditingProject: (editingProjectId, editingProjectTitle) => set({ editingProjectId, editingProjectTitle }),\n    setEditingProjectTitle: (editingProjectTitle) => set({ editingProjectTitle }),\n    stopEditingProject: () => set({ editingProjectId: null }),\n    toggleSelectedProjectId: (id, selected) => set((state) => ({ selectedProjectIds: selected ? [...new Set([...state.selectedProjectIds, id])] : state.selectedProjectIds.filter((item) => item !== id) })),\n    setDeleteProjectIds: (deleteProjectIds) => set({ deleteProjectIds }),\n    removeSelectedProjectIds: (ids) => set((state) => ({ selectedProjectIds: state.selectedProjectIds.filter((id) => !ids.includes(id)) })),\n}));\n",
  "plugins/infinite-canvas/skills/open-canvas/SKILL.md": "---\nname: open-canvas\ndescription: 打开 Infinite Canvas 在线或本地画布，并自动连接本地 Canvas Agent。用户要求打开、启动、进入或使用 Infinite Canvas 画布时使用。\n---\n\n# Open Infinite Canvas\n\n默认打开在线版。只有用户明确要求使用本地项目时，才启动本地前端。\n\n## 在线版\n\n1. 启动本地 Canvas Agent 并保持运行：\n\n```bash\nnpx -y @basketikun/canvas-agent\n```\n\n2. 从启动输出取得 `Local URL` 和 `Connect token`。\n\n3. 在 Codex 右侧浏览器打开：\n\n```text\nhttps://canvas.best/canvas?mode=new&agentUrl=<Local URL>&agentToken=<Connect token>\n```\n\n## 本地版\n\n1. 在 Infinite Canvas 项目中启动前端，并使用 Vite 输出的 `Local` 地址：\n\n```bash\ncd web\nbun install\nbun run dev\n```\n\n2. 启动本地 Canvas Agent：\n\n```bash\nnpx -y @basketikun/canvas-agent\n```\n\n3. 从启动输出取得 `Local URL` 和 `Connect token`，在 Codex 右侧浏览器打开：\n\n```text\n<Vite Local 地址>/canvas?mode=new&agentUrl=<Local URL>&agentToken=<Connect token>\n```\n\n## MCP 与连接地址\n\n插件在新的 Codex 任务中加载时会自动启动 `npx -y @basketikun/canvas-agent mcp`。这个 MCP 进程负责提供画布工具，不提供网页连接服务；\n上面启动的普通 Canvas Agent 负责提供 `Local URL` 和 `Connect token`。两个进程读取同一份本地配置，因此不需要用户手动填写地址或 token。\n\n## 打开模式\n\n用户没有明确指定打开方式时，始终使用 `mode=new` 新建画布。只有用户明确要求时才替换为：\n\n- 最近画布：`mode=recent`\n- 自己选择：`mode=choose`\n",

};

async function createFixture(overrides = {}) {
  const root = await mkdtemp(path.join(tmpdir(), "infinite-canvas-patch-"));
  const files = { ...FIXTURE_FILES, ...overrides };
  await Promise.all(
    Object.entries(files).map(async ([relativePath, source]) => {
      const filePath = path.join(root, relativePath);
      await mkdir(path.dirname(filePath), { recursive: true });
      await writeFile(filePath, source);
    }),
  );
  return root;
}


test("renders selectable grid and list canvas views and consumes one-shot launches", async (t) => {
  const root = await createFixture();
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root, { bridgeSource: "export {}\n" });

  const pageSource = await readFile(
    path.join(root, "web/src/pages/canvas/index.tsx"),
    "utf8",
  );
  const cardSource = await readFile(
    path.join(root, "web/src/components/canvas/canvas-project-card.tsx"),
    "utf8",
  );
  const uiStoreSource = await readFile(
    path.join(root, "web/src/stores/canvas/use-canvas-ui-store.ts"),
    "utf8",
  );
  const openCanvasSkillSource = await readFile(
    path.join(root, "plugins/infinite-canvas/skills/open-canvas/SKILL.md"),
    "utf8",
  );

  assert.match(pageSource, /LayoutGrid, List/);
  assert.match(pageSource, /aria-label="画布视图"/);
  assert.match(pageSource, /viewMode === "grid" \? "primary" : "text"/);
  assert.match(pageSource, /viewMode === "list" \? "primary" : "text"/);
  assert.match(pageSource, /setViewMode\("grid"\)/);
  assert.match(pageSource, /setViewMode\("list"\)/);
  assert.match(pageSource, /viewMode === "grid" \? "grid gap-5 sm:grid-cols-2 xl:grid-cols-3"/);
  assert.match(pageSource, /divide-y/);
  assert.match(pageSource, /viewMode=\{viewMode\}/);
  assert.match(pageSource, /const selectedProjects = projects\.filter/);
  assert.match(pageSource, /projectIds\.every\(\(id\) => selectedIds\.includes\(id\)\)/);
  assert.match(pageSource, /setSelectedIds\(allSelected \? \[\] : projectIds\)/);
  assert.match(pageSource, /allSelected \? "全不选" : "全选"/);
  assert.match(pageSource, /setDeleteIds\(selectedProjects\.map\(\(project\) => project\.id\)\)/);
  assert.match(pageSource, /sessionStorage\.getItem\(launchStorageKey\)/);
  assert.match(pageSource, /sessionStorage\.setItem\(launchStorageKey, projectId\)/);
  assert.match(pageSource, /enterProject\(projectId, true\)/);
  assert.match(pageSource, /projectSearchParams\.delete\("mode"\)/);
  assert.match(pageSource, /projectSearchParams\.delete\("launchId"\)/);

  assert.match(cardSource, /CanvasProjectViewMode/);
  assert.match(cardSource, /viewMode === "grid"/);
  assert.match(cardSource, /grid-cols-\[auto_minmax\(0,1fr\)_auto_auto\]/);
  assert.match(cardSource, /viewMode === "grid" \? "flex items-start gap-3" : "contents"/);
  assert.match(cardSource, /flex min-w-0 cursor-pointer items-center gap-3 text-left/);
  assert.match(cardSource, /hidden shrink-0 whitespace-nowrap text-xs text-stone-500 sm:block/);
  assert.match(cardSource, /hidden whitespace-nowrap text-xs text-stone-500 md:block/);
  assert.match(cardSource, /checked=\{selected\}/);
  assert.match(cardSource, /toggleSelected\(project\.id, event\.target\.checked\)/);
  assert.match(cardSource, /aria-label=\{`选择 \$\{project\.title\}`\}/);
  assert.match(cardSource, /exportCanvasProjects\(\[project\]/);
  assert.match(cardSource, /startEditing\(project\.id, project\.title\)/);
  assert.match(cardSource, /setDeleteIds\(\[project\.id\]\)/);

  assert.match(uiStoreSource, /export type CanvasProjectViewMode = "grid" \| "list"/);
  assert.match(uiStoreSource, /projectViewMode: CanvasProjectViewMode/);
  assert.match(uiStoreSource, /projectViewMode: "grid"/);
  assert.match(uiStoreSource, /setProjectViewMode: \(mode: CanvasProjectViewMode\) => void/);
  assert.match(uiStoreSource, /setProjectViewMode: \(projectViewMode\) => set\(\{ projectViewMode \}\)/);
  assert.match(uiStoreSource, /setSelectedProjectIds: \(ids: string\[\]\) => void/);
  assert.match(uiStoreSource, /setSelectedProjectIds: \(selectedProjectIds\) => set\(\{ selectedProjectIds: \[\.\.\.new Set\(selectedProjectIds\)\] \}\)/);

  assert.match(openCanvasSkillSource, /默认使用 `mode=recent`/);
  assert.match(openCanvasSkillSource, /只有用户明确要求新建画布时，才使用 `mode=new`/);
  assert.match(openCanvasSkillSource, /launchId/);
});

test("fails closed when the canvas view layout marker changes", async (t) => {
  const root = await createFixture({
    "web/src/pages/canvas/index.tsx": FIXTURE_FILES["web/src/pages/canvas/index.tsx"].replace(
      'className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3"',
      'className="grid gap-4 md:grid-cols-2"',
    ),
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream canvas project view layout marker did not match exactly once/,
  );
});

test("fails closed when the canvas launch mode marker changes", async (t) => {
  const root = await createFixture({
    "plugins/infinite-canvas/skills/open-canvas/SKILL.md": FIXTURE_FILES[
      "plugins/infinite-canvas/skills/open-canvas/SKILL.md"
    ].replace("始终使用 `mode=new` 新建画布", "默认打开画布列表"),
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream canvas default open mode marker did not match exactly once/,
  );
});

test("injects subpath routing, bridge, per-user storage, and managed key redaction", async (t) => {
  const root = await createFixture();
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root, { bridgeSource: "export const bridgeFixture = true\n" });

  const mainSource = await readFile(path.join(root, "web/src/main.tsx"), "utf8");
  const routerSource = await readFile(path.join(root, "web/src/router.tsx"), "utf8");
  const indexSource = await readFile(path.join(root, "web/index.html"), "utf8");
  const configSource = await readFile(
    path.join(root, "web/src/stores/use-config-store.ts"),
    "utf8",
  );
  const localForageSource = await readFile(
    path.join(root, "web/src/lib/localforage-storage.ts"),
    "utf8",
  );
  const canvasStoreSource = await readFile(
    path.join(root, "web/src/stores/canvas/use-canvas-store.ts"),
    "utf8",
  );
  const appSyncSource = await readFile(path.join(root, "web/src/services/app-sync.ts"), "utf8");
  const bridgeSource = await readFile(path.join(root, "web/src/lib/new-api-bridge.ts"), "utf8");
  const storageSource = await readFile(path.join(root, "web/src/lib/new-api-storage.ts"), "utf8");
  const syncSource = await readFile(path.join(root, "web/src/lib/new-api-sync.ts"), "utf8");
  const imagePageSource = await readFile(path.join(root, "web/src/pages/image/index.tsx"), "utf8");
  const pluginStorageSource = await readFile(
    path.join(root, "web/src/lib/canvas/canvas-event-bus.ts"),
    "utf8",
  );
  const configModalSource = await readFile(
    path.join(root, "web/src/components/layout/app-config-modal.tsx"),
    "utf8",
  );

  assert.match(mainSource, /installNewApiBridge\(\)/);
  assert.match(mainSource, /initializeNewApiInfiniteCanvasSync\(\)/);
  assert.match(routerSource, /basename: import\.meta\.env\.BASE_URL/);
  assert.match(indexSource, /window\.__NEW_API_USER_ID__/);
  assert.doesNotMatch(indexSource, /new_api_user/);
  assert.doesNotMatch(indexSource, /location\.search/);
  assert.doesNotMatch(indexSource, /sessionStorage/);
  assert.match(indexSource, /infinite-canvas:theme_store:" \+ namespace/);
  assert.match(configSource, /const MANAGED_CHANNEL_ID = "new-api-managed"/);
  assert.match(configSource, /const MANAGED_IMAGE_CHANNEL_ID = "new-api-managed-image"/);
  assert.match(configSource, /const MANAGED_MEDIA_CHANNEL_ID = "new-api-managed-media"/);
  assert.match(
    configSource,
    /const MANAGED_CHANNEL_IDS = new Set\(\[MANAGED_CHANNEL_ID, MANAGED_IMAGE_CHANNEL_ID, MANAGED_MEDIA_CHANNEL_ID\]\)/,
  );
  assert.match(
    configSource,
    /MANAGED_CHANNEL_IDS\.has\(channel\.id\) \? \{ \.\.\.channel, apiKey: "" \}/,
  );
  assert.match(
    configSource,
    /decoded\?\.channelId === MANAGED_CHANNEL_ID && requestedModel === "gpt-image-2"/,
  );
  assert.match(configSource, /item\.id === MANAGED_IMAGE_CHANNEL_ID/);
  assert.match(localForageSource, /configureNewApiLocalForage\(\)/);
  assert.match(localForageSource, /ensureLegacyInfiniteCanvasStorageMigration\(\)/);
  assert.match(localForageSource, /notifyNewApiInfiniteCanvasStorageChanged\(\)/);
  assert.match(localForageSource, /namespacedStorageKey\(name\)/);
  assert.match(canvasStoreSource, /namespacedStorageKey\("infinite-canvas:canvas_store"\)/);
  assert.match(appSyncSource, /namespacedLocalForageName\("infinite-canvas"\)/);
  assert.equal(bridgeSource, "export const bridgeFixture = true\n");
  assert.match(storageSource, /NEW_API_STORAGE_NAMESPACE/);
  assert.match(storageSource, /normalizeInjectedUserId\(window\.__NEW_API_USER_ID__\)/);
  assert.match(storageSource, /const MIGRATION_VERSION = 3/);
  assert.match(storageSource, /window\.__NEW_API_USER_ID__/);
  assert.doesNotMatch(storageSource, /URLSearchParams/);
  assert.doesNotMatch(storageSource, /new_api_user/);
  assert.match(storageSource, /LEGACY_PLUGIN_DATABASE_NAME/);
  assert.match(storageSource, /listNewApiInfiniteCanvasPluginStoreNames/);
  assert.match(storageSource, /copyMissingPluginStoreRecords/);
  assert.match(syncSource, /waitForPersistedStores/);
  assert.match(syncSource, /const PLUGIN_RECORD_KIND = "plugin-record"/);
  assert.match(syncSource, /listNewApiInfiniteCanvasPluginStoreNames/);
  assert.match(pluginStorageSource, /namespacedLocalForageName\("infinite-canvas-plugins"\)/);
  assert.equal(
    (pluginStorageSource.match(/notifyNewApiInfiniteCanvasStorageChanged\(\)/g) || []).length,
    2,
  );
  assert.match(imagePageSource, /NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT/);
  assert.match(configModalSource, /const managedChannelIds = new Set/);
  assert.match(configModalSource, /New API 托管渠道由宿主账号下发/);
  assert.match(configModalSource, /managedChannelIds\.has\(channel\.id\) \? \(/);
  assert.match(configModalSource, /New API 托管 · 只读/);
});

test("restores synced image nodes from storageKey when content is empty", async (t) => {
  const root = await createFixture();
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root, { bridgeSource: "export {}\n" });

  const helperSource = await readFile(
    path.join(root, "web/src/lib/canvas/canvas-generation-helpers.ts"),
    "utf8",
  );
  const imageTypeGuard = helperSource.indexOf(
    "if (node.type !== CanvasNodeType.Image) return node;",
  );
  const storageHydration = helperSource.indexOf(
    "if (node.metadata?.storageKey) return { ...node, metadata: { ...node.metadata, content: await resolveImageUrl(node.metadata.storageKey, content) } };",
  );
  const emptyContentGuard = helperSource.indexOf("if (!content) return node;");

  assert.ok(imageTypeGuard >= 0);
  assert.ok(storageHydration > imageTypeGuard);
  assert.ok(emptyContentGuard > storageHydration);
});

test("supports the fork canvas side panel import formatting", async (t) => {
  const root = await createFixture({
    "web/src/components/canvas/canvas-side-panel.tsx": `import {
  CANVAS_SIDE_PANEL_MAX_WIDTH,
  CANVAS_SIDE_PANEL_MIN_WIDTH,
  CANVAS_SIDE_PANEL_MOTION_MS,
  useCanvasSidePanelStore
} from "@/stores/use-canvas-side-panel-store";
localStorage.setItem("canvas-side-panel-width", String(nextWidth));
`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root, { bridgeSource: "export {}\n" });

  const source = await readFile(
    path.join(root, "web/src/components/canvas/canvas-side-panel.tsx"),
    "utf8",
  );
  assert.match(source, /CANVAS_SIDE_PANEL_MOTION_MS,\n\s+CANVAS_SIDE_PANEL_WIDTH_KEY,/);
  assert.match(source, /localStorage\.setItem\(CANVAS_SIDE_PANEL_WIDTH_KEY,/);
});

test("runs legacy migration before importing persisted stores or starting sync", async (t) => {
  const root = await createFixture();
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root, { bridgeSource: "export const bridgeFixture = true\n" });

  const mainSource = await readFile(path.join(root, "web/src/main.tsx"), "utf8");
  const indexSource = await readFile(path.join(root, "web/index.html"), "utf8");
  const migrationIndex = mainSource.indexOf("await ensureLegacyInfiniteCanvasStorageMigration()");
  const providersImportIndex = mainSource.indexOf(
    'await import("@/components/layout/app-providers")',
  );
  const routerImportIndex = mainSource.indexOf('await import("@/router")');
  const syncImportIndex = mainSource.indexOf('await import("@/lib/new-api-sync")');

  assert.ok(migrationIndex >= 0);
  assert.ok(providersImportIndex > migrationIndex);
  assert.ok(routerImportIndex > migrationIndex);
  assert.ok(syncImportIndex > migrationIndex);
  assert.doesNotMatch(mainSource, /^import \{ AppProviders \} from/m);
  assert.doesNotMatch(mainSource, /^import \{ router \} from/m);
  assert.doesNotMatch(mainSource, /^import \{ initializeNewApiInfiniteCanvasSync \} from/m);
  assert.doesNotMatch(
    indexSource,
    /localStorage\.setItem\(themeKey, localStorage\.getItem\("infinite-canvas:theme_store"\)\)/,
  );
});

test("default bridge accepts same-origin configuration and splits media traffic into a managed channel", async (t) => {
  const root = await createFixture();
  t.after(() => rm(root, { recursive: true, force: true }));

  await applyUpstreamPatch(root);

  const bridgeSource = await readFile(path.join(root, "web/src/lib/new-api-bridge.ts"), "utf8");
  assert.match(bridgeSource, /event\.origin !== window\.location\.origin/);
  assert.match(bridgeSource, /event\.source !== window\.parent/);
  assert.match(bridgeSource, /id: MANAGED_CHANNEL_ID/);
  assert.match(bridgeSource, /id: MANAGED_IMAGE_CHANNEL_ID/);
  assert.match(bridgeSource, /id: MANAGED_MEDIA_CHANNEL_ID/);
  assert.match(
    bridgeSource,
    /createManagedImageChannel\(imageApiUrl, apiKey, apiFormat, profileName\)/,
  );
  assert.match(bridgeSource, /imageModel = splitImageChannel \? MODEL_IDS\.image/);
  assert.match(bridgeSource, /videoModel = splitMediaChannel\s+\? MODEL_IDS\.video/);
  assert.match(bridgeSource, /useConfigStore\.persist\.onFinishHydration/);
  assert.match(bridgeSource, /useConfigStore\.subscribe/);
  assert.match(bridgeSource, /managedConfigurationMatches\(config, activeConfigureMessage\)/);
  assert.match(bridgeSource, /repairingManagedConfiguration/);

  const configModalSource = await readFile(
    path.join(root, "web/src/components/layout/app-config-modal.tsx"),
    "utf8",
  );
  assert.match(configModalSource, /新增渠道/);
  assert.match(configModalSource, /setEditingChannelId\(channel\.id\)/);
  assert.match(configModalSource, /deleteChannel\(channel\.id\)/);
  assert.match(configModalSource, /managedChannelIds\.has\(channel\.id\) \? \(/);
  assert.match(configModalSource, /New API 托管 · 只读/);

  const configSource = await readFile(
    path.join(root, "web/src/stores/use-config-store.ts"),
    "utf8",
  );
  assert.match(configSource, /const MANAGED_MEDIA_CHANNEL_ID = "new-api-managed-media"/);
  assert.match(
    configSource,
    /new Set\(\[MANAGED_CHANNEL_ID, MANAGED_IMAGE_CHANNEL_ID, MANAGED_MEDIA_CHANNEL_ID\]\)/,
  );
});

test("fails closed when an upstream routing marker changes", async (t) => {
  const root = await createFixture({
    "web/src/router.tsx": "export const router = createHashRouter([]);\n",
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream router options marker did not match exactly once/,
  );
});

test("fails closed when the managed-key persistence marker changes", async (t) => {
  const root = await createFixture({
    "web/src/stores/use-config-store.ts": `import { nanoid } from "nanoid";
export const CONFIG_STORE_KEY = "infinite-canvas:ai_config_store";
partialize: (state) => ({ config: state.config }),
`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream config redaction marker did not match exactly once/,
  );
});

test("fails closed when the initial sync marker changes", async (t) => {
  const root = await createFixture({
    "web/src/main.tsx": 'import { otherRouter } from "@/router";\n',
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream main persisted store imports marker did not match exactly once/,
  );
});

test("fails closed when plugin storage mutation markers change", async (t) => {
  const root = await createFixture({
    "web/src/lib/canvas/canvas-event-bus.ts": `import type { PluginStorage } from "@/types/canvas-plugin";
localforage.createInstance({ name: "infinite-canvas-plugins", storeName: pluginId })
        set: async (key, value) => {
            void store!.setItem(key, value);
        },
        remove: async (key) => {
            await store!.removeItem(key);
        },
`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream plugin storage set notification marker did not match exactly once/,
  );
});

test("fails closed when localForage migration or notification markers change", async (t) => {
  const root = await createFixture({
    "web/src/lib/localforage-storage.ts": `import localforage from "localforage";
localforage.config({
    name: "infinite-canvas",
    storeName: "app_state",
});
export const localForageStorage = {
    getItem: async () => null,
    setItem: async () => {},
    removeItem: async () => {},
};
`,
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(
    applyUpstreamPatch(root, { bridgeSource: "export {}\n" }),
    /upstream localforage migration before get marker did not match exactly once/,
  );
});
