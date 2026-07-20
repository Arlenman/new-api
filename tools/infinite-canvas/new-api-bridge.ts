import { useConfigStore, type AiConfig, type ModelChannel } from "@/stores/use-config-store";

const PARENT_SOURCE = "new-api" as const;
const CHILD_SOURCE = "infinite-canvas" as const;
const MANAGED_CHANNEL_ID = "new-api-managed";
const MANAGED_IMAGE_CHANNEL_ID = "new-api-managed-image";
const MANAGED_MEDIA_CHANNEL_ID = "new-api-managed-media";
const MANAGED_CHANNEL_IDS = new Set([
  MANAGED_CHANNEL_ID,
  MANAGED_IMAGE_CHANNEL_ID,
  MANAGED_MEDIA_CHANNEL_ID,
]);
const MODEL_IDS = {
  image: `${MANAGED_IMAGE_CHANNEL_ID}::gpt-image-2`,
  video: `${MANAGED_MEDIA_CHANNEL_ID}::grok-imagine-video`,
  text: `${MANAGED_CHANNEL_ID}::gpt-5.5`,
  audio: `${MANAGED_MEDIA_CHANNEL_ID}::gpt-4o-mini-tts`,
} as const;

type ConfigureMessage = {
  source: typeof PARENT_SOURCE;
  type: "new-api:infinite-canvas:configure";
  mode: "new-api" | "tool";
  apiUrl: string;
  imageApiUrl?: string;
  mediaApiUrl?: string;
  apiKey: string;
  apiFormat?: "openai" | "gemini";
  profileName?: string;
};

type ProbeMessage = {
  source: typeof PARENT_SOURCE;
  type: "new-api:infinite-canvas:probe";
};

type BridgeMessage = ConfigureMessage | ProbeMessage;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  Boolean(value) && typeof value === "object";

function isValidApiUrl(value: string) {
  try {
    const url = new URL(value);
    return (
      (url.protocol === "http:" || url.protocol === "https:") &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  } catch {
    return false;
  }
}

function isManagedNewApiUrl(value: string, pathname: "/pg" | "/v1") {
  try {
    const url = new URL(value);
    return (
      url.origin === window.location.origin &&
      (url.pathname.replace(/\/+$/, "") || "/") === pathname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  } catch {
    return false;
  }
}

function isBridgeMessage(value: unknown): value is BridgeMessage {
  if (!isRecord(value) || value.source !== PARENT_SOURCE || typeof value.type !== "string") {
    return false;
  }
  if (value.type === "new-api:infinite-canvas:probe") return true;
  if (
    value.type !== "new-api:infinite-canvas:configure" ||
    (value.mode !== "new-api" && value.mode !== "tool") ||
    typeof value.apiUrl !== "string" ||
    (value.imageApiUrl !== undefined &&
      (typeof value.imageApiUrl !== "string" || !isValidApiUrl(value.imageApiUrl))) ||
    (value.mediaApiUrl !== undefined &&
      (typeof value.mediaApiUrl !== "string" || !isValidApiUrl(value.mediaApiUrl))) ||
    typeof value.apiKey !== "string" ||
    !isValidApiUrl(value.apiUrl) ||
    value.apiKey.trim().length === 0 ||
    (value.profileName !== undefined && typeof value.profileName !== "string") ||
    (value.apiFormat !== undefined && value.apiFormat !== "openai" && value.apiFormat !== "gemini")
  ) {
    return false;
  }
  if (value.mode === "tool") return true;

  return (
    value.apiKey.trim().startsWith("utrs_") &&
    (value.apiFormat === undefined || value.apiFormat === "openai") &&
    typeof value.imageApiUrl === "string" &&
    typeof value.mediaApiUrl === "string" &&
    isManagedNewApiUrl(value.apiUrl, "/pg") &&
    isManagedNewApiUrl(value.imageApiUrl, "/pg") &&
    isManagedNewApiUrl(value.mediaApiUrl, "/v1")
  );
}

let activeConfigureMessage: ConfigureMessage | null = null;

function send(
  type: "new-api:infinite-canvas:ready" | "new-api:infinite-canvas:configured",
  mode?: ConfigureMessage["mode"],
) {
  if (window.parent === window) return;
  window.parent.postMessage(
    { source: CHILD_SOURCE, type, ...(mode ? { mode } : {}) },
    window.location.origin,
  );
}

function createManagedChannel(
  apiUrl: string,
  apiKey: string,
  apiFormat: "openai" | "gemini",
  includeImage: boolean,
  includeMedia: boolean,
  profileName: string,
): ModelChannel {
  return {
    id: MANAGED_CHANNEL_ID,
    name: includeImage ? profileName : `${profileName} · 通用`,
    baseUrl: apiUrl.replace(/\/+$/, ""),
    apiKey,
    apiFormat,
    models: [
      ...(includeImage ? [{ name: "gpt-image-2", capability: "image" as const }] : []),
      ...(includeMedia ? [{ name: "grok-imagine-video", capability: "video" as const }] : []),
      { name: "gpt-5.5", capability: "text" },
      ...(includeMedia ? [{ name: "gpt-4o-mini-tts", capability: "audio" as const }] : []),
    ],
  };
}

function createManagedMediaChannel(
  apiUrl: string,
  apiKey: string,
  apiFormat: "openai" | "gemini",
  profileName: string,
): ModelChannel {
  return {
    id: MANAGED_MEDIA_CHANNEL_ID,
    name: `${profileName} · 视频与音频`,
    baseUrl: apiUrl.replace(/\/+$/, ""),
    apiKey,
    apiFormat,
    models: [
      { name: "grok-imagine-video", capability: "video" },
      { name: "gpt-4o-mini-tts", capability: "audio" },
    ],
  };
}

function createManagedImageChannel(
  apiUrl: string,
  apiKey: string,
  apiFormat: "openai" | "gemini",
  profileName: string,
): ModelChannel {
  return {
    id: MANAGED_IMAGE_CHANNEL_ID,
    name: `${profileName} · 图片`,
    baseUrl: apiUrl.replace(/\/+$/, ""),
    apiKey,
    apiFormat,
    models: [{ name: "gpt-image-2", capability: "image" }],
  };
}

function usesManagedChannel(modelId: string) {
  return MANAGED_CHANNEL_IDS.has(modelId.split("::", 1)[0]);
}

function createManagedConfiguration(current: AiConfig, message: ConfigureMessage): AiConfig {
  const hadManagedChannels = current.channels.some((channel) =>
    MANAGED_CHANNEL_IDS.has(channel.id),
  );
  const preservedChannels = current.channels.filter(
    (channel) => !MANAGED_CHANNEL_IDS.has(channel.id),
  );
  const apiKey = message.apiKey.trim();
  const apiFormat = message.apiFormat || "openai";
  const profileName =
    message.profileName?.trim() || (message.mode === "new-api" ? "New API" : "Custom API");
  const imageApiUrl = (message.imageApiUrl || message.apiUrl).replace(/\/+$/, "");
  const mediaApiUrl = (message.mediaApiUrl || message.apiUrl).replace(/\/+$/, "");
  const apiUrl = message.apiUrl.replace(/\/+$/, "");
  const splitImageChannel = imageApiUrl !== apiUrl;
  const splitMediaChannel = mediaApiUrl !== apiUrl;
  const channel = createManagedChannel(
    apiUrl,
    apiKey,
    apiFormat,
    !splitImageChannel,
    !splitMediaChannel,
    profileName,
  );
  const managedChannels = [
    ...(splitImageChannel
      ? [createManagedImageChannel(imageApiUrl, apiKey, apiFormat, profileName)]
      : []),
    channel,
    ...(splitMediaChannel
      ? [createManagedMediaChannel(mediaApiUrl, apiKey, apiFormat, profileName)]
      : []),
  ];
  const imageModel = splitImageChannel ? MODEL_IDS.image : `${MANAGED_CHANNEL_ID}::gpt-image-2`;
  const videoModel = splitMediaChannel
    ? MODEL_IDS.video
    : `${MANAGED_CHANNEL_ID}::grok-imagine-video`;
  const audioModel = splitMediaChannel
    ? MODEL_IDS.audio
    : `${MANAGED_CHANNEL_ID}::gpt-4o-mini-tts`;
  const useManagedModel = !hadManagedChannels || usesManagedChannel(current.model);
  const nextModel = useManagedModel ? imageModel : current.model;
  const nextImageModel =
    !hadManagedChannels || usesManagedChannel(current.imageModel) ? imageModel : current.imageModel;
  const nextVideoModel =
    !hadManagedChannels || usesManagedChannel(current.videoModel) ? videoModel : current.videoModel;
  const nextTextModel =
    !hadManagedChannels || usesManagedChannel(current.textModel) ? MODEL_IDS.text : current.textModel;
  const nextAudioModel =
    !hadManagedChannels || usesManagedChannel(current.audioModel) ? audioModel : current.audioModel;
  return {
    ...current,
    channelMode: "local",
    baseUrl: useManagedModel ? channel.baseUrl : current.baseUrl,
    apiKey: useManagedModel ? channel.apiKey : current.apiKey,
    apiFormat: useManagedModel ? channel.apiFormat : current.apiFormat,
    channels: [...managedChannels, ...preservedChannels],
    model: nextModel,
    imageModel: nextImageModel,
    videoModel: nextVideoModel,
    textModel: nextTextModel,
    audioModel: nextAudioModel,
    models: [
      imageModel,
      videoModel,
      MODEL_IDS.text,
      audioModel,
      ...preservedChannels.flatMap((item) =>
        item.models.map((model) => `${item.id}::${model.name}`),
      ),
    ],
  };
}

function managedConfigurationMatches(current: AiConfig, message: ConfigureMessage) {
  const expected = createManagedConfiguration(current, message);
  if (
    current.channelMode !== expected.channelMode ||
    current.baseUrl !== expected.baseUrl ||
    current.apiKey !== expected.apiKey ||
    current.apiFormat !== expected.apiFormat ||
    current.model !== expected.model ||
    current.imageModel !== expected.imageModel ||
    current.videoModel !== expected.videoModel ||
    current.textModel !== expected.textModel ||
    current.audioModel !== expected.audioModel ||
    current.models.length !== expected.models.length ||
    current.models.some((model, index) => model !== expected.models[index])
  ) {
    return false;
  }

  const managedChannels = current.channels.filter((channel) => MANAGED_CHANNEL_IDS.has(channel.id));
  const expectedManagedChannels = expected.channels.filter((channel) =>
    MANAGED_CHANNEL_IDS.has(channel.id),
  );
  if (managedChannels.length !== expectedManagedChannels.length) return false;

  return expectedManagedChannels.every((expectedChannel) => {
    const channel = managedChannels.find((item) => item.id === expectedChannel.id);
    return Boolean(
      channel &&
        channel.name === expectedChannel.name &&
        channel.baseUrl === expectedChannel.baseUrl &&
        channel.apiKey === expectedChannel.apiKey &&
        channel.apiFormat === expectedChannel.apiFormat &&
        channel.models.length === expectedChannel.models.length &&
        channel.models.every((model, index) => {
          const expectedModel = expectedChannel.models[index];
          return (
            model.name === expectedModel.name &&
            model.capability === expectedModel.capability &&
            model.script === expectedModel.script
          );
        })
    );
  });
}

function applyManagedConfiguration(message: ConfigureMessage) {
  const nextConfig = createManagedConfiguration(useConfigStore.getState().config, message);
  useConfigStore.setState({ config: nextConfig });
}

export function installNewApiBridge() {
  if (window.parent === window) return;

  let repairingManagedConfiguration = false;
  const repairManagedConfiguration = (config: AiConfig) => {
    if (
      !activeConfigureMessage ||
      repairingManagedConfiguration ||
      managedConfigurationMatches(config, activeConfigureMessage)
    ) {
      return;
    }

    repairingManagedConfiguration = true;
    try {
      applyManagedConfiguration(activeConfigureMessage);
    } finally {
      repairingManagedConfiguration = false;
    }
  };

  useConfigStore.persist.onFinishHydration(() => {
    repairManagedConfiguration(useConfigStore.getState().config);
  });
  useConfigStore.subscribe((state) => repairManagedConfiguration(state.config));
  window.addEventListener("message", (event) => {
    if (
      event.source !== window.parent ||
      event.origin !== window.location.origin ||
      !isBridgeMessage(event.data)
    ) {
      return;
    }
    if (event.data.type === "new-api:infinite-canvas:probe") {
      send("new-api:infinite-canvas:ready");
      return;
    }
    activeConfigureMessage = event.data;
    applyManagedConfiguration(event.data);
    send("new-api:infinite-canvas:configured", event.data.mode);
  });
  window.addEventListener("load", () => send("new-api:infinite-canvas:ready"), { once: true });
  window.setTimeout(() => send("new-api:infinite-canvas:ready"), 0);
}
