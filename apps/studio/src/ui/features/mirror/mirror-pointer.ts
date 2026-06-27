import type { PointerEvent } from "react";

import type { MirrorSessionStatus } from "../../../state/features/mirror/index.js";

export interface DevicePoint {
  readonly x: number;
  readonly y: number;
  readonly srcWidth: number;
  readonly srcHeight: number;
}

export function eventToDevicePoint(
  e: PointerEvent<HTMLCanvasElement>,
  session: MirrorSessionStatus,
): DevicePoint {
  const canvas = e.currentTarget;
  const rect = canvas.getBoundingClientRect();
  const vw = canvas.width || session.videoWidth || rect.width;
  const vh = canvas.height || session.videoHeight || rect.height;
  const scale = Math.min(rect.width / vw, rect.height / vh);
  const drawnW = vw * scale;
  const drawnH = vh * scale;
  const offsetX = (rect.width - drawnW) / 2;
  const offsetY = (rect.height - drawnH) / 2;
  const relX = (e.clientX - rect.left - offsetX) / drawnW;
  const relY = (e.clientY - rect.top - offsetY) / drawnH;
  const x = Math.max(0, Math.min(vw - 1, Math.round(relX * vw)));
  const y = Math.max(0, Math.min(vh - 1, Math.round(relY * vh)));
  return { x, y, srcWidth: vw, srcHeight: vh };
}

/**
 * Maps raw client coordinates (e.g. from a WebKit gesture event,
 * which is not a React PointerEvent) to a normalised device point in
 * [0..1], applying the same letterbox correction as
 * `eventToDevicePoint`. Used by the trackpad-pinch path where only
 * `clientX/clientY` are available.
 */
export function clientToDeviceRatio(
  canvas: HTMLCanvasElement,
  clientX: number,
  clientY: number,
  session: MirrorSessionStatus,
): { readonly xRatio: number; readonly yRatio: number } {
  const rect = canvas.getBoundingClientRect();
  const vw = canvas.width || session.videoWidth || rect.width;
  const vh = canvas.height || session.videoHeight || rect.height;
  const scale = Math.min(rect.width / vw, rect.height / vh);
  const drawnW = vw * scale;
  const drawnH = vh * scale;
  const offsetX = (rect.width - drawnW) / 2;
  const offsetY = (rect.height - drawnH) / 2;
  const relX = (clientX - rect.left - offsetX) / drawnW;
  const relY = (clientY - rect.top - offsetY) / drawnH;
  return {
    xRatio: Math.max(0, Math.min(1, relX)),
    yRatio: Math.max(0, Math.min(1, relY)),
  };
}
