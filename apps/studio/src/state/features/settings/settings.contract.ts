import type { StudioSettings } from "../../../domain/features/settings/index.js";

export type { StudioSettings };

export interface SettingsSnapshot {
  readonly settings: StudioSettings;
  readonly loaded: boolean;
}

export interface SettingsApi {
  getSnapshot(): SettingsSnapshot;
  subscribe(listener: () => void): () => void;
  load(): Promise<void>;
  update(patch: Partial<StudioSettings>): Promise<void>;
}
