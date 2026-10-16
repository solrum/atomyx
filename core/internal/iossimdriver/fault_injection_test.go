package iossimdriver_test

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
)

// hidFaultInjectionDispatchTimeout shortens the HID client's dispatch
// timeout so a hung fake HID endpoint fails fast in this test instead
// of waiting out the production defaultDispatchTimeout.
const hidFaultInjectionDispatchTimeout = 30 * time.Millisecond

// hidFaultInjectionBudget is the outer bound this test holds a touch
// method call to: generous enough that scheduling noise never trips
// it, but far short of a real hang, so a method that regresses to
// blocking indefinitely fails the test instead of hanging it.
const hidFaultInjectionBudget = 2 * time.Second

// TestIosSimDriver_Tap_HidTimeout_SurfacesTypedTimeout proves that a
// hung HID helper connection, one that accepts the WebSocket
// connection and a dispatch but never answers it, causes Tap to
// return within hidFaultInjectionBudget with a diag.CodeToolTimeout
// diagnostic rather than hanging.
func TestIosSimDriver_Tap_HidTimeout_SurfacesTypedTimeout(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	srv.setRespond(func(map[string]any) map[string]any { return nil }) // accepted, never answered.
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options:            iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort:            1,
		HidDispatchTimeout: hidFaultInjectionDispatchTimeout,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	start := time.Now()
	err = d.Tap(context.Background(), wireBuildPoint(195, 422))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Tap() against a hung HID helper returned no error")
	}
	if elapsed > hidFaultInjectionBudget {
		t.Fatalf("Tap() took %v against a hung HID helper, want it bounded by %v rather than hanging",
			elapsed, hidFaultInjectionBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("Tap() against a hung HID helper: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeToolTimeout, err)
	}
}

// forbiddenTransportSubstrings are the shapes a leaked wire-call
// detail (a bridge protocol name, an address, or a port) takes. A
// tool-facing IosSimDriver error must never contain one of these,
// regardless of which method produced it or why the underlying
// dispatch failed.
var forbiddenTransportSubstrings = []string{
	"tcp", "127.0.0.1", "localhost", ":22087", "dial", "iproxy", "websocket",
	"http", "GET ", "POST ", ":8765", "/tree", "/actions",
}

// hidDispatchMethodCases enumerates every IosSimDriver method that
// dispatches a touch event directly over the HID connection instead
// of through the embedded IosDriver delegate: the three
// driver.Driver touch primitives IosSimDriver overrides, plus the
// three StreamingTouchCapable phases the same HID path serves. Only
// the first three are part of driver.Driver, so
// TestIosSimDriver_BoundaryTranslation_CoversEveryDriverMethod checks
// coverage against that subset; TestIosSimDriver_BoundaryTranslation_EveryWireCallMethod
// drives all six against the same two faults.
var hidDispatchMethodCases = []struct {
	name         string
	onDriverPort bool
	call         func(ctx context.Context, d *iossimdriver.IosSimDriver) error
}{
	{"Tap", true, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.Tap(ctx, wireBuildPoint(1, 1))
	}},
	{"LongPress", true, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.LongPress(ctx, wireBuildPoint(1, 1), 100)
	}},
	{"Swipe", true, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.Swipe(ctx, wireBuildPoint(0, 0), wireBuildPoint(1, 1), 100)
	}},
	{"StreamingTouchDown", false, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.StreamingTouchDown(ctx, wireBuildPoint(1, 1), 0)
	}},
	{"StreamingTouchMove", false, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.StreamingTouchMove(ctx, wireBuildPoint(1, 1), 0)
	}},
	{"StreamingTouchUp", false, func(ctx context.Context, d *iossimdriver.IosSimDriver) error {
		return d.StreamingTouchUp(ctx, wireBuildPoint(1, 1), 0)
	}},
}

// boundaryTranslationSkipped names every driver.Driver method absent
// from hidDispatchMethodCases's driver.Driver subset, with the reason
// TestIosSimDriver_BoundaryTranslation_EveryWireCallMethod does not
// exercise it directly. TestIosSimDriver_BoundaryTranslation_CoversEveryDriverMethod
// fails when a method sits in neither this map nor that subset, so a
// port method that starts routing through HID cannot silently skip
// this classification.
var boundaryTranslationSkipped = map[string]string{
	"Platform":     "returns a static string, never an error",
	"Capabilities": "returns a struct, never an error",
	"IsConnected":  "returns a bool, never an error",
	"AgentInfo":    "returns the delegate's stored value, never an error and never issues a wire call",
	"Disconnect":   "best-effort by contract, never returns a non-nil error",
	"Ping": "delegates entirely to the embedded IosDriver, whose own fail boundary " +
		"already classifies this failure; see iosdriver's boundary translation gate",
	"Hierarchy":         "delegates entirely to the embedded IosDriver's own fail boundary",
	"WaitForIdle":       "delegates entirely to the embedded IosDriver, never issues a wire call",
	"DispatchGesture":   "delegates entirely to the embedded IosDriver's own fail boundary",
	"InputText":         "delegates entirely to the embedded IosDriver's own fail boundary",
	"EraseText":         "delegates entirely to the embedded IosDriver's own fail boundary",
	"PressKey":          "delegates entirely to the embedded IosDriver's own fail boundary",
	"HideKeyboard":      "delegates entirely to the embedded IosDriver's own fail boundary",
	"LaunchApp":         "delegates entirely to the embedded IosDriver's own fail boundary",
	"OpenLink":          "delegates entirely to the embedded IosDriver's own fail boundary",
	"SetPermissions":    "delegates entirely to the embedded IosDriver's own fail boundary",
	"SetOrientation":    "delegates entirely to the embedded IosDriver's own fail boundary",
	"SetLocation":       "delegates entirely to the embedded IosDriver's own fail boundary",
	"StopApp":           "delegates entirely to the embedded IosDriver's own fail boundary",
	"KillApp":           "delegates entirely to the embedded IosDriver's own fail boundary",
	"CurrentForeground": "delegates entirely to the embedded IosDriver's own fail boundary",
	"ListApps":          "delegates entirely to the embedded IosDriver's own fail boundary",
	"InstallApp":        "delegates entirely to the embedded IosDriver's own fail boundary",
	"UninstallApp":      "delegates entirely to the embedded IosDriver's own fail boundary",
	"ClearAppData":      "delegates entirely to the embedded IosDriver's own fail boundary",
	"IsAppInstalled":    "delegates entirely to the embedded IosDriver's own fail boundary",
	"Screenshot":        "delegates entirely to the embedded IosDriver's own fail boundary",
	"DeviceInfo":        "delegates entirely to the embedded IosDriver's own fail boundary",
	"ScreenSize":        "delegates entirely to the embedded IosDriver's own fail boundary",
	"Connect": "its delegate half is already covered by iosdriver's own gate; its HID-dial " +
		"half is checked directly by TestIosSimDriver_Connect_HidRejected_SurfacesTypedRejection",
}

// TestIosSimDriver_BoundaryTranslation_CoversEveryDriverMethod proves
// that hidDispatchMethodCases's driver.Driver subset plus
// boundaryTranslationSkipped account for every method on
// driver.Driver, so a method added to the port, or one that starts
// routing through HID, cannot silently escape the boundary-translation
// classification TestIosSimDriver_BoundaryTranslation_EveryWireCallMethod
// checks.
func TestIosSimDriver_BoundaryTranslation_CoversEveryDriverMethod(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range hidDispatchMethodCases {
		if tc.onDriverPort {
			covered[tc.name] = true
		}
	}
	for name := range boundaryTranslationSkipped {
		covered[name] = true
	}

	driverType := reflect.TypeOf((*driver.Driver)(nil)).Elem()
	for i := 0; i < driverType.NumMethod(); i++ {
		name := driverType.Method(i).Name
		if !covered[name] {
			t.Errorf("driver.Driver method %s is covered by neither hidDispatchMethodCases nor boundaryTranslationSkipped; add it to one", name)
		}
	}
}

// deadHidSimDriver builds an IosSimDriver whose HID connection was
// never connected, so every touch dispatch it issues fails
// immediately with HidClientErrNotConnected, the same classification
// a helper that refuses every connection attempt produces. The
// embedded delegate is wired to a healthy fakeConn so normalize's
// ScreenSize lookup succeeds before the HID dispatch itself is
// exercised; the dialer is never invoked because Connect is never
// called.
func deadHidSimDriver(t *testing.T) *iossimdriver.IosSimDriver {
	t.Helper()
	d, err := iossimdriver.New(newDefaultFakeConn(), process.NewFake(), &fakeDialer{}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return d
}

// rejectingHidSimDriver builds an IosSimDriver connected to a fake HID
// helper that answers every dispatch with ok:false, so every touch
// dispatch it issues completes but is reported as an agent rejection
// rather than a connectivity failure.
func rejectingHidSimDriver(t *testing.T) *iossimdriver.IosSimDriver {
	t.Helper()
	return rejectingHidSimDriverWithMessage(t, "the requested action failed")
}

// rejectingHidSimDriverWithMessage builds an IosSimDriver connected to
// a fake HID helper that answers every dispatch with ok:false and
// message as the reported error text, standing in for a helper whose
// own rejection text names a route or a private symbol.
func rejectingHidSimDriverWithMessage(t *testing.T, message string) *iossimdriver.IosSimDriver {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	srv.setRespond(func(msg map[string]any) map[string]any {
		return map[string]any{"ok": false, "id": msg["id"], "error": message}
	})
	go srv.run()
	t.Cleanup(func() { _ = clientConn.Close() })

	d, err := iossimdriver.New(newDefaultFakeConn(), process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() against a rejecting HID helper: %v", err)
	}
	return d
}

// assertNoForbiddenTransportSubstring fails t when message contains
// any shape of a leaked bridge protocol name, address, or port,
// case-insensitively.
func assertNoForbiddenTransportSubstring(t *testing.T, methodName, message string) {
	t.Helper()
	lower := strings.ToLower(message)
	for _, forbidden := range forbiddenTransportSubstrings {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("%s error %q contains forbidden substring %q", methodName, message, forbidden)
		}
	}
}

// TestIosSimDriver_BoundaryTranslation_EveryWireCallMethod is the
// enforcement gate for the boundary IosSimDriver.failHid implements:
// driven with minimal valid arguments, every method in
// hidDispatchMethodCases must return an error whose string names no
// bridge protocol, address, or port, whether the HID helper could not
// be reached at all (a connection that was never established) or was
// reached and reported the dispatch itself failed (a helper that
// rejects every command).
func TestIosSimDriver_BoundaryTranslation_EveryWireCallMethod(t *testing.T) {
	t.Run("dead endpoint", func(t *testing.T) {
		for _, tc := range hidDispatchMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := deadHidSimDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against a dead HID endpoint returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
					t.Fatalf("%s against a dead HID endpoint: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentUnreachable, err)
				}
			})
		}
	})

	t.Run("agent rejection", func(t *testing.T) {
		for _, tc := range hidDispatchMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := rejectingHidSimDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against a HID helper that rejects every command returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentRejected {
					t.Fatalf("%s against a HID helper that rejects every command: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentRejected, err)
				}
			})
		}
	})
}

// TestIosSimDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage
// proves that when the HID helper's own rejection text names a
// dot-qualified Swift error type, failHid neutralizes it before it
// reaches the caller instead of passing the helper's message through
// unchanged, even though the helper is the party that should never
// have produced that shape in the first place.
func TestIosSimDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage(t *testing.T) {
	const leaking = `tap dispatch failed: SynthesizerError.dispatchFailed(reason: "x")`
	d := rejectingHidSimDriverWithMessage(t, leaking)

	err := d.Tap(context.Background(), wireBuildPoint(1, 1))
	if err == nil {
		t.Fatal("Tap() against a HID helper reporting a leaking rejection message returned no error")
	}
	code, message, _ := diag.Describe(err)
	if code != diag.CodeAgentRejected {
		t.Fatalf("Tap() against a HID helper reporting a leaking rejection message: diag.Describe code = %q, want %q", code, diag.CodeAgentRejected)
	}
	if strings.Contains(message, "SynthesizerError") {
		t.Fatalf("Tap() against a HID helper reporting a leaking rejection message: message = %q, want the error type stripped", message)
	}
}

// erroringDialer fails every Dial call with a synthetic dial error,
// standing in for a HID helper that refuses every connection attempt
// at the socket level rather than at the WebSocket handshake.
type erroringDialer struct{ err error }

func (e erroringDialer) Dial(context.Context, int) (net.Conn, error) { return nil, e.err }

var _ iossimdriver.Dialer = erroringDialer{}

// TestIosSimDriver_Connect_HidRejected_SurfacesTypedRejection proves
// that Connect classifies a HID dial failure as
// diag.CodeAgentUnreachable, with a message naming no address or
// port, even though the underlying dial error (produced here by
// erroringDialer) does name one.
func TestIosSimDriver_Connect_HidRejected_SurfacesTypedRejection(t *testing.T) {
	dialer := erroringDialer{err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused")}
	d, err := iossimdriver.New(newDefaultFakeConn(), process.NewFake(), dialer, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	connectErr := d.Connect(context.Background())
	if connectErr == nil {
		t.Fatal("Connect() against a refusing HID dialer returned no error")
	}
	assertNoForbiddenTransportSubstring(t, "Connect", connectErr.Error())
	if code, _, _ := diag.Describe(connectErr); code != diag.CodeAgentUnreachable {
		t.Fatalf("Connect() against a refusing HID dialer: diag.Describe code = %q, want %q (error: %v)",
			code, diag.CodeAgentUnreachable, connectErr)
	}
}
