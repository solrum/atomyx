/**
 * High-level selector shape used by tools, CLI, and other feature
 * consumers. `Selector` is a plain data object — it describes WHAT
 * the caller is looking for, not HOW to find it. The compilation
 * into a filter chain (with priority broadening policy) lives in
 * `priority-broadening.ts`.
 *
 * Why a typed object instead of just an `ElementFilter`:
 *
 *   - Serializable. Agents pass selectors as JSON in tool calls,
 *     MCP / HTTP / WebSocket surfaces all deserialize into this
 *     shape without evaling functions.
 *
 *   - Tool-friendly. MCP tool schemas can describe `Selector`
 *     fields for LLM introspection ("here are the fields you can
 *     use to find elements"). A raw function has no schema.
 *
 *   - Persistable. Recorded test cases store selectors as JSON,
 *     replay compiles them back into filters at run time.
 *
 * Filter composition (low-level) and `Selector` (high-level) are
 * both public API — advanced callers drop to filter composition
 * when they need AND/OR/NOT or spatial constraints; default callers
 * pass `Selector` objects and let priority broadening pick the
 * right field.
 *
 * All fields are optional. At least one content-matching field
 * (`id`, `text`, `label`, `hint`) SHOULD be provided, otherwise
 * the selector matches every element in the tree.
 */
export interface Selector {
  /** Match canonical `id` attribute exactly or by pattern. */
  readonly id?: string | RegExp;
  /** Match canonical `text` attribute. */
  readonly text?: string | RegExp;
  /** Match canonical `label` (a11y) attribute. */
  readonly label?: string | RegExp;
  /** Match canonical `hint` attribute. */
  readonly hint?: string | RegExp;
  /** Match canonical `value` attribute. */
  readonly value?: string | RegExp;

  /** Constrain to normalized semantic role (e.g. "button"). */
  readonly role?: string;

  /** Constrain to enabled state. */
  readonly enabled?: boolean;
  /** Constrain to clickable state. */
  readonly clickable?: boolean;
  /** Constrain to focused state. */
  readonly focused?: boolean;

  /**
   * Post-broadening index selection. Priority broadening may
   * return multiple matches (e.g. two elements with the same
   * text); `nth` picks a specific one in document order. Zero-
   * based. Use `0` for "first match".
   */
  readonly nth?: number;
}

/**
 * Validate a selector has at least one content-matching field.
 * Returns null for empty/null-content selectors — consumer may
 * choose to reject, warn, or proceed based on policy.
 */
export function selectorHasContent(s: Selector): boolean {
  return (
    s.id !== undefined ||
    s.text !== undefined ||
    s.label !== undefined ||
    s.hint !== undefined ||
    s.value !== undefined
  );
}
