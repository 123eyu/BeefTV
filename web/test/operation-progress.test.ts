import { afterEach, expect, test } from "bun:test";
import { createTransferSampler, operationProgressView, type OperationProgress } from "@/lib/operation-progress";
import { captureUserScope } from "@/lib/user-scope-guard";
import { setActiveUserScope } from "@/lib/user-scope";
import { startOperation, useOperationProgress } from "@/stores/use-operation-progress";
import { beginTaskReferenceObservation, publishTaskReferenceProgress } from "@/services/task-reference-progress";
import type { GenerationTask } from "@/services/api/task-center";

const base: OperationProgress = { id: "upload", title: "参考视频", detail: "正在上传", phase: "transferring", loadedBytes: 2 * 1024 ** 2, totalBytes: 8 * 1024 ** 2, bytesPerSecond: 1024 ** 2, startedAt: 1000, updatedAt: 3000 };
afterEach(() => useOperationProgress.setState({ entries: {} }));

test("真实字节、速度和剩余时间来自同一个传输样本", () => {
    expect(operationProgressView(base, 3000)).toMatchObject({ percent: 25, size: "2.0 MB / 8.0 MB", speed: "1.0 MB/s", timing: "预计剩余 6 秒" });
    expect(operationProgressView({ ...base, totalBytes: undefined }, 3000)).toMatchObject({ percent: null, size: "2.0 MB", timing: "已用 2 秒" });
});
test("等待确认和停止传输时不保留假速度或预计完成时间", () => {
    expect(operationProgressView(base, 7000)).toMatchObject({ speed: "", timing: "已用 6 秒" });
    expect(operationProgressView({ ...base, loadedBytes: base.totalBytes, phase: "processing" }, 3000)).toMatchObject({ percent: null, speed: "", timing: "已用 2 秒" });
});
test("重传归零后重新采样，不沿用上一轮速度", () => {
    const sample = createTransferSampler(0);
    expect(sample(1024, 4096, 1000).bytesPerSecond).toBe(1024);
    expect(sample(0, 4096, 1200).bytesPerSecond).toBe(0);
    expect(sample(512, 4096, 2200).bytesPerSecond).toBe(512);
});
test("反复收到相同字节数不会让停滞传输一直显示速度", () => {
    const sample = createTransferSampler(0);
    expect(sample(1024, 4096, 1000).bytesPerSecond).toBe(1024);
    sample(1024, 4096, 2000);
    sample(1024, 4096, 3000);
    expect(sample(1024, 4096, 4000).bytesPerSecond).toBe(0);
});
test("轮询保留服务端采样时间，不把旧速度当成实时速度", () => {
    const operation = startOperation("上传参考视频");
    operation.update({ phase: "transferring", loadedBytes: 512, totalBytes: 1024, bytesPerSecond: 256, updatedAt: 3000 });
    const entry = Object.values(useOperationProgress.getState().entries)[0];
    expect(entry.updatedAt).toBe(3000);
    expect(operationProgressView(entry, 7000).speed).toBe("");
    operation.dismiss();
});
test("字节到头仍然等待确认，失败不会被显示为完成", () => {
    const operation = startOperation("上传参考视频");
    operation.progress(100, 100);
    expect(Object.values(useOperationProgress.getState().entries)[0].phase).toBe("processing");
    operation.fail();
    expect(Object.values(useOperationProgress.getState().entries)[0].phase).toBe("error");
});
test("较早的响应晚到时不能重新显示已经结束的参考上传", () => {
    const owner = captureUserScope();
    const oldRequest = beginTaskReferenceObservation();
    const latestRequest = beginTaskReferenceObservation();
    const task = { id: "ordered-reference-task", status: "running", referenceProgress: { label: "参考视频 1", phase: "transferring", loadedBytes: 512, totalBytes: 1024, bytesPerSecond: 256, startedAt: 1000, updatedAt: 3000 } } as GenerationTask;
    publishTaskReferenceProgress(task, owner, oldRequest);
    publishTaskReferenceProgress({ ...task, referenceProgress: undefined }, owner, latestRequest);
    const before = Object.values(useOperationProgress.getState().entries);
    expect(before).toHaveLength(0);
    publishTaskReferenceProgress(task, owner, oldRequest);
    expect(Object.values(useOperationProgress.getState().entries)).toEqual(before);
});
test("参考上传在重试空档消失时不显示成功，明确任务成功才显示完成", () => {
    const owner = captureUserScope();
    const task = { id: "retry-reference-task", status: "running", referenceProgress: { label: "参考视频 1", phase: "processing", loadedBytes: 1024, totalBytes: 1024, bytesPerSecond: 0, startedAt: 1000, updatedAt: 3000 } } as GenerationTask;
    publishTaskReferenceProgress(task, owner);
    publishTaskReferenceProgress({ ...task, referenceProgress: undefined }, owner);
    expect(Object.values(useOperationProgress.getState().entries)).toHaveLength(0);
    publishTaskReferenceProgress(task, owner);
    publishTaskReferenceProgress({ ...task, status: "succeeded", referenceProgress: undefined }, owner);
    expect(Object.values(useOperationProgress.getState().entries).map(item => item.phase)).toEqual(["done"]);
});
test("切换账号清除旧进度，旧回调不能把它重新显示", () => {
    const operation = startOperation("私有素材", { owner: captureUserScope() });
    setActiveUserScope("another-preview-account");
    operation.progress(512, 1024);
    expect(Object.keys(useOperationProgress.getState().entries)).toHaveLength(0);
    operation.dismiss();
    setActiveUserScope(null);
});
