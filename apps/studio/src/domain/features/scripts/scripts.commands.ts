/**
 * Built-in Atomyx command names, mirrored from
 * `@atomyx/script`'s DEFAULT_COMMANDS. Hardcoded here so the
 * UI bundle does not drag Orchestra runtime via transitive
 * imports. A new command requires adding its name to this list
 * in the same PR that registers it under `DEFAULT_COMMANDS`.
 */
export const SCRIPT_COMMAND_NAMES = [
  "launchApp",
  "tap",
  "type",
  "waitFor",
  "assertVisible",
  "assertNotVisible",
  "screenshot",
  "swipe",
  "pressKey",
  "back",
  "sleep",
  "capture",
  "assertApi",
  "extract",
  "handle",
  "branch",
  "runFlow",
  "pointer",
] as const;

export type ScriptCommandName = (typeof SCRIPT_COMMAND_NAMES)[number];

/**
 * Matches YAML map keys — two capture groups: leading indent +
 * key characters. Intentionally does not anchor on value type:
 * any `foo:` at the start of a line (after whitespace + optional
 * `-`) counts so command keys and property keys highlight the
 * same way.
 */
export function buildYamlKeyRegex(): RegExp {
  return /(^[ \t]*(?:-[ \t]+)?)([A-Za-z_][A-Za-z0-9_]*)(?=:)/gm;
}

/**
 * Matches bare-command sequence items: `- <name>` without a
 * trailing colon. The caller filters by `SCRIPT_COMMAND_NAMES`
 * so arbitrary identifiers do not get classed as commands.
 */
export function buildCommandRegex(): RegExp {
  return /(^[ \t]*-[ \t]+)([A-Za-z_][A-Za-z0-9_]*)(?![:A-Za-z0-9_])/gm;
}
