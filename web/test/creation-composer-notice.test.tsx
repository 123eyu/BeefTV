import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CreationComposer } from "../src/pages/create/creation-workspace";
import { defaultModelCapabilityConfig, modelCapabilityConfigFor } from "../src/lib/model-capabilities";
import { createModelChannel, defaultConfig } from "../src/stores/use-config-store";

const model = "seedance-2.0";
const capabilityConfig = defaultModelCapabilityConfig("newapi", model);
const channel = createModelChannel({ id: "beefapi", name: "BeefAPI", scope: "user", interfaceType: "newapi", baseUrl: "https://enterprise.beefapi.com", models: [model] });
channel.modelProfiles = [{ model, displayName: model, capability: "video", protocol: "newapi", capabilityConfig, videoCapabilitiesVersion: "fixture-v1" }];
const config = { ...defaultConfig, channels: [channel], model: `beefapi::${model}`, videoModel: `beefapi::${model}` };
const videoProfile = modelCapabilityConfigFor(config, `beefapi::${model}`).video!;

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

function render(notice?: string, currentConfig = config) {
    const currentCapabilities = modelCapabilityConfigFor(currentConfig, `beefapi::${model}`);
    return renderToStaticMarkup(
        <QueryClientProvider client={queryClient}>
        <CreationComposer
            variant="thread"
            mode="video"
            prompt=""
            setPrompt={() => {}}
            busy={false}
            generationActive={false}
            referenceReplacementBusy={false}
            attachments={[]}
            referenceLimits={{ image: videoProfile.references.maxImages, video: videoProfile.references.maxVideos, audio: videoProfile.references.maxAudios }}
            references={[]}
            onRemoveAttachment={() => {}}
            onClearAttachments={() => {}}
            onClearComposer={() => {}}
            onReorderAttachments={() => {}}
            onReplaceAttachment={() => {}}
            onReplaceReferenceFiles={() => {}}
            onOpenLibrary={() => {}}
            onModeChange={() => {}}
            model={`beefapi::${model}`}
            modelRequirements={{}}
            videoProfile={currentCapabilities.video!}
            imageProfile={currentCapabilities.image!}
            config={currentConfig}
            onModelChange={() => {}}
            ratio={videoProfile.defaultRatio}
            setRatio={() => {}}
            seconds="3"
            setSeconds={() => {}}
            quality="auto"
            setQuality={() => {}}
            videoQuality={videoProfile.defaultResolution.replace(/p$/i, "")}
            setVideoQuality={() => {}}
            count="1"
            setCount={() => {}}
            textStreaming
            setTextStreaming={() => {}}
            textThinking={false}
            setTextThinking={() => {}}
            promptOptimizerProvider={null}
            composerFocusRef={{ current: null }}
            notice={notice}
            onPromptFocus={() => {}}
            onSubmit={() => {}}
        />
        </QueryClientProvider>,
    );
}

test("被拦下的说明显示在输入框内，而不是被全局隐藏的 Toast 里", () => {
    const text = "输出时长为 3 秒，当前模型只支持 4–15 秒；请改成 4–15 秒后再提交";
    const html = render(text);
    expect(html).toContain("creation-composer-notice");
    expect(html).toContain('role="alert"');
    expect(html).toContain(text);
    // 说明出现在发送按钮之前，也就是输入框区域里，不是页面角落的浮层。
    expect(html.indexOf("creation-composer-notice")).toBeLessThan(html.indexOf("creation-submit"));
});

test("没有拦截说明时不渲染这段提示", () => {
    expect(render()).not.toContain("creation-composer-notice");
});

test("缺少视频目录时提示刷新、保留参考入口，不显示空设置或零秒", () => {
    const unavailable = structuredClone(config);
    unavailable.channels[0].modelProfiles![0].videoCapabilitiesVersion = undefined;
    const html = render(undefined, unavailable);
    expect(html).toContain("请在设置中刷新模型列表");
    expect(html).toContain('aria-label="添加更多参考内容"');
    expect(html).not.toContain('aria-label="生成设置：');
    expect(html).not.toContain('aria-label="视频时长：');
    expect(html).not.toContain("0秒");
    expect(render(undefined, config)).toContain('aria-label="视频时长：');
});
