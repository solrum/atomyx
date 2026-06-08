import { getFeature } from "../../../state/core/registry.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import type { SettingsApi } from "../../../state/features/settings/index.js";
import { SETTINGS_KEY } from "../../../state/features/settings/index.js";

let installed = false;

export function installAutoSaveOnBlur(): void {
  if (installed) return;
  installed = true;
  window.addEventListener("blur", () => {
    if (!getFeature<SettingsApi>(SETTINGS_KEY).getSnapshot().settings.autoSaveOnBlur) return;
    const anyDirty = getFeature<EditorApi>(EDITOR_KEY).getSnapshot().tabs.some((t) => t.dirty);
    if (!anyDirty) return;
    void getFeature<EditorApi>(EDITOR_KEY).saveAll();
  });
}
