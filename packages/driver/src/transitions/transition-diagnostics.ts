import type { CompactElement, ResolvedElementState } from "./compact-element.js";

/**
 * Transition diagnostics. Pure functions that operate on flat
 * `CompactElement` lists derived from `TreeNode` via
 * `treeNodeToCompactElements`.
 *
 * Used by `TransitionClassifier` (thin class wrapper) and by
 * `tap-and-wait-transition` in `@atomyx/mcp` to classify WHY a
 * tap-and-wait attempt failed:
 *
 *   - still_loading              — loading indicator / motion
 *   - still_loading_with_cancel  — cancelable loading overlay
 *   - dialog_or_error_shown      — overlay with action buttons
 *   - overlay_unknown            — large overlay, no clear content
 *   - partial_transition         — old screen gone, new not here
 *   - no_change_detected         — nothing happened
 */

export type LoadingSignal = {
  detected: boolean;
  matches: string[];
  structural: boolean;
  motion: boolean;
};

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

const DIALOG_ROLE_KEYWORDS = [
  "dialog",
  "alert",
  "modal",
  "popup",
  "sheet",
  "snackbar",
  "toast",
];

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
    const db =
      Math.abs(prev.bounds.left - el.bounds.left) +
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
    !s.resourceId && !s.contentDesc && !s.text
      ? `l=${el.label}|role=${el.role}`
      : null,
  ].filter(Boolean);
  return parts.join("|");
}

export type AppearedElement = {
  label: string;
  role: string;
  selector: Record<string, string>;
  looksLikeDialog: boolean;
};

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

export function compactSnapshot(
  summary: CompactElement[],
  max = 25,
): string[] {
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

export type TargetStateChange = {
  changed: boolean;
  vanished: boolean;
  disabled: boolean;
  unclickable: boolean;
  boundsDelta: number;
  roleChanged: boolean;
  reason: string;
};

export type OverlayAnalysis = {
  detected: boolean;
  coverageRatio: number;
  kind: "dialog" | "loading" | "loading_with_cancel" | "unknown";
  hasClickableAction: boolean;
  hasLoadingRole: boolean;
  innerLabels: string[];
  overlayBounds?: { left: number; top: number; right: number; bottom: number };
};

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

function screenArea(summary: CompactElement[]): number {
  let maxRight = 0;
  let maxBottom = 0;
  for (const el of summary) {
    if (el.bounds.right > maxRight) maxRight = el.bounds.right;
    if (el.bounds.bottom > maxBottom) maxBottom = el.bounds.bottom;
  }
  return maxRight * maxBottom || 1;
}

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
  const inner = after.filter(
    (el) => el !== candidate && contains(candidate!.bounds, el.bounds),
  );
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

export function detectTargetStateChange(
  pre: ResolvedElementState | null | undefined,
  post: ResolvedElementState | null | undefined,
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
      reason: "target vanished after tap (replaced or navigated away)",
    };
  }
  const disabled = pre.enabled === true && post.enabled === false;
  const unclickable = pre.clickable === true && post.clickable === false;
  const pb = pre.bounds;
  const qb = post.bounds;
  const boundsDelta =
    pb && qb
      ? Math.abs(pb.left - qb.left) +
        Math.abs(pb.top - qb.top) +
        Math.abs(pb.right - qb.right) +
        Math.abs(pb.bottom - qb.bottom)
      : 0;
  const roleChanged = (pre.role ?? "") !== (post.role ?? "");
  const changed = disabled || unclickable || boundsDelta >= 8 || roleChanged;
  const reasons: string[] = [];
  if (disabled) reasons.push("enabled=true→false");
  if (unclickable) reasons.push("clickable=true→false");
  if (boundsDelta >= 8) reasons.push(`boundsDelta=${boundsDelta}px`);
  if (roleChanged) reasons.push(`role ${pre.role}→${post.role}`);
  return {
    changed,
    vanished: false,
    disabled,
    unclickable,
    boundsDelta,
    roleChanged,
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
    loading.matches = [
      ...loading.matches,
      `motion: ${opts.motion.movedElementCount} elements moving`,
    ];
  }
  const appeared = diffAppeared(beforeTap, afterPoll);
  const dialogAppeared = appeared.filter((a) => a.looksLikeDialog);
  const currentSnapshot = compactSnapshot(afterPoll);
  const targetStateChange = opts.targetStateChange;
  const overlay = opts.overlay;

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
      "A loading scrim covers the screen. Backend request has not completed. " +
      "Auto-extending timeout; if the scrim persists beyond maxTimeoutMs, treat as infrastructure issue.";
  } else if (overlay?.detected && overlay.kind === "unknown") {
    classification = "overlay_unknown";
    hint =
      `An overlay covering ${(overlay.coverageRatio * 100).toFixed(0)}% of the screen appeared but ` +
      "contains neither clickable actions nor a structural loading role. Inspect `currentSnapshot` " +
      "and `overlay.overlayBounds` to determine its nature. Do NOT guess.";
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
