package androiddriver_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/wire"
)

// clientTimeout bounds the fake HTTP client used throughout this file,
// standing in for defaultHTTPClientTimeout so a hung agent's fallback
// bound fires in milliseconds instead of the production duration.
const clientTimeout = 50 * time.Millisecond

// faultInjectionBudget is the outer bound every assertion in this file
// holds a driver call to: generous enough that scheduling noise never
// trips it, but far short of a real hang, so a method that regresses
// to blocking indefinitely fails the test instead of hanging it.
const faultInjectionBudget = 2 * time.Second

// newFaultInjectionAgent starts a real local HTTP server that answers
// every path in healthyPaths immediately with a minimal well-formed
// response, and accepts every other request without ever answering
// it. It stands in for an Android agent whose control server accepted
// a request but then hung.
//
// A hung handler goroutine is released deterministically at test
// cleanup rather than by relying on the client's own disconnect
// reaching the server: whether http.Client.Timeout firing client-side
// tears down the underlying connection promptly enough for the
// server to observe it through r.Context is not a bound this test
// depends on, so a hung request always releases on its own once the
// test that provoked it is done asserting.
func newFaultInjectionAgent(t *testing.T, healthyPaths ...string) *httptest.Server {
	t.Helper()
	healthy := make(map[string]bool, len(healthyPaths))
	for _, p := range healthyPaths {
		healthy[p] = true
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy[r.URL.Path] {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(faultInjectionOkBody(r.URL.Path)))
			return
		}
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}

// faultInjectionOkBody returns the minimal well-formed JSON response
// path's real route requires, so a driver method depending on that
// route's response shape (Connect's capability handshake) does not
// fail decoding before the fault-injection assertion ever runs.
func faultInjectionOkBody(path string) string {
	switch path {
	case "/ping":
		return `{"ok":true,"agent":"fault-injection","sdkInt":1,"mechanism":"test",` +
			`"capabilities":{"canMultiPointer":false,"canPressure":false,"secureFields":true}}`
	default:
		return `{"ok":true,"accessibilityConnected":true}`
	}
}

// newFaultInjectionClient builds an HTTPClient pointed at server with
// its client-wide round-trip timeout shortened to clientTimeout, so
// the production fallback bound's own classification path fires
// quickly instead of waiting out defaultHTTPClientTimeout.
func newFaultInjectionClient(t *testing.T, server *httptest.Server) *transport.HTTPClient {
	t.Helper()
	host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split fault-injection server address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse fault-injection server port: %v", err)
	}
	return transport.NewHTTPClient(host, port, transport.WithHTTPClient(&http.Client{Timeout: clientTimeout}))
}

// connectedFaultInjectionDriver builds an AndroidDriver against server
// and connects it. process.NewFake answers every adb invocation with
// an empty success, so Connect's adb-forward step never depends on a
// real adb binary.
func connectedFaultInjectionDriver(t *testing.T, server *httptest.Server) *androiddriver.AndroidDriver {
	t.Helper()
	d := androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() against a healthy agent: %v", err)
	}
	return d
}

// minimalGesture is the smallest gesture DispatchGesture accepts
// without rejecting it locally before ever reaching the wire: one
// pointer, one waypoint. AndroidDriver.DispatchGesture returns a local
// error for zero pointers, so this is the minimal shape that actually
// issues the wire call under test.
var minimalGesture = driver.Gesture{
	Pointers: []driver.GesturePointer{{
		ID:        "0",
		Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}}},
	}},
}

// TestAndroidDriver_WireCallTimeout is the fault-injection table every
// AndroidDriver port method that issues a wire call is checked
// against: driven with minimal valid arguments over a context with no
// deadline of its own, against an agent that accepts the request but
// never answers it, each must return within faultInjectionBudget with
// a diag.CodeToolTimeout diagnostic rather than hang.
//
// Connect and Ping are deliberately absent from this table: both
// already apply their own typed classification (diag.CodeAgentUnreachable)
// to any handshake failure, including a hang, which
// TestAndroidDriver_Connect_HungHealth_SurfacesTypedUnreachable and
// TestAndroidDriver_Ping_Hung_SurfacesTypedUnreachable check directly.
// WaitForIdle and DeviceInfo never issue a wire call. InstallApp,
// UninstallApp, ClearAppData, and IsAppInstalled issue a host
// subprocess call through the injected process.Runner (adb) rather
// than the bounded HTTP transport; a hung subprocess is a distinct,
// unaddressed gap outside the classification this table checks.
// wireCallMethodCases enumerates every AndroidDriver method that
// issues an HTTP wire call, driven with minimal valid arguments over
// a plain context.Context so a caller can force the underlying
// transport call to fail without needing a device-specific setup.
// TestAndroidDriver_WireCallTimeout and
// TestAndroidDriver_BoundaryTranslation_EveryWireCallMethod both
// drive this same table against a differently-faulty agent, so a
// method added to the port only has to be added here once.
var wireCallMethodCases = []struct {
	name string
	call func(ctx context.Context, d driver.Driver) error
}{
	{"Hierarchy", func(ctx context.Context, d driver.Driver) error { _, err := d.Hierarchy(ctx); return err }},
	{"Tap", func(ctx context.Context, d driver.Driver) error { return d.Tap(ctx, wire.Point{X: 1, Y: 1}) }},
	{"LongPress", func(ctx context.Context, d driver.Driver) error {
		return d.LongPress(ctx, wire.Point{X: 1, Y: 1}, 100)
	}},
	{"Swipe", func(ctx context.Context, d driver.Driver) error {
		return d.Swipe(ctx, wire.Point{X: 0, Y: 0}, wire.Point{X: 1, Y: 1}, 100)
	}},
	{"DispatchGesture", func(ctx context.Context, d driver.Driver) error {
		_, err := d.DispatchGesture(ctx, minimalGesture)
		return err
	}},
	{"InputText", func(ctx context.Context, d driver.Driver) error { return d.InputText(ctx, "x") }},
	{"EraseText", func(ctx context.Context, d driver.Driver) error { return d.EraseText(ctx, 1) }},
	{"PressKey", func(ctx context.Context, d driver.Driver) error {
		_, err := d.PressKey(ctx, driver.KeyBack)
		return err
	}},
	{"HideKeyboard", func(ctx context.Context, d driver.Driver) error { _, err := d.HideKeyboard(ctx); return err }},
	{"LaunchApp", func(ctx context.Context, d driver.Driver) error {
		return d.LaunchApp(ctx, "dev.atomyx.faultinjection", driver.LaunchArgs{})
	}},
	{"StopApp", func(ctx context.Context, d driver.Driver) error {
		return d.StopApp(ctx, "dev.atomyx.faultinjection")
	}},
	{"KillApp", func(ctx context.Context, d driver.Driver) error {
		return d.KillApp(ctx, "dev.atomyx.faultinjection")
	}},
	{"CurrentForeground", func(ctx context.Context, d driver.Driver) error {
		_, err := d.CurrentForeground(ctx)
		return err
	}},
	{"ListApps", func(ctx context.Context, d driver.Driver) error { _, err := d.ListApps(ctx); return err }},
	{"Screenshot", func(ctx context.Context, d driver.Driver) error { _, err := d.Screenshot(ctx); return err }},
	{"ScreenSize", func(ctx context.Context, d driver.Driver) error { _, err := d.ScreenSize(ctx); return err }},
}

func TestAndroidDriver_WireCallTimeout(t *testing.T) {
	for _, tc := range wireCallMethodCases {
		t.Run(tc.name, func(t *testing.T) {
			server := newFaultInjectionAgent(t, "/health", "/ping")
			d := connectedFaultInjectionDriver(t, server)

			start := time.Now()
			err := tc.call(context.Background(), d)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("%s against a hung agent returned no error", tc.name)
			}
			if elapsed > faultInjectionBudget {
				t.Fatalf("%s took %v to fail against a %v client timeout, want it bounded by %v rather than hanging",
					tc.name, elapsed, clientTimeout, faultInjectionBudget)
			}
			if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
				t.Fatalf("%s against a hung agent: diag.Describe code = %q, want %q (error: %v)",
					tc.name, code, diag.CodeToolTimeout, err)
			}
		})
	}
}

// TestAndroidDriver_Connect_HungHealth_SurfacesTypedUnreachable proves
// that Connect classifies a hung /health check as diag.CodeAgentUnreachable,
// the same code it reports for every other reachability failure, rather
// than hanging or surfacing an unclassified error.
func TestAndroidDriver_Connect_HungHealth_SurfacesTypedUnreachable(t *testing.T) {
	server := newFaultInjectionAgent(t) // every route, including /health, hangs.
	d := androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})

	start := time.Now()
	err := d.Connect(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Connect() against a hung agent returned no error")
	}
	if elapsed > faultInjectionBudget {
		t.Fatalf("Connect() took %v against a hung agent, want it bounded by %v rather than hanging", elapsed, faultInjectionBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("Connect() against a hung agent: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeAgentUnreachable, err)
	}
}

// TestAndroidDriver_Ping_Hung_SurfacesTypedUnreachable proves that
// Ping classifies a hung /health check as diag.CodeAgentUnreachable
// within its own healthProbeTimeout sub-budget, rather than hanging
// or surfacing an unclassified error.
func TestAndroidDriver_Ping_Hung_SurfacesTypedUnreachable(t *testing.T) {
	server := newFaultInjectionAgent(t) // every route, including /health, hangs.
	d := androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})

	start := time.Now()
	err := d.Ping(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping() against a hung agent returned no error")
	}
	if elapsed > faultInjectionBudget {
		t.Fatalf("Ping() took %v against a hung agent, want it bounded by %v rather than hanging", elapsed, faultInjectionBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("Ping() against a hung agent: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeAgentUnreachable, err)
	}
}

// forbiddenTransportSubstrings are the shapes a leaked wire-call
// detail (an HTTP route, host, or port) takes. A tool-facing
// AndroidDriver error must never contain one of these, regardless of
// which method produced it or why the underlying request failed.
var forbiddenTransportSubstrings = []string{
	"http", "127.0.0.1", "localhost", "GET ", "POST ", ":8765", "/tree", "/actions",
}

// boundaryTranslationSkipped names every driver.Driver method absent
// from wireCallMethodCases, with the reason
// TestAndroidDriver_BoundaryTranslation_EveryWireCallMethod does not
// exercise it. TestAndroidDriver_BoundaryTranslation_CoversEveryDriverMethod
// fails when a method sits in neither this map nor
// wireCallMethodCases, so a port method added later cannot silently
// skip this classification.
var boundaryTranslationSkipped = map[string]string{
	"Platform":     "returns a static string, never an error",
	"Capabilities": "returns a struct, never an error",
	"IsConnected":  "returns a bool, never an error",
	"Disconnect":   "best-effort by contract, never returns a non-nil error",
	"WaitForIdle":  "returns a defensive true unconditionally, never issues a wire call",
	"DeviceInfo":   "derives its result from opts.Serial alone, never issues a wire call",
	"Connect": "already checked directly by TestAndroidDriver_Connect_HungHealth_SurfacesTypedUnreachable " +
		"and its sibling failure tests, which assert its own typed classification",
	"Ping": "already checked directly by TestAndroidDriver_Ping_Hung_SurfacesTypedUnreachable " +
		"and its sibling failure tests, which assert its own typed classification",
	"InstallApp":     "issues a host subprocess call through process.Runner (adb), not the HTTP transport this boundary classifies",
	"UninstallApp":   "issues a host subprocess call through process.Runner (adb), not the HTTP transport this boundary classifies",
	"ClearAppData":   "issues a host subprocess call through process.Runner (adb), not the HTTP transport this boundary classifies",
	"IsAppInstalled": "issues a host subprocess call through process.Runner (adb), not the HTTP transport this boundary classifies",
}

// TestAndroidDriver_BoundaryTranslation_CoversEveryDriverMethod proves
// that wireCallMethodCases plus boundaryTranslationSkipped account for
// every method on driver.Driver, so a method added to the port
// without being triaged into one of the two fails this test instead
// of silently escaping the boundary-translation classification
// TestAndroidDriver_BoundaryTranslation_EveryWireCallMethod checks.
func TestAndroidDriver_BoundaryTranslation_CoversEveryDriverMethod(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range wireCallMethodCases {
		covered[tc.name] = true
	}
	for name := range boundaryTranslationSkipped {
		covered[name] = true
	}

	driverType := reflect.TypeOf((*driver.Driver)(nil)).Elem()
	for i := 0; i < driverType.NumMethod(); i++ {
		name := driverType.Method(i).Name
		if !covered[name] {
			t.Errorf("driver.Driver method %s is covered by neither wireCallMethodCases nor boundaryTranslationSkipped; add it to one", name)
		}
	}
}

// deadEndpointDriver builds an AndroidDriver whose transport points at
// a port nothing is listening on, so every wire call it issues fails
// with a dial failure: connection refused.
func deadEndpointDriver(t *testing.T) *androiddriver.AndroidDriver {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse listener port: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	client := transport.NewHTTPClient(host, port, transport.WithHTTPClient(&http.Client{Timeout: clientTimeout}))
	return androiddriver.New(client, process.NewFake(), androiddriver.Options{Serial: "fault-injection"})
}

// agentRejectionDriver builds an AndroidDriver whose transport reaches
// a real HTTP server that answers every route with a failing status
// that is not the caller-input status (400), so every wire call it
// issues completes but is reported as a generic agent rejection
// rather than a connectivity failure or an invalid-argument rejection.
func agentRejectionDriver(t *testing.T) *androiddriver.AndroidDriver {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("request rejected by policy"))
	}))
	t.Cleanup(server.Close)
	return androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})
}

// invalidArgumentRejectionDriver builds an AndroidDriver whose
// transport reaches a real HTTP server that answers every route with
// HTTP status 400 and a JSON error field carrying message verbatim,
// standing in for the Android control server refusing a request
// because of the caller's own arguments, such as an unsupported key
// name.
func invalidArgumentRejectionDriver(t *testing.T, message string) *androiddriver.AndroidDriver {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(map[string]string{"error": message})
		if err != nil {
			t.Fatalf("marshal invalid-argument rejection body: %v", err)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})
}

// leakingRejectionDriver builds an AndroidDriver whose transport
// reaches a real HTTP server that answers every route with a failing
// status that is not the caller-input status (400) and a JSON error
// field carrying leakingMessage verbatim, standing in for a native
// agent whose own rejection text names a route or a private symbol.
func leakingRejectionDriver(t *testing.T, leakingMessage string) *androiddriver.AndroidDriver {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(map[string]string{"error": leakingMessage})
		if err != nil {
			t.Fatalf("marshal leaking rejection body: %v", err)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "fault-injection"})
}

// TestAndroidDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage
// proves that when the agent's own rejection text names a route, fail
// neutralizes it before it reaches the caller instead of passing the
// agent's message through unchanged, even though the agent is the
// party that should never have produced that shape in the first
// place.
func TestAndroidDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage(t *testing.T) {
	const leaking = "Unknown GET /tree"
	d := leakingRejectionDriver(t, leaking)

	err := d.Tap(context.Background(), wire.Point{X: 1, Y: 1})
	if err == nil {
		t.Fatal("Tap() against an agent reporting a leaking rejection message returned no error")
	}
	code, message, _ := diag.Describe(err)
	if code != diag.CodeAgentRejected {
		t.Fatalf("Tap() against an agent reporting a leaking rejection message: diag.Describe code = %q, want %q", code, diag.CodeAgentRejected)
	}
	if strings.Contains(message, "/tree") {
		t.Fatalf("Tap() against an agent reporting a leaking rejection message: message = %q, want the route stripped", message)
	}
}

// assertNoForbiddenTransportSubstring fails t when message contains
// any shape of a leaked HTTP route, host, or port, case-insensitively.
func assertNoForbiddenTransportSubstring(t *testing.T, methodName, message string) {
	t.Helper()
	lower := strings.ToLower(message)
	for _, forbidden := range forbiddenTransportSubstrings {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("%s error %q contains forbidden substring %q", methodName, message, forbidden)
		}
	}
}

// TestAndroidDriver_BoundaryTranslation_EveryWireCallMethod is the
// enforcement gate for the boundary AndroidDriver.fail implements:
// driven with minimal valid arguments, every method in
// wireCallMethodCases must return an error whose string names no HTTP
// route, host, or port, whether the agent could not be reached at all
// (a dead endpoint) or was reached and reported the request itself
// failed (an agent that rejects every request).
func TestAndroidDriver_BoundaryTranslation_EveryWireCallMethod(t *testing.T) {
	t.Run("dead endpoint", func(t *testing.T) {
		for _, tc := range wireCallMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := deadEndpointDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against a dead endpoint returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
					t.Fatalf("%s against a dead endpoint: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentUnreachable, err)
				}
			})
		}
	})

	t.Run("agent rejection", func(t *testing.T) {
		for _, tc := range wireCallMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := agentRejectionDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against an agent that rejects every request returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentRejected {
					t.Fatalf("%s against an agent that rejects every request: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentRejected, err)
				}
			})
		}
	})

	t.Run("invalid argument", func(t *testing.T) {
		for _, tc := range wireCallMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := invalidArgumentRejectionDriver(t, "the argument was invalid")
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against an agent that rejects every request with status 400 returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
					t.Fatalf("%s against an agent that rejects every request with status 400: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeInvalidArgument, err)
				}
			})
		}
	})
}

// TestAndroidDriver_PressKey_UnsupportedKey_SurfacesInvalidArgument
// proves that PressKey against an emulator that rejects an unknown key
// name with HTTP status 400 is reported as diag.CodeInvalidArgument,
// carrying the agent's own message, rather than the generic
// diag.CodeAgentRejected every other agent rejection surfaces.
func TestAndroidDriver_PressKey_UnsupportedKey_SurfacesInvalidArgument(t *testing.T) {
	const message = "Unsupported key: x. Supported keys: back, home."
	d := invalidArgumentRejectionDriver(t, message)

	_, err := d.PressKey(context.Background(), driver.KeyBack)
	if err == nil {
		t.Fatal("PressKey() against an agent rejecting an unsupported key returned no error")
	}
	code, gotMessage, remedy := diag.Describe(err)
	if code != diag.CodeInvalidArgument {
		t.Fatalf("PressKey() against an agent rejecting an unsupported key: diag.Describe code = %q, want %q", code, diag.CodeInvalidArgument)
	}
	if gotMessage != message {
		t.Fatalf("PressKey() against an agent rejecting an unsupported key: message = %q, want %q", gotMessage, message)
	}
	if remedy != diag.InvalidArgumentRemedy {
		t.Fatalf("PressKey() against an agent rejecting an unsupported key: remedy = %q, want %q", remedy, diag.InvalidArgumentRemedy)
	}
}
