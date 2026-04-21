/**
 * Script-level selector — the shape that YAML test scripts use
 * to identify elements. Simpler than the full MCP `Selector`
 * (no regex, no value/hint in V1) because script syntax
 * prioritizes readability over power.
 *
 * Bare strings in YAML become `{ text: "..." }`.
 * Object form supports all fields below.
 */
export interface ScriptSelector {
  /** Match element by visible text. */
  readonly text?: string;
  /** Match element by stable resource id / accessibility id. */
  readonly id?: string;
  /** Match element by accessibility label. */
  readonly label?: string;
  /** Match element by input hint / placeholder. */
  readonly hint?: string;
  /** Constrain to semantic role (e.g. "button"). */
  readonly role?: string;
  /** Pick the nth match (0-indexed) when multiple match. */
  readonly nth?: number;
}
