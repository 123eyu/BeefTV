type PrepareEditor = () => void | Promise<void>;
import { getActiveUserScope } from "@/lib/user-scope";
import { canvasSyncProgressKey, type SyncProjectProgress } from "@/stores/use-sync-progress-store";

export class DesktopUpdateSaveInProgressError extends Error {
    constructor() {
        super("仍有内容正在写入，请等待保存结束后再更新。");
    }
}

// Progress includes historical read/import errors, deleted projects and other
// accounts. Those records are not evidence of an ongoing write.
export function assertDesktopUpdateProgressSettled(
    records: Record<string, SyncProjectProgress>,
    projectIds: ReadonlySet<string>,
    scope = getActiveUserScope(),
    allowFailed = false,
) {
    const progress = Object.entries(records).filter(([key, item]) => key === canvasSyncProgressKey(item.projectId, scope)).map(([, item]) => item);
    if (progress.some(item => ["pending", "uploading", "saving"].includes(item.phase))) throw new DesktopUpdateSaveInProgressError();
    if (!allowFailed && progress.some(item => projectIds.has(item.projectId) && (item.phase === "error" || item.phase === "conflict"))) {
        throw new Error("有画布保存失败或存在内容冲突，请处理后重试，或选择仍然更新。");
    }
}

// Mounted editors own their pending writes; the updater must await them before
// asking the native host to exit. Unmounting removes the old workspace callback.
const editors = new Set<PrepareEditor>();

export function registerDesktopUpdatePreparation(prepare: PrepareEditor) {
    editors.add(prepare);
    return () => {
        editors.delete(prepare);
    };
}

export async function prepareDesktopEditorsForUpdate() {
    for (const prepare of [...editors]) await prepare();
}
