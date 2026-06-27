import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  MirrorWindowApi,
  MirrorWindowSnapshot,
} from "./mirror-window.contract.js";
import { createZustandMirrorWindow } from "./mirror-window.zustand.js";

export type {
  MirrorWindowApi,
  MirrorWindowSnapshot,
  MirrorWindowMode,
  MirrorWindowPosition,
  MirrorDock,
} from "./mirror-window.contract.js";

export const MIRROR_WINDOW_KEY = "mirror-window";

export { createZustandMirrorWindow as createMirrorWindow };

export function useMirrorWindow(): MirrorWindowSnapshot &
  Pick<
    MirrorWindowApi,
    "toggle" | "open" | "close" | "setMode" | "setDock" | "setPosition" | "setScrubStep"
  > {
  const api = getFeature<MirrorWindowApi>(MIRROR_WINDOW_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    toggle: api.toggle,
    open: api.open,
    close: api.close,
    setMode: api.setMode,
    setDock: api.setDock,
    setPosition: api.setPosition,
    setScrubStep: api.setScrubStep,
  };
}
