import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import test from "node:test";
import vm from "node:vm";

const require = createRequire(import.meta.url);
const ts = require("../../web/default/node_modules/typescript/lib/typescript.js");
const source = await readFile(new URL("./new-api-bridge.ts", import.meta.url), "utf8");

function createDefaultConfig() {
  return {
    channelMode: "local",
    baseUrl: "https://api.openai.com",
    apiKey: "",
    apiFormat: "openai",
    channels: [
      {
        id: "default",
        name: "默认渠道",
        baseUrl: "https://api.openai.com",
        apiKey: "",
        apiFormat: "openai",
        models: [{ name: "gpt-image-2", capability: "image" }],
      },
    ],
    model: "default::gpt-image-2",
    imageModel: "default::gpt-image-2",
    videoModel: "default::grok-imagine-video",
    textModel: "default::gpt-5.5",
    audioModel: "default::gpt-4o-mini-tts",
    models: ["default::gpt-image-2"],
  };
}

function loadBridge() {
  const listeners = new Map();
  const hydrationListeners = new Set();
  const storeListeners = new Set();
  const parent = {
    messages: [],
    postMessage(message, origin) {
      this.messages.push({ message, origin });
    },
  };
  const window = {
    parent,
    location: { origin: "https://new-api.example.com" },
    addEventListener(type, listener) {
      listeners.set(type, listener);
    },
    setTimeout(callback) {
      callback();
    },
  };
  let state = { config: createDefaultConfig() };
  let setStateCount = 0;
  const useConfigStore = {
    getState() {
      return state;
    },
    setState(patch) {
      setStateCount += 1;
      state = { ...state, ...patch };
      for (const listener of storeListeners) listener(state);
    },
    subscribe(listener) {
      storeListeners.add(listener);
      return () => storeListeners.delete(listener);
    },
    persist: {
      onFinishHydration(listener) {
        hydrationListeners.add(listener);
        return () => hydrationListeners.delete(listener);
      },
    },
  };

  const output = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
    fileName: "new-api-bridge.ts",
  }).outputText;
  const module = { exports: {} };
  const context = vm.createContext({ module, exports: module.exports, URL, window });
  const wrapper = vm.runInContext(`(function (require, module, exports) { ${output}\n})`, context);
  wrapper(
    (specifier) => {
      if (specifier === "@/stores/use-config-store") return { useConfigStore };
      throw new Error(`Unexpected import: ${specifier}`);
    },
    module,
    module.exports,
  );

  return {
    bridge: module.exports,
    dispatch(data) {
      listeners.get("message")?.({
        origin: window.location.origin,
        source: parent,
        data,
      });
    },
    finishHydration(config = createDefaultConfig()) {
      state = { ...state, config };
      for (const listener of hydrationListeners) listener(state);
    },
    getConfig() {
      return state.config;
    },
    replaceConfig(config) {
      useConfigStore.setState({ config });
    },
    getSetStateCount() {
      return setStateCount;
    },
    hydrationListeners,
    parent,
  };
}

function newApiConfiguration(overrides = {}) {
  return {
    source: "new-api",
    type: "new-api:infinite-canvas:configure",
    mode: "new-api",
    apiUrl: "https://new-api.example.com/pg",
    imageApiUrl: "https://new-api.example.com/pg",
    mediaApiUrl: "https://new-api.example.com/v1",
    apiKey: "utrs_runtime-session",
    profileName: "New API · test2 · A组",
    ...overrides,
  };
}

function assertManagedConfig(config) {
  assert.equal(config.baseUrl, "https://new-api.example.com/pg");
  assert.equal(config.apiKey, "utrs_runtime-session");
  assert.equal(config.model, "new-api-managed::gpt-image-2");
  assert.equal(config.imageModel, "new-api-managed::gpt-image-2");
  assert.equal(config.textModel, "new-api-managed::gpt-5.5");
  assert.equal(config.videoModel, "new-api-managed-media::grok-imagine-video");
  assert.equal(config.audioModel, "new-api-managed-media::gpt-4o-mini-tts");

  const managedChannel = config.channels.find((channel) => channel.id === "new-api-managed");
  assert.deepEqual(
    managedChannel && {
      name: managedChannel.name,
      baseUrl: managedChannel.baseUrl,
      apiKey: managedChannel.apiKey,
      capabilities: Array.from(managedChannel.models, (model) => model.capability),
    },
    {
      name: "New API · test2 · A组",
      baseUrl: "https://new-api.example.com/pg",
      apiKey: "utrs_runtime-session",
      capabilities: ["image", "text"],
    },
  );
  const mediaChannel = config.channels.find(
    (channel) => channel.id === "new-api-managed-media",
  );
  assert.deepEqual(
    mediaChannel && {
      name: mediaChannel.name,
      baseUrl: mediaChannel.baseUrl,
      apiKey: mediaChannel.apiKey,
      capabilities: Array.from(mediaChannel.models, (model) => model.capability),
    },
    {
      name: "New API · test2 · A组 · 视频与音频",
      baseUrl: "https://new-api.example.com/v1",
      apiKey: "utrs_runtime-session",
      capabilities: ["video", "audio"],
    },
  );
  assert.equal(
    config.channels.some((channel) => channel.id === "new-api-managed-image"),
    false,
  );
}

test("rejects non-runtime or non-authoritative New API configuration", () => {
  const invalidConfigurations = [
    newApiConfiguration({ apiKey: "sk-real-key" }),
    newApiConfiguration({ apiUrl: "https://attacker.example.com/pg" }),
    newApiConfiguration({ imageApiUrl: "https://new-api.example.com/v1" }),
    newApiConfiguration({ mediaApiUrl: "https://new-api.example.com/pg" }),
    newApiConfiguration({ apiFormat: "gemini" }),
  ];

  for (const configuration of invalidConfigurations) {
    const harness = loadBridge();
    harness.bridge.installNewApiBridge();
    harness.dispatch(configuration);

    assert.equal(
      harness.getConfig().channels.some((channel) => channel.id.startsWith("new-api-managed")),
      false,
    );
    assert.equal(
      harness.parent.messages.some(
        ({ message }) => message.type === "new-api:infinite-canvas:configured",
      ),
      false,
    );
  }
});

test("reapplies managed Images and Responses configuration after persisted-state hydration", () => {
  const harness = loadBridge();
  harness.bridge.installNewApiBridge();

  assert.equal(harness.hydrationListeners.size, 1);
  harness.dispatch(newApiConfiguration());
  assertManagedConfig(harness.getConfig());

  const hydratedConfig = createDefaultConfig();
  hydratedConfig.channels.unshift(
    {
      id: "new-api-managed",
      name: "stale core",
      baseUrl: "https://stale.example.com",
      apiKey: "stale-core-key",
      apiFormat: "openai",
      models: [{ name: "gpt-5.5", capability: "text" }],
    },
    {
      id: "new-api-managed-image",
      name: "stale image",
      baseUrl: "https://stale.example.com",
      apiKey: "stale-image-key",
      apiFormat: "openai",
      models: [{ name: "gpt-image-2", capability: "image" }],
    },
    {
      id: "new-api-managed-media",
      name: "stale media",
      baseUrl: "https://stale.example.com",
      apiKey: "stale-media-key",
      apiFormat: "openai",
      models: [{ name: "grok-imagine-video", capability: "video" }],
    },
  );
  hydratedConfig.baseUrl = "https://stale.example.com";
  hydratedConfig.apiKey = "";
  hydratedConfig.model = "new-api-managed::gpt-image-2";
  hydratedConfig.imageModel = "new-api-managed::gpt-image-2";
  hydratedConfig.videoModel = "new-api-managed-media::grok-imagine-video";
  hydratedConfig.textModel = "new-api-managed::gpt-5.5";
  hydratedConfig.audioModel = "new-api-managed-media::gpt-4o-mini-tts";
  hydratedConfig.models = [
    hydratedConfig.model,
    hydratedConfig.videoModel,
    hydratedConfig.textModel,
    hydratedConfig.audioModel,
    "default::gpt-image-2",
  ];
  harness.finishHydration(hydratedConfig);
  assertManagedConfig(harness.getConfig());
  assert.equal(
    harness.getConfig().channels.some((channel) => channel.baseUrl === "https://stale.example.com"),
    false,
  );
});

test("repairs managed channels and managed model bindings overwritten after New API configuration", () => {
  const harness = loadBridge();
  harness.bridge.installNewApiBridge();
  harness.dispatch(newApiConfiguration());

  const tamperedConfig = structuredClone(harness.getConfig());
  tamperedConfig.baseUrl = "https://tampered.example.com/v1";
  tamperedConfig.apiKey = "sk-tampered";
  tamperedConfig.apiFormat = "gemini";
  tamperedConfig.model = "new-api-managed::tampered-active";
  tamperedConfig.imageModel = "new-api-managed::tampered-image";
  tamperedConfig.videoModel = "new-api-managed-media::tampered-video";
  tamperedConfig.textModel = "new-api-managed::tampered-text";
  tamperedConfig.audioModel = "new-api-managed-media::tampered-audio";
  tamperedConfig.models = ["new-api-managed::tampered-active"];
  const managedChannel = tamperedConfig.channels.find(
    (channel) => channel.id === "new-api-managed",
  );
  managedChannel.baseUrl = "https://tampered.example.com/v1";
  managedChannel.apiKey = "sk-tampered";
  managedChannel.apiFormat = "gemini";
  managedChannel.models = [{ name: "tampered-model", capability: "text" }];
  const managedMediaChannel = tamperedConfig.channels.find(
    (channel) => channel.id === "new-api-managed-media",
  );
  managedMediaChannel.baseUrl = "https://tampered.example.com/v1";
  managedMediaChannel.apiKey = "sk-tampered";
  managedMediaChannel.apiFormat = "gemini";
  managedMediaChannel.models = [{ name: "tampered-video", capability: "video" }];

  const setStateCountBeforeTamper = harness.getSetStateCount();
  harness.replaceConfig(tamperedConfig);

  assertManagedConfig(harness.getConfig());
  assert.equal(harness.getSetStateCount() - setStateCountBeforeTamper, 2);
});

test("keeps user-created third-party channels editable and selected beside managed channels", () => {
  const harness = loadBridge();
  harness.bridge.installNewApiBridge();
  harness.dispatch(newApiConfiguration());

  const customChannel = {
    id: "custom-third-party",
    name: "第三方服务",
    baseUrl: "https://third-party.example.com/v1",
    apiKey: "sk-local-test-only",
    apiFormat: "openai",
    models: [
      { name: "third-party-image", capability: "image" },
      { name: "third-party-video", capability: "video" },
      { name: "third-party-text", capability: "text" },
      { name: "third-party-audio", capability: "audio" },
    ],
  };
  const customConfig = structuredClone(harness.getConfig());
  customConfig.channels.push(customChannel);
  customConfig.baseUrl = customChannel.baseUrl;
  customConfig.apiKey = customChannel.apiKey;
  customConfig.apiFormat = customChannel.apiFormat;
  customConfig.model = `${customChannel.id}::third-party-image`;
  customConfig.imageModel = `${customChannel.id}::third-party-image`;
  customConfig.videoModel = `${customChannel.id}::third-party-video`;
  customConfig.textModel = `${customChannel.id}::third-party-text`;
  customConfig.audioModel = `${customChannel.id}::third-party-audio`;
  customConfig.models.push(
    customConfig.model,
    customConfig.videoModel,
    customConfig.textModel,
    customConfig.audioModel,
  );
  harness.replaceConfig(customConfig);

  const editedConfig = structuredClone(harness.getConfig());
  const editedChannel = editedConfig.channels.find((channel) => channel.id === customChannel.id);
  editedChannel.name = "第三方服务（已编辑）";
  editedChannel.baseUrl = "https://edited-third-party.example.com/v1";
  editedChannel.apiKey = "sk-local-edited-test-only";
  editedConfig.baseUrl = editedChannel.baseUrl;
  editedConfig.apiKey = editedChannel.apiKey;
  harness.replaceConfig(editedConfig);
  harness.dispatch(
    newApiConfiguration({
      apiKey: "utrs_refreshed-session",
      profileName: "New API · refreshed",
    }),
  );

  const config = harness.getConfig();
  assert.equal(config.model, `${customChannel.id}::third-party-image`);
  assert.equal(config.imageModel, `${customChannel.id}::third-party-image`);
  assert.equal(config.videoModel, `${customChannel.id}::third-party-video`);
  assert.equal(config.textModel, `${customChannel.id}::third-party-text`);
  assert.equal(config.audioModel, `${customChannel.id}::third-party-audio`);
  assert.equal(config.baseUrl, editedChannel.baseUrl);
  assert.equal(config.apiKey, editedChannel.apiKey);
  assert.deepEqual(
    JSON.parse(JSON.stringify(config.channels.find((channel) => channel.id === customChannel.id))),
    JSON.parse(JSON.stringify(editedChannel)),
  );
  assert.equal(
    config.channels.find((channel) => channel.id === "new-api-managed")?.apiKey,
    "utrs_refreshed-session",
  );
  assert.equal(
    config.channels.find((channel) => channel.id === "new-api-managed-media")?.apiKey,
    "utrs_refreshed-session",
  );

  const configWithoutCustomChannel = structuredClone(config);
  configWithoutCustomChannel.channels = configWithoutCustomChannel.channels.filter(
    (channel) => channel.id !== customChannel.id,
  );
  configWithoutCustomChannel.model = "new-api-managed::gpt-image-2";
  configWithoutCustomChannel.imageModel = "new-api-managed::gpt-image-2";
  configWithoutCustomChannel.videoModel = "new-api-managed-media::grok-imagine-video";
  configWithoutCustomChannel.textModel = "new-api-managed::gpt-5.5";
  configWithoutCustomChannel.audioModel = "new-api-managed-media::gpt-4o-mini-tts";
  harness.replaceConfig(configWithoutCustomChannel);

  assert.equal(
    harness.getConfig().channels.some((channel) => channel.id === customChannel.id),
    false,
  );
});

test("keeps custom tool mode on one compatible managed channel", () => {
  const harness = loadBridge();
  harness.bridge.installNewApiBridge();
  harness.dispatch(
    newApiConfiguration({
      mode: "tool",
      apiUrl: "https://custom.example.com/v1",
      imageApiUrl: "https://custom.example.com/v1",
      mediaApiUrl: "https://custom.example.com/v1",
      apiKey: "sk-custom",
      profileName: "Custom API",
    }),
  );

  const config = harness.getConfig();
  const managedChannels = config.channels.filter((channel) =>
    channel.id.startsWith("new-api-managed"),
  );
  assert.equal(managedChannels.length, 1);
  assert.equal(managedChannels[0].id, "new-api-managed");
  assert.equal(managedChannels[0].baseUrl, "https://custom.example.com/v1");
  assert.deepEqual(
    Array.from(managedChannels[0].models, (model) => model.capability),
    ["image", "video", "text", "audio"],
  );
  assert.equal(config.videoModel, "new-api-managed::grok-imagine-video");
  assert.equal(config.audioModel, "new-api-managed::gpt-4o-mini-tts");
});

test("does not retain invalid configuration for a later hydration callback", () => {
  const harness = loadBridge();
  harness.bridge.installNewApiBridge();
  harness.dispatch(newApiConfiguration({ apiKey: "" }));
  harness.finishHydration();

  assert.equal(
    harness.getConfig().channels.some((channel) => channel.id.startsWith("new-api-managed")),
    false,
  );
  assert.equal(
    harness.parent.messages.some(
      ({ message }) => message.type === "new-api:infinite-canvas:configured",
    ),
    false,
  );
});
