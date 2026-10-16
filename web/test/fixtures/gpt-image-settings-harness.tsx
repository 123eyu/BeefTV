import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ImageSettingsPanel } from "../../src/components/image-settings-panel";
import { canvasThemes } from "../../src/lib/canvas-theme";
import { defaultConfig, createModelChannel, type AiConfig } from "../../src/stores/use-config-store";
import { requestGeneration } from "../../src/services/api/image";

const models = ["gpt-image-2", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst"];
const channel = createModelChannel({ id: "quality-check", baseUrl: "https://image.example.invalid/v1", apiKey: "fixture-only", models,
    modelProfiles: models.map((model) => ({ model, capability: "image", protocol: "openai-image" })) });

function Harness() {
    const [config, setConfig] = useState<AiConfig>({ ...defaultConfig, channels: [channel], model: "quality-check::gpt-image-2", imageModel: "quality-check::gpt-image-2", quality: "low", size: "1024x1024", count: "1" });
    const [status, setStatus] = useState("");
    const theme = new URLSearchParams(location.search).get("theme") === "dark" ? "dark" : "light";
    return <main className="p-4" style={{ width: 360, maxWidth: "100%", color: canvasThemes[theme].node.text, background: canvasThemes[theme].canvas.background }}>
        <label>模型<select aria-label="模型" value={config.model} onChange={(event) => setConfig((old) => ({ ...old, model: event.target.value, quality: "low" }))}>
            {models.map((model) => <option key={model} value={`quality-check::${model}`}>{model}</option>)}
        </select></label>
        <ImageSettingsPanel config={config} theme={canvasThemes[theme]} onConfigChange={(key, value) => setConfig((old) => ({ ...old, [key]: value }))} />
        <button onClick={async () => { setStatus(""); try { await requestGeneration(config, "fixture cat"); setStatus("已校验"); } catch (error) { setStatus(String(error)); } }}>校验请求</button>
        <output data-testid="selection">{JSON.stringify({ quality: config.quality, size: config.size })}</output>
        <output role="status">{status}</output>
    </main>;
}
createRoot(document.getElementById("root")!).render(<Harness />);
