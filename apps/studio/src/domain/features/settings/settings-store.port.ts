import type { StudioSettings } from "./settings.types.js";

/**
 * Load and persist user settings. The filesystem layout belongs
 * to the platform adapter, not the contract.
 */
export interface SettingsStore {
  load(): Promise<StudioSettings>;
  save(settings: StudioSettings): Promise<void>;
}
