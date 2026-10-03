package cli

import (
	"io"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
)

// Deps are the dependencies Run needs to execute a command. A caller
// injects a real device.CompositeProbe and device.DriverFactory in
// production, or a device.StaticProbe and device.MockDriverFactory in
// a test.
type Deps struct {
	// Probe scans for connected devices, backing list-devices.
	Probe device.Probe
	// Factory builds a connected driver and orchestra for the
	// platform and device a run targets.
	Factory device.WrappingFactory
	// Runners identifies the iOS runner already served on the default
	// control port, so a run without --device targets that runner's
	// device. A nil value means no runner is ever found.
	Runners device.RunnerIdentifier
	// Clock times a script run's steps. Defaults to
	// orchestra.SystemClock{} when nil.
	Clock orchestra.Clock
	// Logger is the structured log sink a run or suite command writes
	// its step trace through. Defaults to logging.NoopLogger{} when
	// nil, which a caller only wants in a test that asserts on
	// command output rather than on logged entries.
	Logger logging.Logger
	// Stdout and Stderr are the streams every command writes to.
	// Both default to io.Discard when nil, which a caller only
	// wants in a test that reads output some other way.
	Stdout io.Writer
	Stderr io.Writer
	// Session carries the device arbitration dependencies: the lease
	// manager, the identity resolver and the orphan reaper. Its Factory
	// is always Deps.Factory. A zero value disables exclusion, which
	// only a test wants.
	Session session.Deps
	// Timeouts bounds a device acquisition. A zero DeviceAcquireMs means
	// session.DefaultDeviceAcquireTimeoutMs.
	Timeouts session.Timeouts
	// Browser opens the inspector page. Nil means the inspect command
	// only prints the URL.
	Browser BrowserOpener
	// Stdin is read by the inspect command for the Enter key that asks
	// for a fresh URL. Nil means no key is read.
	Stdin io.Reader
	// HostCommand runs a host tool for the doctor bundle's version
	// queries. Nil records every tool as unavailable.
	HostCommand HostCommand
	// Debug, when true, appends a stack trace to an unhandled error
	// reported on Stderr, matching ATOMYX_DEBUG=1.
	Debug bool
}

func (d Deps) stdout() io.Writer {
	if d.Stdout == nil {
		return io.Discard
	}
	return d.Stdout
}

func (d Deps) stderr() io.Writer {
	if d.Stderr == nil {
		return io.Discard
	}
	return d.Stderr
}

func (d Deps) clock() orchestra.Clock {
	if d.Clock == nil {
		return orchestra.SystemClock{}
	}
	return d.Clock
}

func (d Deps) logger() logging.Logger {
	if d.Logger == nil {
		return logging.NoopLogger{}
	}
	return d.Logger
}
