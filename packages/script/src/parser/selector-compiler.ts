import type { ScriptSelector } from "@atomyx/shared/script";
import type { Selector } from "@atomyx/driver/selectors";

/**
 * Compile a script-level selector into the core `Selector`
 * type that Orchestra understands. Script selectors are
 * intentionally simpler than MCP selectors (no regex) — this
 * conversion is trivial by design.
 */
export function compileScriptSelector(input: ScriptSelector): Selector {
  return {
    id: input.id,
    text: input.text,
    label: input.label,
    hint: input.hint,
    role: input.role,
    nth: input.nth,
  };
}

/**
 * Expand a YAML selector value into a canonical ScriptSelector.
 *
 * Two entry forms are accepted:
 *
 *   - Bare string (`- tap: "Login"`): treated as visible-content
 *     match. Expanded to `{ text, label }` so priority broadening
 *     matches both native (text) and Flutter (label) apps without
 *     the author knowing which platform exposes which.
 *   - Object (`- tap: { id: "..." }` etc.): passed through as-is
 *     with one exception — if the caller set `text` but not
 *     `label`, the `label` is mirrored from `text` for the same
 *     Flutter-parity reason. Explicit `label` wins; all other
 *     fields (`id`, `hint`, `role`, `nth`) are preserved verbatim.
 */
export function expandSelectorShorthand(
  value: unknown,
): ScriptSelector {
  if (typeof value === "string") {
    return { text: value, label: value };
  }
  if (typeof value === "object" && value !== null) {
    const obj = value as Record<string, unknown>;
    if (typeof obj["text"] === "string" && obj["label"] === undefined) {
      return { ...obj, label: obj["text"] } as ScriptSelector;
    }
    return obj as ScriptSelector;
  }
  throw new ScriptParseError(
    `Invalid selector: expected string or object, got ${typeof value}`,
  );
}

export class ScriptParseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ScriptParseError";
  }
}
