import { popupRegistry } from "../../shell/popup-registry.js";
import { POPUP_IDS } from "../../shell/popup-ids.js";
import { RunConfigsDialog } from "./run-configs-dialog.js";

popupRegistry.register({ id: POPUP_IDS.runConfigs, Component: RunConfigsDialog });
