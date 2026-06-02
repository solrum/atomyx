import type {
  EffectiveAttributes,
  Theme,
  ThemeOverrides,
} from "../../../domain/features/theme/index.js";

export type { EffectiveAttributes, Theme, ThemeOverrides };

export interface ThemeListEntry {
  readonly id: string;
  readonly label: string;
  readonly source: "built-in" | "user" | "workspace";
}

export interface ThemeSnapshot {
  readonly available: readonly ThemeListEntry[];
  readonly library: ReadonlyMap<string, Theme>;
  readonly activeId: string | null;
  readonly overrides: ThemeOverrides;
  readonly effective: EffectiveAttributes;
  readonly issues: readonly string[];
}

export interface ThemeApi {
  getSnapshot(): ThemeSnapshot;
  subscribe(listener: () => void): () => void;
  reload(workspacePath?: string): Promise<void>;
  setActiveId(id: string): Promise<void>;
  setOverride(
    key: keyof EffectiveAttributes,
    bundle: EffectiveAttributes[keyof EffectiveAttributes] | undefined,
  ): Promise<void>;
  clearOverrides(): Promise<void>;
  /** Reveal the user-level themes folder in the platform file manager. */
  openThemesDir(): Promise<void>;
}
