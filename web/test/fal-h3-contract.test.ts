import { expect, test } from "bun:test";
import { beefAPIVideoContract } from "../src/lib/beefapi-video-contracts";
import { portraitPriceLines, sanitizeVideoPriceQuote } from "../src/lib/seedance-portrait";
import { createModelChannel, defaultConfig, normalizeConfigSnapshot, resolveModelRequestConfig } from "../src/stores/use-config-store";
import { createVideoGenerationsTask } from "../src/services/api/video-provider-newapi";
import { createVideoTransport } from "../src/services/api/video-transport";
import { videoResponseTools } from "../src/services/api/video-response";
import { mergeFetchedChannelModelProfiles } from "../src/lib/channel-model-catalog";
import { modelCapabilityConfigFor } from "../src/lib/model-capabilities";

test("H3 preserves CNY seconds and 768p without accepting unverified model aliases", () => {
    expect(beefAPIVideoContract("minimax-h3-max-turbo")?.protocol).toBe("newapi-channel-2");
    expect(beefAPIVideoContract("minimax-h3-max-turbo-other")).toBeUndefined();
    const price = {currency: "CNY", mode: "seconds", rates: {"768p": {output: 0.2441304, reference_video: 0}}};
    expect(sanitizeVideoPriceQuote(price)).toEqual(price);
    expect(portraitPriceLines(price)).toEqual(["768p：¥0.2441 / 秒"]);
    expect(sanitizeVideoPriceQuote({...price, currency: "USD"})).toBeNull();
});

test("H3 direct transport keeps end-only roles, 768p and local media", async () => {
    const model = "minimax-h3-max-turbo";
    const channel = createModelChannel({id: "beefapi", baseUrl: "https://beeftv.app", models: [model]});
    channel.modelProfiles = mergeFetchedChannelModelProfiles(channel, [{id: model, supportedEndpointTypes: ["openai-video"], videoCapabilitiesVersion: "test-gateway-h3", videoCapabilities: {
        references: {promptMaxChars: 50000, minImages: 0, maxImages: 2, maxImageBytes: 31457280, maxVideos: 0, maxAudios: 1, maxAudioBytes: 15000000, minAudioDurationSeconds: 2},
        duration: {selection: "range", min: 1, max: 15, step: 1, default: 5},
        ratios: ["16:9", "9:16", "1:1", "21:9", "4:3", "3:4"], defaultRatio: "16:9",
        resolutions: ["768p", "480p", "1080p"], defaultResolution: "768p",
        generateAudio: {supported: false, default: true}, watermark: {supported: false, default: false},
        operations: ["text_to_video", "image_to_video", "audio_to_video"], defaultOperation: "text_to_video",
    }}]);
    const snapshot = normalizeConfigSnapshot({config: {...defaultConfig, channels: [channel], vquality: "768", videoSeconds: "5"}}).config;
    const config = resolveModelRequestConfig(snapshot, `beefapi::${model}`);
    expect(config.interfaceType).toBe("newapi-channel-2");
    expect(modelCapabilityConfigFor(snapshot, `beefapi::${model}`).video!.resolutions).toEqual(["768p", "480p", "1080p"]);
    const calls: unknown[] = [];
    const deps = {response: videoResponseTools, transport: {...createVideoTransport(config), post: async <T>(url: string, body: unknown) => { calls.push({url, body}); return {id: "original", status: "queued"} as T; }}};
    const image = {id: "last", name: "last.png", type: "image/png", dataUrl: "data:image/png;base64,dGVzdA=="};
    await createVideoGenerationsTask(deps, config, model, "test", [image], [], [], {videoEndFrameNodeId: "last"});
    expect(calls[0]).toMatchObject({body: {model, last_frame_url: image.dataUrl, resolution: "768p", seconds: "5"}});
    const payload = (calls[0] as {body: Record<string, unknown>}).body;
    expect(payload.first_frame_url).toBeUndefined(); expect(payload.generate_audio).toBeUndefined(); expect(payload.image_urls).toBeUndefined();
    await expect(createVideoGenerationsTask(deps, config, model, "test", [image], [], [], {videoEndFrameNodeId: "missing"})).rejects.toThrow("首帧或尾帧");
    expect(calls).toHaveLength(1);
});
