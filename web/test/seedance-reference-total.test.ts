import { expect, test } from "bun:test";
import { modelCapabilityConfigFor } from "../src/lib/model-capabilities";
import { assertVideoCapability } from "../src/services/api/video-validation";

const image = (id: string) => ({ id, name: id, type: "image/png", dataUrl: "", width: 512, height: 512 });
const video = (id: string) => ({ id, name: id, type: "video/mp4", url: `https://example.com/${id}.mp4`, width: 1280, height: 720, durationMs: 5000 });
const audio = (id: string) => ({ id, name: id, type: "audio/mpeg", url: `https://example.com/${id}.mp3`, durationMs: 3000 });

function profile(baseUrl: string, protocol: "newapi" | "newapi-channel-2" | "volcengine-ark-video", model: string) {
    const channel = { id: "test", models: [model], baseUrl, modelProfiles: [{ model, protocol }] };
    return modelCapabilityConfigFor({ channels: [channel] }, `test::${model}`).video!;
}

test("企业网关线路的 seedance-2.0 合计上限是腾讯写的 12 个文件", () => {
    const seedance20 = profile("https://enterprise.beefapi.com", "newapi", "seedance-2.0");
    expect(seedance20.references.maxTotalReferences).toBe(12);
    // 9 张图 + 3 段视频 + 3 段音频 = 15，超过腾讯 1.0-pro 的合计上限。
    const images = Array.from({ length: 9 }, (_, index) => image(`i${index}`));
    const videos = Array.from({ length: 3 }, (_, index) => video(`v${index}`));
    const audios = Array.from({ length: 3 }, (_, index) => audio(`a${index}`));
    expect(() => assertVideoCapability(seedance20, images, videos, audios, "5")).toThrow("最多支持 12 个参考素材");
    // 正好 12 个（9 张图 + 3 段音频）仍然放行。
    expect(() => assertVideoCapability(seedance20, images, [], audios, "5")).not.toThrow();
});

test("1.5-pro（seedance-2.5）与火山直连没有合计 12 的限制", () => {
    expect(profile("https://enterprise.beefapi.com", "newapi", "seedance-2.5").references.maxTotalReferences).toBeUndefined();
    expect(profile("https://ark.cn-beijing.volces.com/api/v3", "newapi", "seedance-2.0").references.maxTotalReferences).toBeUndefined();
    expect(profile("https://ark.cn-beijing.volces.com/api/v3", "volcengine-ark-video", "doubao-seedance-2-0").references.maxTotalReferences).toBeUndefined();
    // 合计上限跟着网关线路走：同一模型走我们自己的企业网关时才按腾讯收紧。
    expect(profile("https://beeftv.app", "newapi", "seedance-2.0").references.maxTotalReferences).toBe(12);
    // 托管网页里的系统渠道走相对地址，按渠道 id 认出企业网关。
    const hosted = modelCapabilityConfigFor({ channels: [{ id: "beefapi", scope: "system", baseUrl: "/api/ai/system/beefapi", apiKey: "system", models: ["seedance-2.0"], modelProfiles: [{ model: "seedance-2.0", protocol: "newapi" }] }] }, "beefapi::seedance-2.0").video!;
    expect(hosted.references.maxTotalReferences).toBe(12);
});

test("seedance 输出时长下限按官方写到 4 秒", () => {
    for (const model of ["seedance-2.0", "seedance-2.5"]) {
        const target = profile("https://enterprise.beefapi.com", "newapi", model);
        expect(target.duration.min).toBe(4);
        expect(() => assertVideoCapability(target, [image("i0")], [], [], "3")).toThrow("视频时长不在当前模型支持范围内");
        expect(() => assertVideoCapability(target, [image("i0")], [], [], "4")).not.toThrow();
    }
});
