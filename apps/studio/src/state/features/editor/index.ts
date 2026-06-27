import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  EditorApi,
  EditorGroup,
  EditorSnapshot,
  EditorTab,
} from "./editor.contract.js";
import {
  createZustandEditor,
  type EditorDeps,
} from "./editor.zustand.js";

export type { EditorApi, EditorGroup, EditorSnapshot, EditorTab };

export const EDITOR_KEY = "editor";

export function createEditor(deps: EditorDeps): EditorApi {
  return createZustandEditor(deps);
}

type EditorMethodNames = Exclude<
  keyof EditorApi,
  "getSnapshot" | "subscribe"
>;

export function useEditor(): EditorSnapshot & Pick<EditorApi, EditorMethodNames> {
  const api = getFeature<EditorApi>(EDITOR_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    openFile: api.openFile,
    closeFile: api.closeFile,
    closeOthers: api.closeOthers,
    closeToRight: api.closeToRight,
    closeAll: api.closeAll,
    activate: api.activate,
    updateContent: api.updateContent,
    saveActive: api.saveActive,
    saveAll: api.saveAll,
    reopenLastClosed: api.reopenLastClosed,
    renameTab: api.renameTab,
    nextTab: api.nextTab,
    previousTab: api.previousTab,
    togglePinned: api.togglePinned,
    reloadFromDisk: api.reloadFromDisk,
    splitRight: api.splitRight,
    closeGroup: api.closeGroup,
    focusGroup: api.focusGroup,
  };
}
