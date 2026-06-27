import { createStore } from "zustand/vanilla";
import type {
  MirrorDock,
  MirrorWindowApi,
  MirrorWindowMode,
  MirrorWindowPosition,
  MirrorWindowSnapshot,
} from "./mirror-window.contract.js";

export function createZustandMirrorWindow(): MirrorWindowApi {
  const store = createStore<MirrorWindowSnapshot>(() => ({
    isOpen: false,
    mode: "compact",
    dock: "right",
    position: { x: 60, y: 60 },
    scrubStep: null,
  }));

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    toggle: () => store.setState({ isOpen: !store.getState().isOpen }),
    open: () => store.setState({ isOpen: true }),
    close: () => store.setState({ isOpen: false }),
    setMode: (mode: MirrorWindowMode) => store.setState({ mode }),
    setDock: (dock: MirrorDock) => store.setState({ dock }),
    setPosition: (position: MirrorWindowPosition) =>
      store.setState({ position, dock: "free" }),
    setScrubStep: (scrubStep) => store.setState({ scrubStep }),
  };
}
