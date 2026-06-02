import { registerActionHandler } from "../../state/features/actions/index.js";
import { getFeature } from "../../state/core/registry.js";
import type { NavHistoryApi } from "../../state/features/nav-history/index.js";
import { NAV_HISTORY_KEY } from "../../state/features/nav-history/index.js";
import type { EditorApi } from "../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../state/features/editor/index.js";
import type { BookmarksApi } from "../../state/features/bookmarks/index.js";
import { BOOKMARKS_KEY } from "../../state/features/bookmarks/index.js";
import type { PopupsApi } from "../../state/features/popups/index.js";
import { POPUPS_KEY } from "../../state/features/popups/index.js";
import type { LayoutApi } from "../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../state/features/layout/index.js";
import {
  triggerFindInFile,
  triggerGoToLine,
  triggerFormatDocument,
  jumpActiveEditorTo,
  getActiveCursor,
} from "../features/editor/index.js";
import { POPUP_IDS } from "./popup-ids.js";

/**
 * Registers every app-shell-owned action handler with the actions
 * feature. Returns a dispose that tears them down together —
 * intended to be wired from a single useEffect in app-shell.tsx.
 */
export function installAppShellActions(): () => void {
  const handlers = [
    registerActionHandler("workbench.action.openSettings", () =>
      getFeature<LayoutApi>(LAYOUT_KEY).toggleSettingsView(),
    ),
    registerActionHandler("workbench.action.findEverywhere", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.findEverywhere),
    ),
    registerActionHandler("file.openRecent", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.recentFiles),
    ),
    registerActionHandler("editor.findInFile", () => triggerFindInFile()),
    registerActionHandler("editor.findInPath", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.findInPath),
    ),
    registerActionHandler("run.editConfigurations", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.runConfigs),
    ),
    registerActionHandler("editor.gotoLine", () => triggerGoToLine()),
    registerActionHandler("editor.formatDocument", () => triggerFormatDocument()),
    registerActionHandler("navigate.recentLocations", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.recentLocations),
    ),
    registerActionHandler("navigate.back", () => {
      const store = getFeature<NavHistoryApi>(NAV_HISTORY_KEY);
      const loc = store.back();
      if (!loc) return;
      store.beginNavigation();
      void getFeature<EditorApi>(EDITOR_KEY)
        .openFile(loc.path)
        .then(() => {
          requestAnimationFrame(() => {
            jumpActiveEditorTo(loc.line, loc.column);
            store.endNavigation();
          });
        })
        .catch(() => store.endNavigation());
    }),
    registerActionHandler("bookmark.toggle", () => {
      const path = getFeature<EditorApi>(EDITOR_KEY).getSnapshot().activePath;
      const cursor = getActiveCursor();
      if (!path || !cursor) return;
      getFeature<BookmarksApi>(BOOKMARKS_KEY).toggle(path, cursor.line);
    }),
    registerActionHandler("bookmark.show", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.bookmarks),
    ),
    registerActionHandler("help.keymap", () => getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.keymap)),
    registerActionHandler("file.goToFile", () =>
      getFeature<PopupsApi>(POPUPS_KEY).open(POPUP_IDS.fileSwitcher),
    ),
    registerActionHandler("navigate.forward", () => {
      const store = getFeature<NavHistoryApi>(NAV_HISTORY_KEY);
      const loc = store.forward();
      if (!loc) return;
      store.beginNavigation();
      void getFeature<EditorApi>(EDITOR_KEY)
        .openFile(loc.path)
        .then(() => {
          requestAnimationFrame(() => {
            jumpActiveEditorTo(loc.line, loc.column);
            store.endNavigation();
          });
        })
        .catch(() => store.endNavigation());
    }),
  ];
  return () => {
    for (const dispose of handlers) dispose();
  };
}
