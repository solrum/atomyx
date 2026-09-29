package orchestra

import "dev.atomyx/internal/clock"

// Clock, SystemClock, and FakeClock are aliases onto the shared
// clock package so every existing caller that names them through
// orchestra keeps compiling unchanged, while the concrete types
// themselves live in a leaf package other packages may depend on
// without importing orchestra.
type (
	Clock       = clock.Clock
	SystemClock = clock.SystemClock
	FakeClock   = clock.FakeClock
)

// NewFakeClock constructs a FakeClock starting at startMs.
func NewFakeClock(startMs int64) *FakeClock {
	return clock.NewFakeClock(startMs)
}
