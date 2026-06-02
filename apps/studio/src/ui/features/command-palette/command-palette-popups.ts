import { popupRegistry } from "../../shell/popup-registry.js";
import { POPUP_IDS } from "../../shell/popup-ids.js";
import { FindEverywhere } from "./find-everywhere.js";
import { RecentFilesPopup } from "./recent-files-popup.js";
import { FileSwitcher } from "./file-switcher.js";
import { FindInPath } from "./find-in-path.js";
import { KeymapHelp } from "./keymap-help.js";
import { BookmarksPopup } from "./bookmarks-popup.js";
import { RecentLocations } from "./recent-locations.js";

popupRegistry.register({ id: POPUP_IDS.findEverywhere, Component: FindEverywhere });
popupRegistry.register({ id: POPUP_IDS.recentFiles, Component: RecentFilesPopup });
popupRegistry.register({ id: POPUP_IDS.fileSwitcher, Component: FileSwitcher });
popupRegistry.register({ id: POPUP_IDS.findInPath, Component: FindInPath });
popupRegistry.register({ id: POPUP_IDS.keymap, Component: KeymapHelp });
popupRegistry.register({ id: POPUP_IDS.bookmarks, Component: BookmarksPopup });
popupRegistry.register({ id: POPUP_IDS.recentLocations, Component: RecentLocations });
