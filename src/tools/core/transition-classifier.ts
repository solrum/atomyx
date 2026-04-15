import type { CompactElement } from "../../adapters/device-controller.port.js";
import {
  analyzeOverlay,
  classifyFailure,
  detectLoading,
  detectMotion,
  detectTargetStateChange,
  diffAppeared,
  type LoadingSignal,
  type OverlayAnalysis,
  type TargetStateChange,
  type TransitionDiagnostics,
} from "../transition-diagnostics.js";

export type {
  LoadingSignal,
  OverlayAnalysis,
  TargetStateChange,
  TransitionDiagnostics,
};

/**
 * Classifies the outcome of a tap-and-wait-transition attempt into one of:
 *
 *   - still_loading              — loading indicator still visible
 *   - still_loading_after_max    — beyond the hard deadline
 *   - dialog_or_error_shown      — overlay with clickable content = dialog
 *   - still_loading_with_cancel  — hybrid (loading with a Cancel button)
 *   - overlay_unknown            — overlay with no clear content
 *   - partial_transition         — old screen gone, new screen not here
 *   - no_change_detected         — nothing happened
 *
 * Wraps the pure functions in `transition-diagnostics.ts` as an injectable
 * strategy. Encapsulates: loading-role detection, overlay dialog/scrim
 * classification, diff of elements that appeared after the tap, motion
 * sampling between poll iterations, target element state change detection.
 */
export class TransitionClassifier {
  detectLoading(summary: CompactElement[], extraKeywords?: string[]): LoadingSignal {
    return detectLoading(summary, extraKeywords);
  }

  detectMotion(
    before: CompactElement[],
    after: CompactElement[],
    minDeltaPx = 4,
  ): { detected: boolean; movedElementCount: number } {
    return detectMotion(before, after, minDeltaPx);
  }

  detectTargetStateChange(
    pre: Parameters<typeof detectTargetStateChange>[0],
    post: Parameters<typeof detectTargetStateChange>[1],
  ): TargetStateChange {
    return detectTargetStateChange(pre, post);
  }

  analyzeOverlay(
    before: CompactElement[],
    after: CompactElement[],
    minCoverage = 0.6,
  ): OverlayAnalysis | undefined {
    return analyzeOverlay(before, after, minCoverage);
  }

  diffAppeared(
    before: CompactElement[],
    after: CompactElement[],
  ): ReturnType<typeof diffAppeared> {
    return diffAppeared(before, after);
  }

  /**
   * The final classification call — takes all signals and returns a
   * single diagnostic shape with `classification` + `hint` for the agent.
   */
  classify(
    beforeTap: CompactElement[],
    afterPoll: CompactElement[],
    absentOk: boolean,
    appearOk: boolean,
    opts: {
      loadingSignal?: LoadingSignal;
      motion?: { detected: boolean; movedElementCount: number };
      loadingKeywords?: string[];
      targetStateChange?: TargetStateChange;
      overlay?: OverlayAnalysis;
    } = {},
  ): TransitionDiagnostics {
    return classifyFailure(beforeTap, afterPoll, absentOk, appearOk, opts);
  }
}
