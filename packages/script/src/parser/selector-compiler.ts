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
 * Expand a bare string into a ScriptSelector. In YAML,
 * `- tap: "Login"` matches against both text AND label.
 *
 * Why both: Flutter apps expose visible text as `label` in the
 * accessibility tree, not `text`. Native Android/iOS use `text`.
 * Setting both lets priority broadening try label first (higher
 * priority) then text — cross-platform without user knowing.
 */
export function expandSelectorShorthand(
  value: unknown,
): ScriptSelector {
  if (typeof value === "string") {
    return { text: value, label: value };
  }
  if (typeof value === "object" && value !== null) {
    return value as ScriptSelector;
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
