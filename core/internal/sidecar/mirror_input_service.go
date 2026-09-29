package sidecar

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// pinchHalfGapFraction is the half-gap between the two synthesised
// pinch fingers at scale 1.0, as a fraction of the device's shorter
// screen edge. The fingers start at center plus or minus (this times
// fromScale) and end at center plus or minus (this times toScale), so
// the gap grows on zoom-in and shrinks on zoom-out.
const pinchHalfGapFraction = 0.18

// defaultLongPressMs, defaultSwipeMs, and defaultPinchMs match the
// TypeScript sidecar's InputService defaults, used when a caller
// omits durationMs.
const (
	defaultLongPressMs = 500
	defaultSwipeMs     = 200
	defaultPinchMs     = 250
)

// mirrorInputService dispatches ratio-based input and streaming touch
// events for the Studio mirror canvas against the session's currently
// selected device. Mirror taps arrive keyed by a deviceId that the
// mirror session owns regardless of which device the session
// currently points at; this service does not itself switch device
// selection, matching inputService's convention of operating on
// whatever device selectDevice most recently chose.
//
// Readiness gate: each action checks whether the platform agent
// (XCUITest for iOS, the Atomyx Agent APK for Android) reports its
// ready state before dispatching, via the injected
// IosAgentStatusProvider / AndroidAgentStatusProvider. An action
// against a device whose agent has not reached ready state fails with
// DriverNotReadyError.
//
// Streaming touch methods additionally require the selected device's
// driver to implement driver.StreamingTouchCapable; when it does not,
// StreamingTouchNotSupportedError is returned so the caller can fall
// back to the classified touch path.
//
// Screen size is cached per device selection, the same pattern
// inputService uses, because streaming touch events fire many times
// per drag gesture.
type mirrorInputService struct {
	session      *Session
	iosAgent     IosAgentStatusProvider
	androidAgent AndroidAgentStatusProvider

	mu             sync.Mutex
	cachedDeviceID string
	cachedScreen   wire.Size
	hasCache       bool
}

func newMirrorInputService(session *Session, iosAgent IosAgentStatusProvider, androidAgent AndroidAgentStatusProvider) *mirrorInputService {
	return &mirrorInputService{session: session, iosAgent: iosAgent, androidAgent: androidAgent}
}

func (s *mirrorInputService) TapRatio(ctx context.Context, deviceID string, xRatio, yRatio float64, bundleID string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	point, err := s.ratioToPoint(ctx, selection, xRatio, yRatio)
	if err != nil {
		return err
	}
	if err := s.attachBundle(ctx, selection, bundleID); err != nil {
		return err
	}
	return selection.Orchestra.TapAt(ctx, point)
}

func (s *mirrorInputService) LongPressRatio(ctx context.Context, deviceID string, xRatio, yRatio float64, durationMs *int, bundleID string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	point, err := s.ratioToPoint(ctx, selection, xRatio, yRatio)
	if err != nil {
		return err
	}
	if err := s.attachBundle(ctx, selection, bundleID); err != nil {
		return err
	}
	duration := defaultLongPressMs
	if durationMs != nil {
		duration = *durationMs
	}
	return selection.Orchestra.LongPressAt(ctx, point, duration)
}

func (s *mirrorInputService) SwipeRatio(ctx context.Context, deviceID string, fromXRatio, fromYRatio, toXRatio, toYRatio float64, durationMs *int, bundleID string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	from, err := s.ratioToPoint(ctx, selection, fromXRatio, fromYRatio)
	if err != nil {
		return err
	}
	to, err := s.ratioToPoint(ctx, selection, toXRatio, toYRatio)
	if err != nil {
		return err
	}
	if err := s.attachBundle(ctx, selection, bundleID); err != nil {
		return err
	}
	duration := defaultSwipeMs
	if durationMs != nil {
		duration = *durationMs
	}
	return selection.Orchestra.SwipeAt(ctx, from, to, duration)
}

// PinchRatio dispatches a two-finger pinch centred on a normalised
// point. The two fingers are placed symmetrically on the horizontal
// axis around the centre; their gap is proportional to scale, so the
// device sees them spread apart (zoom-in) or close together
// (zoom-out).
func (s *mirrorInputService) PinchRatio(ctx context.Context, deviceID string, centerXRatio, centerYRatio, fromScale, toScale float64, durationMs *int, bundleID string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	screen, err := s.screenSize(ctx, selection)
	if err != nil {
		return err
	}
	cx := clampF(math.Round(centerXRatio*screen.Width), 0, screen.Width-1)
	cy := clampF(math.Round(centerYRatio*screen.Height), 0, screen.Height-1)
	baseHalfGap := math.Min(screen.Width, screen.Height) * pinchHalfGapFraction
	fromGap := baseHalfGap * fromScale
	toGap := baseHalfGap * toScale

	duration := defaultPinchMs
	if durationMs != nil {
		duration = *durationMs
	}
	if duration < 1 {
		duration = 1
	}
	durationSeconds := float64(duration) / 1000

	left := func(gap float64) wire.Point {
		return wire.Point{X: clampF(math.Round(cx-gap), 0, screen.Width-1), Y: cy}
	}
	right := func(gap float64) wire.Point {
		return wire.Point{X: clampF(math.Round(cx+gap), 0, screen.Width-1), Y: cy}
	}

	gesture := driver.Gesture{
		Pointers: []driver.GesturePointer{
			{
				ID: "pinch-a",
				Waypoints: []driver.GestureWaypoint{
					{Phase: driver.GesturePhaseDown, Point: left(fromGap), AtOffsetSeconds: 0},
					{Phase: driver.GesturePhaseMove, Point: left(toGap), AtOffsetSeconds: durationSeconds},
					{Phase: driver.GesturePhaseUp, Point: left(toGap), AtOffsetSeconds: durationSeconds},
				},
			},
			{
				ID: "pinch-b",
				Waypoints: []driver.GestureWaypoint{
					{Phase: driver.GesturePhaseDown, Point: right(fromGap), AtOffsetSeconds: 0},
					{Phase: driver.GesturePhaseMove, Point: right(toGap), AtOffsetSeconds: durationSeconds},
					{Phase: driver.GesturePhaseUp, Point: right(toGap), AtOffsetSeconds: durationSeconds},
				},
			},
		},
	}

	if err := s.attachBundle(ctx, selection, bundleID); err != nil {
		return err
	}
	_, err = selection.Orchestra.DispatchGesture(ctx, gesture)
	return err
}

func (s *mirrorInputService) InputText(ctx context.Context, deviceID, text string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	return selection.Orchestra.TypeText(ctx, text)
}

// EraseText deletes count characters backward from the cursor by
// sending count delete keystrokes via the text path, not via
// Orchestra.EraseText: the driver's eraseText primitive clears the
// whole field (select-all plus delete semantics), which is wrong for
// a per-keystroke backspace, while the delete control character
// U+0008 is interpreted as a single backward delete by the device's
// text-entry layer.
func (s *mirrorInputService) EraseText(ctx context.Context, deviceID string, count int) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	if count <= 0 {
		return nil
	}
	return selection.Orchestra.TypeText(ctx, strings.Repeat("\u0008", count))
}

func (s *mirrorInputService) PressKey(ctx context.Context, deviceID, key string) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	_, err = selection.Orchestra.PressKey(ctx, driver.KeyCode(key))
	return err
}

func (s *mirrorInputService) StreamingDown(ctx context.Context, deviceID string, xRatio, yRatio float64, touchID int) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	streaming, err := s.requireStreamingDriver(deviceID, selection)
	if err != nil {
		return err
	}
	point, err := s.ratioToPoint(ctx, selection, xRatio, yRatio)
	if err != nil {
		return err
	}
	return streaming.StreamingTouchDown(ctx, point, touchID)
}

func (s *mirrorInputService) StreamingMove(ctx context.Context, deviceID string, xRatio, yRatio float64, touchID int) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	streaming, err := s.requireStreamingDriver(deviceID, selection)
	if err != nil {
		return err
	}
	point, err := s.ratioToPoint(ctx, selection, xRatio, yRatio)
	if err != nil {
		return err
	}
	return streaming.StreamingTouchMove(ctx, point, touchID)
}

func (s *mirrorInputService) StreamingUp(ctx context.Context, deviceID string, xRatio, yRatio float64, touchID int) error {
	selection, err := s.assertAgentReady(deviceID)
	if err != nil {
		return err
	}
	streaming, err := s.requireStreamingDriver(deviceID, selection)
	if err != nil {
		return err
	}
	point, err := s.ratioToPoint(ctx, selection, xRatio, yRatio)
	if err != nil {
		return err
	}
	return streaming.StreamingTouchUp(ctx, point, touchID)
}

// attachBundle attaches to bundleID without resetting its state
// before a coordinate-based action dispatches, because iOS cannot
// tap without an app reference. A no-op when bundleID is empty.
func (s *mirrorInputService) attachBundle(ctx context.Context, selection *DeviceSelection, bundleID string) error {
	if bundleID == "" {
		return nil
	}
	noReset := true
	return selection.Orchestra.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &noReset})
}

// assertAgentReady checks the agent readiness for deviceID against
// the session's currently selected device, returning
// DriverNotReadyError when no device is selected or the platform
// agent has not yet reached ready state. Taps that arrive while the
// agent is still starting up are rejected so they do not queue and
// replay as ghost inputs once the agent comes up.
func (s *mirrorInputService) assertAgentReady(deviceID string) (*DeviceSelection, error) {
	selection := s.session.Device()
	if selection == nil {
		return nil, &DriverNotReadyError{
			Message: fmt.Sprintf("device-id not ready: no device selected (id=%s)", deviceID),
		}
	}
	if selection.Platform == "ios" {
		status := s.iosAgent.Status(deviceID)
		if status.State != IosAgentStateReady {
			return nil, &DriverNotReadyError{
				Message: fmt.Sprintf("device-id not ready: state=%s (id=%s)", status.State, deviceID),
			}
		}
	} else {
		status := s.androidAgent.Status(deviceID)
		if status.State != AndroidAgentStateReady {
			return nil, &DriverNotReadyError{
				Message: fmt.Sprintf("device-id not ready: state=%s (id=%s)", status.State, deviceID),
			}
		}
	}
	return selection, nil
}

// requireStreamingDriver returns selection's driver narrowed to
// driver.StreamingTouchCapable, or StreamingTouchNotSupportedError
// when the driver does not implement it, so the caller can fall back
// to the classified touch path without crashing.
func (s *mirrorInputService) requireStreamingDriver(deviceID string, selection *DeviceSelection) (driver.StreamingTouchCapable, error) {
	streaming, ok := driver.AsStreamingTouchCapable(selection.Driver)
	if !ok {
		return nil, &StreamingTouchNotSupportedError{
			Message: fmt.Sprintf(
				"streaming touch not supported for this device (id=%s). The HID path requires a simulator on arm64 with Xcode 26 or later.",
				deviceID,
			),
		}
	}
	return streaming, nil
}

func (s *mirrorInputService) screenSize(ctx context.Context, selection *DeviceSelection) (wire.Size, error) {
	s.mu.Lock()
	if s.hasCache && s.cachedDeviceID == selection.ID {
		size := s.cachedScreen
		s.mu.Unlock()
		return size, nil
	}
	s.mu.Unlock()

	size, err := selection.Driver.ScreenSize(ctx)
	if err != nil {
		return wire.Size{}, err
	}
	s.mu.Lock()
	s.cachedDeviceID = selection.ID
	s.cachedScreen = size
	s.hasCache = true
	s.mu.Unlock()
	return size, nil
}

func (s *mirrorInputService) ratioToPoint(ctx context.Context, selection *DeviceSelection, xRatio, yRatio float64) (wire.Point, error) {
	screen, err := s.screenSize(ctx, selection)
	if err != nil {
		return wire.Point{}, err
	}
	return wire.Point{
		X: clampF(math.Round(xRatio*screen.Width), 0, screen.Width-1),
		Y: clampF(math.Round(yRatio*screen.Height), 0, screen.Height-1),
	}, nil
}
