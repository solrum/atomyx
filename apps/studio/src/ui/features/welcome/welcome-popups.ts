import { popupRegistry } from "../../shell/popup-registry.js";
import { POPUP_IDS } from "../../shell/popup-ids.js";
import { NewTestWizard } from "./welcome-new-test-wizard.js";

popupRegistry.register({ id: POPUP_IDS.wizard, Component: NewTestWizard });
