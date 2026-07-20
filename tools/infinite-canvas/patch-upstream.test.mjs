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
const PROMPT_SOURCE_STORE_KEY = "infinite-canvas:prompt_source_store";
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
