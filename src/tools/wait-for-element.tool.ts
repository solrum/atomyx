import type { Selector } from "../adapters/device-controller.port.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";

export interface WaitForElementArgs {
  selector: Selector;
  absent?: boolean;
  timeoutMs?: number;
  intervalMs?: number;
}

export interface WaitForElementResult {
  ok: boolean;
  found: boolean;
  waitedMs: number;
  reason?: string;
  selectorTried?: Selector;
}

/**
 * Poll until a selector matches (or disappears when `absent=true`).
 * Includes the early-probe optimization: if we're waiting for an
 * element to APPEAR but nothing on screen even partially matches the
 * query string, fail fast with a hint to call get_ui_tree. Prevents
 * agents from burning the full timeoutMs on a guessed-wrong label.
 */
export class WaitForElementTool extends Tool<{
  args: WaitForElementArgs;
  result: WaitForElementResult;
}> {
  readonly name = "wait_for_element";
  readonly description =
    "Poll until selector matches (or absent=true to wait for disappearance). Prefer over wait_for_idle.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["selector"],
    properties: {
      selector: {
        type: "object",
        properties: {
          resourceId: { type: "string" },
          contentDesc: { type: "string" },
          text: { type: "string" },
          textContains: { type: "string" },
          hint: { type: "string" },
        },
      },
      absent: { type: "boolean", default: false },
      timeoutMs: { type: "number", default: 5000 },
      intervalMs: { type: "number", default: 300 },
    },
  };

  async execute(args: WaitForElementArgs, ctx: AtomyxContext): Promise<WaitForElementResult> {
    const ctl = requireController(ctx);
    const timeoutMs = args.timeoutMs ?? 5000;
    const intervalMs = args.intervalMs ?? 300;
    const absent = args.absent ?? false;
    const start = Date.now();

    // Early probe: if waiting for presence but nothing on screen looks
    // even remotely like the query, fail fast.
    if (!absent) {
      const initial = await ctl.resolveSelector(args.selector);
      if (!initial.found) {
        const summary = await ctl.getUiSummary().catch(() => []);
        if (summary.length > 0) {
          const needle = (
            args.selector.text ??
            args.selector.textContains ??
            args.selector.contentDesc ??
            ""
          ).toLowerCase();
          if (needle) {
            const anyClose = summary.some(
              (e) =>
                (e.label ?? "").toLowerCase().includes(needle) ||
                Object.values(e.selector ?? {}).some((v) => v.toLowerCase().includes(needle)),
            );
            if (!anyClose) {
              return {
                ok: false,
                found: false,
                waitedMs: 0,
                reason:
                  "NOT FOUND and no element on screen even partially matches. You likely guessed " +
                  "a label in the wrong language or wrong copy. Call get_ui_tree to see the actual " +
                  "labels before retrying.",
                selectorTried: args.selector,
              };
            }
          }
        }
      }
    }

    while (Date.now() - start < timeoutMs) {
      const r = await ctl.resolveSelector(args.selector);
      const conditionMet = absent ? !r.found : r.found;
      if (conditionMet) {
        return { ok: true, found: r.found, waitedMs: Date.now() - start };
      }
      await new Promise((r) => setTimeout(r, intervalMs));
    }
    const final = await ctl.resolveSelector(args.selector);
    return { ok: false, found: final.found, waitedMs: Date.now() - start };
  }
}
