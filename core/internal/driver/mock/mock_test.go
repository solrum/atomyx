package mock

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

func TestDefaultCapabilities(t *testing.T) {
	d := New()
	got := d.Capabilities()
	want := driver.Capabilities{
		CanScreenshot:     true,
		CanEraseText:      true,
		CanWaitForIdle:    false,
		CanSetLocation:    false,
		CanSetOrientation: true,
		CanHideKeyboard:   true,
		CanMultiPointer:   false,
		CanPressure:       false,
		SupportedKeyCodes: []driver.KeyCode{driver.KeyBack, driver.KeyHome, driver.KeyEnter},
	}
	if got.CanScreenshot != want.CanScreenshot ||
		got.CanEraseText != want.CanEraseText ||
		got.CanWaitForIdle != want.CanWaitForIdle ||
		got.CanSetLocation != want.CanSetLocation ||
		got.CanSetOrientation != want.CanSetOrientation ||
		got.CanHideKeyboard != want.CanHideKeyboard ||
		got.CanMultiPointer != want.CanMultiPointer ||
		got.CanPressure != want.CanPressure {
		t.Fatalf("Capabilities() = %+v, want %+v", got, want)
	}
	if len(got.SupportedKeyCodes) != len(want.SupportedKeyCodes) {
		t.Fatalf("SupportedKeyCodes = %v, want %v", got.SupportedKeyCodes, want.SupportedKeyCodes)
	}
	for i, k := range want.SupportedKeyCodes {
		if got.SupportedKeyCodes[i] != k {
			t.Fatalf("SupportedKeyCodes[%d] = %q, want %q", i, got.SupportedKeyCodes[i], k)
		}
	}
}

func TestSetCapabilitiesOverride(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanWaitForIdle: true})
	if !d.Capabilities().CanWaitForIdle {
		t.Fatal("SetCapabilities did not take effect")
	}
}

func TestStageHierarchySequenceIsSticky(t *testing.T) {
	d := New()
	ctx := context.Background()
	tree1 := wire.TreeNodeWire{Attributes: map[string]string{"id": "one"}}
	tree2 := wire.TreeNodeWire{Attributes: map[string]string{"id": "two"}}
	d.StageHierarchy(tree1).StageHierarchy(tree2)

	got1, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("first Hierarchy: %v", err)
	}
	if got1.Attributes["id"] != "one" {
		t.Fatalf("first Hierarchy = %+v, want tree1", got1)
	}

	got2, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("second Hierarchy: %v", err)
	}
	if got2.Attributes["id"] != "two" {
		t.Fatalf("second Hierarchy = %+v, want tree2", got2)
	}

	got3, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("third Hierarchy: %v", err)
	}
	if got3.Attributes["id"] != "two" {
		t.Fatalf("third Hierarchy = %+v, want tree2 repeated (sticky final state)", got3)
	}
}

func TestHierarchyWithNothingStagedErrors(t *testing.T) {
	d := New()
	if _, err := d.Hierarchy(context.Background()); err == nil {
		t.Fatal("Hierarchy with nothing staged returned no error")
	}
}

func TestHangOnNextHierarchyRespectsCancellation(t *testing.T) {
	d := New()
	d.HangOnNextHierarchy = true

	// A deadline, rather than an immediate cancel, gives the
	// goroutine below room to reach its blocking wait before the
	// context ends, so the test exercises the hang-then-cancel path
	// instead of the already-canceled fast path Hierarchy also
	// honors.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := d.Hierarchy(ctx)
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Hierarchy() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hierarchy did not return after its context's deadline passed")
	}

	if d.HangOnNextHierarchy {
		t.Fatal("HangOnNextHierarchy did not reset after being consumed")
	}
	if len(d.Calls) != 1 || d.Calls[0].Method != "Hierarchy" {
		t.Fatalf("Calls = %+v, want a single recorded Hierarchy call", d.Calls)
	}
}

func TestLaunchAppPlainRecordsUnconditionally(t *testing.T) {
	d := New()
	ctx := context.Background()
	if err := d.LaunchApp(ctx, "a", driver.LaunchArgs{}); err != nil {
		t.Fatalf("first LaunchApp: %v", err)
	}
	if err := d.LaunchApp(ctx, "b", driver.LaunchArgs{}); err != nil {
		t.Fatalf("second LaunchApp: %v", err)
	}
	if len(d.Calls) != 2 || d.Calls[0].Method != "LaunchApp" || d.Calls[1].Method != "LaunchApp" {
		t.Fatalf("Calls = %+v, want two unconditional LaunchApp entries", d.Calls)
	}
}

func TestLaunchAppFreshStopsTargetFirst(t *testing.T) {
	d := New()
	ctx := context.Background()
	noReset := false
	if err := d.LaunchApp(ctx, "a", driver.LaunchArgs{NoReset: &noReset}); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	if len(d.Calls) != 2 {
		t.Fatalf("Calls = %+v, want [StopApp, LaunchApp]", d.Calls)
	}
	if d.Calls[0].Method != "StopApp" || d.Calls[0].Args[0] != "a" {
		t.Fatalf("Calls[0] = %+v, want StopApp(\"a\")", d.Calls[0])
	}
	if d.Calls[1].Method != "LaunchApp" {
		t.Fatalf("Calls[1] = %+v, want LaunchApp", d.Calls[1])
	}
}

func TestLaunchAppAttachSkipsWhenAlreadyForeground(t *testing.T) {
	d := New()
	ctx := context.Background()
	if err := d.LaunchApp(ctx, "a", driver.LaunchArgs{}); err != nil {
		t.Fatalf("plain LaunchApp: %v", err)
	}
	d.ClearCalls()

	attach := true
	if err := d.LaunchApp(ctx, "a", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp: %v", err)
	}
	if len(d.Calls) != 0 {
		t.Fatalf("Calls = %+v, want no entry for a reused attach", d.Calls)
	}
}

func TestLaunchAppAttachFallsBackWhenNotForeground(t *testing.T) {
	d := New()
	ctx := context.Background()
	attach := true
	if err := d.LaunchApp(ctx, "a", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp: %v", err)
	}
	if len(d.Calls) != 1 || d.Calls[0].Method != "LaunchApp" {
		t.Fatalf("Calls = %+v, want a fallback plain LaunchApp entry", d.Calls)
	}
	foreground, err := d.CurrentForeground(ctx)
	if err != nil {
		t.Fatalf("CurrentForeground: %v", err)
	}
	if foreground.BundleID == nil || *foreground.BundleID != "a" {
		t.Fatalf("CurrentForeground = %+v, want bundleId \"a\"", foreground)
	}
}

func TestOnSwipeHook(t *testing.T) {
	d := New()
	var got [2]wire.Point
	d.OnSwipe = func(from, to wire.Point) { got = [2]wire.Point{from, to} }

	from := wire.Point{X: 1, Y: 2}
	to := wire.Point{X: 3, Y: 4}
	if err := d.Swipe(context.Background(), from, to, 100); err != nil {
		t.Fatalf("Swipe: %v", err)
	}
	if got != [2]wire.Point{from, to} {
		t.Fatalf("OnSwipe received %+v, want [%+v %+v]", got, from, to)
	}
}

func TestOnHideKeyboardHook(t *testing.T) {
	d := New()
	d.OnHideKeyboard = func() *driver.KeyResult {
		return &driver.KeyResult{Ok: false, Reason: "no keyboard"}
	}
	result, err := d.HideKeyboard(context.Background())
	if err != nil {
		t.Fatalf("HideKeyboard: %v", err)
	}
	if result.Ok || result.Reason != "no keyboard" {
		t.Fatalf("HideKeyboard() = %+v, want the OnHideKeyboard override", result)
	}
}

func TestClearCallsLeavesHierarchyQueueIntact(t *testing.T) {
	d := New()
	ctx := context.Background()
	tree1 := wire.TreeNodeWire{Attributes: map[string]string{"id": "one"}}
	tree2 := wire.TreeNodeWire{Attributes: map[string]string{"id": "two"}}
	d.StageHierarchy(tree1).StageHierarchy(tree2)

	if _, err := d.Hierarchy(ctx); err != nil {
		t.Fatalf("first Hierarchy: %v", err)
	}
	d.ClearCalls()
	if len(d.Calls) != 0 {
		t.Fatalf("Calls after ClearCalls = %+v, want empty", d.Calls)
	}

	got, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("second Hierarchy: %v", err)
	}
	if got.Attributes["id"] != "two" {
		t.Fatalf("Hierarchy after ClearCalls = %+v, want tree2 (queue unaffected)", got)
	}
	if len(d.Calls) != 1 || d.Calls[0].Method != "Hierarchy" {
		t.Fatalf("Calls = %+v, want a single recorded Hierarchy call", d.Calls)
	}
}

func TestDispatchGesture_RejectsMultiPointerWithoutCapability(t *testing.T) {
	d := New()
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{validTapPointer("a"), validTapPointer("b")}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a multi-pointer rejection")
	}
}

func TestDispatchGesture_AcceptsMultiPointerWithCapability(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{validTapPointer("a"), validTapPointer("b")}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err != nil {
		t.Fatalf("DispatchGesture() error = %v, want the multi-pointer gesture accepted", err)
	}
}

func TestDispatchGesture_RejectsPressureWithoutCapability(t *testing.T) {
	d := New()
	pressure := 0.5
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Pressure: &pressure}, {Phase: driver.GesturePhaseUp, Pressure: &pressure}}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a pressure rejection")
	}
}

func TestDispatchGesture_AcceptsPressureWithCapability(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanPressure: true})
	pressure := 0.5
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Pressure: &pressure}, {Phase: driver.GesturePhaseUp, Pressure: &pressure}}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err != nil {
		t.Fatalf("DispatchGesture() error = %v, want the pressure waypoint accepted", err)
	}
}

// TestDispatchGesture_CaptureReturnsSyntheticImage asserts Mock's own
// capture support: a gesture with a capture waypoint succeeds and
// returns one non-empty PNG-encoded image, so a caller composing the
// pointer command's gesture path over Mock can rely on a real,
// decodable payload with no device attached.
func TestDispatchGesture_CaptureReturnsSyntheticImage(t *testing.T) {
	d := New()
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "single", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseCapture, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 1}},
		}},
	}}

	result, err := d.DispatchGesture(context.Background(), gesture)
	if err != nil {
		t.Fatalf("DispatchGesture() error = %v, want the capture waypoint accepted", err)
	}
	if len(result.CapturedImages) != 1 {
		t.Fatalf("CapturedImages = %+v, want exactly one entry", result.CapturedImages)
	}
	img := result.CapturedImages[0]
	if len(img.Data) == 0 {
		t.Fatal("CapturedImages[0].Data is empty, want a non-empty synthetic image")
	}
	if img.MimeType != "image/png" {
		t.Fatalf("CapturedImages[0].MimeType = %q, want \"image/png\"", img.MimeType)
	}
	if _, decodeErr := png.Decode(bytes.NewReader(img.Data)); decodeErr != nil {
		t.Fatalf("CapturedImages[0].Data does not decode as PNG: %v", decodeErr)
	}
}

// TestDispatchGesture_NoCaptureReturnsNoImages asserts a gesture with
// no capture waypoint is unaffected by capture support: it dispatches
// exactly as before and returns an empty CapturedImages.
func TestDispatchGesture_NoCaptureReturnsNoImages(t *testing.T) {
	d := New()
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "single", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 1}},
		}},
	}}

	result, err := d.DispatchGesture(context.Background(), gesture)
	if err != nil {
		t.Fatalf("DispatchGesture() error = %v", err)
	}
	if len(result.CapturedImages) != 0 {
		t.Fatalf("CapturedImages = %+v, want none for a gesture with no capture waypoint", result.CapturedImages)
	}
}

func TestEraseText_RejectsWithoutCapability(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanEraseText: false})

	if err := d.EraseText(context.Background(), 1); err == nil {
		t.Fatal("EraseText() error = nil, want a rejection when CanEraseText is false")
	}
}

func TestEraseText_AcceptsWithCapability(t *testing.T) {
	d := New()

	if err := d.EraseText(context.Background(), 1); err != nil {
		t.Fatalf("EraseText() error = %v, want the default CanEraseText true to accept the call", err)
	}
}

func TestHideKeyboard_RejectsWithoutCapability(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanHideKeyboard: false})

	if _, err := d.HideKeyboard(context.Background()); err == nil {
		t.Fatal("HideKeyboard() error = nil, want a rejection when CanHideKeyboard is false")
	}
}

func TestHideKeyboard_AcceptsWithCapability(t *testing.T) {
	d := New()

	if _, err := d.HideKeyboard(context.Background()); err != nil {
		t.Fatalf("HideKeyboard() error = %v, want the default CanHideKeyboard true to accept the call", err)
	}
}

func TestPressKey_RejectsCodeNotInSupportedKeyCodes(t *testing.T) {
	d := New()

	if _, err := d.PressKey(context.Background(), driver.KeyCode("unsupported")); err == nil {
		t.Fatal("PressKey() error = nil, want a rejection for a code outside SupportedKeyCodes")
	}
}

func TestPressKey_AcceptsSupportedCode(t *testing.T) {
	d := New()

	if _, err := d.PressKey(context.Background(), driver.KeyBack); err != nil {
		t.Fatalf("PressKey() error = %v, want KeyBack accepted as a default supported code", err)
	}
}

func TestScreenshot_DefaultsToEmptyPayload(t *testing.T) {
	d := New()

	got, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Screenshot() = %v, want an empty payload by default", got)
	}
}

func TestScreenshot_DataOverrideTakesPrecedence(t *testing.T) {
	d := New()
	d.ScreenshotData = []byte("not-a-png")

	got, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	if string(got) != "not-a-png" {
		t.Fatalf("Screenshot() = %q, want the ScreenshotData override", got)
	}
}

func TestInstallUninstallAndClearAppData(t *testing.T) {
	d := New()
	ctx := context.Background()

	if err := d.InstallApp(ctx, "a"); err != nil {
		t.Fatalf("InstallApp: %v", err)
	}
	installed, err := d.IsAppInstalled(ctx, "a")
	if err != nil {
		t.Fatalf("IsAppInstalled: %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled after InstallApp reported false")
	}

	if err := d.ClearAppData(ctx, "a", driver.ClearAppDataOptions{Source: "artifact.ipa"}); err != nil {
		t.Fatalf("ClearAppData: %v", err)
	}
	if len(d.ClearedApps) != 1 || d.ClearedApps[0] != "a" {
		t.Fatalf("ClearedApps = %v, want [\"a\"]", d.ClearedApps)
	}

	if err := d.UninstallApp(ctx, "a"); err != nil {
		t.Fatalf("UninstallApp: %v", err)
	}
	installed, err = d.IsAppInstalled(ctx, "a")
	if err != nil {
		t.Fatalf("IsAppInstalled: %v", err)
	}
	if installed {
		t.Fatal("IsAppInstalled after UninstallApp reported true")
	}
}

func TestAgentInfo_ReportsTheConfiguredValueWithoutRecordingACall(t *testing.T) {
	d := New()
	if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
		t.Fatalf("AgentInfo() on a fresh mock = %+v, want the zero value", got)
	}
	want := driver.AgentInfo{ProtocolVersion: 3, AgentVersion: "1.2.3"}
	d.Agent = want
	if got := d.AgentInfo(); got != want {
		t.Fatalf("AgentInfo() = %+v, want %+v", got, want)
	}
	if len(d.Calls) != 0 {
		t.Fatalf("AgentInfo() recorded %d calls, want none", len(d.Calls))
	}
}

func TestForeignAutomationReportsStagedSessions(t *testing.T) {
	d := New()
	if got := d.ForeignAutomation(); got != nil {
		t.Fatalf("ForeignAutomation() = %v, want nil", got)
	}
	want := driver.ForeignAutomationSession{Name: "com.example.UITests.xctrunner", Kind: "XCUITest runner"}
	d.SetForeignAutomation(want)
	reporter, ok := driver.AsForeignAutomationReporter(d)
	if !ok {
		t.Fatal("mock driver does not implement ForeignAutomationReporter")
	}
	got := reporter.ForeignAutomation()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ForeignAutomation() = %v, want [%v]", got, want)
	}
	d.SetForeignAutomation()
	if got := d.ForeignAutomation(); got != nil {
		t.Fatalf("after clearing, ForeignAutomation() = %v, want nil", got)
	}
}

func validTapPointer(id string) driver.GesturePointer {
	return driver.GesturePointer{ID: id, Waypoints: []driver.GestureWaypoint{
		{Phase: driver.GesturePhaseDown},
		{Phase: driver.GesturePhaseUp, AtOffsetSeconds: 0.05},
	}}
}

func TestLaunchAppRecordsArguments(t *testing.T) {
	d := New()
	args := driver.LaunchArgs{Arguments: map[string]string{"route": "/focus"}}
	if err := d.LaunchApp(context.Background(), "a", args); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	if len(d.Calls) != 1 {
		t.Fatalf("Calls = %+v, want one LaunchApp entry", d.Calls)
	}
	recorded, ok := d.Calls[0].Args[1].(driver.LaunchArgs)
	if !ok || recorded.Arguments["route"] != "/focus" {
		t.Fatalf("recorded args = %#v, want the launch arguments", d.Calls[0].Args)
	}
}

func TestOpenLink_RecordsTheLinkAndTheApp(t *testing.T) {
	d := New()
	if err := d.OpenLink(context.Background(), "myapp://home", driver.OpenLinkOptions{AppID: "com.example.app"}); err != nil {
		t.Fatalf("OpenLink() error = %v", err)
	}
	if len(d.OpenedLinks) != 1 || d.OpenedLinks[0] != "myapp://home" {
		t.Fatalf("OpenedLinks = %v, want [myapp://home]", d.OpenedLinks)
	}
	last := d.Calls[len(d.Calls)-1]
	if last.Method != "OpenLink" || len(last.Args) != 2 || last.Args[1] != "com.example.app" {
		t.Fatalf("last call = %+v, want OpenLink with the app id", last)
	}
}

func TestOpenLink_FailsAsUnsupportedWhenTheCapabilityIsOff(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanOpenLink: false})

	err := d.OpenLink(context.Background(), "myapp://home", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeLinkUnsupported {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeLinkUnsupported)
	}
	if len(d.OpenedLinks) != 0 {
		t.Fatalf("OpenedLinks = %v, want none", d.OpenedLinks)
	}
}

func TestOpenLink_RejectsAnInvalidLinkAndSurfacesTheInjectedError(t *testing.T) {
	d := New()
	if err := d.OpenLink(context.Background(), "no scheme", driver.OpenLinkOptions{}); err == nil {
		t.Fatal("OpenLink() accepted a link without a scheme")
	}
	d.OpenLinkErr = errors.New("injected")
	if err := d.OpenLink(context.Background(), "myapp://home", driver.OpenLinkOptions{}); !errors.Is(err, d.OpenLinkErr) {
		t.Fatalf("OpenLink() error = %v, want the injected error", err)
	}
}

func TestSetPermissions_RecordsTheAppAndPermissions(t *testing.T) {
	d := New()
	permissions := map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow}
	if err := d.SetPermissions(context.Background(), "com.example.app", permissions); err != nil {
		t.Fatalf("SetPermissions() error = %v", err)
	}
	permissions[driver.PermissionCamera] = driver.PermissionDeny
	if len(d.PermissionChanges) != 1 || d.PermissionChanges[0].AppID != "com.example.app" ||
		d.PermissionChanges[0].Permissions[driver.PermissionCamera] != driver.PermissionAllow {
		t.Fatalf("PermissionChanges = %+v, want one snapshot of the allow request", d.PermissionChanges)
	}
	if last := d.Calls[len(d.Calls)-1]; last.Method != "SetPermissions" || last.Args[0] != "com.example.app" {
		t.Fatalf("last call = %+v, want SetPermissions for the app", last)
	}
}

func TestSetPermissions_FailsAsUnsupportedWhenTheCapabilityIsOff(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanSetPermissions: false})

	err := d.SetPermissions(context.Background(), "com.example.app", map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodePermissionsUnsupported {
		t.Fatalf("SetPermissions() code = %q (err %v), want %q", code, err, diag.CodePermissionsUnsupported)
	}
	if len(d.PermissionChanges) != 0 {
		t.Fatalf("PermissionChanges = %v, want none", d.PermissionChanges)
	}
}

func TestSetPermissions_RejectsInvalidInputAndSurfacesTheInjectedError(t *testing.T) {
	d := New()
	if err := d.SetPermissions(context.Background(), "com.example.app", nil); err == nil {
		t.Fatal("SetPermissions() accepted an empty map")
	}
	d.SetPermissionsErr = errors.New("injected")
	err := d.SetPermissions(context.Background(), "com.example.app", map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if !errors.Is(err, d.SetPermissionsErr) {
		t.Fatalf("SetPermissions() error = %v, want the injected error", err)
	}
}

func TestSetOrientation_RecordsTheOrientationAndTurnsTheScreen(t *testing.T) {
	d := New()
	ctx := context.Background()
	portrait := d.Screen

	if err := d.SetOrientation(ctx, driver.OrientationLandscapeLeft); err != nil {
		t.Fatalf("SetOrientation(landscapeLeft) error = %v", err)
	}
	size, _ := d.ScreenSize(ctx)
	if size.Width != portrait.Height || size.Height != portrait.Width {
		t.Fatalf("ScreenSize() = %+v after landscape, want the portrait size turned", size)
	}
	if err := d.SetOrientation(ctx, driver.OrientationLandscapeRight); err != nil {
		t.Fatalf("SetOrientation(landscapeRight) error = %v", err)
	}
	if size, _ = d.ScreenSize(ctx); size.Width <= size.Height {
		t.Fatalf("ScreenSize() = %+v after a second landscape, want it unchanged", size)
	}
	if err := d.SetOrientation(ctx, driver.OrientationPortrait); err != nil {
		t.Fatalf("SetOrientation(portrait) error = %v", err)
	}
	if size, _ = d.ScreenSize(ctx); size != portrait {
		t.Fatalf("ScreenSize() = %+v after portrait, want %+v", size, portrait)
	}
	want := []driver.Orientation{driver.OrientationLandscapeLeft, driver.OrientationLandscapeRight, driver.OrientationPortrait}
	if len(d.Orientations) != len(want) {
		t.Fatalf("Orientations = %v, want %v", d.Orientations, want)
	}
	for i := range want {
		if d.Orientations[i] != want[i] {
			t.Fatalf("Orientations = %v, want %v", d.Orientations, want)
		}
	}
	if last := d.Calls[len(d.Calls)-1]; last.Method != "SetOrientation" || last.Args[0] != driver.OrientationPortrait {
		t.Fatalf("last call = %+v, want SetOrientation to portrait", last)
	}
}

func TestSetOrientation_FailsAsUnsupportedWhenTheCapabilityIsOff(t *testing.T) {
	d := New()
	d.SetCapabilities(driver.Capabilities{CanSetOrientation: false})

	err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft)
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationUnsupported {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeOrientationUnsupported)
	}
	if len(d.Orientations) != 0 {
		t.Fatalf("Orientations = %v, want none", d.Orientations)
	}
}

func TestSetOrientation_RejectsInvalidInputAndSurfacesTheInjectedError(t *testing.T) {
	d := New()
	err := d.SetOrientation(context.Background(), "sideways")
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("SetOrientation(sideways) code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	d.SetOrientationErr = errors.New("injected")
	if err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft); !errors.Is(err, d.SetOrientationErr) {
		t.Fatalf("SetOrientation() error = %v, want the injected error", err)
	}
}
