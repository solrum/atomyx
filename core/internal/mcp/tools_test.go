package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// singleMockDriverFactory is a device.Factory that always returns the
// same pre-staged mock.Driver, so a test can control exactly what
// Hierarchy returns before select_device wires it into the session.
type singleMockDriverFactory struct {
	driver *mockdriver.Driver
}

func (f singleMockDriverFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.driver.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.driver,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.driver, Clock: orchestra.SystemClock{}}),
	}, nil
}

// testServer builds a server + connected in-process client for deps
// and registers cleanup on t.
func testServer(t *testing.T, deps ServerDeps) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	server := NewServer(Info{Name: "atomyx-test", Version: "0.0.0-test"}, deps)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "atomyx-test-client", Version: "0.0.0-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession, ctx
}

// callTool invokes name over cs and decodes its first text content
// block as JSON. It fails the test on a transport error but leaves
// isError results for the caller to assert on.
func callTool(t *testing.T, cs *mcp.ClientSession, ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	decoded := map[string]any{}
	if len(result.Content) > 0 {
		if text, ok := result.Content[0].(*mcp.TextContent); ok {
			_ = json.Unmarshal([]byte(text.Text), &decoded)
		}
	}
	return result, decoded
}

func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		return ""
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0]: got %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

// TestNoActiveDeviceReturnsErrNoActiveDeviceMessage also exercises
// errorResult's coded path end to end: errNoActiveDevice carries
// diag.CodeDeviceNotFound and a remedy, and the tool result must
// surface both alongside the message.
func TestNoActiveDeviceReturnsErrNoActiveDeviceMessage(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	result, decoded := callTool(t, cs, ctx, "tap", map[string]any{"x": float64(1), "y": float64(2)})
	if !result.IsError {
		t.Fatal("tap with no active device: expected isError true")
	}
	if decoded["code"] != diag.CodeDeviceNotFound {
		t.Fatalf("tap error code: got %v, want %s", decoded["code"], diag.CodeDeviceNotFound)
	}
	if decoded["message"] != errNoActiveDevice.Error() {
		t.Fatalf("tap error message: got %v, want %q", decoded["message"], errNoActiveDevice.Error())
	}
	if hint, _ := decoded["hint"].(string); hint == "" {
		t.Fatalf("tap error hint: got %v, want a non-empty remedy", decoded["hint"])
	}
}

func TestListDevicesFiltersByPlatform(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: "emulator-5554", Platform: device.PlatformAndroid, Name: "Pixel", Kind: device.KindEmulator, State: device.StateOnline},
		{ID: "sim-1", Platform: device.PlatformIOS, Name: "iPhone", Kind: device.KindSimulator, State: device.StateOnline},
	})
	cs, ctx := testServer(t, ServerDeps{
		Probe:   probe,
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	_, decoded := callTool(t, cs, ctx, "list_devices", map[string]any{"platform": "android"})
	devices, ok := decoded["devices"].([]any)
	if !ok || len(devices) != 1 {
		t.Fatalf("list_devices(android): got devices=%v, want 1 android entry", decoded["devices"])
	}
	entry := devices[0].(map[string]any)
	if entry["platform"] != "android" || entry["id"] != "emulator-5554" {
		t.Fatalf("list_devices(android) entry: got %v", entry)
	}
}

func TestSelectDeviceThenDisconnect(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: "emulator-5554", Platform: device.PlatformAndroid, Kind: device.KindEmulator},
	})
	cs, ctx := testServer(t, ServerDeps{
		Probe:   probe,
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "android",
		"id":       "emulator-5554",
	})
	if result.IsError {
		t.Fatalf("select_device: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("select_device: got %v, want ok:true", decoded)
	}
	active, ok := decoded["active"].(map[string]any)
	if !ok || active["platform"] != "android" || active["id"] != "emulator-5554" {
		t.Fatalf("select_device active: got %v", decoded["active"])
	}

	_, disconnected := callTool(t, cs, ctx, "disconnect_device", map[string]any{})
	if disconnected["ok"] != true {
		t.Fatalf("disconnect_device: got %v, want ok:true", disconnected)
	}
	wasActive, ok := disconnected["wasActive"].(map[string]any)
	if !ok || wasActive["id"] != "emulator-5554" {
		t.Fatalf("disconnect_device wasActive: got %v", disconnected["wasActive"])
	}

	// Idempotent: disconnecting again with no active device still
	// reports ok:true and an explicit null wasActive.
	_, secondDisconnect := callTool(t, cs, ctx, "disconnect_device", map[string]any{})
	if secondDisconnect["ok"] != true {
		t.Fatalf("second disconnect_device: got %v", secondDisconnect)
	}
	if v, present := secondDisconnect["wasActive"]; !present || v != nil {
		t.Fatalf("second disconnect_device wasActive: got %v, want explicit null", v)
	}
}

// stubKindDriver wraps a mock.Driver but reports a fixed DeviceInfo
// kind, standing in for what a real driver derives from the device it
// is actually bound to (AndroidDriver from the serial prefix,
// IosDriver from its Kind option) without requiring adb or iproxy.
type stubKindDriver struct {
	*mockdriver.Driver
	kind driver.DeviceKind
	err  error
}

func (d *stubKindDriver) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	if d.err != nil {
		return driver.DeviceInfo{}, d.err
	}
	return driver.DeviceInfo{Kind: d.kind}, nil
}

// kindStubFactory is a device.Factory that always returns a
// stubKindDriver reporting kind, so a test can drive
// DeviceSession.Select's kind-resolution path for a device kind a
// serial-agnostic mock cannot otherwise model.
type kindStubFactory struct {
	kind  driver.DeviceKind
	err   error
	built *[]device.Descriptor
}

func (f kindStubFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	if f.built != nil {
		*f.built = append(*f.built, desc)
	}
	drv := &stubKindDriver{Driver: mockdriver.New(), kind: f.kind, err: f.err}
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

// stubProbe is a device.Probe that reports a fixed device list.
type stubProbe struct{ devices []device.Descriptor }

func (p stubProbe) Scan(context.Context) []device.Descriptor { return p.devices }

// TestSelectDeviceResolvesKindFromProbe asserts Select builds its
// descriptor through the shared device resolver: a physical iPhone gets
// the device kind even when the caller's kind hint says simulator, and a
// listed simulator gets the simulator kind even when the hint says
// device.
func TestSelectDeviceResolvesKindFromProbe(t *testing.T) {
	probe := stubProbe{devices: []device.Descriptor{
		{ID: "PHONE-UDID", Platform: device.PlatformIOS, Kind: device.KindDevice},
		{ID: "SIM-UDID", Platform: device.PlatformIOS, Kind: device.KindSimulator},
	}}
	cases := []struct {
		name string
		id   string
		hint SelectKind
		want device.Kind
	}{
		{name: "physical iPhone with simulator hint", id: "PHONE-UDID", hint: SelectKindSimulator, want: device.KindDevice},
		{name: "physical iPhone without hint", id: "phone-udid", want: device.KindDevice},
		{name: "simulator with device hint", id: "SIM-UDID", hint: SelectKindDevice, want: device.KindSimulator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var built []device.Descriptor
			session := NewDeviceSession(DeviceSessionDeps{
				Factory: kindStubFactory{kind: driver.DeviceKindDevice, built: &built},
				Probe:   probe,
			})
			if _, err := session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: tc.id, Kind: tc.hint, Port: 4242}); err != nil {
				t.Fatalf("Select: %v", err)
			}
			if len(built) != 1 || built[0].Kind != tc.want {
				t.Fatalf("Factory descriptors = %+v, want one with kind %q", built, tc.want)
			}
			if built[0].Port != 4242 {
				t.Fatalf("Factory descriptor port = %d, want 4242", built[0].Port)
			}
		})
	}
}

// TestSelectDeviceRejectsUnlistedIOSDevice asserts an iOS id the probe
// does not find fails with DEVICE_NOT_FOUND before any driver is built.
func TestSelectDeviceRejectsUnlistedIOSDevice(t *testing.T) {
	var built []device.Descriptor
	session := NewDeviceSession(DeviceSessionDeps{
		Factory: kindStubFactory{kind: driver.DeviceKindSimulator, built: &built},
		Probe:   stubProbe{},
	})
	_, err := session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: "GHOST"})
	if err == nil {
		t.Fatal("Select: expected an error for an unlisted iOS id")
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeDeviceNotFound {
		t.Fatalf("Select error code = %q, want %q", code, diag.CodeDeviceNotFound)
	}
	if len(built) != 0 {
		t.Fatalf("Factory built %d drivers, want none", len(built))
	}
}

// TestSelectDeviceReportsBoundDriverKind asserts DeviceSession.Select
// reports the kind the bound driver's own DeviceInfo call returns,
// not a transport default guessed from the caller's platform and
// omitted kind argument. A physical Android serial and an
// "emulator-" prefixed one must resolve differently even though
// neither call passes an explicit kind.
func TestSelectDeviceReportsBoundDriverKind(t *testing.T) {
	cases := []struct {
		name       string
		platform   device.Platform
		id         string
		driverKind driver.DeviceKind
		wantKind   SelectKind
	}{
		{name: "android physical serial reports device", platform: device.PlatformAndroid, id: "R58N90ABCDE", driverKind: driver.DeviceKindDevice, wantKind: SelectKindDevice},
		{name: "android emulator serial reports emulator", platform: device.PlatformAndroid, id: "emulator-5554", driverKind: driver.DeviceKindEmulator, wantKind: SelectKindEmulator},
		{name: "ios omitted kind reports simulator", platform: device.PlatformIOS, id: "SIM-BOOTED", driverKind: driver.DeviceKindSimulator, wantKind: SelectKindSimulator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := NewDeviceSession(DeviceSessionDeps{
				Factory: kindStubFactory{kind: tc.driverKind},
				Probe:   stubProbe{devices: []device.Descriptor{{ID: "SIM-BOOTED", Platform: device.PlatformIOS, Kind: device.KindSimulator}}},
			})
			active, err := session.Select(context.Background(), SelectDeviceInput{Platform: tc.platform, ID: tc.id})
			if err != nil {
				t.Fatalf("Select: unexpected error: %v", err)
			}
			if active.Kind != tc.wantKind {
				t.Fatalf("Select active.Kind: got %q, want %q", active.Kind, tc.wantKind)
			}
		})
	}
}

// TestSelectDeviceFallsBackToDescriptorKindWhenDeviceInfoFails asserts
// a DeviceInfo failure on the newly-bound driver does not fail
// Select: it falls back to the descriptor's own kind rather than
// surfacing the DeviceInfo error to the caller.
func TestSelectDeviceFallsBackToDescriptorKindWhenDeviceInfoFails(t *testing.T) {
	session := NewDeviceSession(DeviceSessionDeps{
		Factory: kindStubFactory{err: errors.New("device info unavailable")},
	})
	active, err := session.Select(context.Background(), SelectDeviceInput{
		Platform: device.PlatformAndroid,
		ID:       "emulator-5554",
	})
	if err != nil {
		t.Fatalf("Select: unexpected error: %v", err)
	}
	if active.Kind != SelectKindEmulator {
		t.Fatalf("Select active.Kind: got %q, want %q (descriptor fallback)", active.Kind, SelectKindEmulator)
	}
}

func sampleTree() wire.TreeNodeWire {
	enabled := true
	clickable := true
	return wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{
			{
				Attributes: map[string]string{
					"role":   "button",
					"id":     "login-button",
					"text":   "Log in",
					"bounds": "0,100,100,150",
				},
				Enabled:   &enabled,
				Clickable: &clickable,
			},
			{
				Attributes: map[string]string{
					"role": "other",
					"id":   "com.samsung.android.app.cocktailbarservice:edge_handle",
				},
			},
		},
	}
}

func selectMockDevice(t *testing.T, cs *mcp.ClientSession, ctx context.Context) {
	t.Helper()
	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "android",
		"id":       "emulator-5554",
	})
	if result.IsError || decoded["ok"] != true {
		t.Fatalf("select_device setup failed: isError=%v decoded=%v", result.IsError, decoded)
	}
}

func TestGetUiTreeFiltersSystemOverlaysByDefault(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	_, decoded := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})
	nodes, ok := decoded["nodes"].([]any)
	if !ok {
		t.Fatalf("get_ui_tree nodes: got %v", decoded["nodes"])
	}
	for _, n := range nodes {
		node := n.(map[string]any)
		if id, _ := node["id"].(string); id == "com.samsung.android.app.cocktailbarservice:edge_handle" {
			t.Fatalf("get_ui_tree: system overlay node leaked into default result: %v", node)
		}
	}
	// container + button, overlay filtered out.
	if int(decoded["total"].(float64)) != 2 {
		t.Fatalf("get_ui_tree total: got %v, want 2 (overlay excluded)", decoded["total"])
	}

	_, withOverlays := callTool(t, cs, ctx, "get_ui_tree", map[string]any{"includeSystemOverlays": true})
	if int(withOverlays["total"].(float64)) != 3 {
		t.Fatalf("get_ui_tree(includeSystemOverlays) total: got %v, want 3", withOverlays["total"])
	}
}

func TestFindElementAndTapSelector(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	_, found := callTool(t, cs, ctx, "find_element", map[string]any{"id": "login-button"})
	if found["found"] != true || found["id"] != "login-button" || found["text"] != "Log in" {
		t.Fatalf("find_element: got %v", found)
	}
	center, ok := found["center"].(map[string]any)
	if !ok || center["x"] != float64(50) || center["y"] != float64(125) {
		t.Fatalf("find_element center: got %v", found["center"])
	}

	result, tapped := callTool(t, cs, ctx, "tap", map[string]any{
		"selector": map[string]any{"id": "login-button"},
	})
	if result.IsError {
		t.Fatalf("tap(selector): unexpected isError: %v", tapped)
	}
	if tapped["ok"] != true {
		t.Fatalf("tap(selector): got %v, want ok:true", tapped)
	}
}

func TestWaitForElementTimesOut(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	_, decoded := callTool(t, cs, ctx, "wait_for_element", map[string]any{
		"selector":       map[string]any{"id": "does-not-exist"},
		"timeoutMs":      float64(50),
		"pollIntervalMs": float64(10),
	})
	if decoded["found"] != false {
		t.Fatalf("wait_for_element timeout: got %v, want found:false", decoded)
	}
	if _, ok := decoded["elapsedMs"]; !ok {
		t.Fatalf("wait_for_element timeout: missing elapsedMs in %v", decoded)
	}
}

func TestRunLifecycleAndBugPersistenceRoundTrip(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	_, started := callTool(t, cs, ctx, "start_run", map[string]any{"name": "regression pass"})
	if started["ok"] != true {
		t.Fatalf("start_run: got %v", started)
	}
	runID, _ := started["id"].(string)
	if runID == "" {
		t.Fatalf("start_run: missing id in %v", started)
	}
	if _, present := started["erroredPredecessor"]; present {
		t.Fatalf("start_run: unexpected erroredPredecessor on first run: %v", started)
	}

	// report_bug with no active device: screenshot capture is skipped,
	// the bug record is still persisted.
	_, bug := callTool(t, cs, ctx, "report_bug", map[string]any{
		"title":       "Login button unresponsive",
		"description": "Tapping login does nothing.",
	})
	if bug["ok"] != true {
		t.Fatalf("report_bug: got %v", bug)
	}
	bugID, _ := bug["id"].(string)
	if bugID == "" || bug["runId"] != runID {
		t.Fatalf("report_bug: got %v", bug)
	}
	if _, present := bug["screenshotPath"]; present {
		t.Fatalf("report_bug: expected no screenshotPath key with no active device, got %v", bug)
	}

	_, listed := callTool(t, cs, ctx, "list_bugs", map[string]any{})
	if listed["count"] != float64(1) || listed["totalMatching"] != float64(1) {
		t.Fatalf("list_bugs: got %v", listed)
	}
	if v, present := listed["nextOffset"]; !present || v != nil {
		t.Fatalf("list_bugs nextOffset: got %v, want explicit null", v)
	}

	_, fetched := callTool(t, cs, ctx, "get_bug", map[string]any{"id": bugID})
	if fetched["ok"] != true {
		t.Fatalf("get_bug: got %v", fetched)
	}

	_, deleted := callTool(t, cs, ctx, "delete_bug", map[string]any{"id": bugID})
	if deleted["ok"] != true {
		t.Fatalf("delete_bug: got %v", deleted)
	}
	_, missing := callTool(t, cs, ctx, "get_bug", map[string]any{"id": bugID})
	if missing["ok"] != false {
		t.Fatalf("get_bug after delete: got %v, want ok:false", missing)
	}

	_, finished := callTool(t, cs, ctx, "finish_run", map[string]any{"status": "passed"})
	if finished["ok"] != true || finished["id"] != runID || finished["status"] != "passed" {
		t.Fatalf("finish_run: got %v", finished)
	}
	if finished["findingsCount"] != float64(1) {
		t.Fatalf("finish_run findingsCount: got %v, want 1", finished["findingsCount"])
	}

	_, gotRun := callTool(t, cs, ctx, "get_run", map[string]any{"id": runID})
	if gotRun["ok"] != true {
		t.Fatalf("get_run: got %v", gotRun)
	}

	_, updated := callTool(t, cs, ctx, "update_run_summary", map[string]any{"id": runID, "summary": "flaky login button"})
	if updated["ok"] != true || updated["summary"] != "flaky login button" {
		t.Fatalf("update_run_summary: got %v", updated)
	}

	_, runsList := callTool(t, cs, ctx, "list_runs", map[string]any{})
	if runsList["count"] != float64(1) {
		t.Fatalf("list_runs: got %v", runsList)
	}

	_, deletedRun := callTool(t, cs, ctx, "delete_run", map[string]any{"id": runID})
	if deletedRun["ok"] != true {
		t.Fatalf("delete_run: got %v", deletedRun)
	}
	_, missingRun := callTool(t, cs, ctx, "get_run", map[string]any{"id": runID})
	if missingRun["ok"] != false {
		t.Fatalf("get_run after delete: got %v, want ok:false", missingRun)
	}
}

func TestCaseStudyRoundTrip(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	_, added := callTool(t, cs, ctx, "add_case_study", map[string]any{
		"title":    "Obscurement dismissal",
		"trigger":  "A system dialog blocked the tap target.",
		"solution": "Dismiss the dialog, then retry the original selector.",
	})
	if added["ok"] != true {
		t.Fatalf("add_case_study: got %v", added)
	}

	_, studies := callTool(t, cs, ctx, "get_case_studies", map[string]any{})
	if _, present := studies["ok"]; present {
		t.Fatalf("get_case_studies: result must not carry an ok field, got %v", studies)
	}
	list, ok := studies["studies"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("get_case_studies: got %v", studies["studies"])
	}
}

func TestRunScriptAgainstMockDevice(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - launchApp\n  - back\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("run_script: got %v, want ok:true", decoded)
	}
	if decoded["scriptName"] != "Smoke" {
		t.Fatalf("run_script scriptName: got %v", decoded["scriptName"])
	}
	artifacts, ok := decoded["artifacts"].(map[string]any)
	if !ok || len(artifacts) != 0 {
		t.Fatalf("run_script artifacts: got %v, want {}", decoded["artifacts"])
	}
}

// TestRunScriptProxyRequiredWithNoProxyArgumentReportsStructuredAbort
// confirms a script declaring `proxy: required` is refused with a
// structured top level abort, naming the diagnostic code, an
// operator-facing message, and a remedy, rather than silently
// reporting an empty, unexplained failure, when the call supplies no
// proxy argument.
func TestRunScriptProxyRequiredWithNoProxyArgumentReportsStructuredAbort(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Needs capture\nproxy: required\nsteps:\n  - launchApp\n  - back\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("run_script: got ok=%v, want false", decoded["ok"])
	}
	if steps, ok := decoded["steps"].([]any); !ok || len(steps) != 0 {
		t.Fatalf("run_script steps: got %v, want an empty array", decoded["steps"])
	}
	abort, ok := decoded["abort"].(map[string]any)
	if !ok {
		t.Fatalf("run_script abort: missing or wrong shape: %v", decoded["abort"])
	}
	if abort["reason"] != "proxyRequired" {
		t.Errorf("abort.reason: got %v, want %q", abort["reason"], "proxyRequired")
	}
	if abort["code"] != diag.CodeProxyRequired {
		t.Errorf("abort.code: got %v, want %q", abort["code"], diag.CodeProxyRequired)
	}
	if abort["message"] != "This script needs network capture, but no proxy is configured." {
		t.Errorf("abort.message: got %v", abort["message"])
	}
	remedy, _ := abort["remedy"].(string)
	if !strings.Contains(remedy, "--proxy") || !strings.Contains(remedy, "run_script") {
		t.Errorf("abort.remedy: got %v, want it to name both the --proxy flag and the run_script tool", remedy)
	}
}

// TestRunScriptProxyRequiredWithProxyArgumentRuns confirms the same
// script proceeds through its steps once the call supplies a proxy
// argument, so the refusal above is specific to a missing capture
// adapter rather than to the proxy: required declaration itself.
func TestRunScriptProxyRequiredWithProxyArgumentRuns(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	capturePath := filepath.Join(t.TempDir(), "capture.jsonl")
	script := "appId: com.example.app\nname: Needs capture\nproxy: required\nsteps:\n  - launchApp\n  - back\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{
		"script": script,
		"proxy":  "file:" + capturePath,
	})
	if result.IsError {
		t.Fatalf("run_script: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("run_script: got %v, want ok:true", decoded)
	}
	if _, present := decoded["abort"]; present {
		t.Errorf("run_script: got abort=%v, want it omitted for a successful run", decoded["abort"])
	}
}

func TestInvalidArgumentsReportSchemaViolation(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	// input_text requires "text"; omitting it must fail schema
	// validation before any device or business logic runs.
	result, _ := callTool(t, cs, ctx, "input_text", map[string]any{"x": float64(1), "y": float64(2)})
	if !result.IsError {
		t.Fatal("input_text without text: expected isError true")
	}
}

// allTextOf joins every text block of result, so a test sees the notices
// the middleware appends after the tool's own content.
func allTextOf(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range result.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}
