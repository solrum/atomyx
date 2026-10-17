package driver

import (
	"context"
	"fmt"
	"strings"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

// Orientation is a platform-neutral name for the way a device is held.
// Left and right name the direction the top edge of the device turns
// from upright: landscapeLeft is a quarter turn counterclockwise and
// landscapeRight a quarter turn clockwise.
type Orientation string

const (
	OrientationPortrait       Orientation = "portrait"
	OrientationLandscapeLeft  Orientation = "landscapeLeft"
	OrientationLandscapeRight Orientation = "landscapeRight"
)

const (
	// OrientationSettleTimeoutMs bounds how long SetOrientation waits
	// for the reported screen size to reflect the new orientation.
	OrientationSettleTimeoutMs = 10000

	// OrientationPollIntervalMs is the gap between screen size reads
	// while waiting.
	OrientationPollIntervalMs = 250
)

// AllOrientations returns every orientation name in a stable order.
func AllOrientations() []Orientation {
	return []Orientation{OrientationPortrait, OrientationLandscapeLeft, OrientationLandscapeRight}
}

// ParseOrientation returns the Orientation named by name, or false when
// no orientation has that name.
func ParseOrientation(name string) (Orientation, bool) {
	for _, o := range AllOrientations() {
		if string(o) == name {
			return o, true
		}
	}
	return "", false
}

// IsLandscape reports whether o lays the screen out wider than tall.
func (o Orientation) IsLandscape() bool {
	return o == OrientationLandscapeLeft || o == OrientationLandscapeRight
}

// ValidateOrientation rejects a name no adapter knows. The error carries
// diag.CodeInvalidArgument and lists the accepted names.
func ValidateOrientation(o Orientation) error {
	if _, ok := ParseOrientation(string(o)); ok {
		return nil
	}
	names := make([]string, 0, len(AllOrientations()))
	for _, known := range AllOrientations() {
		names = append(names, string(known))
	}
	return diag.New(diag.CodeInvalidArgument,
		fmt.Sprintf("%q is not an orientation, use one of: %s", o, strings.Join(names, ", ")),
		diag.InvalidArgumentRemedy, nil)
}

// SizeMatchesOrientation reports whether a screen of the given size is
// laid out the way o requires: wider than tall for a landscape
// orientation and taller than wide for portrait. The size cannot tell
// landscapeLeft from landscapeRight.
func SizeMatchesOrientation(o Orientation, size wire.Size) bool {
	if o.IsLandscape() {
		return size.Width > size.Height
	}
	return size.Height > size.Width
}

// AwaitOrientation reads the screen size through readSize until it
// matches o, and fails with diag.CodeOrientationNotSet when it does not
// within OrientationSettleTimeoutMs. The size is read at least once, so
// a screen that already matches returns without sleeping.
//
// A read error whose code marks the agent as momentarily out of reach
// does not end the wait. When the deadline passes without a matching
// size, the last read error is returned if the final attempt failed,
// and the orientation error otherwise. Any other read error, including
// a context cancellation, is returned immediately.
func AwaitOrientation(ctx context.Context, clk clock.Clock, o Orientation, readSize func(context.Context) (wire.Size, error)) error {
	deadline := clk.NowMs() + OrientationSettleTimeoutMs
	var lastErr error
	for {
		size, err := readSize(ctx)
		if err != nil && !diag.IsTransient(err) {
			return err
		}
		lastErr = err
		if err == nil && SizeMatchesOrientation(o, size) {
			return nil
		}
		if clk.NowMs() >= deadline {
			if lastErr != nil {
				return lastErr
			}
			return diag.NewOrientationNotSet(fmt.Sprintf(
				"the screen did not become %s within %d seconds", o, OrientationSettleTimeoutMs/1000), nil)
		}
		if err := clk.Sleep(ctx, OrientationPollIntervalMs); err != nil {
			return err
		}
	}
}
