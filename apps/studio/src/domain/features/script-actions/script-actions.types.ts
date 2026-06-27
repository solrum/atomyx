import type { UiTreeNode } from "../runtime/index.js";

export type { UiTreeNode };

/**
 * Kind of selector field a node can produce a YAML script step
 * from. Priority order matches `@atomyx/driver`'s `compileSelector`
 * broadening: `id` > `label` > `text` > `hint`. `coords` is the
 * last-resort fallback when no stable attribute exists.
 */
export type SelectorKind = "id" | "label" | "text" | "hint" | "role-nth";

export interface SelectorCandidate {
  readonly kind: SelectorKind;
  readonly display: string;
  /**
   * Shortest valid selector form for this candidate — a quoted
   * scalar for `text`, an inline flow mapping for `id` / `label` /
   * `hint`. Commands use this as the right-hand side of the step
   * so the generated YAML stays on one or two lines.
   */
  readonly toYamlInline: () => string;
  /**
   * Selector rendered as YAML block-mapping lines — `["role:
   * \"text-field\"", "nth: 0"]`. Each entry is one `key: value`
   * line with no leading spaces; callers prepend indentation to
   * place the selector inside a block step. Needed for multi-
   * field selectors (`role-nth`) so actions with more than one
   * field (e.g. `type`) don't produce flow-in-flow mappings.
   */
  readonly toYamlBlockLines: () => readonly string[];
}

/**
 * Marker placeholder in a generated YAML step that callers can jump
 * to post-insert so the user types over it immediately. The exact
 * token is opaque — consumers receive `{ yaml, placeholders }`
 * from `buildYaml` rather than searching the string.
 */
export interface PlaceholderRange {
  readonly offset: number;
  readonly length: number;
}

export interface BuiltYaml {
  readonly yaml: string;
  readonly placeholders: readonly PlaceholderRange[];
}

/**
 * Action the context menu offers for a given node. `buildYaml`
 * returns a ready-to-insert YAML snippet plus placeholder ranges
 * (consumers use them to position the cursor on the first value
 * the user typically overrides — `"TODO"` for `type`).
 *
 * `appliesTo(node)` lets actions opt out for irrelevant nodes
 * (e.g. `type` hides on a Button) so the menu only offers what
 * the node can reasonably do.
 */
export interface ScriptAction {
  readonly id: string;
  readonly label: string;
  readonly requiresSelector: true;
  readonly appliesTo: (node: UiTreeNode) => boolean;
  readonly buildYaml: (node: UiTreeNode, selector: SelectorCandidate) => BuiltYaml;
}
