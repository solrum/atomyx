package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

func TestOrchestra_WaitForTransition_ResolvesWhenAppearMatches(t *testing.T) {
	d := mock.New()
	noTarget := node("root", "", "0,0,100,100")
	withTarget := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Ready", "0,0,50,50")},
	}
	d.StageHierarchyRepeated(noTarget, 2).StageHierarchyRepeated(withTarget, 2)
	o, clock := newTestOrchestra(d)

	result, err := o.WaitForTransition(context.Background(), WaitForTransitionParams{
		WaitForAppear: &Selector{ID: lit("target")},
		TimeoutMs:     1000,
		MaxTimeoutMs:  5000,
		IntervalMs:    100,
		ResolvedBy:    "id",
	})
	if err != nil {
		t.Fatalf("WaitForTransition returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, want true: %+v", result)
	}
	if result.ResolvedBy != "id" {
		t.Fatalf("ResolvedBy = %q, want %q", result.ResolvedBy, "id")
	}
	if result.WaitedMs != 100 {
		t.Fatalf("WaitedMs = %d, want 100", result.WaitedMs)
	}
	if clock.NowMs() != 100 {
		t.Fatalf("clock advanced to %d, want 100", clock.NowMs())
	}
}

func TestOrchestra_WaitForTransition_TimesOutAtHardDeadline(t *testing.T) {
	d := mock.New()
	// The staged tree never carries the "nope" target and never
	// changes, so mock.Driver's sticky-final-state behavior returns
	// it for every poll and every classification read. BeforeSnapshot
	// matches it exactly so the screen has no newly-appeared element
	// for AnalyzeOverlay to flag, isolating the classification to the
	// waitForAppear condition alone.
	tree := node("root", "", "0,0,100,100")
	d.StageHierarchy(tree)
	o, _ := newTestOrchestra(d)

	result, err := o.WaitForTransition(context.Background(), WaitForTransitionParams{
		WaitForAppear:  &Selector{ID: lit("nope")},
		TimeoutMs:      100,
		MaxTimeoutMs:   100,
		IntervalMs:     50,
		TargetPre:      ResolvedElementState{Found: false},
		BeforeSnapshot: TreeNodeToCompactElements(tree),
	})
	if err != nil {
		t.Fatalf("WaitForTransition returned error: %v", err)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false (target never appears)")
	}
	if result.WaitedMs != 100 {
		t.Fatalf("WaitedMs = %d, want 100 (the hard deadline)", result.WaitedMs)
	}
	if result.Classification != "partial_transition" {
		t.Fatalf("Classification = %q, want %q", result.Classification, "partial_transition")
	}
}

func TestOrchestra_WaitForTransition_ExtendsDeadlineWhileLoadingPersists(t *testing.T) {
	d := mock.New()
	// A structural loading-role element with no counterpart in
	// BeforeSnapshot keeps DetectLoading reporting Detected across
	// every poll, so the soft deadline keeps extending until the hard
	// deadline, never resolving early since "nope" never appears.
	loading := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root", AttrBounds: "0,0,100,100"},
		Children:   []wire.TreeNodeWire{nodeWithRole("spinner", "", "progressbar", "10,10,20,20")},
	}
	d.StageHierarchy(loading)
	o, _ := newTestOrchestra(d)

	result, err := o.WaitForTransition(context.Background(), WaitForTransitionParams{
		WaitForAppear:  &Selector{ID: lit("nope")},
		TimeoutMs:      100,
		MaxTimeoutMs:   300,
		IntervalMs:     50,
		TargetPre:      ResolvedElementState{Found: false},
		BeforeSnapshot: TreeNodeToCompactElements(loading),
	})
	if err != nil {
		t.Fatalf("WaitForTransition returned error: %v", err)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false (target never appears)")
	}
	if result.WaitedMs != 300 {
		t.Fatalf("WaitedMs = %d, want 300 (the extended wait ran to the hard deadline)", result.WaitedMs)
	}
	if result.Classification != "still_loading" {
		t.Fatalf("Classification = %q, want %q", result.Classification, "still_loading")
	}
}
