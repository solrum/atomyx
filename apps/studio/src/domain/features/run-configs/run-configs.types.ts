/**
 * One saved way to run the workspace. Users pick a config from
 * the toolbar dropdown, hit Play, and it maps directly onto the
 * runtime `runScript` call without asking for device / app /
 * script again.
 *
 * Stored per-workspace in `.atomyx/run-configs.json` so a team
 * can commit shared configs alongside the scripts they target.
 * The active id lives in the per-developer `workspace.json` so
 * two developers can pick different configs on the same project.
 */
export interface RunConfig {
  readonly id: string;
  readonly name: string;
  readonly deviceId: string | null;
  readonly appId: string | null;
  /** Relative to the workspace root. `null` = use the active editor tab. */
  readonly scriptPath: string | null;
  readonly env: Readonly<Record<string, string>>;
}

export const RUN_CONFIGS_SCHEMA_VERSION = 1;

export interface RunConfigsFile {
  readonly schemaVersion: number;
  readonly configs: readonly RunConfig[];
}

export const EMPTY_RUN_CONFIGS_FILE: RunConfigsFile = {
  schemaVersion: RUN_CONFIGS_SCHEMA_VERSION,
  configs: [],
};

export function makeConfigId(name: string): string {
  const slug = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  const suffix = Math.random().toString(36).slice(2, 6);
  return `${slug || "config"}-${suffix}`;
}
