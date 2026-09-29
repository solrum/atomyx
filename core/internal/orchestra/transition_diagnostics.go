package orchestra

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"dev.atomyx/internal/wire"
)

// Default thresholds for DetectMotion, AnalyzeOverlay, and
// CompactSnapshot. Go has no default-argument syntax, so these
// functions have no built-in fallback for their threshold parameters:
// production call sites pass one of these constants explicitly to get
// the package's standard sensitivity instead of each picking its own
// number.
const (
	DefaultMotionMinDeltaPx    = 4.0
	DefaultOverlayMinCoverage  = 0.6
	DefaultCompactSnapshotSize = 25
	// overlayInnerLabelMaxLen and overlayInnerLabelCap bound
	// AnalyzeOverlay's innerLabels collection: a label longer than
	// this is assumed to be a paragraph of body text rather than an
	// action affordance, and at most this many labels are kept.
	overlayInnerLabelMaxLen = 120
	overlayInnerLabelCap    = 8
	// targetStateChangeBoundsDeltaPx is the minimum summed edge
	// movement (left+top+right+bottom, in points) that counts the
	// tap target itself as having moved between the pre-tap and
	// post-tap reads.
	targetStateChangeBoundsDeltaPx = 8.0
)

// jsToFixed0 formats value rounded to zero decimal places with ties
// broken away from zero. Go's "%.0f" verb rounds half to even instead,
// so the two disagree on exact .5 ties (62.5 formats as "62" via "%.0f"
// but "63" here). The hint text that quotes a computed percentage is a
// stable, user-facing string, so it must round exact ties the same way
// on every call rather than depend on which rounding mode "%.0f"
// happens to apply, which is why it goes through this instead of
// "%.0f" directly.
func jsToFixed0(value float64) string {
	rounded := math.Floor(math.Abs(value) + 0.5)
	if value < 0 {
		rounded = -rounded
	}
	return fmt.Sprintf("%.0f", rounded)
}

var loadingRolePatterns = []*regexp.Regexp{
	regexp.MustCompile(`progressbar`),
	regexp.MustCompile(`progressindicator`),
	regexp.MustCompile(`activityindicator`),
	regexp.MustCompile(`loadingview`),
	regexp.MustCompile(`loadingindicator`),
	regexp.MustCompile(`loader`),
	regexp.MustCompile(`\bspinner\b`),
	regexp.MustCompile(`shimmer`),
	regexp.MustCompile(`skeleton`),
	regexp.MustCompile(`lottieanimationview`),
}

var dialogRoleKeywords = []string{
	"dialog", "alert", "modal", "popup", "sheet", "snackbar", "toast",
}

func matchesLoadingRole(lowercaseRole string) bool {
	for _, re := range loadingRolePatterns {
		if re.MatchString(lowercaseRole) {
			return true
		}
	}
	return false
}

// LoadingSignal is DetectLoading's result.
type LoadingSignal struct {
	Detected   bool
	Matches    []string
	Structural bool
	Motion     bool
}

// DetectLoading scans summary for elements whose role matches a
// known loading-indicator vocabulary (progress bar, activity
// indicator, spinner, shimmer, skeleton, and similar), and
// additionally for elements whose label contains one of
// extraKeywords when the caller supplies any. Structural is true
// only when a role match fired; a keyword-only match sets Detected
// but not Structural.
func DetectLoading(summary []CompactElement, extraKeywords []string) LoadingSignal {
	matches := make([]string, 0)
	structural := false
	for _, el := range summary {
		role := strings.ToLower(el.Role)
		if matchesLoadingRole(role) {
			structural = true
			m := "role=" + el.Role
			if el.Label != "" {
				m += ` label="` + el.Label + `"`
			}
			matches = append(matches, m)
			continue
		}
		if len(extraKeywords) == 0 {
			continue
		}
		label := strings.ToLower(el.Label)
		for _, k := range extraKeywords {
			if strings.Contains(label, strings.ToLower(k)) {
				matches = append(matches, `label="`+el.Label+`"`)
				break
			}
		}
	}
	return LoadingSignal{Detected: len(matches) > 0, Matches: matches, Structural: structural, Motion: false}
}

// MotionResult is DetectMotion's result.
type MotionResult struct {
	Detected          bool
	MovedElementCount int
}

// DetectMotion compares before and after by element identity
// (elementKey) and counts how many elements present in both samples
// moved by at least minDeltaPx, summed across all four edges.
// Elements with no counterpart in before (newly appeared) are not
// counted as moved.
func DetectMotion(before, after []CompactElement, minDeltaPx float64) MotionResult {
	byKey := make(map[string]CompactElement, len(before))
	for _, el := range before {
		byKey[elementKey(el)] = el
	}
	moved := 0
	for _, el := range after {
		prev, ok := byKey[elementKey(el)]
		if !ok {
			continue
		}
		db := math.Abs(prev.Bounds.Left-el.Bounds.Left) +
			math.Abs(prev.Bounds.Top-el.Bounds.Top) +
			math.Abs(prev.Bounds.Right-el.Bounds.Right) +
			math.Abs(prev.Bounds.Bottom-el.Bounds.Bottom)
		if db >= minDeltaPx {
			moved++
		}
	}
	return MotionResult{Detected: moved > 0, MovedElementCount: moved}
}

// elementKey identifies an element across two samples for diffing
// and motion comparison. It concatenates every content field the
// element's selector carries (resourceId, contentDesc, text, since an
// element may match on more than one), falling back to its label and
// role only when none of the three selector fields is present.
func elementKey(el CompactElement) string {
	resourceID := el.Selector["resourceId"]
	contentDesc := el.Selector["contentDesc"]
	text := el.Selector["text"]
	parts := make([]string, 0, 4)
	if resourceID != "" {
		parts = append(parts, "r="+resourceID)
	}
	if contentDesc != "" {
		parts = append(parts, "d="+contentDesc)
	}
	if text != "" {
		parts = append(parts, "t="+text)
	}
	if resourceID == "" && contentDesc == "" && text == "" {
		parts = append(parts, fmt.Sprintf("l=%s|role=%s", el.Label, el.Role))
	}
	return strings.Join(parts, "|")
}

// AppearedElement is one element DiffAppeared reports as newly
// present in after.
type AppearedElement struct {
	Label           string
	Role            string
	Selector        map[string]string
	LooksLikeDialog bool
}

// DiffAppeared returns every element in after whose elementKey has no
// match in before, excluding elements with neither a label nor a
// resourceId nor a contentDesc (there is nothing for a caller to act
// on from those). LooksLikeDialog is true when the element's role
// contains one of the dialog-family keywords (dialog, alert, modal,
// popup, sheet, snackbar, toast).
func DiffAppeared(before, after []CompactElement) []AppearedElement {
	beforeKeys := make(map[string]struct{}, len(before))
	for _, el := range before {
		beforeKeys[elementKey(el)] = struct{}{}
	}
	appeared := make([]AppearedElement, 0)
	for _, el := range after {
		if _, ok := beforeKeys[elementKey(el)]; ok {
			continue
		}
		resourceID := el.Selector["resourceId"]
		contentDesc := el.Selector["contentDesc"]
		if el.Label == "" && resourceID == "" && contentDesc == "" {
			continue
		}
		role := strings.ToLower(el.Role)
		looksLikeDialog := false
		for _, k := range dialogRoleKeywords {
			if strings.Contains(role, k) {
				looksLikeDialog = true
				break
			}
		}
		sel := el.Selector
		if sel == nil {
			sel = map[string]string{}
		}
		appeared = append(appeared, AppearedElement{
			Label:           el.Label,
			Role:            el.Role,
			Selector:        sel,
			LooksLikeDialog: looksLikeDialog,
		})
	}
	return appeared
}

// CompactSnapshot renders up to max elements of summary as short
// human-readable lines, for the agent to inspect when a timeout
// classification needs manual follow-up.
func CompactSnapshot(summary []CompactElement, max int) []string {
	n := len(summary)
	if n > max {
		n = max
	}
	out := make([]string, 0, n)
	for _, el := range summary[:n] {
		resourceID := el.Selector["resourceId"]
		contentDesc := el.Selector["contentDesc"]
		text := el.Selector["text"]
		var selStr string
		switch {
		case resourceID != "":
			selStr = `resourceId="` + resourceID + `"`
		case contentDesc != "":
			selStr = `contentDesc="` + contentDesc + `"`
		case text != "":
			selStr = `text="` + text + `"`
		default:
			selStr = "[no-selector]"
		}
		line := "[" + selStr + "] " + el.Role
		if el.Label != "" {
			line += ` "` + el.Label + `"`
		}
		if el.Clickable {
			line += " (clickable)"
		}
		out = append(out, line)
	}
	return out
}

// TargetStateChange is DetectTargetStateChange's result.
type TargetStateChange struct {
	Changed     bool
	Vanished    bool
	Disabled    bool
	Unclickable bool
	BoundsDelta float64
	RoleChanged bool
	Reason      string
}

// DetectTargetStateChange compares the tap target's resolved state
// before and after a tap. It reports Changed when the target went
// from enabled to disabled, from clickable to unclickable, moved by
// at least targetStateChangeBoundsDeltaPx summed across its four
// edges, or changed role, any of which means the tap was accepted
// and something is happening even though the caller's
// waitForAbsent/waitForAppear condition has not yet resolved.
func DetectTargetStateChange(pre, post ResolvedElementState) TargetStateChange {
	if !pre.Found {
		return TargetStateChange{Reason: "target not found pre-tap, so comparison is not possible"}
	}
	if !post.Found {
		return TargetStateChange{
			Changed:  true,
			Vanished: true,
			Reason:   "target vanished after tap (replaced or navigated away)",
		}
	}
	disabled := pre.Enabled != nil && *pre.Enabled && post.Enabled != nil && !*post.Enabled
	unclickable := pre.Clickable != nil && *pre.Clickable && post.Clickable != nil && !*post.Clickable
	var boundsDelta float64
	if pre.Bounds != nil && post.Bounds != nil {
		pb, qb := *pre.Bounds, *post.Bounds
		boundsDelta = math.Abs(pb.Left-qb.Left) + math.Abs(pb.Top-qb.Top) +
			math.Abs(pb.Right-qb.Right) + math.Abs(pb.Bottom-qb.Bottom)
	}
	roleChanged := pre.Role != post.Role
	changed := disabled || unclickable || boundsDelta >= targetStateChangeBoundsDeltaPx || roleChanged

	reasons := make([]string, 0, 4)
	if disabled {
		reasons = append(reasons, "enabled changed from true to false")
	}
	if unclickable {
		reasons = append(reasons, "clickable changed from true to false")
	}
	if boundsDelta >= targetStateChangeBoundsDeltaPx {
		reasons = append(reasons, fmt.Sprintf("boundsDelta=%vpx", boundsDelta))
	}
	if roleChanged {
		reasons = append(reasons, fmt.Sprintf("role changed from %s to %s", pre.Role, post.Role))
	}
	reason := strings.Join(reasons, ", ")
	if reason == "" {
		reason = "no change"
	}
	return TargetStateChange{
		Changed:     changed,
		Vanished:    false,
		Disabled:    disabled,
		Unclickable: unclickable,
		BoundsDelta: boundsDelta,
		RoleChanged: roleChanged,
		Reason:      reason,
	}
}

// OverlayAnalysis is AnalyzeOverlay's result.
type OverlayAnalysis struct {
	Detected           bool
	CoverageRatio      float64
	Kind               string // "dialog" | "loading" | "loading_with_cancel" | "unknown"
	HasClickableAction bool
	HasLoadingRole     bool
	InnerLabels        []string
	OverlayBounds      *wire.Bounds
}

func overlayElementArea(b wire.Bounds) float64 {
	return math.Max(0, b.Right-b.Left) * math.Max(0, b.Bottom-b.Top)
}

func overlayContains(outer, inner wire.Bounds) bool {
	return inner.Left >= outer.Left &&
		inner.Top >= outer.Top &&
		inner.Right <= outer.Right &&
		inner.Bottom <= outer.Bottom
}

func screenArea(summary []CompactElement) float64 {
	var maxRight, maxBottom float64
	for _, el := range summary {
		if el.Bounds.Right > maxRight {
			maxRight = el.Bounds.Right
		}
		if el.Bounds.Bottom > maxBottom {
			maxBottom = el.Bounds.Bottom
		}
	}
	area := maxRight * maxBottom
	if area == 0 {
		return 1
	}
	return area
}

// AnalyzeOverlay looks for the largest element in after that (a) has
// no counterpart in before (elementKey) and (b) covers at least
// minCoverage of the screen (the largest bounds among all elements in
// after). It returns nil when no element qualifies.
//
// The qualifying element's Kind is classified by what renders inside
// its bounds: an inner element with a short (at most 120 UTF-16 code
// units) non-empty label makes it "dialog" (or "loading_with_cancel"
// when a loading-role element is also inside); an inner loading-role
// element with no such label makes it "loading"; neither makes it
// "unknown".
func AnalyzeOverlay(before, after []CompactElement, minCoverage float64) *OverlayAnalysis {
	beforeKeys := make(map[string]struct{}, len(before))
	for _, el := range before {
		beforeKeys[elementKey(el)] = struct{}{}
	}
	screen := screenArea(after)
	var candidate *CompactElement
	candidateArea := 0.0
	for i := range after {
		el := &after[i]
		if _, ok := beforeKeys[elementKey(*el)]; ok {
			continue
		}
		a := overlayElementArea(el.Bounds)
		if a/screen < minCoverage {
			continue
		}
		if a > candidateArea {
			candidate = el
			candidateArea = a
		}
	}
	if candidate == nil {
		return nil
	}

	hasActionLabel := false
	hasLoadingRole := false
	innerLabels := make([]string, 0, overlayInnerLabelCap)
	for i := range after {
		if &after[i] == candidate {
			continue
		}
		el := after[i]
		if !overlayContains(candidate.Bounds, el.Bounds) {
			continue
		}
		if matchesLoadingRole(strings.ToLower(el.Role)) {
			hasLoadingRole = true
		}
		label := strings.TrimSpace(el.Label)
		if len(label) > 0 && utf16Len(label) <= overlayInnerLabelMaxLen {
			hasActionLabel = true
			if len(innerLabels) < overlayInnerLabelCap {
				innerLabels = append(innerLabels, label)
			}
		}
	}

	var kind string
	switch {
	case hasActionLabel && hasLoadingRole:
		kind = "loading_with_cancel"
	case hasActionLabel:
		kind = "dialog"
	case hasLoadingRole:
		kind = "loading"
	default:
		kind = "unknown"
	}

	bounds := candidate.Bounds
	return &OverlayAnalysis{
		Detected:           true,
		CoverageRatio:      candidateArea / screen,
		Kind:               kind,
		HasClickableAction: hasActionLabel,
		HasLoadingRole:     hasLoadingRole,
		InnerLabels:        innerLabels,
		OverlayBounds:      &bounds,
	}
}

// TransitionDiagnostics is ClassifyFailure's result: the full set of
// signals it computed, the classification it derived from them, and
// a human-readable hint for the classification.
type TransitionDiagnostics struct {
	Loading           LoadingSignal
	Appeared          []AppearedElement
	DialogAppeared    []AppearedElement
	CurrentSnapshot   []string
	TargetStateChange *TargetStateChange
	Overlay           *OverlayAnalysis
	Classification    string
	Hint              string
}

// ClassifyFailureOptions carries the signals ClassifyFailure needs
// but does not compute itself, because computing them (a fresh
// DetectLoading pass, a fresh DetectMotion pass, or a driver read for
// TargetStateChange/Overlay) requires the caller's own before/after
// samples and selectors.
type ClassifyFailureOptions struct {
	LoadingSignal     *LoadingSignal
	Motion            *MotionResult
	LoadingKeywords   []string
	TargetStateChange *TargetStateChange
	Overlay           *OverlayAnalysis
}

// ClassifyFailure derives WHY a tap-and-wait-transition attempt timed
// out, from a fixed priority order of signals:
//
//  1. TargetStateChange.Changed, the tap target itself changed
//     state, so the app is processing (still_loading).
//  2. Overlay.Kind == "dialog", a blocking dialog/alert appeared
//     (dialog_or_error_shown).
//  3. Overlay.Kind == "loading_with_cancel", a cancelable loading
//     scrim (still_loading_with_cancel).
//  4. Overlay.Kind == "loading", a loading scrim with no clear
//     content (still_loading).
//  5. Overlay.Kind == "unknown", a large overlay that is neither
//     (overlay_unknown).
//  6. Loading.Detected (no overlay), a loading indicator is on
//     screen (still_loading).
//  7. A dialog-roled element appeared without a qualifying overlay
//     (dialog_or_error_shown).
//  8. The old anchor is gone but the new anchor never appeared
//     (partial_transition).
//  9. None of the above (no_change_detected).
//
// Each classification carries a hint written for a downstream agent
// deciding what to do next; the hint text and the classification
// values themselves are a stable contract other tooling depends on.
func ClassifyFailure(beforeTap, afterPoll []CompactElement, absentOk, appearOk bool, opts ClassifyFailureOptions) TransitionDiagnostics {
	var loading LoadingSignal
	if opts.LoadingSignal != nil {
		loading = *opts.LoadingSignal
	} else {
		loading = DetectLoading(afterPoll, opts.LoadingKeywords)
	}
	if opts.Motion != nil && opts.Motion.Detected {
		loading.Motion = true
		loading.Detected = true
		matches := make([]string, len(loading.Matches), len(loading.Matches)+1)
		copy(matches, loading.Matches)
		matches = append(matches, fmt.Sprintf("motion: %d elements moving", opts.Motion.MovedElementCount))
		loading.Matches = matches
	}

	appeared := DiffAppeared(beforeTap, afterPoll)
	dialogAppeared := make([]AppearedElement, 0, len(appeared))
	for _, a := range appeared {
		if a.LooksLikeDialog {
			dialogAppeared = append(dialogAppeared, a)
		}
	}
	currentSnapshot := CompactSnapshot(afterPoll, DefaultCompactSnapshotSize)
	targetStateChange := opts.TargetStateChange
	overlay := opts.Overlay

	var classification, hint string
	switch {
	case targetStateChange != nil && targetStateChange.Changed:
		classification = "still_loading"
		hint = fmt.Sprintf(
			"Target element state changed after tap (%s). "+
				"The tap was accepted and the app is processing. Wait for the operation to finish.",
			targetStateChange.Reason)
	case overlay != nil && overlay.Detected && overlay.Kind == "dialog":
		classification = "dialog_or_error_shown"
		hint = "An overlay with clickable action buttons appeared. This is a dialog/alert, NOT a loading " +
			"scrim. The flow is blocked until the user acts. Inspect `overlay.innerLabels` for the " +
			"actual message and action options. Do NOT guess at backend causes, because the dialog tells you."
	case overlay != nil && overlay.Detected && overlay.Kind == "loading_with_cancel":
		classification = "still_loading_with_cancel"
		hint = "A loading overlay with a Cancel option is visible (e.g. 'Please wait… [Cancel]'). The app " +
			"is processing a request but is cancelable. Keep waiting unless you intentionally want to abort."
	case overlay != nil && overlay.Detected && overlay.Kind == "loading":
		classification = "still_loading"
		hint = "A loading scrim covers the screen. Backend request has not completed. " +
			"Auto-extending timeout; if the scrim persists beyond maxTimeoutMs, treat as infrastructure issue."
	case overlay != nil && overlay.Detected && overlay.Kind == "unknown":
		classification = "overlay_unknown"
		hint = fmt.Sprintf(
			"An overlay covering %s%% of the screen appeared but "+
				"contains neither clickable actions nor a structural loading role. Inspect `currentSnapshot` "+
				"and `overlay.overlayBounds` to determine its nature. Do NOT guess.",
			jsToFixed0(overlay.CoverageRatio*100))
	case loading.Detected:
		classification = "still_loading"
		hint = "A loading indicator is visible on screen (no overlay). The request has not completed. " +
			"Consider increasing timeoutMs, or investigate whether the backend is hung."
	case len(dialogAppeared) > 0:
		classification = "dialog_or_error_shown"
		hint = "A dialog-roled element appeared after the tap (no full overlay). Inspect `dialogAppeared` " +
			"for the actual message, do NOT guess at credential/backend causes."
	case absentOk && !appearOk:
		classification = "partial_transition"
		hint = "Old screen is gone but the expected new-screen anchor did not appear. The app may have " +
			"navigated to an unexpected screen. Dump UI tree to inspect."
	default:
		classification = "no_change_detected"
		hint = "Nothing observable changed after the tap. The tap may have missed the target, the handler " +
			"may be a no-op, or the target may be disabled. Verify the selector actually matched a " +
			"clickable + enabled element."
	}

	return TransitionDiagnostics{
		Loading:           loading,
		Appeared:          appeared,
		DialogAppeared:    dialogAppeared,
		CurrentSnapshot:   currentSnapshot,
		TargetStateChange: targetStateChange,
		Overlay:           overlay,
		Classification:    classification,
		Hint:              hint,
	}
}
