package mcp

import (
	"context"
	"errors"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
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
// handler. Probe, Factory, Storage, and RunStarter are required.
// Probe backs list_devices, Factory backs select_device, Storage holds
// bug reports, case studies, and flat run records, and RunStarter
// creates the run folders of new runs. RunStarter also names the storage
// root, and the run catalog that reads every stored run and the
// directory of screenshots taken with no run open both derive from it,
// so the server never holds a second root. RunLiveness decides which
// unfinished runs are still running, and defaults to the lock every run
// writer holds under that root, so a run another process is writing
// lists as running. RunStore, Clock, and Logger default to a fresh
// in-memory RunStore, the system clock, and a no-op logger when left
// zero-valued.
//
// Session carries the identity resolver, lease manager and orphan reaper
// device selection goes through, and a composition root sets all three.
// Left zero, selection takes no lease. Timeouts left at zero take the
// session package defaults.
type ServerDeps struct {
	Probe    device.Probe
	Runners  device.RunnerIdentifier
	Factory  device.WrappingFactory
	Storage  storage.Storage
	RunStore *storage.RunStore
	// RunStarter creates the run folder of each run the server starts.
	RunStarter  RunStarter
	RunLiveness runfolder.Liveness
	Clock       orchestra.Clock
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
// pass it to Serve to run it over stdio. It returns an error when a
// required dependency is missing.
func NewServer(info Info, deps ServerDeps) (*Server, error) {
	if deps.RunStarter == nil || deps.RunStarter.Root() == "" {
		return nil, errors.New("mcp: ServerDeps needs a RunStarter that names its storage root")
	}
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
	runRoot := deps.RunStarter.Root()
	liveness := deps.RunLiveness
	if liveness == nil {
		liveness = runfolder.NewLockLiveness(runRoot)
	}
	runs := storage.NewRunCatalog(deps.Storage, runRoot, liveness)

	runSession := newRunSession(runSessionDeps{
		Store: runStore, Catalog: runs, Starter: deps.RunStarter, Clock: clk, Logger: logger,
	})
	devices := NewDeviceSession(DeviceSessionDeps{
		Factory: deps.Factory, Probe: deps.Probe, Runners: deps.Runners, Clock: clk, TimerClock: timers, Logger: logger,
		Session: deps.Session, Timeouts: deps.Timeouts, Runs: runSession,
	})
	traits := newToolTraits()
	tc := &ToolContext{
		Session:    devices,
		Traits:     traits,
		Logger:     logger,
		Storage:    deps.Storage,
		Runs:       runs,
		RunSession: runSession,
		RunStore:   runStore,
		Clock:      clk,
	}

	// Single choke point for every tools/call request: value-constraint
	// validation, the per-call timeout wrapper and the session's idle
	// bookkeeping apply uniformly to all registered tools without each
	// tool's handler knowing about any of them. Registered before any AddTool call so it is in place for
	// every tool the loop below adds.
	server.AddReceivingMiddleware(newToolCallMiddleware(logger, timers, devices, devices, traits))

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

	return &Server{Server: server, session: devices}, nil
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
