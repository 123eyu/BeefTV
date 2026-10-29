import { useCallback, type MouseEvent as ReactMouseEvent } from "react";

import { canvasPageUiStore } from "@/stores/canvas/use-canvas-page-ui-store";
import type { Position } from "@/types/canvas";

function isCanvasTextEditingTarget(target: EventTarget | null) {
    return target instanceof Element && Boolean(target.closest("input, textarea, select, [contenteditable]:not([contenteditable='false'])"));
}

type UseCanvasContextMenuOptions = {
    projectId: string;
    selectedNodeIdsRef: { current: Set<string> };
    screenToCanvas: (clientX: number, clientY: number) => Position;
    closeConnectionCreateMenu: () => void;
};

/** 画布、节点和连线的右键菜单。选中和菜单都写当前画布的 store。 */
export function useCanvasContextMenu({
    projectId,
    selectedNodeIdsRef,
    screenToCanvas,
    closeConnectionCreateMenu,
}: UseCanvasContextMenuOptions) {
    const pageUi = () => canvasPageUiStore(projectId).getState();

    const handleCanvasContextMenu = useCallback(
        (event: ReactMouseEvent) => {
            const target = event.target instanceof Element ? event.target : null;
            if (target?.closest("[data-node-id],[data-connection-id]")) return;

            event.preventDefault();
            event.stopPropagation();
            if (target?.closest("[data-canvas-no-zoom],.ant-modal,.ant-popover,.ant-dropdown")) {
                pageUi().setContextMenu(null);
                return;
            }

            closeConnectionCreateMenu();
            pageUi().setContextMenu({ type: "canvas", x: event.clientX, y: event.clientY, position: screenToCanvas(event.clientX, event.clientY) });
        },
        [closeConnectionCreateMenu, projectId, screenToCanvas],
    );

    const handleNodeContextMenu = useCallback(
        (event: ReactMouseEvent, id: string) => {
            if (isCanvasTextEditingTarget(event.target)) return;
            event.preventDefault();
            event.stopPropagation();
            const { setSelectedNodeIds, setSelectedConnectionId, setToolbarNodeId, setDialogNodeId, setContextMenu } = pageUi();
            setSelectedNodeIds((current) => {
                if (current.has(id) && current.size > 1) return current;
                return new Set([id]);
            });
            setSelectedConnectionId(null);
            closeConnectionCreateMenu();
            setToolbarNodeId(null);
            setDialogNodeId(null);
            setContextMenu({ type: "node", x: event.clientX, y: event.clientY, nodeId: id });
        },
        [closeConnectionCreateMenu, projectId],
    );

    const handleConnectionSelect = useCallback(
        (connectionId: string) => {
            const { setSelectedConnectionId, setSelectedNodeIds, setContextMenu } = pageUi();
            setSelectedConnectionId(connectionId);
            selectedNodeIdsRef.current = new Set();
            setSelectedNodeIds(new Set());
            setContextMenu(null);
        },
        [projectId, selectedNodeIdsRef],
    );

    const handleConnectionContextMenu = useCallback(
        (event: ReactMouseEvent<SVGPathElement>, connectionId: string) => {
            const { setSelectedConnectionId, setSelectedNodeIds, setContextMenu } = pageUi();
            setSelectedConnectionId(connectionId);
            selectedNodeIdsRef.current = new Set();
            setSelectedNodeIds(new Set());
            closeConnectionCreateMenu();
            setContextMenu({ type: "connection", x: event.clientX, y: event.clientY, connectionId });
        },
        [closeConnectionCreateMenu, projectId, selectedNodeIdsRef],
    );

    return {
        handleCanvasContextMenu,
        handleNodeContextMenu,
        handleConnectionSelect,
        handleConnectionContextMenu,
    };
}
