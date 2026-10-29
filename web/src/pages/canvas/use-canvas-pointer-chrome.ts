import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from "react";

import type { CanvasNodeData } from "@/types/canvas";

import { canvasNodeDragEndChrome, canvasNodeInteractionStartChrome } from "./canvas-pointer-chrome";
import { canvasPageUiStore } from "@/stores/canvas/use-canvas-page-ui-store";

type UseCanvasPointerSelectionChromeOptions = {
    projectId: string;
    nodesRef: { current: CanvasNodeData[] };
    segmentRunningMode?: string | null;
};

export function useCanvasPointerSelectionChrome({
    projectId,
    nodesRef,
    segmentRunningMode,
}: UseCanvasPointerSelectionChromeOptions) {
    const handleCanvasSelectionStart = useCallback(() => {
        canvasPageUiStore(projectId).getState().setContextMenu(null);
    }, [projectId]);

    const handleNodeInteractionStart = useCallback((selectionModifier: boolean) => {
        canvasPageUiStore(projectId).getState().setContextMenu(null);
        canvasPageUiStore(projectId).getState().setHoveredNodeId(null);
        // Keep the source node's extraction state visible while FFmpeg is
        // producing the independent video/audio outputs.
        const chrome = canvasNodeInteractionStartChrome({
            segmentRunningAudio: segmentRunningMode === "audio",
            selectionModifier,
        });
        if (chrome.ignore) return;
        canvasPageUiStore(projectId).getState().setToolbarNodeId(null);
        if (chrome.closeDialog) canvasPageUiStore(projectId).getState().setDialogNodeId(null);
    }, [projectId, segmentRunningMode]);

    const handleNodeDragEnd = useCallback(
        (nodeId: string) => {
            const chrome = canvasNodeDragEndChrome(nodesRef.current.find((item) => item.id === nodeId));
            const { setDialogNodeId, setToolbarNodeId } = canvasPageUiStore(projectId).getState();
            setDialogNodeId(chrome.dialogNodeId);
            if (chrome.toolbarNodeId) setToolbarNodeId(chrome.toolbarNodeId);
        },
        [nodesRef, projectId],
    );

    const handleCanvasDeselect = useCallback(() => {
        canvasPageUiStore(projectId).getState().setContextMenu(null);
        canvasPageUiStore(projectId).getState().setHoveredNodeId(null);
        canvasPageUiStore(projectId).getState().setToolbarNodeId(null);
        canvasPageUiStore(projectId).getState().setDialogNodeId(null);
    }, [projectId]);

    return {
        handleCanvasSelectionStart,
        handleNodeInteractionStart,
        handleNodeDragEnd,
        handleCanvasDeselect,
    };
}

type UseCanvasNodeToolbarHoverOptions = {
    projectId: string;
    nodeDraggingRef: { current: boolean };
    nodeImageSettingsOpen: boolean;
};

export function useCanvasNodeToolbarHover({
    projectId,
    nodeDraggingRef,
    nodeImageSettingsOpen,
}: UseCanvasNodeToolbarHoverOptions) {
    const toolbarHideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const keepNodeToolbar = useCallback(
        (nodeId: string) => {
            if (nodeDraggingRef.current || nodeImageSettingsOpen) return;
            if (toolbarHideTimerRef.current) {
                clearTimeout(toolbarHideTimerRef.current);
                toolbarHideTimerRef.current = null;
            }
            canvasPageUiStore(projectId).getState().setToolbarNodeId(nodeId);
        },
        [nodeDraggingRef, nodeImageSettingsOpen, projectId],
    );

    const hideNodeToolbar = useCallback(() => {
        if (toolbarHideTimerRef.current) clearTimeout(toolbarHideTimerRef.current);
        toolbarHideTimerRef.current = setTimeout(() => {
            canvasPageUiStore(projectId).getState().setToolbarNodeId(null);
            toolbarHideTimerRef.current = null;
        }, 120);
    }, [projectId]);

    useEffect(() => () => {
        if (toolbarHideTimerRef.current) clearTimeout(toolbarHideTimerRef.current);
    }, []);

    const handleCanvasNodeHoverStart = useCallback(
        (nodeId: string) => {
            if (nodeDraggingRef.current) return;
            canvasPageUiStore(projectId).getState().setHoveredNodeId(nodeId);
            keepNodeToolbar(nodeId);
        },
        [keepNodeToolbar, nodeDraggingRef],
    );

    const handleCanvasNodeHoverEnd = useCallback(
        (nodeId: string) => {
            canvasPageUiStore(projectId).getState().setHoveredNodeId((current) => (current === nodeId ? null : current));
            hideNodeToolbar();
        },
        [hideNodeToolbar, projectId],
    );

    return {
        keepNodeToolbar,
        hideNodeToolbar,
        handleCanvasNodeHoverStart,
        handleCanvasNodeHoverEnd,
    };
}
