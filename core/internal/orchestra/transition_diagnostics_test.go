package orchestra

import (
	"strings"
	"testing"

	"dev.atomyx/internal/wire"
)

func el(role, label, text, resourceID string, bounds wire.Bounds, clickable, enabled bool) CompactElement {
	var selector map[string]string
	if resourceID != "" || label != "" || text != "" {
		selector = make(map[string]string, 3)
		if resourceID != "" {
			selector["resourceId"] = resourceID
		}
		if label != "" {
			selector["contentDesc"] = label
		}
		if text != "" {
			selector["text"] = text
		}
	}
	return CompactElement{
		Role: role, Label: label, Text: text, ResourceID: resourceID,
		Bounds: bounds, Clickable: clickable, Enabled: enabled, Selector: selector,
	}
}

var defaultBounds = wire.Bounds{Left: 0, Top: 0, Right: 100, Bottom: 100}

func TestTreeNodeToCompactElements(t *testing.T) {
	tree := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container", "bounds": "0,0,430,932"},
		Children: []wire.TreeNodeWire{
			{
				Attributes: map[string]string{"role": "button", "id": "login", "text": "Sign in", "label": "Sign in", "bounds": "100,400,330,460"},
				Clickable:  boolPtr(true),
				Enabled:    boolPtr(true),
			},
		},
	}
	compact := TreeNodeToCompactElements(tree)
	if len(compact) != 2 {
		t.Fatalf("expected 2 compact elements, got %d", len(compact))
	}
	var btn *CompactElement
	for i := range compact {
		if compact[i].ResourceID == "login" {
			btn = &compact[i]
		}
	}
	if btn == nil {
		t.Fatal("expected to find login button")
	}
	if !btn.Clickable || btn.Role != "button" {
		t.Fatalf("unexpected button state: %+v", btn)
	}
}

func TestTreeNodeToCompactElementsDropsNodesWithNoBounds(t *testing.T) {
	tree := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{
			{Attributes: map[string]string{"role": "text", "text": "no bounds"}},
		},
	}
	compact := TreeNodeToCompactElements(tree)
	if len(compact) != 0 {
		t.Fatalf("expected 0 compact elements, got %d", len(compact))
	}
}

func TestDetectLoadingStructuralRole(t *testing.T) {
	els := []CompactElement{
		el("ProgressBar", "", "", "", defaultBounds, false, true),
		el("button", "", "", "", defaultBounds, false, true),
	}
	r := DetectLoading(els, nil)
	if !r.Detected || !r.Structural {
		t.Fatalf("expected detected+structural, got %+v", r)
	}
}

func TestDetectLoadingIgnoresNonLoadingRoleWithoutKeywords(t *testing.T) {
	els := []CompactElement{el("button", "Loading...", "", "", defaultBounds, false, true)}
	r := DetectLoading(els, nil)
	if r.Detected {
		t.Fatalf("expected not detected, got %+v", r)
	}
}

func TestDetectLoadingMatchesExtraKeyword(t *testing.T) {
	els := []CompactElement{el("text", "Loading...", "", "", defaultBounds, false, true)}
	r := DetectLoading(els, []string{"loading"})
	if !r.Detected {
		t.Fatalf("expected detected via keyword, got %+v", r)
	}
}

func TestDetectMotionReportsMovement(t *testing.T) {
	before := []CompactElement{el("other", "", "", "x", wire.Bounds{Left: 0, Top: 0, Right: 100, Bottom: 100}, false, true)}
	after := []CompactElement{el("other", "", "", "x", wire.Bounds{Left: 20, Top: 20, Right: 120, Bottom: 120}, false, true)}
	r := DetectMotion(before, after, DefaultMotionMinDeltaPx)
	if !r.Detected || r.MovedElementCount != 1 {
		t.Fatalf("expected 1 moved element, got %+v", r)
	}
}

func TestDetectMotionIgnoresSubThresholdMotion(t *testing.T) {
	before := []CompactElement{el("other", "", "", "x", defaultBounds, false, true)}
	after := []CompactElement{el("other", "", "", "x", wire.Bounds{Left: 1, Top: 1, Right: 101, Bottom: 101}, false, true)}
	r := DetectMotion(before, after, 10)
	if r.Detected {
		t.Fatalf("expected no motion above threshold 10, got %+v", r)
	}
}

func TestDiffAppearedFindsNewElements(t *testing.T) {
	before := []CompactElement{el("other", "", "", "a", defaultBounds, false, true)}
	after := []CompactElement{
		el("other", "", "", "a", defaultBounds, false, true),
		el("dialog", "Error occurred", "", "b", defaultBounds, false, true),
	}
	r := DiffAppeared(before, after)
	if len(r) != 1 || r[0].Label != "Error occurred" || !r[0].LooksLikeDialog {
		t.Fatalf("unexpected diff result: %+v", r)
	}
}

func TestClassifyFailureLoadingScrim(t *testing.T) {
	before := []CompactElement{el("other", "Login", "", "btn", defaultBounds, false, true)}
	after := []CompactElement{
		el("other", "Login", "", "btn", defaultBounds, false, true),
		el("ProgressBar", "", "", "", defaultBounds, false, true),
	}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{})
	if r.Classification != "still_loading" {
		t.Fatalf("expected still_loading, got %s (%s)", r.Classification, r.Hint)
	}
}

func TestClassifyFailureNoChange(t *testing.T) {
	before := []CompactElement{el("other", "Login", "", "btn", defaultBounds, false, true)}
	after := []CompactElement{el("other", "Login", "", "btn", defaultBounds, false, true)}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{})
	if r.Classification != "no_change_detected" {
		t.Fatalf("expected no_change_detected, got %s", r.Classification)
	}
}

func TestClassifyFailurePartialTransition(t *testing.T) {
	before := []CompactElement{el("other", "Old", "", "old", defaultBounds, false, true)}
	after := []CompactElement{el("other", "Other", "", "other", defaultBounds, false, true)}
	r := ClassifyFailure(before, after, true, false, ClassifyFailureOptions{})
	if r.Classification != "partial_transition" {
		t.Fatalf("expected partial_transition, got %s", r.Classification)
	}
}

func TestClassifyFailureOverlayDialog(t *testing.T) {
	screen := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 932}
	before := []CompactElement{el("container", "", "", "", screen, false, true)}
	overlayBounds := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 700}
	after := []CompactElement{
		el("container", "", "", "", screen, false, true),
		el("dialog", "", "", "overlay", overlayBounds, false, true),
		el("button", "OK", "", "ok-btn", wire.Bounds{Left: 165, Top: 600, Right: 265, Bottom: 650}, true, true),
	}
	overlay := AnalyzeOverlay(before, after, DefaultOverlayMinCoverage)
	if overlay == nil || overlay.Kind != "dialog" {
		t.Fatalf("expected overlay kind dialog, got %+v", overlay)
	}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{Overlay: overlay})
	if r.Classification != "dialog_or_error_shown" {
		t.Fatalf("expected dialog_or_error_shown, got %s", r.Classification)
	}
}

func TestClassifyFailureOverlayLoadingWithCancel(t *testing.T) {
	screen := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 932}
	before := []CompactElement{el("container", "", "", "", screen, false, true)}
	overlayBounds := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 700}
	after := []CompactElement{
		el("container", "", "", "", screen, false, true),
		el("dialog", "", "", "overlay", overlayBounds, false, true),
		el("progressbar", "", "", "spinner", wire.Bounds{Left: 165, Top: 550, Right: 265, Bottom: 590}, false, true),
		el("button", "Cancel", "", "cancel-btn", wire.Bounds{Left: 165, Top: 600, Right: 265, Bottom: 650}, true, true),
	}
	overlay := AnalyzeOverlay(before, after, DefaultOverlayMinCoverage)
	if overlay == nil || overlay.Kind != "loading_with_cancel" {
		t.Fatalf("expected overlay kind loading_with_cancel, got %+v", overlay)
	}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{Overlay: overlay})
	if r.Classification != "still_loading_with_cancel" {
		t.Fatalf("expected still_loading_with_cancel, got %s", r.Classification)
	}
}

func TestClassifyFailureOverlayUnknown(t *testing.T) {
	screen := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 932}
	before := []CompactElement{el("container", "", "", "", screen, false, true)}
	overlayBounds := wire.Bounds{Left: 0, Top: 0, Right: 430, Bottom: 700}
	after := []CompactElement{
		el("container", "", "", "", screen, false, true),
		el("other", "", "", "overlay", overlayBounds, false, true),
	}
	overlay := AnalyzeOverlay(before, after, DefaultOverlayMinCoverage)
	if overlay == nil || overlay.Kind != "unknown" {
		t.Fatalf("expected overlay kind unknown, got %+v", overlay)
	}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{Overlay: overlay})
	if r.Classification != "overlay_unknown" {
		t.Fatalf("expected overlay_unknown, got %s", r.Classification)
	}
}

func TestJsToFixed0RoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{62.5, "63"},
		{0.5, "1"},
		{2.5, "3"},
		{100.0, "100"},
		{62.4999, "62"},
		{62.5000001, "63"},
		{-62.5, "-63"},
	}
	for _, c := range cases {
		if got := jsToFixed0(c.value); got != c.want {
			t.Fatalf("jsToFixed0(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestClassifyFailureOverlayUnknownRoundsCoverageTieAwayFromZero(t *testing.T) {
	screen := wire.Bounds{Left: 0, Top: 0, Right: 1000, Bottom: 1000}
	before := []CompactElement{el("container", "", "", "", screen, false, true)}
	// overlayBounds covers exactly 62.5% of the 1,000,000-unit screen area,
	// landing overlay.CoverageRatio*100 on the exact tie the TypeScript
	// port's toFixed(0) rounds up (62.5 -> "63") while Go's "%.0f" would
	// round to even ("62").
	overlayBounds := wire.Bounds{Left: 0, Top: 0, Right: 1000, Bottom: 625}
	after := []CompactElement{
		el("container", "", "", "", screen, false, true),
		el("other", "", "", "overlay", overlayBounds, false, true),
	}
	overlay := AnalyzeOverlay(before, after, DefaultOverlayMinCoverage)
	if overlay == nil || overlay.Kind != "unknown" {
		t.Fatalf("expected overlay kind unknown, got %+v", overlay)
	}
	if overlay.CoverageRatio != 0.625 {
		t.Fatalf("expected coverage ratio 0.625, got %v", overlay.CoverageRatio)
	}
	r := ClassifyFailure(before, after, false, false, ClassifyFailureOptions{Overlay: overlay})
	if !strings.Contains(r.Hint, "An overlay covering 63% of the screen") {
		t.Fatalf("expected hint to round 62.5%% up to 63%%, got %q", r.Hint)
	}
}

func TestDetectTargetStateChangeNotFoundPreTap(t *testing.T) {
	r := DetectTargetStateChange(ResolvedElementState{Found: false}, ResolvedElementState{Found: true})
	if r.Changed {
		t.Fatalf("expected changed=false when pre not found, got %+v", r)
	}
}

func TestDetectTargetStateChangeVanished(t *testing.T) {
	pre := ResolvedElementState{Found: true, Role: "button", Enabled: boolPtr(true), Clickable: boolPtr(true), Bounds: &defaultBounds}
	r := DetectTargetStateChange(pre, ResolvedElementState{Found: false})
	if !r.Changed || !r.Vanished {
		t.Fatalf("expected changed+vanished, got %+v", r)
	}
}

func TestDetectTargetStateChangeDisabled(t *testing.T) {
	pre := ResolvedElementState{Found: true, Role: "button", Enabled: boolPtr(true), Bounds: &defaultBounds}
	post := ResolvedElementState{Found: true, Role: "button", Enabled: boolPtr(false), Bounds: &defaultBounds}
	r := DetectTargetStateChange(pre, post)
	if !r.Changed || !r.Disabled {
		t.Fatalf("expected changed+disabled, got %+v", r)
	}
}

func TestDetectTargetStateChangeNoChange(t *testing.T) {
	pre := ResolvedElementState{Found: true, Role: "button", Enabled: boolPtr(true), Clickable: boolPtr(true), Bounds: &defaultBounds}
	post := ResolvedElementState{Found: true, Role: "button", Enabled: boolPtr(true), Clickable: boolPtr(true), Bounds: &defaultBounds}
	r := DetectTargetStateChange(pre, post)
	if r.Changed || r.Reason != "no change" {
		t.Fatalf("expected no change, got %+v", r)
	}
}
