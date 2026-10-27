import type { ViewportTransform } from "@/types/canvas";

export const CANVAS_VIRTUALIZATION_REFRESH_INTERVAL_MS = 64;
/** Screen-space pre-render margin while a pan or zoom gesture is in flight. */
export const CANVAS_GESTURE_RENDER_PADDING_PX = 320;
/** Nodes mounted earlier in a gesture stay mounted until it ends, up to this many. */
export const CANVAS_GESTURE_RETAIN_LIMIT = 240;
/** Base enter padding in screen pixels (see canvasNodeRenderPadding). */
const CANVAS_MIN_ENTER_PADDING_PX = 128;
/** Movement budget for one refresh interval inside the pre-rendered margin. */
const CANVAS_REFRESH_SAFETY_PX = 128;

type ViewportSize = { width: number; height: number };

/**
 * A page-level refresh mounts nodes for a new camera window. It is only needed
 * once the live view gets close to the edge of what the last refresh already
 * mounted; until then the compositor transform alone moves the world.
 *
 * `renderedDuringGesture` says whether `rendered` was refreshed with the larger
 * gesture padding, or is still the committed camera with the base padding.
 */
export function shouldRefreshCanvasVirtualization(rendered: ViewportTransform, next: ViewportTransform, lastRefreshAt: number, now: number, size?: ViewportSize, renderedDuringGesture = false) {
    if (rendered.x === next.x && rendered.y === next.y && rendered.k === next.k) return false;
    if (now - lastRefreshAt < CANVAS_VIRTUALIZATION_REFRESH_INTERVAL_MS) return false;
    if (!size) return true;
    const padding = renderedDuringGesture ? CANVAS_GESTURE_RENDER_PADDING_PX : CANVAS_MIN_ENTER_PADDING_PX;
    const margin = Math.max(0, padding - CANVAS_REFRESH_SAFETY_PX) / rendered.k;
    const coveredLeft = -rendered.x / rendered.k - margin;
    const coveredTop = -rendered.y / rendered.k - margin;
    const coveredRight = (size.width - rendered.x) / rendered.k + margin;
    const coveredBottom = (size.height - rendered.y) / rendered.k + margin;
    const liveLeft = -next.x / next.k;
    const liveTop = -next.y / next.k;
    const liveRight = (size.width - next.x) / next.k;
    const liveBottom = (size.height - next.y) / next.k;
    return liveLeft < coveredLeft || liveTop < coveredTop || liveRight > coveredRight || liveBottom > coveredBottom;
}

