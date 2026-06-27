import { invoke } from "@tauri-apps/api/core";
import type { SettingsStore } from "../../../domain/features/settings/index.js";
import { DEFAULT_SETTINGS, type StudioSettings } from "../../../domain/features/settings/index.js";

/**
 * Filesystem-backed settings store. Delegates serialization + disk
 * I/O to the Rust backend so defaults and migrations stay in one
 * place.
 */
export class FsSettingsStore implements SettingsStore {
  async load(): Promise<StudioSettings> {
    const loaded = await invoke<StudioSettings | null>("settings_load");
    if (!loaded) return DEFAULT_SETTINGS;
    return { ...DEFAULT_SETTINGS, ...loaded };
  }

  async save(settings: StudioSettings): Promise<void> {
    await invoke("settings_save", { settings });
  }
}
