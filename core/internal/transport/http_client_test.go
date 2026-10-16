package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

func TestHTTPClient_Health_IssuesGetAndDecodes(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"accessibilityConnected":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	if gotMethod != "GET" || gotPath != "/health" {
		t.Fatalf("request = %s %s, want GET /health", gotMethod, gotPath)
	}
	want := HealthResponse{Ok: true, AccessibilityConnected: true}
	if got != want {
		t.Fatalf("Health() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_Ping_IssuesGetAndDecodesCapabilities(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"agent":"atomyx-android","protocolVersion":1,"agentVersion":"0.1.0","sdkInt":34,"mechanism":"accessibility","capabilities":{"canMultiPointer":true,"canPressure":false}}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if gotMethod != "GET" || gotPath != "/ping" {
		t.Fatalf("request = %s %s, want GET /ping", gotMethod, gotPath)
	}
	want := PingResponse{
		Ok:              true,
		Agent:           "atomyx-android",
		SdkInt:          34,
		Mechanism:       "accessibility",
		ProtocolVersion: 1,
		AgentVersion:    "0.1.0",
		Capabilities: PingCapabilities{
			CanMultiPointer: true,
			CanPressure:     false,
		},
	}
	if got != want {
		t.Fatalf("Ping() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_Tree_DecodesRawAgentTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"elementId":"root","className":"android.widget.FrameLayout","clickable":false,"enabled":true,"focused":false,"selected":false,"checkable":false,"checked":false,"visible":true,"isIme":false,"children":[{"elementId":"btn1","className":"android.widget.Button","resourceId":"com.example:id/save","text":"Save","bounds":{"left":10,"top":20,"right":110,"bottom":60},"clickable":true,"enabled":true,"focused":false,"selected":false,"checkable":false,"checked":false,"visible":true,"isIme":false,"children":[]}]}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Tree(context.Background())
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if got.ElementID != "root" || len(got.Children) != 1 {
		t.Fatalf("Tree() = %+v, unexpected root shape", got)
	}
	child := got.Children[0]
	if child.ResourceID != "com.example:id/save" || child.Text != "Save" || child.Clickable == nil || !*child.Clickable {
		t.Fatalf("Tree() child = %+v, unexpected shape", child)
	}
	wantBounds := &AndroidBounds{Left: 10, Top: 20, Right: 110, Bottom: 60}
	if !reflect.DeepEqual(child.Bounds, wantBounds) {
		t.Fatalf("Tree() child.Bounds = %+v, want %+v", child.Bounds, wantBounds)
	}
}

func TestHTTPClient_TapCoords_IssuesPostWithRealBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody TapCoordsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.TapCoords(context.Background(), TapCoordsRequest{X: 10, Y: 20})
	if err != nil {
		t.Fatalf("TapCoords() error = %v", err)
	}

	if gotMethod != "POST" || gotPath != "/actions/tap_coords" {
		t.Fatalf("request = %s %s, want POST /actions/tap_coords", gotMethod, gotPath)
	}
	if wantBody := (TapCoordsRequest{X: 10, Y: 20}); gotBody != wantBody {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if want := (GestureResult{Ok: true}); got != want {
		t.Fatalf("TapCoords() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_Swipe_RejectedResponse_DecodesReasonAndCode(t *testing.T) {
	var gotBody SwipeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"reason":"gesture cancelled by the accessibility service","code":"gesture_cancelled"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Swipe(context.Background(), SwipeRequest{FromX: 0, FromY: 0, ToX: 100, ToY: 200, DurationMs: 300})
	if err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	wantBody := SwipeRequest{FromX: 0, FromY: 0, ToX: 100, ToY: 200, DurationMs: 300}
	if gotBody != wantBody {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if got.Ok {
		t.Fatalf("Swipe() Ok = true, want false")
	}
	if got.Reason == nil || *got.Reason != "gesture cancelled by the accessibility service" {
		t.Fatalf("Swipe() Reason = %v, want the cancellation message", got.Reason)
	}
	if got.Code == nil || *got.Code != "gesture_cancelled" {
		t.Fatalf("Swipe() Code = %v, want gesture_cancelled", got.Code)
	}
}

func TestHTTPClient_DispatchGesture_IssuesPostWithPointers(t *testing.T) {
	var gotPath string
	var gotBody DispatchGestureRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"pointers":1}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	req := DispatchGestureRequest{
		Pointers: []GesturePointer{
			{
				ID: "finger1",
				Waypoints: []GestureWaypoint{
					{Phase: "down", X: 10, Y: 20, AtOffsetSeconds: 0},
					{Phase: "up", X: 10, Y: 20, AtOffsetSeconds: 0.1},
				},
			},
		},
	}
	got, err := client.DispatchGesture(context.Background(), req)
	if err != nil {
		t.Fatalf("DispatchGesture() error = %v", err)
	}
	if gotPath != "/actions/dispatch_gesture" {
		t.Fatalf("request path = %s, want /actions/dispatch_gesture", gotPath)
	}
	if !reflect.DeepEqual(gotBody, req) {
		t.Fatalf("request body = %+v, want %+v", gotBody, req)
	}
	if !got.Ok || got.Pointers == nil || *got.Pointers != 1 {
		t.Fatalf("DispatchGesture() = %+v, want ok=true pointers=1", got)
	}
}

func TestHTTPClient_ClearFocusedInput_IssuesPostWithEmptyBody(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"reason":""}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.ClearFocusedInput(context.Background())
	if err != nil {
		t.Fatalf("ClearFocusedInput() error = %v", err)
	}
	if gotPath != "/actions/clear_focused_input" {
		t.Fatalf("request path = %s, want /actions/clear_focused_input", gotPath)
	}
	if gotBody != "{}" {
		t.Fatalf("request body = %q, want {}", gotBody)
	}
	if want := (ClearFocusedInputResult{Ok: true, Reason: ""}); !reflect.DeepEqual(got, want) {
		t.Fatalf("ClearFocusedInput() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_ClearFocusedInput_DecodesFailureDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": false,
			"reason": "all strategies failed",
			"strategiesTried": ["select_all_delete", "long_press_clear"],
			"lastValue": "partial",
			"focusedNodeDesc": "EditText(resourceId=com.app:id/search)",
			"screenWidth": 1080,
			"screenHeight": 2400
		}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.ClearFocusedInput(context.Background())
	if err != nil {
		t.Fatalf("ClearFocusedInput() error = %v", err)
	}
	want := ClearFocusedInputResult{
		Ok:              false,
		Reason:          "all strategies failed",
		StrategiesTried: []string{"select_all_delete", "long_press_clear"},
		LastValue:       "partial",
		FocusedNodeDesc: "EditText(resourceId=com.app:id/search)",
		ScreenWidth:     1080,
		ScreenHeight:    2400,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ClearFocusedInput() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_LaunchApp_IssuesPostWithPackageName(t *testing.T) {
	var gotPath string
	var gotBody LaunchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	sent := LaunchRequest{PackageName: "dev.atomyx.driver.host", Arguments: map[string]string{"route": "/focus"}}
	got, err := client.LaunchApp(context.Background(), sent)
	if err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	if gotPath != "/actions/launch" {
		t.Fatalf("request path = %s, want /actions/launch", gotPath)
	}
	if wantBody := sent; !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if want := (OkResponse{Ok: true}); got != want {
		t.Fatalf("LaunchApp() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_TypeKeyboard_DecodesCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"typed":5,"total":5,"reason":""}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.TypeKeyboard(context.Background(), TypeKeyboardRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("TypeKeyboard() error = %v", err)
	}
	if want := (TypeKeyboardResult{Success: true, Typed: 5, Total: 5, Reason: ""}); got != want {
		t.Fatalf("TypeKeyboard() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_Keyboard_DecodesVisibleLayoutAndKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"visible":true,"packageName":"com.google.android.inputmethod.latin","layout":"qwerty","bounds":{"left":0,"top":1200,"right":1080,"bottom":1920},"keys":[{"label":"a","bounds":{"left":0,"top":1300,"right":100,"bottom":1400}},{"label":"enter","bounds":{"left":980,"top":1300,"right":1080,"bottom":1400}}]}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Keyboard(context.Background())
	if err != nil {
		t.Fatalf("Keyboard() error = %v", err)
	}
	if !got.Visible || got.Layout != "qwerty" {
		t.Fatalf("Keyboard() = %+v, want visible=true layout=qwerty", got)
	}
	if got.PackageName == nil || *got.PackageName != "com.google.android.inputmethod.latin" {
		t.Fatalf("Keyboard() PackageName = %v, want com.google.android.inputmethod.latin", got.PackageName)
	}
	wantBounds := &AndroidBounds{Left: 0, Top: 1200, Right: 1080, Bottom: 1920}
	if !reflect.DeepEqual(got.Bounds, wantBounds) {
		t.Fatalf("Keyboard() Bounds = %+v, want %+v", got.Bounds, wantBounds)
	}
	wantKeys := []KeyboardKey{
		{Label: "a", Bounds: AndroidBounds{Left: 0, Top: 1300, Right: 100, Bottom: 1400}},
		{Label: "enter", Bounds: AndroidBounds{Left: 980, Top: 1300, Right: 1080, Bottom: 1400}},
	}
	if !reflect.DeepEqual(got.Keys, wantKeys) {
		t.Fatalf("Keyboard() Keys = %+v, want %+v", got.Keys, wantKeys)
	}
}

func TestHTTPClient_Screenshot_DecodesBase64AndFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base64":"/9j/4AAQSkZJRgABAQAAAQABAAD/","format":"jpeg"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	want := ScreenshotResponse{Base64: "/9j/4AAQSkZJRgABAQAAAQABAAD/", Format: "jpeg"}
	if got != want {
		t.Fatalf("Screenshot() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_CurrentActivity_DecodesPackageActivitySource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"packageName":"com.android.chrome","activity":"com.google.android.apps.chrome.Main","source":"accessibility"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.CurrentActivity(context.Background())
	if err != nil {
		t.Fatalf("CurrentActivity() error = %v", err)
	}
	want := CurrentActivityResponse{
		PackageName: "com.android.chrome",
		Activity:    "com.google.android.apps.chrome.Main",
		Source:      "accessibility",
	}
	if got != want {
		t.Fatalf("CurrentActivity() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_Apps_DecodesInstalledAppList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"packageName":"com.android.chrome","label":"Chrome"},{"packageName":"com.android.settings","label":"Settings"}]`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.Apps(context.Background())
	if err != nil {
		t.Fatalf("Apps() error = %v", err)
	}
	want := []InstalledApp{
		{PackageName: "com.android.chrome", Label: "Chrome"},
		{PackageName: "com.android.settings", Label: "Settings"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Apps() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_LongPress_IssuesPostWithDurationAndDecodesResult(t *testing.T) {
	var gotPath string
	var gotBody LongPressRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.LongPress(context.Background(), LongPressRequest{X: 50, Y: 60, DurationMs: 800})
	if err != nil {
		t.Fatalf("LongPress() error = %v", err)
	}
	if gotPath != "/actions/long_press" {
		t.Fatalf("request path = %s, want /actions/long_press", gotPath)
	}
	if wantBody := (LongPressRequest{X: 50, Y: 60, DurationMs: 800}); gotBody != wantBody {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if want := (GestureResult{Ok: true}); got != want {
		t.Fatalf("LongPress() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_HideKeyboard_IssuesPostWithEmptyBody(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"reason":"keyboard dismissed"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.HideKeyboard(context.Background())
	if err != nil {
		t.Fatalf("HideKeyboard() error = %v", err)
	}
	if gotPath != "/actions/hide_keyboard" {
		t.Fatalf("request path = %s, want /actions/hide_keyboard", gotPath)
	}
	if gotBody != "{}" {
		t.Fatalf("request body = %q, want {}", gotBody)
	}
	if want := (AckResult{Ok: true, Reason: "keyboard dismissed"}); got != want {
		t.Fatalf("HideKeyboard() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_PressKey_IssuesPostWithKeyAndDecodesOk(t *testing.T) {
	var gotPath string
	var gotBody KeyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.PressKey(context.Background(), KeyRequest{Key: "back"})
	if err != nil {
		t.Fatalf("PressKey() error = %v", err)
	}
	if gotPath != "/actions/key" {
		t.Fatalf("request path = %s, want /actions/key", gotPath)
	}
	if wantBody := (KeyRequest{Key: "back"}); gotBody != wantBody {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if want := (OkResponse{Ok: true}); got != want {
		t.Fatalf("PressKey() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_ForceStopApp_IssuesPostWithPackageName(t *testing.T) {
	var gotPath string
	var gotBody ForceStopRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.ForceStopApp(context.Background(), ForceStopRequest{PackageName: "com.android.chrome"})
	if err != nil {
		t.Fatalf("ForceStopApp() error = %v", err)
	}
	if gotPath != "/actions/force_stop" {
		t.Fatalf("request path = %s, want /actions/force_stop", gotPath)
	}
	if wantBody := (ForceStopRequest{PackageName: "com.android.chrome"}); gotBody != wantBody {
		t.Fatalf("request body = %+v, want %+v", gotBody, wantBody)
	}
	if want := (AckResult{Ok: true}); got != want {
		t.Fatalf("ForceStopApp() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_ForceStopApp_RefusedCarriesReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"reason":"not permitted to force-stop com.android.chrome"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	got, err := client.ForceStopApp(context.Background(), ForceStopRequest{PackageName: "com.android.chrome"})
	if err != nil {
		t.Fatalf("ForceStopApp() error = %v", err)
	}
	want := AckResult{Ok: false, Reason: "not permitted to force-stop com.android.chrome"}
	if got != want {
		t.Fatalf("ForceStopApp() = %+v, want %+v", got, want)
	}
}

func TestHTTPClient_NonTwoXXStatus_ReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, err := client.Health(context.Background())
	if err == nil {
		t.Fatal("Health() error = nil, want an HTTPError")
	}
	var httpErr *HTTPError
	if !isHTTPError(err, &httpErr) {
		t.Fatalf("Health() error = %v (%T), want *HTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError || httpErr.Body != "boom" {
		t.Fatalf("HTTPError = %+v, want status 500 body \"boom\"", httpErr)
	}
}

// TestHTTPClient_DialFailure_ReturnsUnreachable proves that a closed
// listener (no agent process bound to the port at all) classifies as
// IsUnreachable true, the same outcome a caller gets for any other
// way the agent cannot be reached.
func TestHTTPClient_DialFailure_ReturnsUnreachable(t *testing.T) {
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

	client := NewHTTPClient(host, port)
	_, callErr := client.Health(context.Background())
	if callErr == nil {
		t.Fatal("Health() error = nil, want an UnreachableError")
	}
	if !IsUnreachable(callErr) {
		t.Fatalf("Health() error = %v (%T), want IsUnreachable true", callErr, callErr)
	}
	var httpErr *HTTPError
	if !isHTTPError(callErr, &httpErr) {
		t.Fatalf("Health() error = %v, want the underlying *HTTPError still reachable via Unwrap", callErr)
	}
}

// TestHTTPClient_ServerClosesWithoutResponding_ReturnsUnreachable
// proves that a connection dropped mid-request (the shape behind the
// EOF this classification exists to catch) also classifies as
// IsUnreachable true.
func TestHTTPClient_ServerClosesWithoutResponding_ReturnsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server's ResponseWriter does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	if callErr == nil {
		t.Fatal("Health() error = nil, want an UnreachableError")
	}
	if !IsUnreachable(callErr) {
		t.Fatalf("Health() error = %v (%T), want IsUnreachable true", callErr, callErr)
	}
}

// TestHTTPClient_NonTwoXXStatus_ReturnsAgentError proves that the
// agent answering with a failing status and a JSON body shaped like
// the Android agent's RouteResponse failure constructors (an "error"
// string field) classifies as an *AgentError carrying that field's
// value as Message, with no route, host, or port folded into that
// message: only Error() (used for logs, never surfaced to a
// tool-facing caller) carries that detail.
func TestHTTPClient_NonTwoXXStatus_ReturnsAgentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"selector matched no element"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	if callErr == nil {
		t.Fatal("Health() error = nil, want an AgentError")
	}
	if IsUnreachable(callErr) {
		t.Fatalf("Health() error = %v, want IsUnreachable false for an agent-reported failure", callErr)
	}
	var agentErr *AgentError
	if !errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v (%T), want *AgentError", callErr, callErr)
	}
	if agentErr.Message != "selector matched no element" {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, "selector matched no element")
	}
	if agentErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("AgentError.StatusCode = %d, want %d", agentErr.StatusCode, http.StatusBadRequest)
	}
	for _, forbidden := range []string{"http", "127.0.0.1", "get ", "/health"} {
		if strings.Contains(strings.ToLower(agentErr.Message), forbidden) {
			t.Fatalf("AgentError.Message = %q, must not name the route or transport", agentErr.Message)
		}
	}
}

// TestHTTPClient_NonTwoXXStatus_MessageField_IsUsedWhenErrorFieldAbsent
// proves that a JSON body using "message" instead of "error" is also
// accepted, since agentErrorMessage checks both field names.
func TestHTTPClient_NonTwoXXStatus_MessageField_IsUsedWhenErrorFieldAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"app not installed"}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	var agentErr *AgentError
	if !errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v (%T), want *AgentError", callErr, callErr)
	}
	if agentErr.Message != "app not installed" {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, "app not installed")
	}
}

// TestHTTPClient_NonTwoXXStatus_ReasonField_IsUsedWhenErrorAndMessageAbsent
// proves that a JSON body carrying only "reason", the shape the
// Android agent's /health route reports a not-ready outcome with, is
// also accepted by agentErrorMessage.
func TestHTTPClient_NonTwoXXStatus_ReasonField_IsUsedWhenErrorAndMessageAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ok":false,"accessibilityConnected":false,"reason":"The accessibility service is not running. Re-enable it on the device."}`))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	var agentErr *AgentError
	if !errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v (%T), want *AgentError", callErr, callErr)
	}
	want := "The accessibility service is not running. Re-enable it on the device."
	if agentErr.Message != want {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, want)
	}
}

// TestHTTPClient_NonTwoXXStatus_NonJSONBody_FallsBackToNeutralMessage
// proves that a body which is not JSON, such as a stack trace a
// misbehaving handler wrote directly to the response, never reaches
// AgentError.Message verbatim: it falls back to a neutral message
// naming only the status code.
func TestHTTPClient_NonTwoXXStatus_NonJSONBody_FallsBackToNeutralMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("panic: nil pointer dereference at MainActivity.kt:42"))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	var agentErr *AgentError
	if !errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v (%T), want *AgentError", callErr, callErr)
	}
	want := "the agent reported status 500 with no further detail"
	if agentErr.Message != want {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, want)
	}
}

// TestHTTPClient_NonTwoXXStatus_EmptyBody_FallsBackToNeutralMessage
// proves that an empty response body falls back to the same neutral
// message as a non-JSON body, rather than an empty Message.
func TestHTTPClient_NonTwoXXStatus_EmptyBody_FallsBackToNeutralMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	var agentErr *AgentError
	if !errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v (%T), want *AgentError", callErr, callErr)
	}
	want := "the agent reported status 500 with no further detail"
	if agentErr.Message != want {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, want)
	}
}

// TestHTTPClient_DecodeFailure_IsNotClassifiedAsUnreachableOrAgentError
// proves that a malformed response body, received over a request that
// otherwise completed normally, is classified as neither: the agent
// was reached and answered, so reporting it as unreachable would be
// wrong, and it never told the caller its own failure message, so
// reporting it as an agent rejection would be wrong too.
func TestHTTPClient_DecodeFailure_IsNotClassifiedAsUnreachableOrAgentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := newTestHTTPClient(t, server)
	_, callErr := client.Health(context.Background())
	if callErr == nil {
		t.Fatal("Health() error = nil, want a decode failure")
	}
	if IsUnreachable(callErr) {
		t.Fatalf("Health() error = %v, want IsUnreachable false for a decode failure", callErr)
	}
	var agentErr *AgentError
	if errors.As(callErr, &agentErr) {
		t.Fatalf("Health() error = %v, want it not classified as *AgentError", callErr)
	}
}

// TestHTTPClient_ClientTimeoutFallback_ClassifiesAsToolTimeout proves
// that an agent that accepts a request but never answers it cannot
// hang a call forever when the caller's context carries no deadline
// of its own: the client's own Timeout fires instead, and the
// resulting failure is recoverable through diag.Describe as
// diag.CodeToolTimeout rather than being left unclassified. The
// client's Timeout is set well below defaultHTTPClientTimeout so the
// test does not have to wait out the production duration to observe
// the same fallback path firing.
func TestHTTPClient_ClientTimeoutFallback_ClassifiesAsToolTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	const budget = 100 * time.Millisecond
	client := NewHTTPClient(host, port, WithHTTPClient(&http.Client{Timeout: budget}))

	start := time.Now()
	_, callErr := client.Health(context.Background())
	elapsed := time.Since(start)

	var httpErr *HTTPError
	if !isHTTPError(callErr, &httpErr) {
		t.Fatalf("Health() error = %v (%T), want *HTTPError", callErr, callErr)
	}
	if code, _, _ := diag.Describe(callErr); code != diag.CodeToolTimeout {
		t.Fatalf("diag.Describe(Health() error) code = %q, want %q", code, diag.CodeToolTimeout)
	}
	const slack = 2 * time.Second
	if elapsed > budget+slack {
		t.Fatalf("Health() took %v to fail against a %v client timeout, want the unanswered wait bounded by it rather than hanging", elapsed, budget)
	}
}

func TestHTTPClient_ContextCanceled_AbortsInFlightRequest(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := newTestHTTPClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.Health(ctx)
	if err == nil {
		t.Fatal("Health() error = nil, want context.Canceled")
	}
	if ctx.Err() == nil {
		t.Fatal("ctx.Err() = nil after cancellation")
	}
	if !isContextCanceled(err) {
		t.Fatalf("Health() error = %v, want context.Canceled", err)
	}
}

func TestNewHTTPClient_DefaultsToTheDefaultTimeout(t *testing.T) {
	client := NewHTTPClient("127.0.0.1", 8765)
	if client.client.Timeout != defaultHTTPClientTimeout {
		t.Fatalf("client.Timeout = %v, want %v", client.client.Timeout, defaultHTTPClientTimeout)
	}
}

func TestNewHTTPClient_WithHTTPClientOverridesDefaultTimeout(t *testing.T) {
	want := 250 * time.Millisecond
	client := NewHTTPClient("127.0.0.1", 8765, WithHTTPClient(&http.Client{Timeout: want}))
	if client.client.Timeout != want {
		t.Fatalf("client.Timeout = %v, want %v", client.client.Timeout, want)
	}
}

func newTestHTTPClient(t *testing.T, server *httptest.Server) *HTTPClient {
	t.Helper()
	host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return NewHTTPClient(host, port, WithHTTPClient(server.Client()))
}

func isHTTPError(err error, target **HTTPError) bool {
	return errors.As(err, target)
}

func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func TestHTTPClient_Ping_DecodesTheSecureFieldsCapabilityAndIgnoresUnknownOnes(t *testing.T) {
	cases := map[string]struct {
		body string
		want bool
	}{
		"current agent":          {`{"ok":true,"capabilities":{"canMultiPointer":true,"canPressure":false,"secureFields":true,"somethingNew":true}}`, true},
		"agent that predates it": {`{"ok":true,"capabilities":{"canMultiPointer":true,"canPressure":false}}`, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			got, err := newTestHTTPClient(t, server).Ping(context.Background())
			if err != nil {
				t.Fatalf("Ping() error = %v", err)
			}
			if got.Capabilities.SecureFields != tc.want || !got.Capabilities.CanMultiPointer {
				t.Fatalf("Ping() capabilities = %+v, want secureFields %v", got.Capabilities, tc.want)
			}
		})
	}
}
