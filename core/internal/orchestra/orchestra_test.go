package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

func node(id, text, bounds string, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: id, AttrText: text, AttrBounds: bounds},
		Children:   children,
	}
}

func newTestOrchestra(d *mock.Driver) (*Orchestra, *FakeClock) {
	clock := NewFakeClock(0)
	return New(OrchestraDeps{Driver: d, Clock: clock}), clock
}

func TestOrchestra_Find_ResolvesAgainstStagedTree(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			node("btn1", "Login", "0,0,100,50"),
			node("btn2", "Cancel", "0,60,100,110"),
		},
	})
	o, _ := newTestOrchestra(d)

	results, err := o.Find(context.Background(), Selector{Text: lit("Login")})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Node.Attributes[AttrID] != "btn1" {
		t.Fatalf("got id %q, want btn1", results[0].Node.Attributes[AttrID])
	}
}

func TestOrchestra_FindOne_NoMatchReturnsNil(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,100,100"))
	o, _ := newTestOrchestra(d)

	result, err := o.FindOne(context.Background(), Selector{Text: lit("nope")})
	if err != nil {
		t.Fatalf("FindOne returned error: %v", err)
	}
	if result != nil {
		t.Fatalf("got %+v, want nil", result)
	}
}

func TestOrchestra_WaitFor_ResolvesOnceMatchAppears(t *testing.T) {
	d := mock.New()
	empty := node("root", "", "0,0,100,100")
	withMatch := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Ready", "0,0,50,50")},
	}
	d.StageHierarchy(empty).StageHierarchy(empty).StageHierarchy(withMatch)
	o, _ := newTestOrchestra(d)

	results, err := o.WaitFor(context.Background(), Selector{Text: lit("Ready")}, WaitOptions{TimeoutMs: 1000, PollIntervalMs: 10})
	if err != nil {
		t.Fatalf("WaitFor returned error: %v", err)
	}
	if len(results) != 1 || results[0].Node.Attributes[AttrID] != "target" {
		t.Fatalf("got %+v, want one match on \"target\"", results)
	}

	hierarchyCalls := 0
	for _, c := range d.Calls {
		if c.Method == "Hierarchy" {
			hierarchyCalls++
		}
	}
	if hierarchyCalls != 3 {
		t.Fatalf("got %d Hierarchy calls, want 3 (two empty polls then the match)", hierarchyCalls)
	}
}

func TestOrchestra_WaitFor_TimesOutWithoutRealSleep(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,100,100"))
	o, _ := newTestOrchestra(d)

	_, err := o.WaitFor(context.Background(), Selector{Text: lit("never")}, WaitOptions{TimeoutMs: 100, PollIntervalMs: 50})
	var timeoutErr *FindTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("got error %v, want *FindTimeoutError", err)
	}
	if timeoutErr.ElapsedMs != 100 {
		t.Fatalf("ElapsedMs = %d, want 100", timeoutErr.ElapsedMs)
	}
	if timeoutErr.PollCount != 3 {
		t.Fatalf("PollCount = %d, want 3 (t=0, t=50, t=100)", timeoutErr.PollCount)
	}
}

func TestOrchestra_Tap_ResolvesSelectorAndTaps(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("btn", "Login", "150,100,250,150")},
	})
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("btn")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}
	if result.ResolvedBy != "id" {
		t.Fatalf("ResolvedBy = %q, want \"id\"", result.ResolvedBy)
	}

	if len(d.Calls) != 2 || d.Calls[0].Method != "Hierarchy" || d.Calls[1].Method != "Tap" {
		t.Fatalf("got calls %+v, want [Hierarchy, Tap]", d.Calls)
	}
	tappedAt, ok := d.Calls[1].Args[0].(wire.Point)
	if !ok {
		t.Fatalf("Tap call arg is %T, want wire.Point", d.Calls[1].Args[0])
	}
	want := wire.Point{X: 200, Y: 125}
	if tappedAt != want {
		t.Fatalf("tapped at %+v, want %+v", tappedAt, want)
	}
}

func TestOrchestra_Tap_ScrollsOffscreenElementIntoViewThenTaps(t *testing.T) {
	d := mock.New()
	offscreen := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Item", "0,2000,100,2050")},
	}
	onscreen := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Item", "0,400,100,450")},
	}
	d.StageHierarchy(offscreen).StageHierarchy(onscreen)
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}

	wantMethods := []string{"Hierarchy", "Swipe", "Hierarchy", "Tap"}
	if len(d.Calls) != len(wantMethods) {
		t.Fatalf("got %d calls %+v, want methods %v", len(d.Calls), d.Calls, wantMethods)
	}
	for i, want := range wantMethods {
		if d.Calls[i].Method != want {
			t.Fatalf("call %d = %q, want %q (full sequence %+v)", i, d.Calls[i].Method, want, d.Calls)
		}
	}
}

func TestOrchestra_EnsureVisible_ScrollSearchExhaustedFailsAction(t *testing.T) {
	d := mock.New()
	empty := node("root", "", "0,0,100,100")
	d.StageHierarchyRepeated(empty, 20)
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("never-there")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if result.Ok {
		t.Fatal("got Ok=true, want Ok=false (element never present)")
	}
	if result.Reason == "" {
		t.Fatal("expected a non-empty failure reason")
	}

	for _, c := range d.Calls {
		if c.Method == "Tap" {
			t.Fatal("driver.Tap must not be called when the element is never found")
		}
	}
}

func TestOrchestra_Tap_ObscuredElementFailsWithoutTapping(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			node("target", "Confirm", "100,100,200,150"),
			node("dialog-panel", "", "0,0,430,932"),
		},
	})
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if result.Ok {
		t.Fatal("got Ok=true, want Ok=false (target is covered by a later sibling)")
	}
	if result.Obscurer == nil {
		t.Fatal("expected Obscurer to be populated")
	}
	if result.Obscurer.ID != "dialog-panel" {
		t.Fatalf("Obscurer.ID = %q, want \"dialog-panel\"", result.Obscurer.ID)
	}

	for _, c := range d.Calls {
		if c.Method == "Tap" {
			t.Fatal("driver.Tap must not be called when the target is obscured")
		}
	}
}

func TestOrchestra_Tap_GenericContainerDoesNotCountAsObscurer(t *testing.T) {
	d := mock.New()
	wrapper := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			node("target", "Confirm", "100,100,200,150"),
			{Attributes: map[string]string{AttrRole: RoleContainer, AttrBounds: "0,0,430,932"}},
		},
	}
	d.StageHierarchy(wrapper)
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q, want Ok=true (a bare container must not count as an obscurer)", result.Reason)
	}
}

func TestOrchestra_Tap_RetriesAfterDismissingKeyboardObscurer(t *testing.T) {
	d := mock.New()
	obscuredByKeyboard := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			node("target", "Submit", "100,700,200,750"),
			{Attributes: map[string]string{AttrRole: RoleKeyboard, AttrBounds: "0,650,430,932"}},
		},
	}
	clear := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Submit", "100,700,200,750")},
	}
	d.StageHierarchy(obscuredByKeyboard).StageHierarchy(clear)
	o, _ := newTestOrchestra(d)
	o.maybeKeyboardOpen = true

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil {
		t.Fatalf("Tap returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q, want the retry after HideKeyboard to succeed", result.Reason)
	}
	if o.maybeKeyboardOpen {
		t.Fatal("maybeKeyboardOpen must be cleared once the keyboard is confirmed hidden")
	}

	sawHideKeyboard := false
	for _, c := range d.Calls {
		if c.Method == "HideKeyboard" {
			sawHideKeyboard = true
		}
	}
	if !sawHideKeyboard {
		t.Fatalf("expected a HideKeyboard call in the retry path, got %+v", d.Calls)
	}
}

func TestOrchestra_InputText_ClearsFocusesAndTypes(t *testing.T) {
	d := mock.New()
	// The field reports focus, so the focus gate resolves on its first
	// poll (one Hierarchy call between Tap and EraseText).
	d.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{focusedNode("field", "", "0,100,300,150")},
	})
	o, _ := newTestOrchestra(d)

	result, err := o.InputText(context.Background(), Selector{ID: lit("field")}, "hi", InputTextOptions{})
	if err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}
	if !o.maybeKeyboardOpen {
		t.Fatal("expected maybeKeyboardOpen to be set true after typing")
	}

	wantMethods := []string{"Hierarchy", "Tap", "Hierarchy", "EraseText", "InputText"}
	if len(d.Calls) != len(wantMethods) {
		t.Fatalf("got calls %+v, want methods %v", d.Calls, wantMethods)
	}
	for i, want := range wantMethods {
		if d.Calls[i].Method != want {
			t.Fatalf("call %d = %q, want %q (full sequence %+v)", i, d.Calls[i].Method, want, d.Calls)
		}
	}
}

func rootWithChildren(children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{AttrID: "root"}, Children: children}
}

func hasDriverCall(calls []mock.Call, method string) bool {
	for _, c := range calls {
		if c.Method == method {
			return true
		}
	}
	return false
}

func assertDriverTyped(t *testing.T, d *mock.Driver, want string) {
	t.Helper()
	for _, c := range d.Calls {
		if c.Method == "InputText" {
			if len(c.Args) == 0 || c.Args[0] != want {
				t.Fatalf("InputText args = %v, want [%q]", c.Args, want)
			}
			return
		}
	}
	t.Fatalf("expected an InputText dispatch of %q, calls: %+v", want, d.Calls)
}

func TestOrchestra_InputText_TypesImmediatelyWhenTargetAlreadyFocused(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(focusedNode("password", "", "0,200,300,250")))
	o, clock := newTestOrchestra(d)

	result, err := o.InputText(context.Background(), Selector{ID: lit("password")}, "secret", InputTextOptions{})
	if err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}
	if clock.NowMs() != 0 {
		t.Fatalf("expected no wait when the target already holds focus, clock advanced to %dms", clock.NowMs())
	}
	assertDriverTyped(t, d, "secret")
}

func TestOrchestra_InputText_WaitsForFocusToSettleThenTypes(t *testing.T) {
	d := mock.New()
	unfocused := rootWithChildren(node("password", "", "0,200,300,250"))
	focused := rootWithChildren(focusedNode("password", "", "0,200,300,250"))
	// The first tree is consumed resolving the selector into view; the
	// focus gate then polls two unfocused trees before focus finally
	// lands on the target.
	d.StageHierarchy(unfocused)
	d.StageHierarchy(unfocused)
	d.StageHierarchy(unfocused)
	d.StageHierarchy(focused)
	o, clock := newTestOrchestra(d)

	result, err := o.InputText(context.Background(), Selector{ID: lit("password")}, "secret", InputTextOptions{})
	if err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}
	if clock.NowMs() == 0 {
		t.Fatal("expected the focus gate to wait for focus to settle, but no time elapsed")
	}
	assertDriverTyped(t, d, "secret")
}

// TestOrchestra_InputText_RefusesToTypeIntoWrongFocusedField is the
// regression for the input race: the tapped target never takes focus
// because a different field still holds it, so typing would enter the
// text into that other field. InputText must return an InputFocusError
// and dispatch no text.
func TestOrchestra_InputText_RefusesToTypeIntoWrongFocusedField(t *testing.T) {
	d := mock.New()
	// email holds focus; the password target sits below it, disjoint, so
	// the focus gate never matches the target.
	d.StageHierarchy(rootWithChildren(
		focusedNode("email", "", "0,100,300,150"),
		node("password", "", "0,200,300,250"),
	))
	o, _ := newTestOrchestra(d)

	_, err := o.InputText(context.Background(), Selector{ID: lit("password")}, "secret", InputTextOptions{})
	var focusErr *InputFocusError
	if !errors.As(err, &focusErr) {
		t.Fatalf("got error %v (%T), want *InputFocusError", err, err)
	}
	if focusErr.Target != "id" {
		t.Fatalf("got Target %q, want %q", focusErr.Target, "id")
	}
	for _, c := range d.Calls {
		if c.Method == "InputText" || c.Method == "EraseText" {
			t.Fatalf("expected no text dispatch when focus never lands on the target, saw %q", c.Method)
		}
	}
	if !hasDriverCall(d.Calls, "Tap") {
		t.Fatalf("expected a Tap-to-focus before the gate, calls: %+v", d.Calls)
	}
}

func TestOrchestra_TypeText_TypesWhenAFieldIsFocused(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(focusedNode("field", "", "0,100,300,150")))
	o, _ := newTestOrchestra(d)

	if err := o.TypeText(context.Background(), "hi"); err != nil {
		t.Fatalf("TypeText returned error: %v", err)
	}
	assertDriverTyped(t, d, "hi")
}

func TestOrchestra_TypeText_RefusesWhenNothingIsReadyForInput(t *testing.T) {
	d := mock.New()
	// Nothing holds focus and no keyboard is shown, so no field is ready
	// to receive text.
	d.StageHierarchy(node("root", "", "0,0,430,932"))
	o, _ := newTestOrchestra(d)

	err := o.TypeText(context.Background(), "hi")
	var focusErr *InputFocusError
	if !errors.As(err, &focusErr) {
		t.Fatalf("got error %v (%T), want *InputFocusError", err, err)
	}
	if hasDriverCall(d.Calls, "InputText") {
		t.Fatal("expected no text dispatch when no field is ready for input")
	}
}

func TestOrchestra_HideKeyboard_ClearsMaybeKeyboardOpenHint(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)
	o.maybeKeyboardOpen = true

	result, err := o.HideKeyboard(context.Background())
	if err != nil {
		t.Fatalf("HideKeyboard returned error: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false, reason %q", result.Reason)
	}
	if o.maybeKeyboardOpen {
		t.Fatal("expected maybeKeyboardOpen to be cleared")
	}
}

func TestOrchestra_LaunchApp_ClearsMaybeKeyboardOpenHint(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)
	o.maybeKeyboardOpen = true

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp returned error: %v", err)
	}
	if o.maybeKeyboardOpen {
		t.Fatal("expected maybeKeyboardOpen to be cleared")
	}
}

func TestOrchestra_LaunchApp_MissingAppFailsFastWithoutLaunching(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.other"}}
	o, _ := newTestOrchestra(d)

	err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{})
	if err == nil {
		t.Fatal("expected an error for an app id absent from the installed list")
	}
	var coded diag.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("expected a diag.Coded error, got %v", err)
	}
	if coded.Code() != diag.CodeAppNotInstalled {
		t.Fatalf("got code %q, want %q", coded.Code(), diag.CodeAppNotInstalled)
	}
	for _, call := range d.Calls {
		if call.Method == "LaunchApp" {
			t.Fatal("expected the driver launch to be skipped for a missing app")
		}
	}
}

func TestOrchestra_LaunchApp_InstalledAppLaunchesNormally(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp returned error: %v", err)
	}
	launched := false
	for _, call := range d.Calls {
		if call.Method == "LaunchApp" {
			launched = true
		}
	}
	if !launched {
		t.Fatal("expected the driver launch to be invoked for an installed app")
	}
	if d.RunningApp == nil || *d.RunningApp != "com.example.app" {
		t.Fatal("expected the app to become the running app")
	}
}

func TestOrchestra_LaunchApp_ListAppsFailureSurfacesUnchangedWithoutLaunching(t *testing.T) {
	d := mock.New()
	d.ListAppsErr = diag.New(diag.CodeAgentUnreachable, "agent unreachable", "", nil)
	o, _ := newTestOrchestra(d)

	err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{})
	if !errors.Is(err, d.ListAppsErr) {
		t.Fatalf("expected the list-apps error to be returned unchanged, got %v", err)
	}
	for _, call := range d.Calls {
		if call.Method == "LaunchApp" {
			t.Fatal("expected the driver launch to be skipped when the installed-app query fails")
		}
	}
}

// countLaunchCalls counts how many recorded "LaunchApp" driver calls
// carry appID as their bundle id argument, so a test can assert an
// exact dedup count instead of a bare presence/absence check.
func countLaunchCalls(d *mock.Driver, appID string) int {
	count := 0
	for _, call := range d.Calls {
		if call.Method != "LaunchApp" {
			continue
		}
		if len(call.Args) > 0 {
			if bundleID, ok := call.Args[0].(string); ok && bundleID == appID {
				count++
			}
		}
	}
	return count
}

func TestOrchestra_LaunchApp_DedupsRepeatedLaunchWithinFlow(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("first LaunchApp returned error: %v", err)
	}
	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("second LaunchApp returned error: %v", err)
	}

	if got := countLaunchCalls(d, "com.example.app"); got != 1 {
		t.Fatalf("got %d driver-level launches, want exactly 1", got)
	}
}

func TestOrchestra_LaunchApp_DifferentAppIDsBothLaunch(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.one"}, {BundleID: "com.example.two"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.one", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp(one) returned error: %v", err)
	}
	if err := o.LaunchApp(context.Background(), "com.example.two", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp(two) returned error: %v", err)
	}

	if got := countLaunchCalls(d, "com.example.one"); got != 1 {
		t.Fatalf("got %d launches of com.example.one, want 1", got)
	}
	if got := countLaunchCalls(d, "com.example.two"); got != 1 {
		t.Fatalf("got %d launches of com.example.two, want 1", got)
	}
}

func TestOrchestra_ResetLaunchedApps_AllowsRelaunchOfSameApp(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("first LaunchApp returned error: %v", err)
	}
	o.ResetLaunchedApps()
	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp after reset returned error: %v", err)
	}

	if got := countLaunchCalls(d, "com.example.app"); got != 2 {
		t.Fatalf("got %d driver-level launches after reset, want 2", got)
	}
}

// TestOrchestra_LaunchApp_DoesNotSelfHealAfterCrash proves the dedup
// key is "already launched in this flow", not "currently running": a
// follow-up LaunchApp for an app that has gone away mid-flow (crashed)
// stays a no-op, so the flow's own steps fail against the missing app
// instead of a launchApp step silently relaunching it.
func TestOrchestra_LaunchApp_DoesNotSelfHealAfterCrash(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("first LaunchApp returned error: %v", err)
	}

	d.RunningApp = nil // the app crashed: it is no longer running

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("second LaunchApp returned error: %v", err)
	}

	if got := countLaunchCalls(d, "com.example.app"); got != 1 {
		t.Fatalf("got %d driver-level launches after a simulated crash, want exactly 1 (no self-heal)", got)
	}
	if d.RunningApp != nil {
		t.Fatal("expected the crashed app to stay recorded as not running, since dedup must not relaunch it")
	}
}

// TestOrchestra_LaunchApp_FreshBypassesDedupButStaysRecorded exercises
// the escape hatch: an explicit fresh launch (NoReset false) always
// force-stops and relaunches even when the app was already launched
// in this flow, and afterward the app stays recorded, so a later
// default launch does not relaunch it again.
func TestOrchestra_LaunchApp_FreshBypassesDedupButStaysRecorded(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	o, _ := newTestOrchestra(d)

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("first LaunchApp returned error: %v", err)
	}
	fresh := false
	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{NoReset: &fresh}); err != nil {
		t.Fatalf("fresh LaunchApp returned error: %v", err)
	}
	if got := countLaunchCalls(d, "com.example.app"); got != 2 {
		t.Fatalf("got %d driver-level launches, want 2 (fresh must bypass dedup)", got)
	}

	if err := o.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("third LaunchApp returned error: %v", err)
	}
	if got := countLaunchCalls(d, "com.example.app"); got != 2 {
		t.Fatalf("got %d driver-level launches, want still 2 (fresh must leave the app recorded)", got)
	}
}

func TestOrchestra_EraseText_ErrorsWhenUnsupported(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanEraseText: false})
	o, _ := newTestOrchestra(d)

	if err := o.EraseText(context.Background(), 3); err == nil {
		t.Fatal("expected an error when the driver does not support eraseText")
	}
}

func TestOrchestra_ResolvePoint_ErrorsOnFailure(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,100,100"))
	o, _ := newTestOrchestra(d)

	if _, err := o.ResolvePoint(context.Background(), Selector{ID: lit("missing")}); err == nil {
		t.Fatal("expected an error when the selector does not resolve")
	}
}

func TestOrchestra_SwipeDirection_ComputesCenteredEndpoints(t *testing.T) {
	d := mock.New()
	d.Screen = wire.Size{Width: 400, Height: 800}
	o, _ := newTestOrchestra(d)

	if err := o.SwipeDirection(context.Background(), Up, SwipeDirectionOptions{}); err != nil {
		t.Fatalf("SwipeDirection returned error: %v", err)
	}
	if len(d.Calls) != 1 || d.Calls[0].Method != "Swipe" {
		t.Fatalf("got calls %+v, want a single Swipe", d.Calls)
	}
	from := d.Calls[0].Args[0].(wire.Point)
	to := d.Calls[0].Args[1].(wire.Point)
	if from.X != 200 || to.X != 200 {
		t.Fatalf("expected a vertical swipe centered at x=200, got from=%+v to=%+v", from, to)
	}
	if !(from.Y > to.Y) {
		t.Fatalf("swiping up must drag from a lower Y to a higher Y up the screen, got from=%+v to=%+v", from, to)
	}
}
