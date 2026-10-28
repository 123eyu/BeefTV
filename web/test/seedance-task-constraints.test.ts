import { expect, test } from "bun:test";
import { seedanceTaskOptions } from "../src/lib/seedance-task-constraints";
import { explainGenerationError } from "../src/lib/generation-error";
import { resolveVideoOperation } from "../src/lib/model-selection";

test("Seedance 2.5 locks explicit task options and preserves semantic reference", () => {
    for (const model of ["seedance-2.5", "seedance-2.5-official", "doubao-seedance-2-5-260628"]) {
        expect(seedanceTaskOptions(model, "9:16", 6, ["first_frame"], 0)).toEqual({ ratio: "adaptive", duration: 6 });
        expect(seedanceTaskOptions(model, "9:16", 6, ["reference_image"], 1, "reference_to_video")).toEqual({ ratio: "9:16", duration: 6 });
        expect(seedanceTaskOptions(model, "9:16", 6, [], 1, "inpaint")).toEqual({ ratio: "adaptive", duration: -1 });
        expect(seedanceTaskOptions(model, "9:16", 6, [], 1, "extend")).toEqual({ ratio: "adaptive", duration: 6 });
    }
    expect(seedanceTaskOptions("seedance-2.0", "9:16", 6, ["first_frame"], 0)).toEqual({ ratio: "9:16", duration: 6 });
});

test("backend TaskTypeConstraint failure explains how to correct the mode", () => {
    const message = "首尾帧模式的画面比例需跟随首帧。请选择自适应比例后重新生成；如需指定比例，请改用参考生成模式。";
    const error = explainGenerationError({ code: "invalid_params", message });
    expect(error.category).toBe("invalid_params");
    expect(error.reason).toBe("首尾帧模式的画面比例需跟随首帧");
    expect(error.action).toContain("自适应");
    expect(error.blockAutomaticRetry).toBe(true);
    const persisted = `${error.message}排查编号：请求 req_task_constraint_123。`;
    expect(explainGenerationError({ code: "invalid_params", message: persisted }).requestId).toBe("req_task_constraint_123");
    expect(explainGenerationError({ code: "invalid_params", message: persisted, request_id: "req_outer_456" }).requestId).toBe("req_outer_456");
});

test("explicit reference and extension intent survives model selection", () => {
    const input = { imageCount: 1, characterCount: 0, videoCount: 0, audioCount: 0 };
    expect(resolveVideoOperation(input, "reference_to_video")).toBe("reference_to_video");
    expect(resolveVideoOperation({ ...input, videoCount: 1 }, "extend")).toBe("extend");
    expect(resolveVideoOperation({ ...input, videoCount: 1 })).toBe("reference_to_video");
});

test("temporary unavailable error inside failed task is actionable", () => {
 const e=explainGenerationError({error:{code:"model_temporarily_unavailable",message:"无可用线路：当前售价档位 standard 暂无可用线路"}});
 expect(e.category).toBe("provider_unavailable"); expect(e.reason).toBe("模型服务暂时不可用");
});
