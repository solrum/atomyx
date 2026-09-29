package transport

// Android agent route paths served by the Kotlin HttpControlServer
// (dev.atomyx.agent.control, default port 8765). One HTTPClient
// method exists per route in http_client.go; the request and
// response shapes below mirror what that agent accepts and returns.
const (
	pathHealth            = "/health"
	pathPing              = "/ping"
	pathTree              = "/tree"
	pathKeyboard          = "/keyboard"
	pathScreenshot        = "/screenshot"
	pathCurrentActivity   = "/current-activity"
	pathApps              = "/apps"
	pathTapCoords         = "/actions/tap_coords"
	pathLongPress         = "/actions/long_press"
	pathClearFocusedInput = "/actions/clear_focused_input"
	pathHideKeyboard      = "/actions/hide_keyboard"
	pathDispatchGesture   = "/actions/dispatch_gesture"
	pathSwipe             = "/actions/swipe"
	pathTypeKeyboard      = "/actions/type_keyboard"
	pathKey               = "/actions/key"
	pathLaunch            = "/actions/launch"
	pathForceStop         = "/actions/force_stop"
)

// HealthResponse is the response to GET /health. AccessibilityConnected
// reports whether the agent's AccessibilityService is bound; the
// route answers even when it is not, so a caller can distinguish "the
// agent process is unreachable" from "the agent is up but not yet
// granted the accessibility permission".
type HealthResponse struct {
	Ok                     bool `json:"ok"`
	AccessibilityConnected bool `json:"accessibilityConnected"`
}

// PingCapabilities reports which gesture-dispatch features the
// agent's active gesture runner supports.
type PingCapabilities struct {
	CanMultiPointer bool `json:"canMultiPointer"`
	CanPressure     bool `json:"canPressure"`
}

// PingResponse is the response to GET /ping, the capability
// handshake a caller runs once after Health succeeds. Mechanism
// names the agent's active gesture-dispatch backend for log
// correlation only; a caller must branch on Capabilities, not on
// this string.
type PingResponse struct {
	Ok           bool             `json:"ok"`
	Agent        string           `json:"agent"`
	SdkInt       int              `json:"sdkInt"`
	Mechanism    string           `json:"mechanism"`
	Capabilities PingCapabilities `json:"capabilities"`
}

// AndroidBounds is an axis-aligned rectangle in the Android agent's
// own screen-pixel coordinate space.
type AndroidBounds struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// AndroidTreeNode is the raw accessibility node shape returned by
// GET /tree, mirroring the agent's own element representation field
// for field. It is the raw agent tree, not the normalized
// wire.TreeNodeWire shape; normalizing one into the other is a later
// stage than this transport client.
// The seven boolean fields below are pointers because the normalizer
// must distinguish "the agent did not report this field" (nil) from
// an explicit false. A plain bool cannot represent that distinction
// and would collapse both cases to the zero value.
type AndroidTreeNode struct {
	ElementID   string            `json:"elementId"`
	ClassName   string            `json:"className,omitempty"`
	ResourceID  string            `json:"resourceId,omitempty"`
	Text        string            `json:"text,omitempty"`
	ContentDesc string            `json:"contentDesc,omitempty"`
	HintText    string            `json:"hintText,omitempty"`
	Bounds      *AndroidBounds    `json:"bounds,omitempty"`
	Clickable   *bool             `json:"clickable,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Focused     *bool             `json:"focused,omitempty"`
	Selected    *bool             `json:"selected,omitempty"`
	Checkable   *bool             `json:"checkable,omitempty"`
	Checked     *bool             `json:"checked,omitempty"`
	Visible     *bool             `json:"visible,omitempty"`
	IsIme       bool              `json:"isIme"`
	Children    []AndroidTreeNode `json:"children"`
}

// KeyboardKey is one on-screen key reported by GET /keyboard.
type KeyboardKey struct {
	Label  string        `json:"label"`
	Bounds AndroidBounds `json:"bounds"`
}

// KeyboardResponse is the response to GET /keyboard. Layout is one
// of "numeric_pad", "qwerty", "alpha", "phone_alpha", "unknown", or
// "none". PackageName and Bounds are nil when no keyboard is
// currently shown.
type KeyboardResponse struct {
	Visible     bool           `json:"visible"`
	PackageName *string        `json:"packageName,omitempty"`
	Layout      string         `json:"layout"`
	Bounds      *AndroidBounds `json:"bounds,omitempty"`
	Keys        []KeyboardKey  `json:"keys"`
}

// ScreenshotResponse is the response to GET /screenshot. The agent
// only ever encodes JPEG.
type ScreenshotResponse struct {
	Base64 string `json:"base64"`
	Format string `json:"format"`
}

// CurrentActivityResponse is the response to GET /current-activity.
// Source names which lookup path produced the value: "accessibility",
// "usage_stats", or "none" when neither is available.
type CurrentActivityResponse struct {
	PackageName string `json:"packageName"`
	Activity    string `json:"activity"`
	Source      string `json:"source"`
}

// InstalledApp is one entry in the GET /apps response.
type InstalledApp struct {
	PackageName string `json:"packageName"`
	Label       string `json:"label"`
}

// GestureResult is the response shape shared by the tap, long-press,
// and swipe action routes. Reason and Code are present only when Ok
// is false; Code is one of "gesture_rejected", "gesture_cancelled",
// or "gesture_timed_out".
type GestureResult struct {
	Ok     bool    `json:"ok"`
	Reason *string `json:"reason,omitempty"`
	Code   *string `json:"code,omitempty"`
}

// CapturedGestureImage is one base64-encoded screenshot the agent
// took at a mid-gesture capture waypoint.
type CapturedGestureImage struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
}

// DispatchGestureResult is the response to POST
// /actions/dispatch_gesture. Pointers reports the pointer count and
// is present only when Ok is true; Reason and Code are present only
// when Ok is false. CapturedImages holds one entry per
// GesturePhaseCapture waypoint the request carried, in dispatch
// order, and is empty when the request carried none.
type DispatchGestureResult struct {
	Ok             bool                   `json:"ok"`
	Pointers       *int                   `json:"pointers,omitempty"`
	Reason         *string                `json:"reason,omitempty"`
	Code           *string                `json:"code,omitempty"`
	CapturedImages []CapturedGestureImage `json:"capturedImages,omitempty"`
}

// AckResult is the response shape for the hide-keyboard and
// force-stop action routes.
type AckResult struct {
	Ok     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// ClearFocusedInputResult is the response to POST
// /actions/clear_focused_input. On success only Ok is meaningful. On
// failure the agent also reports the strategies it tried and enough
// on-screen state to diagnose why every strategy left the field
// non-empty: the value that remained, a description of the focused
// node, and the screen dimensions at the time.
type ClearFocusedInputResult struct {
	Ok              bool     `json:"ok"`
	Reason          string   `json:"reason,omitempty"`
	StrategiesTried []string `json:"strategiesTried,omitempty"`
	LastValue       string   `json:"lastValue,omitempty"`
	FocusedNodeDesc string   `json:"focusedNodeDesc,omitempty"`
	ScreenWidth     int      `json:"screenWidth,omitempty"`
	ScreenHeight    int      `json:"screenHeight,omitempty"`
}

// TypeKeyboardResult is the response to POST /actions/type_keyboard.
type TypeKeyboardResult struct {
	Success bool   `json:"success"`
	Typed   int    `json:"typed"`
	Total   int    `json:"total"`
	Reason  string `json:"reason"`
}

// OkResponse is the response shape shared by the key and launch
// action routes: an unconditional acknowledgement with no success
// semantics of its own.
type OkResponse struct {
	Ok bool `json:"ok"`
}

// TapCoordsRequest is the request body for POST /actions/tap_coords.
type TapCoordsRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LongPressRequest is the request body for POST /actions/long_press.
// The agent falls back to 800 when the field is absent from the
// request body, but this client always sends DurationMs explicitly
// (including an explicit 0), matching the Android driver's own
// request construction.
type LongPressRequest struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	DurationMs int64   `json:"durationMs"`
}

// SwipeRequest is the request body for POST /actions/swipe. The
// agent falls back to 300 when the field is absent from the request
// body, but this client always sends DurationMs explicitly
// (including an explicit 0), matching the Android driver's own
// request construction.
type SwipeRequest struct {
	FromX      float64 `json:"fromX"`
	FromY      float64 `json:"fromY"`
	ToX        float64 `json:"toX"`
	ToY        float64 `json:"toY"`
	DurationMs int64   `json:"durationMs"`
}

// GestureWaypoint is one time-ordered point in a GesturePointer's
// path. Phase is "down", "move", or "up". Pressure is omitted unless
// the caller supplies it; the agent's accessibility-based gesture
// surface never reports pressure back.
type GestureWaypoint struct {
	Phase           string   `json:"phase"`
	X               float64  `json:"x"`
	Y               float64  `json:"y"`
	AtOffsetSeconds float64  `json:"atOffsetSeconds"`
	Pressure        *float64 `json:"pressure,omitempty"`
}

// GesturePointer is one finger's waypoint path within a
// DispatchGestureRequest.
type GesturePointer struct {
	ID        string            `json:"id"`
	Waypoints []GestureWaypoint `json:"waypoints"`
}

// DispatchGestureRequest is the request body for POST
// /actions/dispatch_gesture. A single pointer maps to a single-touch
// gesture; more than one pointer requires the agent's
// PingResponse.Capabilities.CanMultiPointer to be true.
type DispatchGestureRequest struct {
	Pointers []GesturePointer `json:"pointers"`
}

// TypeKeyboardRequest is the request body for POST
// /actions/type_keyboard. PerKeyDelayMs defaults to 80 and
// ClearFirst defaults to true on the agent when omitted.
type TypeKeyboardRequest struct {
	Text          string `json:"text"`
	PerKeyDelayMs int64  `json:"perKeyDelayMs,omitempty"`
	ClearFirst    *bool  `json:"clearFirst,omitempty"`
}

// KeyRequest is the request body for POST /actions/key.
type KeyRequest struct {
	Key string `json:"key"`
}

// LaunchRequest is the request body for POST /actions/launch.
type LaunchRequest struct {
	PackageName string `json:"packageName"`
}

// ForceStopRequest is the request body for POST /actions/force_stop.
type ForceStopRequest struct {
	PackageName string `json:"packageName"`
}
