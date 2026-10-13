import { useReducedMotion } from "motion/react";
import { Download, LoaderCircle, RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";

import { AppChangelogButton, APP_VERSION } from "@/components/layout/app-changelog-modal";
import { cn } from "@/lib/utils";
import {
    desktopUpdateActionLabel,
    desktopUpdateDetailLabel,
    desktopUpdateProgressLabel,
    desktopUpdateProgressPercent,
    formatDesktopVersionLabel,
    hasResumableDesktopUpdate,
    shouldShowDesktopUpdaterControls,
    userFacingDesktopUpdateError,
    type DesktopUpdateState,
} from "@/services/desktop-update";
import { useDesktopUpdate } from "@/hooks/use-desktop-update";

import "./workspace-sidebar-update.css";

type UpdateStatusLine = { title: string; detail: string; percent: number | null; tone: "active" | "paused" };

function statusLine(state: DesktopUpdateState, persistBusy: boolean, latest: string, percent: number | null): UpdateStatusLine | null {
    if (persistBusy) return { title: "正在保存工作区", detail: "保存完成后开始安装", percent: null, tone: "active" };
    switch (state.status) {
        case "downloading":
            return { title: state.totalBytes > 0 && state.downloadedBytes >= state.totalBytes ? "正在校验更新包" : latest ? `正在下载 ${latest}` : "正在下载更新", detail: desktopUpdateDetailLabel(state), percent: state.downloadedBytes > 0 ? percent : null, tone: "active" };
        case "installing":
            return { title: latest ? `正在安装 ${latest}` : "正在安装更新", detail: "完成后应用会自动重启", percent: null, tone: "active" };
        case "error":
            return hasResumableDesktopUpdate(state)
                ? { title: latest ? `继续下载 ${latest}` : "继续下载", detail: desktopUpdateDetailLabel(state), percent, tone: "paused" }
                : { title: latest ? `重试更新 ${latest}` : "重试更新", detail: desktopUpdateDetailLabel(state), percent: null, tone: "paused" };
        default:
            return null;
    }
}

export function WorkspaceSidebarUpdate({ collapsed }: { collapsed: boolean }) {
    const reducedMotion = useReducedMotion();
    const updater = useDesktopUpdate();
    const { state, persistBusy, actionBusy, runtime } = updater;
    const [saveWarningOpen, setSaveWarningOpen] = useState(false);
    useEffect(() => {
        if (updater.canForceInstall) setSaveWarningOpen(true);
    }, [updater.canForceInstall]);
    const installed = formatDesktopVersionLabel(state.currentVersion || APP_VERSION);
    const latest = formatDesktopVersionLabel(state.latestVersion);
    const showControls = shouldShowDesktopUpdaterControls(updater.snapshot);
    const busy = persistBusy || actionBusy || state.status === "checking" || state.status === "downloading" || state.status === "installing";
    const percent = desktopUpdateProgressPercent(state);
    const resumable = hasResumableDesktopUpdate(state);
    const enabled = runtime === "desktop" && state.status !== "disabled";
    const actionLabel = !enabled ? (state.error || "当前环境不支持自动更新") : persistBusy ? "正在保存" : state.status === "available" ? "下载并安装更新" : showControls ? desktopUpdateActionLabel(state.status) : state.status === "checking" ? "正在检查更新" : state.status === "error" ? "重试检查更新" : "检查更新";
    const errorText = state.status === "error" ? userFacingDesktopUpdateError(state.error) : "";
    const updateLabel = updater.canForceInstall ? "保存未完成，点击查看处理方式" : state.status === "error" ? `${errorText.replace(/[。！，]$/, "")}，${resumable ? "点击继续下载" : "点击重试"}` : latest ? `${actionLabel} ${latest}` : actionLabel;
    const line = showControls ? statusLine(state, persistBusy, latest, percent) : null;
    const amount = state.totalBytes > 0 ? desktopUpdateProgressLabel(state) : "";
    const tooltip = line ? [line.title, amount, line.detail].filter(Boolean).join("\n") : updateLabel;
    const showBar = ["downloading", "ready", "installing"].includes(state.status) || persistBusy || (line?.tone === "paused" && line.percent !== null);
    const handleUpdate = () => {
        if (updater.canForceInstall) {
            setSaveWarningOpen(true);
            return;
        }
        if (state.status === "error") return showControls ? updater.downloadAndInstall() : updater.retry();
        return showControls ? updater.downloadAndInstall() : updater.check();
    };

    return (
        <div className={cn("app-workspace-update", collapsed && "is-collapsed")} data-desktop-update-status={state.status} data-desktop-update-runtime={runtime}>
            <AppChangelogButton className="app-workspace-update-version" showIcon={false} showVersion version={installed} versionClassName="tabular-nums" ariaLabel={installed ? `当前版本 ${installed}，查看更新日志` : "查看更新日志"} />
                <div className="app-workspace-update-progress" role="status" aria-live="polite">
                    <span className="sr-only">{line && state.status !== "error" ? line.title : updateLabel}</span>
                    {showBar ? <UpdateProgressBar state={state} percent={percent} reducedMotion={Boolean(reducedMotion)} paused={state.status === "error"} /> : null}
                    <button type="button" className={cn("app-workspace-update-action", showControls && "has-update", busy && "is-busy")} disabled={!enabled || busy} onClick={() => void handleUpdate()} aria-label={updateLabel} title={tooltip}>
                        {busy ? <LoaderCircle className="size-4 animate-spin" strokeWidth={1.8} aria-hidden="true" /> : state.status === "error" ? <RotateCcw className="size-4" strokeWidth={1.8} aria-hidden="true" /> : <Download className="size-4" strokeWidth={1.8} aria-hidden="true" />}
                    </button>
                </div>
            <AppModal
                open={saveWarningOpen && updater.canForceInstall}
                title="保存未完成，是否仍然更新？"
                onCancel={() => setSaveWarningOpen(false)}
                width={480}
                footer={[
                    <Button key="cancel" onClick={() => setSaveWarningOpen(false)}>返回处理</Button>,
                    <Button key="retry" type="primary" onClick={() => { setSaveWarningOpen(false); void updater.install(); }}>重试保存</Button>,
                    <Button key="force" danger onClick={() => { setSaveWarningOpen(false); void updater.forceInstall(); }}>仍然更新</Button>,
                ]}
            >
                <p>{errorText}</p>
                <p className="mt-3">更新会关闭并重启应用，尚未保存的修改可能丢失。</p>
                <p className="mt-3">你可以先返回处理，或重试保存。</p>
            </AppModal>
        </div>
    );
}

function UpdateProgressBar({ state, percent, reducedMotion, paused }: { state: DesktopUpdateState; percent: number | null; reducedMotion: boolean; paused: boolean }) {
    return (
        <span
            className={cn("app-workspace-update-progress-bar", paused && "is-paused")}
            role="progressbar"
            aria-label="更新准备进度"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent ?? undefined}
            aria-valuetext={desktopUpdateProgressLabel(state)}
        >
            <span style={{ width: `${percent ?? (reducedMotion ? 100 : 32)}%` }} />
        </span>
    );
}
