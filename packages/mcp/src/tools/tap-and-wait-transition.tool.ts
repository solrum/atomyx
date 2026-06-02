import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";
import {
  AttrKeys,
  classifyFailure,
  detectLoading,
  detectMotion,
  detectTargetStateChange,
  diffAppeared,
  analyzeOverlay,
  parseBounds,
  treeNodeToCompactElements,
  type CompactElement,
  type ResolvedElementState,
  type Orchestra,
  type Selector,
} from "@atomyx/driver";

const TapAndWaitArgs = z
  .object({
    selector: SelectorSchema,
    waitForAbsent: SelectorSchema.optional(),
    waitForAppear: SelectorSchema.optional(),
    timeoutMs: z.number().int().positive().default(10_000),
    maxTimeoutMs: z.number().int().positive().default(60_000),
    intervalMs: z.number().int().positive().default(300),
    loadingKeywords: z.array(z.string()).optional(),
  })
  .strict()
  .refine(
    (a) => a.waitForAbsent !== undefined || a.waitForAppear !== undefined,
    "must provide waitForAbsent or waitForAppear (or both)",
  );

/**
 * `tap_and_wait_transition` — atomic tap + screen transition
 * verification with structured failure diagnostics.
 *
 * Uses Orchestra for the tap + hierarchy polling, flattens
 * TreeNode snapshots into CompactElement lists, and on timeout
 * runs `classifyFailure` to get structured diagnostics the agent
 * can react to.
 */
export const tapAndWaitTransitionTool = defineTool({
  name: "tap_and_wait_transition",
  description:
    "Tap + verify screen transition atomically. Use this INSTEAD of plain tap when " +
    "the tap causes navigation / submit / login. Pass waitForAbsent (old anchor) " +
    "and/or waitForAppear (new anchor). On timeout, returns a structured " +
    "classification (still_loading, dialog_or_error_shown, partial_transition, " +
    "no_change_detected) with an actionable hint. After this succeeds, call " +
    "get_ui_tree to inspect the new screen — do NOT use screenshot.",
  inputSchema: TapAndWaitArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const timeoutMs = args.timeoutMs;
    const maxTimeoutMs = args.maxTimeoutMs;
    const intervalMs = args.intervalMs;

    const selector = compileSelectorInput(args.selector) as Selector;
    const absentSelector = args.waitForAbsent
      ? (compileSelectorInput(args.waitForAbsent) as Selector)
      : null;
    const appearSelector = args.waitForAppear
      ? (compileSelectorInput(args.waitForAppear) as Selector)
      : null;

    const beforeTreeRaw = await orchestra.hierarchy({ signal: ctx.signal });
    const beforeCompact = treeNodeToCompactElements(beforeTreeRaw);
    const pre = await resolveAsState(orchestra, selector, ctx.signal);

    const tapResult = await orchestra.tap(selector, { signal: ctx.signal });
    if (!tapResult.ok) {
      return {
        ok: false,
        reason: `tap failed: ${tapResult.reason}`,
        classification: "tap_failed",
        hint: "The tap itself did not succeed. Fix the selector / obscurement issue before retrying.",
        tapResult,
      };
    }

    const startedAt = ctx.clock.now();
    const hardDeadline = startedAt + maxTimeoutMs;
    let softDeadline = startedAt + timeoutMs;
    let lastSample: CompactElement[] = beforeCompact;

    while (ctx.clock.now() < hardDeadline) {
      if (ctx.signal.aborted) throw ctx.signal.reason ?? new DOMException("Aborted", "AbortError");
      await ctx.clock.sleep(intervalMs);

      const currentTree = await orchestra.hierarchy({ signal: ctx.signal });
      const currentCompact = treeNodeToCompactElements(currentTree);

      const absentOk = absentSelector
        ? (await orchestra.find(absentSelector, { signal: ctx.signal })).length === 0
        : true;
      const appearOk = appearSelector
        ? (await orchestra.find(appearSelector, { signal: ctx.signal })).length > 0
        : true;

      if (absentOk && appearOk) {
        return {
          ok: true,
          waitedMs: ctx.clock.now() - startedAt,
          resolvedBy: tapResult.resolvedBy,
        };
      }

      if (ctx.clock.now() >= softDeadline) {
        const loading = detectLoading(currentCompact, args.loadingKeywords);
        const motion = detectMotion(lastSample, currentCompact);
        if (
          (loading.detected || motion.detected) &&
          ctx.clock.now() < hardDeadline
        ) {
          softDeadline = Math.min(ctx.clock.now() + timeoutMs, hardDeadline);
        } else {
          break;
        }
      }
      lastSample = currentCompact;
    }

    const afterTreeRaw = await orchestra.hierarchy({ signal: ctx.signal });
    const afterCompact = treeNodeToCompactElements(afterTreeRaw);
    const post = await resolveAsState(orchestra, selector, ctx.signal);

    const targetStateChange = detectTargetStateChange(pre, post);
    const overlay = analyzeOverlay(beforeCompact, afterCompact);
    const loadingSignal = detectLoading(afterCompact, args.loadingKeywords);
    const motion = detectMotion(beforeCompact, afterCompact);

    const absentOkFinal = absentSelector
      ? (await orchestra.find(absentSelector, { signal: ctx.signal })).length === 0
      : true;
    const appearOkFinal = appearSelector
      ? (await orchestra.find(appearSelector, { signal: ctx.signal })).length > 0
      : false;

    const diag = classifyFailure(
      beforeCompact,
      afterCompact,
      absentOkFinal,
      appearOkFinal,
      {
        loadingSignal,
        motion,
        loadingKeywords: args.loadingKeywords,
        targetStateChange,
        overlay,
      },
    );

    const appeared = diffAppeared(beforeCompact, afterCompact);
    return {
      ok: false,
      waitedMs: ctx.clock.now() - startedAt,
      classification: diag.classification,
      hint: diag.hint,
      dialogLabels: diag.dialogAppeared.map((d) => d.label),
      overlayKind: overlay?.kind,
      targetStateChanged: targetStateChange.reason,
      appeared: appeared.slice(0, 10),
      currentSnapshot: diag.currentSnapshot,
    };
  },
});

async function resolveAsState(
  orchestra: Orchestra,
  selector: Selector,
  signal: AbortSignal,
): Promise<ResolvedElementState> {
  const cursor = await orchestra.findOne(selector, { signal });
  if (!cursor) return { found: false };
  const attrs = cursor.node.attributes;
  return {
    found: true,
    role: attrs[AttrKeys.Role],
    enabled: cursor.node.enabled,
    clickable: cursor.node.clickable,
    bounds: parseBounds(attrs[AttrKeys.Bounds]) ?? undefined,
  };
}
