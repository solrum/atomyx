import type { SettingsStore } from "./settings-store.port.js";
import { DEFAULT_SETTINGS, type StudioSettings } from "./settings.types.js";

/**
 * In-memory settings store for UI/state tests.
 */
export class MockSettingsStore implements SettingsStore {
  private current: StudioSettings;

  constructor(initial: StudioSettings = DEFAULT_SETTINGS) {
    this.current = initial;
  }

  async load(): Promise<StudioSettings> {
    return this.current;
  }

  async save(settings: StudioSettings): Promise<void> {
    this.current = settings;
  }
}
