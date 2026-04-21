/**
 * Result of a high-level action dispatched through the Orchestra
 * command layer. Every mutating Orchestra method returns this shape
 * so feature consumers (MCP tools, Studio replay, Synapse reporter)
 * can surface a consistent outcome without branching on method name.
 *
 * The `ok:false` path carries a `reason` string that is **safe to
 * show to a downstream AI agent**: it should describe WHAT failed
 * and SUGGEST a recovery if one is obvious. Example reasons:
 *
 *   - "element not found for selector {text: 'Login'} after scroll
 *     search (6 up + 6 down swipes)"
 *   - "element is visually obscured by [role=dialog id=confirm-sheet
 *     label='Confirm']. Dismiss the obscuring element or find_element
 *     on the obscurer to tap it directly."
 *   - "element has no bounds; cannot compute tap coordinate"
 *
 * `reason` is NOT a machine-readable error code — consumers wanting
 * programmatic error handling should catch the typed errors thrown
 * by Finder (`FindTimeoutError`) or ScrollController
 * (`ScrollUnreachableError`) directly instead of inspecting `reason`.
 * The Orchestra only converts those into `ActionResult` at the
 * public action boundary.
 */
export type ActionResult =
  | {
      readonly ok: true;
      /**
       * Which selector priority slot actually matched — "id", "text",
       * "label", "hint", or "value" when priority broadening chose a
       * field. `undefined` when the action was purely coordinate-
       * based (e.g. `Orchestra.tapAt`) or the selector had no
       * content fields.
       */
      readonly resolvedBy?: string;
      /**
       * Freeform success detail. Rarely consumed programmatically,
       * useful in logs and tool results to show "what actually
       * happened" beyond the ok flag — "scrolled 3 times then
       * tapped", "typed 12 chars".
       */
      readonly detail?: string;
    }
  | {
      readonly ok: false;
      /** Human-readable failure explanation. Safe to show to an agent. */
      readonly reason: string;
      /**
       * Optional structured obscurer info when the failure was
       * caused by a detected overlay. Consumers with UI (Studio,
       * Synapse) can render the obscurer or offer "tap obscurer
       * instead" as a recovery action.
       */
      readonly obscurer?: {
        readonly role: string;
        readonly id: string;
        readonly label: string;
      };
    };

/** Convenience constructor for success results. */
export function ok(opts?: { resolvedBy?: string; detail?: string }): ActionResult {
  return {
    ok: true,
    resolvedBy: opts?.resolvedBy,
    detail: opts?.detail,
  };
}

/** Convenience constructor for failure results. */
export function fail(
  reason: string,
  opts?: { obscurer?: { role: string; id: string; label: string } },
): ActionResult {
  return {
    ok: false,
    reason,
    obscurer: opts?.obscurer,
  };
}
