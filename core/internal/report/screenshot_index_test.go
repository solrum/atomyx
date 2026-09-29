package report

import "testing"

func TestReportScreenshotIndex_FileNames(t *testing.T) {
	screenshots := []ReportScreenshot{
		{Scope: RootScriptScope, Label: "After Login!!"},
		{Scope: "child", Label: "Step Two"},
	}

	index := BuildScreenshotIndex(screenshots)

	want := []string{"01-after-login.png", "02-child-step-two.png"}
	if len(index.Refs) != len(want) {
		t.Fatalf("expected %d refs, got %d", len(want), len(index.Refs))
	}
	for i, ref := range index.Refs {
		if ref.File != want[i] {
			t.Errorf("ref %d: expected file %q, got %q", i, want[i], ref.File)
		}
	}
}

func TestReportScreenshotIndex_PositionPadding(t *testing.T) {
	screenshots := make([]ReportScreenshot, 11)
	for i := range screenshots {
		screenshots[i] = ReportScreenshot{Scope: RootScriptScope, Label: "shot"}
	}

	index := BuildScreenshotIndex(screenshots)

	if index.Refs[0].File != "01-shot.png" {
		t.Errorf("expected the first ref padded to two digits, got %q", index.Refs[0].File)
	}
	if index.Refs[10].File != "11-shot.png" {
		t.Errorf("expected the eleventh ref unpadded, got %q", index.Refs[10].File)
	}
}

func TestReportScreenshotIndex_EmptyLabelFallsBackToScreenshot(t *testing.T) {
	index := BuildScreenshotIndex([]ReportScreenshot{{Scope: RootScriptScope, Label: "!!!"}})

	if index.Refs[0].File != "01-screenshot.png" {
		t.Errorf("expected a fully-sanitized empty label to fall back to \"screenshot\", got %q", index.Refs[0].File)
	}
}

func TestReportScreenshotIndex_GroupsByScope(t *testing.T) {
	screenshots := []ReportScreenshot{
		{Scope: "a", Label: "one"},
		{Scope: "b", Label: "two"},
		{Scope: "a", Label: "three"},
	}

	index := BuildScreenshotIndex(screenshots)

	if got := len(index.ByScope["a"]); got != 2 {
		t.Errorf("expected 2 refs in scope \"a\", got %d", got)
	}
	if got := len(index.ByScope["b"]); got != 1 {
		t.Errorf("expected 1 ref in scope \"b\", got %d", got)
	}
}

func TestReportScreenshotIndex_ScenarioChildScope(t *testing.T) {
	if got := ScenarioChildScope(RootScriptScope, 0, "scripts/sign-up.yml"); got != "0::scripts/sign-up.yml" {
		t.Errorf("expected a root-scoped child scope with no parent prefix, got %q", got)
	}
	if got := ScenarioChildScope("1::scenarios/checkout.yml", 0, "scripts/add-to-cart.yml"); got != "1::scenarios/checkout.yml::0::scripts/add-to-cart.yml" {
		t.Errorf("expected the parent scope nested in front of the child's own scope, got %q", got)
	}
}

func TestReportScreenshotIndex_SuiteEntryScope(t *testing.T) {
	if got := SuiteEntryScope(2, "scripts/smoke.yml"); got != "2::scripts/smoke.yml" {
		t.Errorf("expected the entry's position prefixed to its path, got %q", got)
	}
}

// TestReportScreenshotIndex_NestedSuiteScenarioScope covers the suite
// scenario nesting case that ScenarioChildScope and SuiteEntryScope
// exist to keep distinct: a scenario entry's own child scope nests
// inside the entry's suite-level scope, so a same-path script in a
// sibling suite entry keys to a different scope and file name even
// though both scripts share the path "scripts/smoke.yml".
func TestReportScreenshotIndex_NestedSuiteScenarioScope(t *testing.T) {
	entryScope := SuiteEntryScope(1, "scenarios/checkout.yml")
	childScope := ScenarioChildScope(entryScope, 0, "scripts/smoke.yml")
	siblingEntryScope := SuiteEntryScope(2, "scripts/smoke.yml")

	if childScope != "1::scenarios/checkout.yml::0::scripts/smoke.yml" {
		t.Errorf("expected the nested child scope to carry both the entry and child positions, got %q", childScope)
	}
	if childScope == siblingEntryScope {
		t.Fatalf("expected the nested scenario child scope and the sibling entry scope to stay distinct, both were %q", childScope)
	}

	screenshots := []ReportScreenshot{
		{Scope: childScope, Label: "nested capture"},
		{Scope: siblingEntryScope, Label: "sibling capture"},
	}
	index := BuildScreenshotIndex(screenshots)

	if got := index.Refs[0].File; got != "01-1-scenarios-checkout-yml-0-scripts-smoke-yml-nested-capture.png" {
		t.Errorf("expected the nested child screenshot's file name to be derived from its full scope, got %q", got)
	}
	if got := index.Refs[1].File; got != "02-2-scripts-smoke-yml-sibling-capture.png" {
		t.Errorf("expected the sibling entry screenshot's file name to be derived from its own scope, got %q", got)
	}
	if len(index.ByScope[childScope]) != 1 || len(index.ByScope[siblingEntryScope]) != 1 {
		t.Fatalf("expected each scope to group exactly its own screenshot, got byScope=%v", index.ByScope)
	}
}
