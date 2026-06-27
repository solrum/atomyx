import darcula from "./theme-intellij-darcula.json";
import light from "./theme-intellij-light.json";
import teal from "./theme-atomyx-dark-teal.json";
import violet from "./theme-atomyx-dark-violet.json";

/**
 * Bundled themes, loaded as JSON at compile time so they ship
 * inside the app binary and do not need a filesystem round-trip
 * on startup.
 *
 * Order matters — the first entry is the fallback when no theme
 * is selected (first run).
 */
export const BUILT_IN_THEME_JSONS: readonly unknown[] = [
  darcula,
  light,
  teal,
  violet,
];

export const DEFAULT_BUILT_IN_THEME_ID = "atomyx-dark-teal";
