import type { AttributeKey } from "../../../domain/features/theme/index.js";

/**
 * Settings-dialog category layout. IntelliJ's
 * Preferences → Editor → Color Scheme tree — each leaf
 * corresponds to a group of attribute keys, shown together in the
 * right pane so the user can scan related effects at a glance.
 */
export interface SettingsCategory {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly keys: readonly AttributeKey[];
}

export const SETTINGS_CATEGORIES: readonly SettingsCategory[] = [
  {
    id: "diagnostic",
    label: "Diagnostic",
    description: "Error, warning, info, and hint decorations.",
    keys: [
      "DIAGNOSTIC_ERROR_FG",
      "DIAGNOSTIC_ERROR_BG",
      "DIAGNOSTIC_WARNING_FG",
      "DIAGNOSTIC_WARNING_BG",
      "DIAGNOSTIC_INFO_FG",
      "DIAGNOSTIC_HINT_FG",
    ],
  },
  {
    id: "inspector",
    label: "Inspector",
    description:
      "Auto-refresh polling for the device UI tree. Each tick re-captures the snapshot for the device the inspector is bound to.",
    keys: [],
  },
];
