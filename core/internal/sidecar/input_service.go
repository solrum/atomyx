package sidecar

import (
	"context"
	"math"
	"sync"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// inputService dispatches coordinate-based input on the currently
// selected device. Callers only know normalized ratios (0..1 across
// their own viewport); inputService queries the driver for the
// device's logical screen size and converts ratios to absolute
// points before dispatching.
type inputService struct {
	session *Session

	mu             sync.Mutex
	cachedDeviceID string
	cachedScreen   wire.Size
	hasCache       bool
}

func newInputService(session *Session) *inputService {
	return &inputService{session: session}
}

// TapRatio taps the point (xRatio, yRatio) expressed as a fraction of
// the device's screen. When bundleID is non-empty, the app is
// attached to (without resetting its state) before the tap dispatches,
// because iOS cannot tap without an app reference.
func (s *inputService) TapRatio(ctx context.Context, xRatio, yRatio float64, bundleID string) error {
	selection, err := requireDeviceOrWireError(s.session)
	if err != nil {
		return err
	}
	screen, err := s.screenSize(ctx, selection)
	if err != nil {
		return err
	}
	x := clampF(math.Round(xRatio*screen.Width), 0, screen.Width-1)
	y := clampF(math.Round(yRatio*screen.Height), 0, screen.Height-1)

	if bundleID != "" {
		noReset := true
		if err := selection.Orchestra.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &noReset}); err != nil {
			return err
		}
	}
	return selection.Orchestra.TapAt(ctx, wire.Point{X: x, Y: y})
}

func (s *inputService) screenSize(ctx context.Context, selection *DeviceSelection) (wire.Size, error) {
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

func clampF(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
