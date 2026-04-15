import type { CompactElement, ResolvedElement } from "../adapters/device-controller.port.js";

/**
 * Heuristic detectors used by `tap_and_wait_transition` to classify WHY a
 * transition check failed, so the agent sees real diagnostics instead of
 * guessing ("credential wrong?", "backend rejected?").
 *
 * Three signals we compute from the compact UI summary:
 *   1. loading indicator visible (spinner / progress bar / loading text)
 *   2. elements that appeared AFTER the tap that were not on the baseline
 *      screen — typically an error dialog, toast, or inline validation
 *   3. a short compact-tree snapshot for the agent to inspect
 */

export type LoadingSignal = {
  detected: boolean;
  matches: string[];
  /** True if we detected loading from a structural role (language-independent). */
  structural: boolean;
  /** True if we detected motion/churn between samples (language-independent). */
  motion: boolean;
};

/**
 * Role patterns (derived from className, language-independent). These cover:
 *   - Android: ProgressBar, LinearProgressIndicator, CircularProgressIndicator
 *   - iOS/XCUITest: XCUIElementTypeActivityIndicator
 *   - Flutter: CircularProgressIndicator, LinearProgressIndicator
 *   - React Native: ActivityIndicator
 *   - Custom animation views: LottieAnimationView, ShimmerFrameLayout, SkeletonView
 *   - Generic: anything with "spinner", "loading", "loader" in the type name
 *
 * className/role is NOT localized, so these patterns work across all locales.
 */
const LOADING_ROLE_PATTERNS: RegExp[] = [
  /progressbar/,
  /progressindicator/,
  /activityindicator/,
  /loadingview/,
  /loadingindicator/,
  /loader/,
  /\bspinner\b/,
  /shimmer/,
  /skeleton/,
  /lottieanimationview/,
];

const DIALOG_ROLE_KEYWORDS = ["dialog", "alert", "modal", "popup", "sheet", "snackbar", "toast"];

/**
 * Language-independent loading detector. Primary signal is structural (role
 * derived from the native class name — not localized). Agents may pass
 * `extraKeywords` to opt-in to app-specific label matching when they already
 * know the target app's loading copy; no defaults to avoid i18n false positives.
 */
export function detectLoading(
  summary: CompactElement[],
  extraKeywords: string[] = [],
): LoadingSignal {
  const matches: string[] = [];
  let structural = false;
  for (const el of summary) {
    const role = (el.role ?? "").toLowerCase();
    if (LOADING_ROLE_PATTERNS.some((re) => re.test(role))) {
      structural = true;
      matches.push(`role=${el.role}${el.label ? ` label="${el.label}"` : ""}`);
      continue;
    }
    if (extraKeywords.length > 0) {
      const label = (el.label ?? "").toLowerCase();
      if (extraKeywords.some((k) => label.includes(k.toLowerCase()))) {
        matches.push(`label="${el.label}"`);
      }
    }
  }
  return { detected: matches.length > 0, matches, structural, motion: false };
}

/**
 * Motion detector: compares two summaries captured ~intervalMs apart and
 * reports whether any element's bounds shifted significantly (animation,
 * shimmer, spinner that doesn't expose a loading role). Language-independent;
 * complements `detectLoading` for custom loading views that draw via Canvas /
 * Lottie / Compose and don't match a role pattern.
 */
export function detectMotion(
  before: CompactElement[],
  after: CompactElement[],
  minDeltaPx = 4,
): { detected: boolean; movedElementCount: number } {
  const byKey = new Map<string, CompactElement>();
  for (const el of before) byKey.set(elementKey(el), el);
  let moved = 0;
  for (const el of after) {
    const prev = byKey.get(elementKey(el));
    if (!prev) continue;
    const db = Math.abs(prev.bounds.left - el.bounds.left) +
      Math.abs(prev.bounds.top - el.bounds.top) +
      Math.abs(prev.bounds.right - el.bounds.right) +
      Math.abs(prev.bounds.bottom - el.bounds.bottom);
    if (db >= minDeltaPx) moved += 1;
  }
  return { detected: moved > 0, movedElementCount: moved };
}

function elementKey(el: CompactElement): string {
  const s = el.selector ?? {};
  const parts = [
    s.resourceId ? `r=${s.resourceId}` : null,
    s.contentDesc ? `d=${s.contentDesc}` : null,
    s.text ? `t=${s.text}` : null,
    !s.resourceId && !s.contentDesc && !s.text ? `l=${el.label}|role=${el.role}` : null,
  ].filter(Boolean);
  return parts.join("|");
}

export type AppearedElement = {
  label: string;
  role: string;
  selector: Record<string, string>;
  looksLikeDialog: boolean;
};

/**
 * Compute elements present in `after` but not in `before`. Use element-key
 * based on stable selector fields so purely re-rendered nodes don't show up.
 */
export function diffAppeared(
  before: CompactElement[],
  after: CompactElement[],
): AppearedElement[] {
  const beforeKeys = new Set(before.map(elementKey));
  const appeared: AppearedElement[] = [];
  for (const el of after) {
    const key = elementKey(el);
    if (beforeKeys.has(key)) continue;
    if (!el.label && !el.selector?.resourceId && !el.selector?.contentDesc) continue;
    const role = (el.role ?? "").toLowerCase();
    // Dialog detection is purely structural (role-based). className — and
    // therefore role — is NOT localized, so this works across all languages.
    // Error-ness is not inferred here; the agent sees the diff and decides.
    const looksLikeDialog = DIALOG_ROLE_KEYWORDS.some((k) => role.includes(k));
    appeared.push({
      label: el.label,
      role: el.role,
      selector: el.selector ?? {},
      looksLikeDialog,
    });
  }
  return appeared;
}

export function compactSnapshot(summary: CompactElement[], max = 25): string[] {
  return summary.slice(0, max).map((el) => {
    const sel = el.selector ?? {};
    const selStr = sel.resourceId
      ? `resourceId="${sel.resourceId}"`
      : sel.contentDesc
      ? `contentDesc="${sel.contentDesc}"`
      : sel.text
      ? `text="${sel.text}"`
      : "[no-selector]";
    return `[${selStr}] ${el.role}${el.label ? ` "${el.label}"` : ""}${el.clickable ? " (clickable)" : ""}`;
  });
}

export type TransitionDiagnostics = {
  loading: LoadingSignal;
  appeared: AppearedElement[];
  dialogAppeared: AppearedElement[];
  currentSnapshot: string[];
  targetStateChange?: TargetStateChange;
  overlay?: OverlayAnalysis;
  classification:
    | "still_loading"
    | "still_loading_with_cancel"
    | "dialog_or_error_shown"
    | "overlay_unknown"
    | "no_change_detected"
    | "partial_transition";
  hint: string;
};

export type TargetStateChange = {
  changed: boolean;
  /** true if target was found pre-tap but not post-tap (vanished / replaced). */
  vanished: boolean;
  /** true if target lost `enabled`. */
  disabled: boolean;
  /** true if target lost `clickable`. */
  unclickable: boolean;
  /** absolute sum of bounds delta in pixels. */
  boundsDelta: number;
  /** Role changed between pre/post. */
  roleChanged: boolean;
  reason: string;
};

export type OverlayAnalysis = {
  detected: boolean;
  coverageRatio: number;
  /** A classification based on what the overlay contains. */
  kind: "dialog" | "loading" | "loading_with_cancel" | "unknown";
  hasClickableAction: boolean;
  hasLoadingRole: boolean;
  innerLabels: string[];
  overlayBounds?: { left: number; top: number; right: number; bottom: number };
};

function area(b: CompactElement["bounds"]): number {
  return Math.max(0, b.right - b.left) * Math.max(0, b.bottom - b.top);
}

function contains(
  outer: CompactElement["bounds"],
  inner: CompactElement["bounds"],
): boolean {
  return (
    inner.left >= outer.left &&
    inner.top >= outer.top &&
    inner.right <= outer.right &&
    inner.bottom <= outer.bottom
  );
}

/**
 * Derive screen bounds from the largest element in the summary. More reliable
 * than hardcoding device resolution.
 */
function screenArea(summary: CompactElement[]): number {
  let maxRight = 0;
  let maxBottom = 0;
  for (const el of summary) {
    if (el.bounds.right > maxRight) maxRight = el.bounds.right;
    if (el.bounds.bottom > maxBottom) maxBottom = el.bounds.bottom;
  }
  return maxRight * maxBottom || 1;
}

/**
 * Detect a new element that covers >minCoverage of the screen area — a very
 * strong signal that something was layered on top of the previous UI (dialog,
 * bottom sheet, loading scrim). Inspects the overlay's inner content to
 * classify it as dialog / loading / loading_with_cancel / unknown.
 *
 * Language-independent: all signals are structural (bounds, role, clickable).
 */
export function analyzeOverlay(
  before: CompactElement[],
  after: CompactElement[],
  minCoverage = 0.6,
): OverlayAnalysis | undefined {
  const beforeKeys = new Set(before.map(elementKey));
  const screen = screenArea(after);
  let candidate: CompactElement | undefined;
  let candidateArea = 0;
  for (const el of after) {
    if (beforeKeys.has(elementKey(el))) continue;
    const a = area(el.bounds);
    if (a / screen < minCoverage) continue;
    if (a > candidateArea) {
      candidate = el;
      candidateArea = a;
    }
  }
  if (!candidate) return undefined;

  // Inner content = elements whose bounds fall inside the overlay.
  const inner = after.filter((el) => el !== candidate && contains(candidate!.bounds, el.bounds));

  // Language-independent signals.
  // NOTE on Flutter: Flutter apps render through a single FlutterView and do
  // NOT expose native `clickable` flags reliably — GestureDetector dispatch
  // happens inside the engine. So we can't require clickable=true. Instead
  // we treat ANY new labeled element inside the overlay as a dialog action
  // candidate (button text, menu entry, etc). Loading scrims are expected to
  // have no labeled descendants, so labeled content inside an overlay is a
  // strong dialog signal even without clickable=true.
  let hasActionLabel = false;
  let hasLoadingRole = false;
  const innerLabels: string[] = [];
  for (const el of inner) {
    const role = (el.role ?? "").toLowerCase();
    if (LOADING_ROLE_PATTERNS.some((re) => re.test(role))) {
      hasLoadingRole = true;
    }
    const label = (el.label ?? "").trim();
    if (label.length > 0 && label.length <= 120) {
      // Labeled content inside an overlay = dialog action or message.
      hasActionLabel = true;
      if (innerLabels.length < 8) innerLabels.push(label);
    }
  }
  const hasClickableAction = hasActionLabel;

  let kind: OverlayAnalysis["kind"];
  if (hasClickableAction && hasLoadingRole) kind = "loading_with_cancel";
  else if (hasClickableAction) kind = "dialog";
  else if (hasLoadingRole) kind = "loading";
  else kind = "unknown";

  return {
    detected: true,
    coverageRatio: candidateArea / screen,
    kind,
    hasClickableAction,
    hasLoadingRole,
    innerLabels,
    overlayBounds: candidate.bounds,
  };
}

/**
 * Compare the target element (the one that was tapped) before and after the
 * tap. Loss of `enabled` / `clickable` / bounds shift are the clearest signals
 * that the tap was accepted and the app is processing — even before any
 * overlay or spinner appears.
 */
export function detectTargetStateChange(
  pre: ResolvedElement | null | undefined,
  post: ResolvedElement | null | undefined,
): TargetStateChange {
  if (!pre || !pre.found) {
    return {
      changed: false,
      vanished: false,
      disabled: false,
      unclickable: false,
      boundsDelta: 0,
      roleChanged: false,
      reason: "target not found pre-tap — cannot compare",
    };
  }
  if (!post || !post.found) {
    return {
      changed: true,
      vanished: true,
      disabled: false,
      unclickable: false,
      boundsDelta: 0,
      roleChanged: false,
      reason: "target vanished after tap (replaced by spinner / navigated away)",
    };
  }
  const disabled = pre.enabled === true && post.enabled === false;
  const unclickable = pre.clickable === true && post.clickable === false;
  const pb = pre.bounds;
  const qb = post.bounds;
  const boundsDelta = pb && qb
    ? Math.abs(pb.left - qb.left) +
      Math.abs(pb.top - qb.top) +
      Math.abs(pb.right - qb.right) +
      Math.abs(pb.bottom - qb.bottom)
    : 0;
  const changed = disabled || unclickable || boundsDelta >= 8;
  const reasons: string[] = [];
  if (disabled) reasons.push("enabled=true→false");
  if (unclickable) reasons.push("clickable=true→false");
  if (boundsDelta >= 8) reasons.push(`boundsDelta=${boundsDelta}px`);
  return {
    changed,
    vanished: false,
    disabled,
    unclickable,
    boundsDelta,
    roleChanged: false,
    reason: reasons.join(", ") || "no change",
  };
}

export function classifyFailure(
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
  const loading = opts.loadingSignal ?? detectLoading(afterPoll, opts.loadingKeywords);
  if (opts.motion?.detected) {
    loading.motion = true;
    loading.detected = true;
    loading.matches = [...loading.matches, `motion: ${opts.motion.movedElementCount} elements moving`];
  }
  const appeared = diffAppeared(beforeTap, afterPoll);
  const dialogAppeared = appeared.filter((a) => a.looksLikeDialog);
  const currentSnapshot = compactSnapshot(afterPoll);
  const targetStateChange = opts.targetStateChange;
  const overlay = opts.overlay;

  // Classification priority (most specific → least specific):
  // 1. Target element was disabled / vanished / became unclickable — strongest
  //    signal that the app accepted the tap and is processing. Treat as loading.
  // 2. Overlay with clickable action buttons inside — dialog/alert. Do NOT
  //    auto-extend; the user flow is BLOCKED until agent reads the message.
  // 3. Overlay with structural loading role or motion inside — loading scrim.
  // 4. Overlay with both action buttons AND loading role — hybrid (e.g.
  //    "Please wait" dialog with a Cancel button). Treat as loading, note
  //    cancelable state.
  // 5. Overlay without either → unknown, return snapshot and let agent decide.
  // 6. Structural loading role anywhere on screen (no overlay) → loading.
  // 7. Motion anywhere (no overlay, no role) → loading last-resort.
  // 8. Partial transition (old gone, new not here) → partial.
  // 9. Nothing → no change.
  let classification: TransitionDiagnostics["classification"];
  let hint: string;

  if (targetStateChange?.changed) {
    classification = "still_loading";
    hint =
      `Target element state changed after tap (${targetStateChange.reason}). ` +
      "The tap was accepted and the app is processing. Wait for the operation to finish.";
  } else if (overlay?.detected && overlay.kind === "dialog") {
    classification = "dialog_or_error_shown";
    hint =
      "An overlay with clickable action buttons appeared — this is a dialog/alert, NOT a loading " +
      "scrim. The flow is blocked until the user acts. Inspect `overlay.innerLabels` for the " +
      "actual message and action options. Do NOT guess at backend causes — the dialog tells you.";
  } else if (overlay?.detected && overlay.kind === "loading_with_cancel") {
    classification = "still_loading_with_cancel";
    hint =
      "A loading overlay with a Cancel option is visible (e.g. 'Please wait… [Cancel]'). The app " +
      "is processing a request but is cancelable. Keep waiting unless you intentionally want to abort.";
  } else if (overlay?.detected && overlay.kind === "loading") {
    classification = "still_loading";
    hint =
      "A loading scrim covers the screen (overlay + structural loading role inside). Backend request " +
      "has not completed. Auto-extending timeout; if the scrim persists beyond maxTimeoutMs, treat " +
      "as infrastructure issue.";
  } else if (overlay?.detected && overlay.kind === "unknown") {
    classification = "overlay_unknown";
    hint =
      `An overlay covering ${(overlay.coverageRatio * 100).toFixed(0)}% of the screen appeared but ` +
      "contains neither clickable actions nor a structural loading role. Inspect `currentSnapshot` " +
      "and `overlay.overlayBounds` to determine whether it is a custom dialog, a splash, or a " +
      "custom loading view. Do NOT guess.";
  } else if (loading.detected) {
    classification = "still_loading";
    hint =
      "A loading indicator is visible on screen (no overlay). The request has not completed. " +
      "Consider increasing timeoutMs, or investigate whether the backend is hung.";
  } else if (dialogAppeared.length > 0) {
    classification = "dialog_or_error_shown";
    hint =
      "A dialog-roled element appeared after the tap (no full overlay). Inspect `dialogAppeared` " +
      "for the actual message, do NOT guess at credential/backend causes.";
  } else if (absentOk && !appearOk) {
    classification = "partial_transition";
    hint =
      "Old screen is gone but the expected new-screen anchor did not appear. The app may have " +
      "navigated to an unexpected screen — dump UI tree to inspect.";
  } else {
    classification = "no_change_detected";
    hint =
      "Nothing observable changed after the tap. The tap may have missed the target, the handler " +
      "may be a no-op, or the target may be disabled. Verify the selector actually matched a " +
      "clickable + enabled element.";
  }

  return {
    loading,
    appeared,
    dialogAppeared,
    currentSnapshot,
    targetStateChange,
    overlay,
    classification,
    hint,
  };
}
