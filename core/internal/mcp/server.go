package mcp

import (
	"context"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Info identifies this server implementation to a connecting client.
type Info struct {
	Name    string
	Version string
}

// ServerDeps are the services NewServer wires into every tool
// handler. Probe and Factory are required: they back list_devices and
// select_device respectively. Storage, RunStore, Clock, and Logger
// default to a file-backed store rooted per storage.NewFileStorage's
// own precedence, a fresh in-memory RunStore, the system clock, and a
// no-op logger when left zero-valued.
//
// Session carries the identity resolver, lease manager and orphan reaper
// device selection goes through, and a composition root sets all three.
// Left zero, selection takes no lease. Timeouts left at zero take the
// session package defaults.
type ServerDeps struct {
	Probe    device.Probe
	Runners  device.RunnerIdentifier
	Factory  device.Factory
	Storage  storage.Storage
	RunStore *storage.RunStore
	Clock    orchestra.Clock
	// TimerClock drives tool call budgets, the device acquisition
	// budget and the idle timers. It defaults to the system clock, and
	// only a test that controls time injects another.
	TimerClock clock.Clock
	Logger     logging.Logger
	Session    session.Deps
	Timeouts   session.Timeouts
}

// NewServer constructs the Atomyx MCP server and registers its tool
// surface. The returned server is not yet connected to a transport;
// pass it to Serve to run it over stdio.
func NewServer(info Info, deps ServerDeps) *Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    info.Name,
		Version: info.Version,
	}, nil)

	clk := deps.Clock
	if clk == nil {
		clk = orchestra.SystemClock{}
	}
	timers := deps.TimerClock
	if timers == nil {
		timers = clock.SystemClock{}
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	runStore := deps.RunStore
	if runStore == nil {
		runStore = storage.NewRunStore()
	}

	tracker := &runTracker{runs: runStore, storage: deps.Storage, clock: clk, logger: logger}
	devices := NewDeviceSession(DeviceSessionDeps{
		Factory: deps.Factory, Probe: deps.Probe, Runners: deps.Runners, Clock: clk, TimerClock: timers, Logger: logger,
		Session: deps.Session, Timeouts: deps.Timeouts, Runs: tracker,
	})
	traits := newToolTraits()
	tc := &ToolContext{
		Session:  devices,
		Traits:   traits,
		Logger:   logger,
		Storage:  deps.Storage,
		RunStore: runStore,
		Clock:    clk,
	}

	// Single choke point for every tools/call request: value-constraint
	// validation, the per-call timeout wrapper and the session's idle
	// bookkeeping apply uniformly to all registered tools without each
	// tool's handler knowing about any of them. Registered before any AddTool call so it is in place for
	// every tool the loop below adds.
	server.AddReceivingMiddleware(newToolCallMiddleware(logger, timers, devices, traits))

	// Registration order matches DEFAULT_TOOLS in
	// packages/mcp/src/tools/index.ts: device + app lifecycle first,
	// then orientation/discovery, core actions, wait/verification, run
	// lifecycle + reporting, run/bug queries, run/bug CRUD, guidance,
	// then script execution. check_health has no TypeScript
	// counterpart; it is registered here, alongside the other
	// device-session tools it reports on.
	registerListDevicesTool(server, traits, deps.Probe)
	registerSelectDeviceTool(server, tc)
	registerDisconnectDeviceTool(server, tc)
	registerCheckHealthTool(server, tc)
	registerListAppsTool(server, tc)
	registerLaunchAppTool(server, tc)

	registerGetUiTreeTool(server, tc)
	registerFindElementTool(server, tc)
	registerScreenshotTool(server, tc)

	registerTapTool(server, tc)
	registerTapAndWaitTransitionTool(server, tc)
	registerInputTextTool(server, tc)
	registerSwipeTool(server, tc)
	registerPressKeyTool(server, tc)

	registerWaitForElementTool(server, tc)

	registerStartRunTool(server, tc)
	registerFinishRunTool(server, tc)
	registerReportBugTool(server, tc)

	registerListRunsTool(server, tc)
	registerGetRunTool(server, tc)
	registerListBugsTool(server, tc)
	registerGetBugTool(server, tc)

	registerUpdateRunSummaryTool(server, tc)
	registerDeleteRunTool(server, tc)
	registerDeleteBugTool(server, tc)

	registerAddCaseStudyTool(server, tc)
	registerGetCaseStudiesTool(server, tc)

	registerRunScriptTool(server, tc)

	return &Server{Server: server, session: devices}
}

// Server is the Atomyx MCP server together with the device session its
// tools share, so whoever runs the server can also release the device
// when it stops.
type Server struct {
	*mcp.Server
	session *DeviceSession
}

// Close releases the active device, stopping every child process its
// driver started. It is idempotent, and a device selection that
// completes after Close is released as well. Call it after Serve
// returns, whatever the reason.
func (s *Server) Close() {
	s.session.Close()
}

// Serve runs server over stdio until ctx is done or the client
// disconnects. It does not release the active device: the caller
// calls Close afterwards.
func Serve(ctx context.Context, server *Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}
