package mcp

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/history"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/storage"
)

// ToolContext is the common dependency bundle every tool handler
// closes over. It is built once per server instance and shared by
// every registered tool; a handler reads only the fields its own
// logic needs.
type ToolContext struct {
	// Session is the server's single device-selection container.
	// Handlers that drive the device call Session.Ensure() on every
	// invocation rather than caching an Orchestra reference, so a
	// mid-session select_device call is observed immediately and a
	// device the session released is taken again.
	Session *DeviceSession
	// Traits holds what tools declared about themselves at registration.
	Traits *toolTraits
	// Logger is the structured logging sink every handler and the
	// run_script command registry write through.
	Logger logging.Logger
	// Storage is the persistence port for bug reports and case
	// studies.
	Storage storage.Storage
	// Runs reads persisted runs from every stored form and deletes them.
	Runs *storage.RunCatalog
	// History is the read port get_test_stats aggregates over. The
	// composition root binds it to the same catalog as Runs, so handlers
	// depend on the contract and not on the storage type.
	History history.Source
	// RunSession creates, finalizes, and edits the MCP runs written to
	// run folders.
	RunSession *runSession
	// RunStore tracks the single in-flight test run.
	RunStore *storage.RunStore
	// Clock is the time source for handlers that poll or measure
	// elapsed time (tap_and_wait_transition).
	Clock orchestra.Clock
	// HTTP sends the requests of httpRequest steps in run_script. Nil
	// makes such a step fail.
	HTTP runner.HTTPDoer
}

// errNoActiveDevice is returned by orchestraOrFail when no device is
// selected. Its message is surfaced to the calling agent verbatim, so
// it must stay actionable: name the tool that fixes the situation.
var errNoActiveDevice = diag.New(
	diag.CodeDeviceNotFound,
	"no active device. Call select_device with a platform and id "+
		"from list_devices before driving the UI",
	"Call select_device with a platform and device id from list_devices, then retry.",
	nil,
)

// orchestraOrFail resolves the active device's Orchestra, taking a
// device the session released or lost again first, or reports
// errNoActiveDevice when none was ever selected. A re-acquire that finds
// the device held by another process reports DEVICE_RELEASED_HELD.
// Callers that need a device for the tool to mean anything at all use
// this and turn a non-nil error into an isError:true result; tools where
// "no device yet" is itself an agent-actionable outcome (report_bug,
// disconnect_device) read ctx.Session.Current() directly instead.
func orchestraOrFail(ctx context.Context, tc *ToolContext) (*orchestra.Orchestra, error) {
	active, err := tc.Session.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return active.Orchestra, nil
}
