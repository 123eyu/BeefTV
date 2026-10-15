import { mergeFetchedChannelModelProfiles } from "../../src/lib/channel-model-catalog";
import { createModelChannel, defaultConfig, normalizeConfigSnapshot } from "../../src/stores/use-config-store";

export const h3Models = ["minimax-h3", "minimax-h3-max", "minimax-h3-max-turbo"];

export function h3AspectConfig(model = "minimax-h3-max-turbo", baseUrl = "https://beeftv.app") {
    const channel = createModelChannel({ id: "beefapi", baseUrl, models: h3Models });
    channel.modelProfiles = mergeFetchedChannelModelProfiles(channel, h3Models.map((id) => ({
        id, supportedEndpointTypes: ["openai-video"], videoCapabilitiesVersion: "aspect-hint-fixture",
        videoCapabilities: {
            references: { promptMaxChars: 50000, minImages: 0, maxImages: 9, maxImageBytes: 31457280, maxVideos: 3, maxVideoBytes: 52428800, maxAudios: 3, maxAudioBytes: 15000000 },
            duration: { selection: "range", min: id === "minimax-h3" ? 5 : 1, max: 15, step: 1, default: 5, ...(id !== "minimax-h3" ? { fractionalMin: 0.92, continuous: true } : {}) },
            ratios: ["16:9", "9:16", "1:1", "21:9", "4:3", "3:4", "adaptive"], defaultRatio: "16:9",
            resolutions: ["768p", "480p", ...(id === "minimax-h3" ? ["2k", "4k"] : ["1080p"])], defaultResolution: "768p",
            generateAudio: { supported: false, default: true }, watermark: { supported: false, default: false },
            operations: ["text_to_video", "image_to_video", "audio_to_video", "reference_to_video"], defaultOperation: "text_to_video",
        },
    })));
    return normalizeConfigSnapshot({ config: { ...defaultConfig, channels: [channel], model: `beefapi::${model}`, videoModel: `beefapi::${model}`, size: "9:16", vquality: "768p", videoSeconds: "15" } }).config;
}
