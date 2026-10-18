import { expect, test } from "bun:test";
import { rebaseCanvasGenerationTarget } from "@/lib/canvas/canvas-generation-live-target";
import { clearInPlaceMediaOutputMetadata } from "@/lib/canvas/canvas-project-generation";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

const captured: CanvasNodeData = {
    id: "source", type: CanvasNodeType.Image, title: "镜头一", width: 320, height: 180, position: { x: 10, y: 20 },
    metadata: { prompt: "原提示词", composerContent: "原提示词", content: "old-media", storageKey: "old-resource", assetId: "old-asset", taskId: "old-task", taskClientOperationId: "old-operation", generationOutputNodeIds: ["old-output"], generationEffectKeys: ["old-effect"], status: "error" },
};
const planned: CanvasNodeData = { ...captured, metadata: { ...clearInPlaceMediaOutputMetadata(captured.metadata), prompt: "确认的提示词", composerContent: "确认的提示词", status: "loading" } };

test("in-place generation honors output and task fields removed from the captured target", () => {
    const next = rebaseCanvasGenerationTarget(structuredClone(captured), planned, captured);
    for (const key of ["content", "storageKey", "assetId", "taskId", "taskClientOperationId", "generationOutputNodeIds", "generationEffectKeys"]) {
        expect(Object.hasOwn(next.metadata!, key)).toBe(false);
    }
    expect(next.metadata).toMatchObject({ prompt: "确认的提示词", status: "loading" });
});

test("output cleanup preserves fields and editor changes made after the snapshot", () => {
    const live: CanvasNodeData = { ...captured, title: "准备时改过的名称", position: { x: 80, y: 90 }, metadata: {
        ...captured.metadata, content: "new-media", taskId: "new-task", generationOutputNodeIds: ["new-output"], prompt: "新的提示词", composerContent: "新的提示词",
    } };
    const next = rebaseCanvasGenerationTarget(live, planned, captured);
    expect(next.title).toBe(live.title);
    expect(next.position).toEqual(live.position);
    expect(next.metadata).toMatchObject({ content: "new-media", taskId: "new-task", generationOutputNodeIds: ["new-output"], prompt: "新的提示词", composerContent: "新的提示词", status: "loading" });
    expect(Object.hasOwn(next.metadata!, "storageKey")).toBe(false);
});
