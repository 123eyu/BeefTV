import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { VideoSettingsPanel } from "../src/components/video-settings-panel";
import { CanvasH3UnsupportedFrames } from "../src/components/canvas/canvas-h3-controls";
import { canvasThemes } from "../src/lib/canvas-theme";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import { createModelChannel, defaultConfig } from "../src/stores/use-config-store";

function renderDuration(seconds: string, continuous: boolean) {
    const model = continuous ? "minimax-h3-max" : "minimax-h3";
    const capabilityConfig = defaultModelCapabilityConfig("newapi", model);
    capabilityConfig.video!.duration = { selection: "range", min: continuous ? 1 : 5, max: 15, step: 1, default: 5, ...(continuous ? { fractionalMin: 0.92, continuous: true } : {}) };
    const channel = createModelChannel({ id: "h3-test", scope: "system", interfaceType: "newapi", models: [model] });
    channel.modelProfiles = [{ model, displayName: model, capability: "video", protocol: "newapi", billingMode: "per_second", unitPriceMicrocredits: 0, capabilityConfig }];
    const config = { ...defaultConfig, channels: [channel], model: `h3-test::${model}`, videoModel: `h3-test::${model}`, videoSeconds: seconds };
    return renderToStaticMarkup(<VideoSettingsPanel config={config} theme={canvasThemes.dark} onConfigChange={() => {}} />);
}

test("shared video settings preserve exact H3 fractional duration in both inputs", () => {
    for (const seconds of ["0.925", "10.92"]) {
        const html = renderDuration(seconds, true);
        expect(html.match(/value="\d+(?:\.\d+)?"/g)?.filter((value) => value === `value="${seconds}"`)).toHaveLength(2);
        expect(html).toContain('min="0.92"');
        expect(html).toContain('step="any"');
        expect(html).toContain('inputMode="decimal"');
    }
});

test("integer H3 duration keeps the shared whole-second controls", () => {
    const html = renderDuration("5", false);
    expect(html).toContain('min="5"');
    expect(html).toContain('step="1"');
    expect(html).toContain('inputMode="numeric"');
    expect(html).not.toContain('step="any"');
});

test("switching away from Max leaves a visible way to clear unsupported saved frames", () => {
    const metadata = { videoEndFrameNodeId: "end", videoMiddleFrameNodeId: "middle", videoMiddleFrameTime: 2.5 };
    for (const model of ["minimax-h3-max-turbo", "minimax-h3-max-style-vhs"]) {
        expect(renderToStaticMarkup(<CanvasH3UnsupportedFrames model={model} metadata={metadata} onChange={() => {}} />)).toContain("清除不支持的帧设置");
    }
    expect(renderToStaticMarkup(<CanvasH3UnsupportedFrames model="minimax-h3-max" metadata={metadata} onChange={() => {}} />)).toBe("");
});
