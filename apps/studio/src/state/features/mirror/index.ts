import { useSyncExternalStore } from "react";

import type { ScreenMirror } from "../../../domain/features/mirror/mirror.port.js";
import { getFeature } from "../../core/registry.js";
import type {
  ClipRequest,
  MirrorApi,
  MirrorSessionStatus,
  MirrorSnapshot,
  MirrorTouchEvent,
  MirrorTouchSink,
} from "./mirror.contract.js";
import { createZustandMirror } from "./mirror.zustand.js";

export type {
  ClipRequest,
  MirrorApi,
  MirrorSessionStatus,
  MirrorSnapshot,
  MirrorTouchEvent,
  MirrorTouchSink,
};

export const MIRROR_KEY = "mirror";

export function createMirror(deps: {
  readonly port: ScreenMirror;
  readonly onInteraction?: () => void;
}): MirrorApi {
  return createZustandMirror(deps);
}

export function useMirror(): MirrorSnapshot {
  const api = getFeature<MirrorApi>(MIRROR_KEY);
  return useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
}
