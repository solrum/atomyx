import { popupRegistry } from "../../shell/popup-registry.js";
import { POPUP_IDS } from "../../shell/popup-ids.js";
import { SettingsDialog } from "./settings-dialog.js";

popupRegistry.register({ id: POPUP_IDS.settings, Component: SettingsDialog });
