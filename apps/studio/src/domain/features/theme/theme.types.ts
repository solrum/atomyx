/**
 * Design-system types — IntelliJ Color Scheme model adapted to
 * Studio.
 *
 * The public contract is:
 *   - An `AttributeKey` is a `SCREAMING_SNAKE_CASE` identifier
 *     from the frozen Phase-1 catalogue (40 keys).
 *   - An `AttributeBundle` is IntelliJ's Attribute node collapsed
 *     into JSON: `{ foreground?, background?, fontStyle? }`.
 *   - A `Theme` is a partial map from keys to bundles plus
 *     inheritance metadata.
 *   - An `EffectiveAttributes` map is what the app actually renders
 *     — the parent chain merged, overrides applied, defaults
 *     filled in. Always complete: every key has a resolved
 *     foreground (possibly from defaults).
 */

export const THEME_SCHEMA_VERSION = 1;

export const MONACO_BASE_THEMES = [
  "vs",
  "vs-dark",
  "hc-black",
  "hc-light",
] as const;
export type MonacoBaseTheme = (typeof MONACO_BASE_THEMES)[number];

export const ATTRIBUTE_KEYS = [
  // Editor chrome (7)
  "EDITOR_BACKGROUND",
  "EDITOR_FOREGROUND",
  "EDITOR_LINE_HIGHLIGHT_BG",
  "EDITOR_SELECTION_BG",
  "EDITOR_CARET",
  "EDITOR_GUTTER_BG",
  "EDITOR_GUTTER_FG",
  // Atomyx syntax (5)
  "ATOMYX_KEYWORD",
  "ATOMYX_COMMAND_BARE",
  "ATOMYX_SELECTOR",
  "ATOMYX_VARIABLE",
  "ATOMYX_VARIABLE_UNDEFINED",
  // YAML syntax (5)
  "SYNTAX_STRING",
  "SYNTAX_NUMBER",
  "SYNTAX_BOOLEAN",
  "SYNTAX_NULL",
  "SYNTAX_COMMENT",
  // Diagnostic (6)
  "DIAGNOSTIC_ERROR_FG",
  "DIAGNOSTIC_ERROR_BG",
  "DIAGNOSTIC_WARNING_FG",
  "DIAGNOSTIC_WARNING_BG",
  "DIAGNOSTIC_INFO_FG",
  "DIAGNOSTIC_HINT_FG",
  // Run state (6)
  "RUN_STEP_PENDING",
  "RUN_STEP_RUNNING",
  "RUN_STEP_PASS",
  "RUN_STEP_FAIL",
  "RUN_STEP_SKIP",
  "RUN_STEP_CURRENT_BG",
] as const;

export type AttributeKey = (typeof ATTRIBUTE_KEYS)[number];

export const ATTRIBUTE_KEY_SET: ReadonlySet<string> = new Set(ATTRIBUTE_KEYS);

export const FONT_STYLES = [
  "normal",
  "bold",
  "italic",
  "bold-italic",
] as const;
export type FontStyle = (typeof FONT_STYLES)[number];

/**
 * One attribute's effect bundle. Every field is optional — a
 * theme declares only the effects it wants to override relative
 * to its parent. `apply-tokens` falls back through the inheritance
 * chain for any unset field.
 */
export interface AttributeBundle {
  readonly foreground?: string;
  readonly background?: string;
  readonly fontStyle?: FontStyle;
}

/**
 * One theme, as authored. `attributes` is partial — only the
 * keys this theme declares. The resolver merges it against its
 * parent chain to produce an `EffectiveAttributes` map.
 */
export interface Theme {
  readonly schemaVersion: number;
  readonly id: string;
  readonly label: string;
  readonly extends?: string;
  readonly monacoBase: MonacoBaseTheme;
  readonly attributes: Readonly<Partial<Record<AttributeKey, AttributeBundle>>>;
}

export type ThemeOverrides = Readonly<
  Partial<Record<AttributeKey, AttributeBundle>>
>;

/**
 * Fully-resolved attribute map. Every key present, every bundle
 * guaranteed to have at least a `foreground` (either declared or
 * taken from `DEFAULT_ATTRIBUTES`). The UI consumes this shape
 * directly.
 */
export type EffectiveAttributes = Readonly<
  Record<AttributeKey, Required<Pick<AttributeBundle, "foreground">> & AttributeBundle>
>;

export interface ResolvedTheme {
  readonly theme: Theme;
  readonly chain: readonly Theme[];
  readonly attributes: EffectiveAttributes;
}
