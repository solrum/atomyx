import type { CompactElement } from "../adapters/device-controller.port.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import { AmbiguityDetector, UiTreeCache } from "./core/index.js";
import { Tool } from "./core/tool.js";
import { filterStable, renderCompactLine, sortByStability } from "./tree-render.js";

export interface GetUiTreeArgs {
  stableOnly?: boolean;
  limit?: number;
}

export type GetUiTreeResult =
  | {
      treeFingerprint: string;
      count: number;
      totalAvailable: number;
      truncated: boolean;
      tree: string;
    }
  | {
      ok: false;
      reason: string;
      treeFingerprint?: string;
      unchangedForMs?: number;
      platform?: string;
    };

/**
 * Dump the current screen as a compact element list. Shared UiTreeCache
 * deduplicates within 1.5s; AmbiguityDetector marks duplicate selector
 * tokens inline so the agent knows to disambiguate via coords or `nth`.
 *
 * Stale a11y detection: when the tree is empty AND `currentForeground()`
 * reports no app, the device binding has gone stale — return an
 * actionable error carrying `platform` so the caller can print the
 * right rebind hint.
 */
export class GetUiTreeTool extends Tool<{
  args: GetUiTreeArgs;
  result: GetUiTreeResult;
}> {
  readonly name = "get_ui_tree";
  readonly description =
    "Dump current screen as a compact element list with explicit `resourceId=\"...\"` / " +
    "`contentDesc=\"...\"` / `text=\"...\"` selectors and inline `@cx,cy` coords. Call ONCE " +
    "per screen — use find_element for lookups between actions. Blocks re-dumps within 1.5s.";
  readonly schema: JsonSchema = {
    type: "object",
    properties: {
      stableOnly: { type: "boolean", default: true },
      limit: { type: "number", default: 40 },
    },
  };

  constructor(
    private readonly cache: UiTreeCache,
    private readonly ambiguity: AmbiguityDetector,
  ) {
    super();
  }

  async execute(args: GetUiTreeArgs, ctx: AtomyxContext): Promise<GetUiTreeResult> {
    const ctl = requireController(ctx);

    // Dedup spam: if the cache has a recent fresh dump AND no mutating
    // tool has invalidated it, block the re-call.
    const peek = this.cache.peek();
    const now = Date.now();
    if (peek && now - peek.at < 1500) {
      return {
        ok: false,
        reason:
          `BLOCKED: get_ui_tree already called ${now - peek.at}ms ago — the cached result is ` +
          `still fresh (fingerprint=${peek.fingerprint}). Use \`find_element(selector)\` to ` +
          `query the cached tree without re-dumping. Only re-dump after an action that might ` +
          `change the screen (tap, type, navigation).`,
        treeFingerprint: peek.fingerprint,
        unchangedForMs: now - peek.at,
      };
    }

    const { elements, fingerprint } = await this.cache.ensureDump(ctl);

    // Stale binding detection — platform-neutral via currentForeground().
    if (elements.length === 0) {
      const foreground = await ctl
        .currentForeground()
        .catch(() => ({ appId: "", screen: undefined }));
      if (!foreground.appId) {
        return {
          ok: false,
          reason:
            "STALE BINDING: tree is empty AND foreground app is unknown. The device " +
            "automation backend is connected but has lost its active-window handle. " +
            "Rebind and retry.",
          platform: ctl.platform,
        };
      }
    }

    const stableOnly = args.stableOnly ?? true;
    const filtered = stableOnly ? filterStable(elements) : elements;
    const sorted = sortByStability(filtered);

    const limit = args.limit ?? 40;
    const limited = sorted.slice(0, limit);

    // Ambiguity marker — duplicate tokens get `(N×)` appended by the
    // render helper so the agent knows to disambiguate via coords / nth.
    const dupCounts = this.ambiguity.computeDuplicateCounts(limited);
    const lines = limited.map((e: CompactElement) =>
      renderCompactLine(e, dupCounts.get(this.ambiguity.tokenOf(e))),
    );

    return {
      treeFingerprint: fingerprint,
      count: limited.length,
      totalAvailable: sorted.length,
      truncated: sorted.length > limit,
      tree: lines.join("\n"),
    };
  }
}
