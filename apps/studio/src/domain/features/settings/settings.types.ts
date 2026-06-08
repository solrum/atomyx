import { DEFAULT_BUILT_IN_THEME_ID } from "../theme/index.js";
import type { AttributeBundle, AttributeKey } from "../theme/index.js";

/**
 * Persisted user settings. Stored as JSON at the app-data dir by
 * the `SettingsStore` platform adapter.
 *
 * `mcp` and `autoUpdate` carry disabled slots today — they exist
 * so a future build can flip the flag without changing the file
 * format, keeping older settings files forward-compatible.
 */

export type StartupBehavior = "reopenLast" | "showWelcome";

export interface StudioSettings {
  /**
   * Id of the active theme from the Atomyx design system. Matches
   * a `Theme.id` resolved through the `ThemeStore` (built-in or
   * user-authored).
   */
  readonly editorThemeId: string;
  /**
   * What Studio does on launch. `reopenLast` jumps straight into
   * the most recently opened workspace; `showWelcome` always
   * presents the Welcome screen with recent projects.
   */
  readonly startupBehavior: StartupBehavior;
  /**
   * Per-attribute overrides layered on top of the active theme.
   * Partial — only keys the user explicitly customized are stored.
   */
  readonly themeOverrides: Readonly<Partial<Record<AttributeKey, AttributeBundle>>>;
  /**
   * Whether Studio uses the bundled JetBrains Mono or the system's
   * default monospace font in the editor.
   */
  readonly useBundledFont: boolean;
  readonly artifactRetention: {
    readonly maxRuns: number;
    readonly maxSizeMB: number;
  };
  readonly mcp: {
    readonly mode: "embedded";
    readonly endpoint: string | null;
  };
  readonly autoUpdate: {
    readonly enabled: boolean;
    readonly channel: "stable" | "beta";
    readonly endpoint: string | null;
  };
  readonly artifactsLocation: "app-data" | "next-to-script";
  readonly fileFilter: {
    readonly extensions: readonly string[];
  };
  readonly stripTrailingWhitespaceOnSave: boolean;
  readonly autoSaveOnBlur: boolean;
  /**
   * UI tree inspector polling. When `enabled`, the inspector
   * re-captures the tree every `intervalMs` for the device whose
   * tree is currently shown. Calls into the device runner are
   * expensive (Android accessibility dump ~200-500ms; iOS XCUITest
   * dump ~500-1500ms), so `intervalMs` is clamped to ≥2000 by the
   * inspector store regardless of what is persisted here.
   */
  readonly inspectorAutoRefresh: {
    readonly enabled: boolean;
    readonly intervalMs: number;
  };
}

export const DEFAULT_SETTINGS: StudioSettings = {
  editorThemeId: DEFAULT_BUILT_IN_THEME_ID,
  startupBehavior: "reopenLast",
  themeOverrides: {},
  useBundledFont: true,
  artifactRetention: {
    maxRuns: 50,
    maxSizeMB: 500,
  },
  mcp: {
    mode: "embedded",
    endpoint: null,
  },
  autoUpdate: {
    enabled: false,
    channel: "stable",
    endpoint: null,
  },
  artifactsLocation: "app-data",
  fileFilter: {
    extensions: [".yml", ".yaml"],
  },
  stripTrailingWhitespaceOnSave: true,
  autoSaveOnBlur: false,
  inspectorAutoRefresh: {
    enabled: false,
    intervalMs: 5000,
  },
};
