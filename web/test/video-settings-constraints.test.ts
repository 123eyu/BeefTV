import { expect, test } from "bun:test";
import { videoSettingsConstraints } from "../src/lib/video-settings-constraints";
import { createVideoGenerationsTask } from "../src/services/api/video-provider-newapi";
import { createVideoTransport } from "../src/services/api/video-transport";
import { videoResponseTools } from "../src/services/api/video-response";
import { resolveModelRequestConfig } from "../src/stores/use-config-store";
import { h3AspectConfig, h3Models } from "./fixtures/h3-aspect-config";

for (const model of h3Models) {
    test(`${model}: settings reflect the actual keyframe payload and restore the selected ratio`, async () => {
        const config = h3AspectConfig(model);
        const savedSize = config.size;
        const bodies: Record<string, unknown>[] = [];
        const resolved = resolveModelRequestConfig(config, config.model);
        const deps = { response: videoResponseTools, transport: { ...createVideoTransport(resolved), post: async <T>(_url: string, body: unknown) => { bodies.push(body as Record<string, unknown>); return { id: "fixture" } as T; } } };
        const images = [{ id: "frame", name: "frame.png", type: "image/png", dataUrl: "data:image/png;base64,dGVzdA==" }];
        const frames = [
            { videoStartFrameNodeId: "frame" },
            { videoEndFrameNodeId: "frame" },
            { videoStartFrameNodeId: "frame", videoEndFrameNodeId: "frame" },
        ];
        for (const metadata of frames) {
            await createVideoGenerationsTask(deps, resolved, model, "Fixture", images, [], [], metadata);
            expect(bodies.at(-1)!.aspect_ratio).toBeUndefined();
            expect(videoSettingsConstraints(config, { imageCount: 1 }, metadata).ratioLabel).toBe(metadata.videoStartFrameNodeId ? "随首帧图片" : "随尾帧图片");
        }
        await createVideoGenerationsTask(deps, resolved, model, "Fixture", [], [], []);
        expect(bodies.at(-1)!.aspect_ratio).toBe("9:16");
        expect(videoSettingsConstraints(config, {})).toEqual({});
        expect(videoSettingsConstraints(config, {}, { videoEndFrameNodeId: "disconnected" })).toEqual({});
        expect(config.size).toBe(savedSize);
    });
}

test("H3 base ordinary references retain an independent ratio", () => {
    const config = h3AspectConfig("minimax-h3");
    for (const metadata of [undefined, { videoEditOperation: "reference_to_video" as const }]) {
        expect(videoSettingsConstraints(config, { imageCount: 1 }, metadata)).toEqual({});
    }
    expect(videoSettingsConstraints(config, { imageCount: 1, videoCount: 1 })).toEqual({});
    expect(videoSettingsConstraints(config, { imageCount: 1, audioCount: 2 })).toEqual({});
});

test("Max and Turbo treat ordinary image inputs as keyframes", () => {
    for (const model of h3Models.slice(1)) expect(videoSettingsConstraints(h3AspectConfig(model), { imageCount: 1 }).ratioLabel).toBe("随首帧图片");
});

test("same-name models on custom endpoints keep their own protocol controls", () => {
    for (const model of h3Models) expect(videoSettingsConstraints(h3AspectConfig(model, "https://custom.example.com"), { imageCount: 1 })).toEqual({});
});

test("switching to Seedance preserves its existing settings constraints", () => {
    const config = { ...h3AspectConfig(), model: "seedance-2.5" };
    expect(videoSettingsConstraints(config, { imageCount: 1 }).ratioLabel).toBe("随首帧");
    expect(videoSettingsConstraints(config, { videoCount: 1 }, { videoEditOperation: "inpaint" })).toEqual({ ratioLabel: "随原视频", durationLocked: true });
    expect(videoSettingsConstraints(config, { characterCount: 1 }).ratioLabel).toBeUndefined();
});

test("a character card cover alone does not determine keyframe mode", () => {
    for (const model of h3Models) {
        const config = h3AspectConfig(model);
        expect(videoSettingsConstraints(config, { characterCount: 1 }).ratioLabel).toBeUndefined();
        expect(videoSettingsConstraints(config, { imageCount: 1, characterCount: 1 }, { videoStartFrameNodeId: "frame" }).ratioLabel).toBe("随首帧图片");
        expect(videoSettingsConstraints(config, { imageCount: 1, characterCount: 1 }, { videoEndFrameNodeId: "frame" }).ratioLabel).toBe("随尾帧图片");
    }
    expect(videoSettingsConstraints(h3AspectConfig(), { characterCount: 1 }).ratioHelp).toContain("所选比例只在没有参考图时生效");
});

test("mixed H3 references with an explicit frame are rejected before dispatch", async () => {
    const config = h3AspectConfig("minimax-h3");
    const resolved = resolveModelRequestConfig(config, config.model);
    const deps = { response: videoResponseTools, transport: { ...createVideoTransport(resolved), post: async () => { throw new Error("must not dispatch"); } } };
    const image = { id: "frame", name: "frame.png", type: "image/png", dataUrl: "data:image/png;base64,dGVzdA==" };
    const video = { id: "video", name: "video.mp4", type: "video/mp4", url: "https://example.com/fixture.mp4" };
    await expect(createVideoGenerationsTask(deps, resolved, "minimax-h3", "Fixture", [image], [video], [], { videoStartFrameNodeId: "frame" })).rejects.toThrow("不能同时指定首帧或尾帧");
    expect(videoSettingsConstraints(config, { imageCount: 1, videoCount: 1 }, { videoStartFrameNodeId: "frame" })).toEqual({});
});

test("character representations mixed with explicit frames explain the pre-dispatch rejection", async () => {
    for (const model of h3Models) {
        const config = h3AspectConfig(model);
        const resolved = resolveModelRequestConfig(config, config.model);
        let dispatched = 0;
        const deps = { response: videoResponseTools, transport: { ...createVideoTransport(resolved), post: async <T>() => { dispatched += 1; return { id: "fixture" } as T; } } };
        const images = ["frame", "character-reference-1"].map((id) => ({ id, name: "frame.png", type: "image/png", dataUrl: "data:image/png;base64,dGVzdA==" }));
        for (const metadata of [{ videoStartFrameNodeId: "frame" }, { videoEndFrameNodeId: "frame" }]) {
            await expect(createVideoGenerationsTask(deps, resolved, model, "Fixture", images, [], [], metadata)).rejects.toThrow("图片须指定为首帧或尾帧");
            expect(videoSettingsConstraints(config, { imageCount: 1, characterCount: 1 }, metadata).ratioHelp).toContain("不能与指定的首尾帧同时使用");
        }
        expect(dispatched).toBe(0);
    }
});
