import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

function replaceExactlyOnce(source, marker, replacement, name) {
    const first = source.indexOf(marker);
    const last = source.lastIndexOf(marker);
    if (first < 0 || first !== last) throw new Error(`upstream ${name} marker did not match exactly once`);
    return `${source.slice(0, first)}${replacement}${source.slice(first + marker.length)}`;
}

async function patchFile(filePath, transforms) {
    let source = await readFile(filePath, "utf8");
    for (const [marker, replacement, name] of transforms) {
        source = replaceExactlyOnce(source, marker, replacement, name);
    }
    await writeFile(filePath, source);
}

export async function applyUpstreamPatch(upstreamRoot, options = {}) {
    const root = path.resolve(upstreamRoot);
    const sourceRoot = path.join(root, "web/src");
    const indexPath = path.join(root, "web/index.html");
    const integrationRoot = path.dirname(fileURLToPath(import.meta.url));
    const bridgeSource = options.bridgeSource ?? await readFile(path.join(integrationRoot, "new-api-bridge.ts"), "utf8");
    const storageSource = options.storageSource ?? await readFile(path.join(integrationRoot, "new-api-storage.ts"), "utf8");
    const syncSource = options.syncSource ?? await readFile(path.join(integrationRoot, "new-api-sync.ts"), "utf8");

    await patchFile(path.join(sourceRoot, "main.tsx"), [
        [
            'import { AppProviders } from "@/components/layout/app-providers";\nimport { initAnalytics } from "@/lib/analytics";\nimport { router } from "@/router";\n',
            'import { installNewApiBridge } from "@/lib/new-api-bridge";\nimport { ensureLegacyInfiniteCanvasStorageMigration } from "@/lib/new-api-storage";\n',
            "main persisted store imports",
        ],
        [
            `initAnalytics();

document.body.style.fontFamily = '"SF Pro Display","SF Pro Text","PingFang SC","Microsoft YaHei","Helvetica Neue",sans-serif';

createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
        <AppProviders>
            <RouterProvider router={router} />
        </AppProviders>
    </React.StrictMode>,
);`,
            `installNewApiBridge();

async function bootstrapNewApiInfiniteCanvas() {
    await ensureLegacyInfiniteCanvasStorageMigration();
    const { AppProviders } = await import("@/components/layout/app-providers");
    const { initAnalytics } = await import("@/lib/analytics");
    const { router } = await import("@/router");
    const { initializeNewApiInfiniteCanvasSync } = await import("@/lib/new-api-sync");

    void initializeNewApiInfiniteCanvasSync();
    initAnalytics();

    document.body.style.fontFamily = '"SF Pro Display","SF Pro Text","PingFang SC","Microsoft YaHei","Helvetica Neue",sans-serif';

    createRoot(document.getElementById("root")!).render(
        <React.StrictMode>
            <AppProviders>
                <RouterProvider router={router} />
            </AppProviders>
        </React.StrictMode>,
    );
}

void bootstrapNewApiInfiniteCanvas();`,
            "main migration-first bootstrap",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "router.tsx"), [[
        '    { path: "*", element: <NotFound /> },\n]);\n',
        '    { path: "*", element: <NotFound /> },\n], { basename: import.meta.env.BASE_URL });\n',
        "router options",
    ]]);
    await patchFile(indexPath, [[
        'var s = JSON.parse(localStorage.getItem("infinite-canvas:theme_store") || "{}");',
        `var normalizeUserId = function (value) {
                    if (typeof value === "string" && !/^[0-9]+$/.test(value)) return "default";
                    if (typeof value !== "string" && typeof value !== "number") return "default";
                    var numericValue = Number(value);
                    return Number.isSafeInteger(numericValue) && numericValue > 0 ? String(numericValue) : "default";
                };
                var namespace = normalizeUserId(window.__NEW_API_USER_ID__);
                var themeKey = namespace === "default" ? "infinite-canvas:theme_store" : "infinite-canvas:theme_store:" + namespace;
                var s = JSON.parse(localStorage.getItem(themeKey) || "{}");`,
        "index theme storage namespace",
    ]]);
    await patchFile(path.join(sourceRoot, "stores/use-config-store.ts"), [
        [
            'import { nanoid } from "nanoid";\n',
            'import { nanoid } from "nanoid";\n\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "config storage import",
        ],
        [
            'export const CONFIG_STORE_KEY = "infinite-canvas:ai_config_store";',
            'export const CONFIG_STORE_KEY = namespacedStorageKey("infinite-canvas:ai_config_store");\nconst MANAGED_CHANNEL_ID = "new-api-managed";\nconst MANAGED_IMAGE_CHANNEL_ID = "new-api-managed-image";\nconst MANAGED_MEDIA_CHANNEL_ID = "new-api-managed-media";\nconst MANAGED_CHANNEL_IDS = new Set([MANAGED_CHANNEL_ID, MANAGED_IMAGE_CHANNEL_ID, MANAGED_MEDIA_CHANNEL_ID]);',
            "config storage key",
        ],
        [
            'partialize: (state) => ({ config: state.config, webdav: state.webdav }),',
            `partialize: (state) => ({
                config: state.config.channels.some((channel) => MANAGED_CHANNEL_IDS.has(channel.id))
                    ? { ...state.config, apiKey: "", channels: state.config.channels.map((channel) => MANAGED_CHANNEL_IDS.has(channel.id) ? { ...channel, apiKey: "" } : channel) }
                    : state.config,
                webdav: state.webdav,
            }),`,
            "config redaction",
        ],
        [
            `export function resolveModelRequestConfig(config: AiConfig, value: string) {
    const channel = resolveModelChannel(config, value);
    return {
        ...config,
        model: modelOptionName(value || config.model),
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
    };
}`,
            `export function resolveModelRequestConfig(config: AiConfig, value: string) {
    const requestedModel = modelOptionName(value || config.model);
    const decoded = decodeChannelModel(value);
    const channel = decoded?.channelId === MANAGED_CHANNEL_ID && requestedModel === "gpt-image-2"
        ? config.channels.find((item) => item.id === MANAGED_IMAGE_CHANNEL_ID) || resolveModelChannel(config, value)
        : resolveModelChannel(config, value);
    return {
        ...config,
        model: requestedModel,
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
    };
}`,
            "managed image channel compatibility",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "components/layout/app-config-modal.tsx"), [
        [
            `];

const webdavDomainKeys: AppSyncDomainKey[] = ["canvas", "assets", "image-workbench", "video-workbench"];`,
            `];

const managedChannelIds = new Set(["new-api-managed", "new-api-managed-image", "new-api-managed-media"]);

const webdavDomainKeys: AppSyncDomainKey[] = ["canvas", "assets", "image-workbench", "video-workbench"];`,
            "config modal managed channel identifiers",
        ],
        [
            `    const webdavReady = Boolean(webdav.url.trim());
    const editingChannel = config.channels.find((channel) => channel.id === editingChannelId) || null;`,
            `    const webdavReady = Boolean(webdav.url.trim());
    const managedChannelsActive = config.channels.some((channel) => managedChannelIds.has(channel.id));
    const editingChannel = config.channels.find((channel) => channel.id === editingChannelId && !managedChannelIds.has(channel.id)) || null;`,
            "config modal managed channel state",
        ],
        [
            `                            <div>
                                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">`,
            `                            <div>
                                {managedChannelsActive ? (
                                    <div className="mb-4 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-200">
                                        New API 托管渠道由宿主账号下发，Base URL、临时凭证、协议和模型绑定均为只读；请返回 New API 选择密钥或渠道。自定义渠道仍可单独新增和编辑。
                                    </div>
                                ) : null}
                                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">`,
            "config modal managed channel notice",
        ],
        [
            `                                            <div className="flex shrink-0 gap-2">
                                                <Button size="small" icon={<Pencil className="size-3.5" />} onClick={() => setEditingChannelId(channel.id)}>
                                                    编辑
                                                </Button>
                                                <Button size="small" danger icon={<Trash2 className="size-3.5" />} onClick={() => deleteChannel(channel.id)} />
                                            </div>`,
            `                                            {managedChannelIds.has(channel.id) ? (
                                                <span className="shrink-0 rounded-full bg-blue-50 px-3 py-1 text-xs font-medium text-blue-700 dark:bg-blue-950/50 dark:text-blue-200">
                                                    New API 托管 · 只读
                                                </span>
                                            ) : (
                                                <div className="flex shrink-0 gap-2">
                                                    <Button size="small" icon={<Pencil className="size-3.5" />} onClick={() => setEditingChannelId(channel.id)}>
                                                        编辑
                                                    </Button>
                                                    <Button size="small" danger icon={<Trash2 className="size-3.5" />} onClick={() => deleteChannel(channel.id)} />
                                                </div>
                                            )}`,
            "config modal managed channel actions",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "lib/localforage-storage.ts"), [
        [
            'import localforage from "localforage";\n',
            'import localforage from "localforage";\n\nimport { configureNewApiLocalForage, ensureLegacyInfiniteCanvasStorageMigration, namespacedStorageKey, notifyNewApiInfiniteCanvasStorageChanged } from "@/lib/new-api-storage";\n',
            "localforage import",
        ],
        [
            'localforage.config({\n    name: "infinite-canvas",\n    storeName: "app_state",\n});',
            'configureNewApiLocalForage();',
            "localforage namespace",
        ],
        [
            '        try {\n            return (await localforage.getItem<string>(name)) || null;',
            '        try {\n            await ensureLegacyInfiniteCanvasStorageMigration();\n            return (await localforage.getItem<string>(name)) || null;',
            "localforage migration before get",
        ],
        [
            '            await localforage.setItem(name, value);',
            '            await ensureLegacyInfiniteCanvasStorageMigration();\n            await localforage.setItem(name, value);',
            "localforage migration before set",
        ],
        [
            '        }\n    },\n    removeItem: async (name) => {',
            '        }\n        notifyNewApiInfiniteCanvasStorageChanged();\n    },\n    removeItem: async (name) => {',
            "localforage change notification",
        ],
        [
            '            await localforage.removeItem(name);',
            '            await ensureLegacyInfiniteCanvasStorageMigration();\n            await localforage.removeItem(name);',
            "localforage migration before remove",
        ],
        [
            '            window.localStorage.removeItem(name);\n        }\n    },',
            '            window.localStorage.removeItem(name);\n        }\n        notifyNewApiInfiniteCanvasStorageChanged();\n    },',
            "localforage remove notification",
        ],
        [
            'return window.localStorage.getItem(name);',
            'return window.localStorage.getItem(namespacedStorageKey(name));',
            "localforage fallback get namespace",
        ],
        [
            'window.localStorage.setItem(name, value);',
            'window.localStorage.setItem(namespacedStorageKey(name), value);',
            "localforage fallback set namespace",
        ],
        [
            'window.localStorage.removeItem(name);',
            'window.localStorage.removeItem(namespacedStorageKey(name));',
            "localforage fallback remove namespace",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "lib/canvas/canvas-generation-helpers.ts"), [
        [
            '            if (node.type !== CanvasNodeType.Image || !content) return node;\n            if (node.metadata?.storageKey) return { ...node, metadata: { ...node.metadata, content: await resolveImageUrl(node.metadata.storageKey, content) } };\n            if (!content.startsWith("data:image/")) return node;',
            '            if (node.type !== CanvasNodeType.Image) return node;\n            if (node.metadata?.storageKey) return { ...node, metadata: { ...node.metadata, content: await resolveImageUrl(node.metadata.storageKey, content) } };\n            if (!content) return node;\n            if (!content.startsWith("data:image/")) return node;',
            "canvas image storage hydration",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/use-prompt-source-store.ts"), [
        [
            'import { persist } from "zustand/middleware";\n',
            'import { persist } from "zustand/middleware";\n\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "prompt storage import",
        ],
        [
            'const PROMPT_SOURCE_STORE_KEY = "infinite-canvas:prompt_source_store";',
            'const PROMPT_SOURCE_STORE_KEY = namespacedStorageKey("infinite-canvas:prompt_source_store");',
            "prompt storage key",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/use-theme-store.ts"), [
        [
            'import { persist } from "zustand/middleware";\n',
            'import { persist } from "zustand/middleware";\n\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "theme storage import",
        ],
        [
            '{ name: "infinite-canvas:theme_store" }',
            '{ name: namespacedStorageKey("infinite-canvas:theme_store") }',
            "theme storage key",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/use-asset-store.ts"), [
        [
            'import { localForageStorage } from "@/lib/localforage-storage";\n',
            'import { localForageStorage } from "@/lib/localforage-storage";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "asset storage import",
        ],
        [
            'const ASSET_STORE_KEY = "infinite-canvas:asset_store";',
            'const ASSET_STORE_KEY = namespacedStorageKey("infinite-canvas:asset_store");',
            "asset storage key",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/canvas/use-plugin-store.ts"), [
        [
            'import { localForageStorage } from "@/lib/localforage-storage";\n',
            'import { localForageStorage } from "@/lib/localforage-storage";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "plugin storage import",
        ],
        [
            'name: "infinite-canvas:plugin_store",',
            'name: namespacedStorageKey("infinite-canvas:plugin_store"),',
            "plugin storage key",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/canvas/use-canvas-store.ts"), [
        [
            'import { localForageStorage } from "@/lib/localforage-storage";\n',
            'import { localForageStorage } from "@/lib/localforage-storage";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "canvas storage import",
        ],
        [
            'const CANVAS_STORE_KEY = "infinite-canvas:canvas_store";',
            'const CANVAS_STORE_KEY = namespacedStorageKey("infinite-canvas:canvas_store");',
            "canvas storage key",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/use-agent-store.ts"), [
        [
            'import type { CanvasAgentOp, CanvasAgentSnapshot } from "@/lib/canvas/canvas-agent-ops";\n',
            'import type { CanvasAgentOp, CanvasAgentSnapshot } from "@/lib/canvas/canvas-agent-ops";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "agent storage import",
        ],
        [
            'const CONNECT_TIMEOUT_MS = 6000;',
            'const AGENT_PANEL_WIDTH_KEY = namespacedStorageKey("canvas-agent-panel-width");\nconst AGENT_URL_KEY = namespacedStorageKey("canvas-agent-url");\nconst AGENT_TOKEN_KEY = namespacedStorageKey("canvas-agent-token");\nconst CONNECT_TIMEOUT_MS = 6000;',
            "agent storage keys",
        ],
        [
            'Number(localStorage.getItem("canvas-agent-panel-width"))',
            'Number(localStorage.getItem(AGENT_PANEL_WIDTH_KEY))',
            "agent panel width read",
        ],
        [
            'localStorage.getItem("canvas-agent-url")',
            'localStorage.getItem(AGENT_URL_KEY)',
            "agent URL read",
        ],
        [
            'localStorage.getItem("canvas-agent-token")',
            'localStorage.getItem(AGENT_TOKEN_KEY)',
            "agent token read",
        ],
        [
            'localStorage.setItem("canvas-agent-url", endpoint);\n        localStorage.setItem("canvas-agent-token", token);',
            'localStorage.setItem(AGENT_URL_KEY, endpoint);\n        localStorage.setItem(AGENT_TOKEN_KEY, token);',
            "agent connection storage",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "stores/use-canvas-side-panel-store.ts"), [
        [
            'import { create } from "zustand";\n',
            'import { create } from "zustand";\n\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "canvas side panel storage import",
        ],
        [
            'const WIDTH_KEY = "canvas-side-panel-width";\nconst OPEN_KEY = "canvas-side-panel-open";',
            'export const CANVAS_SIDE_PANEL_WIDTH_KEY = namespacedStorageKey("canvas-side-panel-width");\nconst OPEN_KEY = namespacedStorageKey("canvas-side-panel-open");',
            "canvas side panel keys",
        ],
        [
            'localStorage.getItem(WIDTH_KEY)',
            'localStorage.getItem(CANVAS_SIDE_PANEL_WIDTH_KEY)',
            "canvas side panel width read",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "components/agent/agent-panel.tsx"), [
        [
            'import { canvasThemes } from "@/lib/canvas-theme";\n',
            'import { canvasThemes } from "@/lib/canvas-theme";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "agent panel storage import",
        ],
        [
            'localStorage.setItem("canvas-agent-panel-width", String(nextWidth));',
            'localStorage.setItem(namespacedStorageKey("canvas-agent-panel-width"), String(nextWidth));',
            "agent panel width storage",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "components/canvas/canvas-local-agent-panel.tsx"), [
        [
            'import { randomId } from "@/lib/utils";\n',
            'import { randomId } from "@/lib/utils";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "local agent storage import",
        ],
        [
            'localStorage.setItem("canvas-agent-url", endpoint);\n        localStorage.setItem("canvas-agent-token", token);',
            'localStorage.setItem(namespacedStorageKey("canvas-agent-url"), endpoint);\n        localStorage.setItem(namespacedStorageKey("canvas-agent-token"), token);',
            "local agent connection storage",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "components/canvas/canvas-side-panel.tsx"), [
        [
            'CANVAS_SIDE_PANEL_MOTION_MS,',
            'CANVAS_SIDE_PANEL_MOTION_MS,\n    CANVAS_SIDE_PANEL_WIDTH_KEY,',
            "canvas side panel width import",
        ],
        [
            'localStorage.setItem("canvas-side-panel-width", String(nextWidth));',
            'localStorage.setItem(CANVAS_SIDE_PANEL_WIDTH_KEY, String(nextWidth));',
            "canvas side panel width storage",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "components/canvas/canvas-node-hover-toolbar.tsx"), [
        [
            'import { useCopyText } from "@/hooks/use-copy-text";\n',
            'import { useCopyText } from "@/hooks/use-copy-text";\nimport { namespacedStorageKey } from "@/lib/new-api-storage";\n',
            "image quick tools storage import",
        ],
        [
            'import { IMAGE_QUICK_TOOLS_STORAGE_KEY, buildImageToolbarTools, defaultImageQuickToolIds, readImageQuickToolsConfig, type ImageQuickToolId } from "./canvas-image-toolbar-tools";\n',
            'import { IMAGE_QUICK_TOOLS_STORAGE_KEY, buildImageToolbarTools, defaultImageQuickToolIds, readImageQuickToolsConfig, type ImageQuickToolId } from "./canvas-image-toolbar-tools";\n\nconst NAMESPACED_IMAGE_QUICK_TOOLS_STORAGE_KEY = namespacedStorageKey(IMAGE_QUICK_TOOLS_STORAGE_KEY);\n',
            "image quick tools namespaced key",
        ],
        [
            'window.localStorage.getItem(IMAGE_QUICK_TOOLS_STORAGE_KEY)',
            'window.localStorage.getItem(NAMESPACED_IMAGE_QUICK_TOOLS_STORAGE_KEY)',
            "image quick tools read",
        ],
        [
            'window.localStorage.removeItem(IMAGE_QUICK_TOOLS_STORAGE_KEY)',
            'window.localStorage.removeItem(NAMESPACED_IMAGE_QUICK_TOOLS_STORAGE_KEY)',
            "image quick tools remove",
        ],
        [
            'window.localStorage.setItem(IMAGE_QUICK_TOOLS_STORAGE_KEY, JSON.stringify(config));',
            'window.localStorage.setItem(NAMESPACED_IMAGE_QUICK_TOOLS_STORAGE_KEY, JSON.stringify(config));',
            "image quick tools storage",
        ],
    ]);
    await patchFile(path.join(sourceRoot, "lib/canvas/canvas-event-bus.ts"), [
        [
            'import type { PluginStorage } from "@/types/canvas-plugin";\n',
            'import { namespacedLocalForageName, notifyNewApiInfiniteCanvasStorageChanged } from "@/lib/new-api-storage";\nimport type { PluginStorage } from "@/types/canvas-plugin";\n',
            "plugin database import",
        ],
        [
            'localforage.createInstance({ name: "infinite-canvas-plugins", storeName: pluginId })',
            'localforage.createInstance({ name: namespacedLocalForageName("infinite-canvas-plugins"), storeName: pluginId })',
            "plugin database namespace",
        ],
        [
            '            await store!.setItem(key, value);',
            `            await store!.setItem(key, value);
            notifyNewApiInfiniteCanvasStorageChanged();`,
            "plugin storage set notification",
        ],
        [
            '            await store!.removeItem(key);',
            `            await store!.removeItem(key);
            notifyNewApiInfiniteCanvasStorageChanged();`,
            "plugin storage remove notification",
        ],
    ]);

    const localForageInstances = [
        ["pages/image/index.tsx", "image page storage import", 'import localforage from "localforage";\n', 'import localforage from "localforage";\n\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n'],
        ["pages/video/index.tsx", "video page storage import", 'import localforage from "localforage";\n', 'import localforage from "localforage";\n\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n'],
        ["services/image-storage.ts", "image storage database import", 'import { nanoid } from "nanoid";\n', 'import { nanoid } from "nanoid";\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n'],
        ["services/file-storage.ts", "file storage database import", 'import { nanoid } from "nanoid";\n', 'import { nanoid } from "nanoid";\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n'],
        ["services/api/prompts.ts", "prompt cache database import", 'import localforage from "localforage";\n', 'import localforage from "localforage";\n\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n'],
    ];
    for (const [relativePath, name, marker, replacement] of localForageInstances) {
        await patchFile(path.join(sourceRoot, relativePath), [
            [marker, replacement, name],
            [
                'name: "infinite-canvas"',
                'name: namespacedLocalForageName("infinite-canvas")',
                `${name} namespace`,
            ],
        ]);
    }
    for (const [relativePath, name] of [["pages/image/index.tsx", "image page remote log refresh"], ["pages/video/index.tsx", "video page remote log refresh"]]) {
        await patchFile(path.join(sourceRoot, relativePath), [
            [
                'import { namespacedLocalForageName } from "@/lib/new-api-storage";\n',
                'import { namespacedLocalForageName, NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT } from "@/lib/new-api-storage";\n',
                `${name} event import`,
            ],
            [
                '    useEffect(() => {\n        void refreshLogs();\n    }, []);',
                '    useEffect(() => {\n        void refreshLogs();\n        const handleRemoteLogsChanged = () => void refreshLogs();\n        window.addEventListener(NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT, handleRemoteLogsChanged);\n        return () => window.removeEventListener(NEW_API_INFINITE_CANVAS_REMOTE_LOGS_CHANGED_EVENT, handleRemoteLogsChanged);\n    }, []);',
                name,
            ],
        ]);
    }
    await patchFile(path.join(sourceRoot, "services/app-sync.ts"), [
        [
            'import localforage from "localforage";\n',
            'import localforage from "localforage";\n\nimport { namespacedLocalForageName } from "@/lib/new-api-storage";\n',
            "app sync database import",
        ],
        [
            'const imageLogStore = localforage.createInstance({ name: "infinite-canvas", storeName: "image_generation_logs" });\nconst videoLogStore = localforage.createInstance({ name: "infinite-canvas", storeName: "video_generation_logs" });',
            'const imageLogStore = localforage.createInstance({ name: namespacedLocalForageName("infinite-canvas"), storeName: "image_generation_logs" });\nconst videoLogStore = localforage.createInstance({ name: namespacedLocalForageName("infinite-canvas"), storeName: "video_generation_logs" });',
            "app sync database namespace",
        ],
    ]);

    await writeFile(path.join(sourceRoot, "lib/new-api-bridge.ts"), bridgeSource);
    await writeFile(path.join(sourceRoot, "lib/new-api-storage.ts"), storageSource);
    await writeFile(path.join(sourceRoot, "lib/new-api-sync.ts"), syncSource);
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
    if (!process.argv[2]) throw new Error("usage: node patch-upstream.mjs <upstream-root>");
    await applyUpstreamPatch(process.argv[2]);
}
