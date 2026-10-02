package device

import (
	"context"

	"dev.atomyx/internal/iosdriver"
)

// RunnerPreparer does the slow, lease-free work a device needs before it
// can be driven, such as compiling the iOS runner. It is separate from
// the Factory so a caller can run it before it takes the device lease
// and under its own time budget.
//
// Prepare returns nil without work for a descriptor that needs none: an
// Android device, a descriptor with no id, or an iOS descriptor whose
// runner is already served. It is safe to call repeatedly, and
// concurrent calls across processes build at most once.
type RunnerPreparer interface {
	Prepare(ctx context.Context, desc Descriptor) error
}

// DefaultRunnerBuildTimeoutMs is the default time a caller allows
// Prepare, including the wait for another process's build of the same
// runner.
const DefaultRunnerBuildTimeoutMs = iosdriver.DefaultBuildTimeoutMs
