import type {
  CompactElement,
  DeviceController,
  ResolvedElement,
  Selector,
} from "../adapters/device-controller.port.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import type {
  OverlayAnalysis,
  TargetStateChange,
  TransitionClassifier,
} from "./core/index.js";
import { Tool } from "./core/tool.js";
import { checkSelectorQuality } from "./selector-quality.js";

const selectorSchema: JsonSchema = {
  type: "object",
  properties: {
    resourceId: { type: "string" },
    contentDesc: { type: "string" },
    text: { type: "string" },
    textContains: { type: "string" },
    hint: { type: "string" },
    nth: { type: "number", default: 0 },
  },
};

export interface TapAndWaitTransitionArgs {
  selector: Selector;
  waitForAbsent?: Selector;
  waitForAppear?: Selector;
  timeoutMs?: number;
  maxTimeoutMs?: number;
  loadingKeywords?: string[];
  intervalMs?: number;
}

export type TapAndWaitTransitionResult =
  | { ok: true; waitedMs: number; selectorWarning?: string }
  | {
      ok: false;
      waitedMs?: number;
      reason?: string;
      classification?: string;
      hint?: string;
      dialogLabels?: string[];
      overlayKind?: string;
      targetStateChanged?: string;
      selectorWarning?: string;
      tapResult?: unknown;
    };

const OVERLAY_RANK: Record<string, number> = {
  dialog: 4,
  loading_with_cancel: 3,
  loading: 2,
  unknown: 1,
};

/**
 * Tap an element and verify a screen transition atomically. Orchestrates
 * the full signal-gathering loop (target state change, overlay analysis,
 * structural loading detection, motion diff, visual delta via screenshot
 * byte length) — each individual signal comes from the injected
 * `TransitionClassifier` strategy.
 *
 * This is the longest Tool class in the codebase because the transition
 * classification has many branches, but every branch delegates to a
 * named method or a classifier call — no business logic is inlined.
 */
export class TapAndWaitTransitionTool extends Tool<{
  args: TapAndWaitTransitionArgs;
  result: TapAndWaitTransitionResult;
}> {
  readonly name = "tap_and_wait_transition";
  readonly description =
    "Tap + verify screen transition atomically. Required for navigation/submit/login. " +
    "Provide at least one of waitForAbsent (old screen anchor) or waitForAppear (new screen " +
    "anchor). Auto-extends on loading. See get_playbook for timeout guidance.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["selector"],
    properties: {
      selector: selectorSchema,
      waitForAbsent: selectorSchema,
      waitForAppear: selectorSchema,
      timeoutMs: { type: "number", default: 10000 },
      maxTimeoutMs: { type: "number", default: 60000 },
      loadingKeywords: { type: "array", items: { type: "string" } },
      intervalMs: { type: "number", default: 300 },
    },
  };

  private readonly VISUAL_DELTA_THRESHOLD = 0.02;

  constructor(private readonly classifier: TransitionClassifier) {
    super();
  }

  async execute(
    args: TapAndWaitTransitionArgs,
    ctx: AtomyxContext,
  ): Promise<TapAndWaitTransitionResult> {
    const hasAbsent = args.waitForAbsent && Object.keys(args.waitForAbsent).length > 0;
    const hasAppear = args.waitForAppear && Object.keys(args.waitForAppear).length > 0;
    if (!hasAbsent && !hasAppear) {
      return {
        ok: false,
        reason:
          "tap_and_wait_transition requires at least one of waitForAbsent or waitForAppear. " +
          "If the tap does not cause a transition, use the `tap` tool instead.",
      };
    }

    const ctl = requireController(ctx);
    const timeoutMs = args.timeoutMs ?? 10000;
    const maxTimeoutMs = Math.max(timeoutMs, args.maxTimeoutMs ?? 60000);
    const intervalMs = args.intervalMs ?? 300;

    const preResolved = await ctl.resolveSelector(args.selector);
    const selectorWarning = checkSelectorQuality(args.selector, preResolved);
    const beforeSummary = await ctl.getUiSummary().catch(() => []);
    const baselineScreenshot = await ctl.screenshot().catch(() => null);
    const baselineBytes = baselineScreenshot ? baselineScreenshot.base64.length : 0;

    const tapResult = await ctl.tap(args.selector);
    if (tapResult && tapResult.ok === false) {
      return { ok: false, reason: `tap failed: ${tapResult.reason ?? "unknown"}`, tapResult };
    }

    return this.pollForTransition({
      ctl,
      preResolved,
      beforeSummary,
      baselineBytes,
      args,
      hasAbsent: !!hasAbsent,
      hasAppear: !!hasAppear,
      timeoutMs,
      maxTimeoutMs,
      intervalMs,
      selectorWarning,
    });
  }

  // ── poll loop ─────────────────────────────────────────────────────

  private async pollForTransition(params: {
    ctl: DeviceController;
    preResolved: ResolvedElement;
    beforeSummary: CompactElement[];
    baselineBytes: number;
    args: TapAndWaitTransitionArgs;
    hasAbsent: boolean;
    hasAppear: boolean;
    timeoutMs: number;
    maxTimeoutMs: number;
    intervalMs: number;
    selectorWarning: string | undefined;
  }): Promise<TapAndWaitTransitionResult> {
    const { ctl, preResolved, beforeSummary, baselineBytes, args, hasAbsent, hasAppear } = params;
    const { timeoutMs, maxTimeoutMs, intervalMs, selectorWarning } = params;

    const start = Date.now();
    const hardDeadline = start + maxTimeoutMs;
    let softDeadline = start + timeoutMs;
    let absentOk = !hasAbsent;
    let appearOk = !hasAppear;

    // Peak-evidence tracking — strongest signal observed at ANY iteration.
    let peakOverlay: OverlayAnalysis | undefined;
    let peakTargetChange: TargetStateChange | undefined;
    let peakVisualDeltaRatio = 0;
    let pollCounter = 0;
    let lastSummary: CompactElement[] = [];

    while (Date.now() < hardDeadline) {
      // Check absent/appear first — if both conditions met, done.
      if (!absentOk) {
        const r = await ctl.resolveSelector(args.waitForAbsent!);
        if (!r.found) absentOk = true;
      }
      if (!appearOk) {
        const r = await ctl.resolveSelector(args.waitForAppear!);
        if (r.found) appearOk = true;
      }
      if (absentOk && appearOk) {
        return {
          ok: true,
          waitedMs: Date.now() - start,
          ...(selectorWarning ? { selectorWarning } : {}),
        };
      }

      // Sample screen state twice (for motion) and gather signals.
      const sample1 = lastSummary.length > 0 ? lastSummary : await ctl.getUiSummary().catch(() => []);
      await new Promise((r) => setTimeout(r, Math.min(200, intervalMs)));
      const sample2 = await ctl.getUiSummary().catch(() => sample1);
      lastSummary = sample2;

      const postResolved = await ctl.resolveSelector(args.selector).catch(() => null);
      const targetChange = this.classifier.detectTargetStateChange(preResolved, postResolved);
      const overlay = this.classifier.analyzeOverlay(beforeSummary, sample2);
      const structuralLoading = this.classifier.detectLoading(sample2, args.loadingKeywords);
      const motion = this.classifier.detectMotion(sample1, sample2);

      // Update peak evidence.
      if (overlay?.detected && this.rank(overlay.kind) > this.rank(peakOverlay?.kind)) {
        peakOverlay = overlay;
      }
      if (targetChange.changed && !peakTargetChange?.changed) {
        peakTargetChange = targetChange;
      }

      // Visual delta — every 3rd iteration to bound screenshot cost.
      pollCounter += 1;
      if (baselineBytes > 0 && pollCounter % 3 === 0) {
        const shot = await ctl.screenshot().catch(() => null);
        if (shot) {
          const deltaRatio = Math.abs(shot.base64.length - baselineBytes) / baselineBytes;
          if (deltaRatio > peakVisualDeltaRatio) peakVisualDeltaRatio = deltaRatio;
        }
      }

      // Dialog early return — user flow is blocked, don't auto-extend.
      if (peakOverlay?.detected && peakOverlay.kind === "dialog") {
        return {
          ok: false,
          waitedMs: Date.now() - start,
          classification: "dialog_or_error_shown",
          hint: "Dialog/alert blocking the flow. Inspect dialogLabels for message.",
          dialogLabels: peakOverlay.innerLabels.slice(0, 4),
          overlayKind: "dialog",
          ...(selectorWarning ? { selectorWarning } : {}),
        };
      }

      // Loading detected → auto-extend soft deadline.
      const loadingNow =
        targetChange.changed ||
        (overlay?.detected && (overlay.kind === "loading" || overlay.kind === "loading_with_cancel")) ||
        structuralLoading.detected ||
        motion.detected ||
        peakVisualDeltaRatio > this.VISUAL_DELTA_THRESHOLD;

      const now = Date.now();
      if (now >= softDeadline - intervalMs && loadingNow && now < hardDeadline) {
        softDeadline = Math.min(now + 5000, hardDeadline);
      }
      if (now >= softDeadline) break;
      await new Promise((r) => setTimeout(r, intervalMs));
    }

    // Deadline hit — classify the failure via the injected classifier.
    return this.buildFailureResult({
      ctl,
      beforeSummary,
      lastSummary,
      preResolved,
      absentOk,
      appearOk,
      hasAbsent,
      hasAppear,
      peakOverlay,
      peakTargetChange,
      peakVisualDeltaRatio,
      args,
      maxTimeoutMs,
      selectorWarning,
      start,
      hardDeadline,
    });
  }

  // ── failure result construction ───────────────────────────────────

  private async buildFailureResult(params: {
    ctl: DeviceController;
    beforeSummary: CompactElement[];
    lastSummary: CompactElement[];
    preResolved: ResolvedElement;
    absentOk: boolean;
    appearOk: boolean;
    hasAbsent: boolean;
    hasAppear: boolean;
    peakOverlay: OverlayAnalysis | undefined;
    peakTargetChange: TargetStateChange | undefined;
    peakVisualDeltaRatio: number;
    args: TapAndWaitTransitionArgs;
    maxTimeoutMs: number;
    selectorWarning: string | undefined;
    start: number;
    hardDeadline: number;
  }): Promise<TapAndWaitTransitionResult> {
    const {
      ctl, beforeSummary, lastSummary, preResolved, absentOk, appearOk,
      peakOverlay, peakTargetChange, peakVisualDeltaRatio, args, maxTimeoutMs,
      selectorWarning, start, hardDeadline,
    } = params;

    const failures: string[] = [];
    if (params.hasAbsent && !absentOk) failures.push("waitForAbsent: old-screen anchor still present");
    if (params.hasAppear && !appearOk) failures.push("waitForAppear: new-screen anchor never appeared");

    const afterSummary = lastSummary.length > 0 ? lastSummary : await ctl.getUiSummary().catch(() => []);
    const finalTargetResolved = await ctl.resolveSelector(args.selector).catch(() => null);
    const finalTargetChange = this.classifier.detectTargetStateChange(preResolved, finalTargetResolved);
    const finalOverlay = this.classifier.analyzeOverlay(beforeSummary, afterSummary);

    const effectiveOverlay =
      peakOverlay && this.rank(peakOverlay.kind) >= this.rank(finalOverlay?.kind)
        ? peakOverlay
        : finalOverlay;
    const effectiveTargetChange = peakTargetChange?.changed ? peakTargetChange : finalTargetChange;

    const diagnostics = this.classifier.classify(beforeSummary, afterSummary, absentOk, appearOk, {
      loadingKeywords: args.loadingKeywords,
      targetStateChange: effectiveTargetChange,
      overlay: effectiveOverlay,
    });

    const hitHardDeadline = Date.now() >= hardDeadline;
    const classification =
      hitHardDeadline && diagnostics.loading.detected
        ? ("still_loading_after_max" as const)
        : diagnostics.classification;

    const hint =
      classification === "still_loading_after_max"
        ? `Loading indicator still visible after maxTimeoutMs=${maxTimeoutMs}ms. ` +
          `The backend or device is hung — NOT a test-case failure. Report as infra issue.`
        : diagnostics.hint;

    const visualSignal = peakVisualDeltaRatio > this.VISUAL_DELTA_THRESHOLD;
    const finalHint = visualSignal && classification === "no_change_detected"
      ? `Visual delta detected (${(peakVisualDeltaRatio * 100).toFixed(1)}% PNG byte change) but a11y ` +
        `tree showed no change. Likely a Flutter-without-Semantics render. Inspect a screenshot manually.`
      : hint;

    const dialogLabels = diagnostics.dialogAppeared.slice(0, 4).map((d) => d.label).filter(Boolean);

    return {
      ok: false,
      waitedMs: Date.now() - start,
      classification,
      hint: finalHint,
      reason: failures.join("; ") || undefined,
      ...(dialogLabels.length > 0 ? { dialogLabels } : {}),
      ...(diagnostics.targetStateChange?.changed
        ? { targetStateChanged: diagnostics.targetStateChange.reason }
        : {}),
      ...(diagnostics.overlay?.detected ? { overlayKind: diagnostics.overlay.kind } : {}),
      ...(selectorWarning ? { selectorWarning } : {}),
    };
  }

  private rank(kind: string | undefined): number {
    return OVERLAY_RANK[kind ?? ""] ?? 0;
  }
}
