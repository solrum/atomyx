import type { MirrorSessionStatus } from "../../../state/features/mirror/index.js";

/**
 * Rendered placement of the video frame inside its canvas element.
 * `object-fit: contain` preserves aspect ratio, so the frame may be
 * letterboxed — `offsetX/Y` is that letterbox, `drawnW/H` is the
 * actual rendered pixel area. `scale` maps one device pixel to
 * `scale` screen pixels.
 */
export interface MirrorFrameLayout {
  readonly scale: number;
  readonly offsetX: number;
  readonly offsetY: number;
  readonly drawnW: number;
  readonly drawnH: number;
  readonly srcWidth: number;
  readonly srcHeight: number;
}

export function computeFrameLayout(
  canvas: HTMLCanvasElement,
  session: MirrorSessionStatus,
): MirrorFrameLayout {
  const rect = canvas.getBoundingClientRect();
  const srcWidth = canvas.width || session.videoWidth || rect.width;
  const srcHeight = canvas.height || session.videoHeight || rect.height;
  const scale = Math.min(rect.width / srcWidth, rect.height / srcHeight);
  const drawnW = srcWidth * scale;
  const drawnH = srcHeight * scale;
  const offsetX = (rect.width - drawnW) / 2;
  const offsetY = (rect.height - drawnH) / 2;
  return { scale, offsetX, offsetY, drawnW, drawnH, srcWidth, srcHeight };
}

export interface DeviceRect {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

/**
 * Dimensions of the coordinate space the driver reports `bounds`
 * in — e.g. Android `AccessibilityNodeInfo.bounds` is measured in
 * device-native pixels (1080×2340 on a stock Pixel), which does
 * NOT match the mirror video frame if scrcpy or simctl downscales
 * (480×1080 is common). Overlay callers must rescale by
 * `videoDim / extentDim` before mapping onto the canvas.
 */
export interface TreeExtent {
  readonly width: number;
  readonly height: number;
}

/**
 * Parse a driver-core `bounds` attribute of the form
 * `"left,top,right,bottom"` (integer pixels on Android, points on
 * iOS — both share the same coord space as the mirror frame). A
 * malformed or missing string returns `null` so callers can skip
 * drawing instead of raising.
 */
export function parseBoundsAttribute(
  raw: string | undefined,
): DeviceRect | null {
  if (!raw) return null;
  const parts = raw.split(",");
  if (parts.length !== 4) return null;
  const nums = parts.map((p) => Number(p.trim()));
  if (nums.some((n) => !Number.isFinite(n))) return null;
  const [l, t, r, b] = nums as [number, number, number, number];
  if (r <= l || b <= t) return null;
  return { x: l, y: t, w: r - l, h: b - t };
}

export interface ScreenRect {
  readonly left: number;
  readonly top: number;
  readonly width: number;
  readonly height: number;
}

/**
 * Map a device-space rectangle onto canvas-space screen pixels.
 *
 *   1. Normalise the rect from `extent` (the coord space the tree
 *      uses) into the video frame's `srcWidth × srcHeight`.
 *   2. Apply the layout's `scale` + letterbox offsets to land on
 *      canvas pixels.
 *
 * When `extent` is omitted or matches `srcWidth × srcHeight`, step
 * 1 is the identity — useful for tests and for adapters that
 * already emit bounds in video-frame coords. Returns `null` on a
 * degenerate layout (frame not rendered yet).
 */
export function deviceRectToScreenRect(
  rect: DeviceRect,
  layout: MirrorFrameLayout,
  extent?: TreeExtent,
): ScreenRect | null {
  if (layout.scale <= 0 || layout.drawnW <= 0 || layout.drawnH <= 0) return null;
  const xRatio =
    extent && extent.width > 0 ? layout.srcWidth / extent.width : 1;
  const yRatio =
    extent && extent.height > 0 ? layout.srcHeight / extent.height : 1;
  const xStep = xRatio * layout.scale;
  const yStep = yRatio * layout.scale;
  return {
    left: layout.offsetX + rect.x * xStep,
    top: layout.offsetY + rect.y * yStep,
    width: rect.w * xStep,
    height: rect.h * yStep,
  };
}
