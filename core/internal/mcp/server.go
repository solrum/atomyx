package mcp

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
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
type ServerDeps struct {
	Probe    device.Probe
	Factory  device.Factory
	Storage  storage.Storage
	RunStore *storage.RunStore
	Clock    orchestra.Clock
	Logger   logging.Logger
}

// NewServer constructs the Atomyx MCP server and registers its tool
// surface. The returned server is not yet connected to a transport;
// pass it to Serve to run it over stdio.
func NewServer(info Info, deps ServerDeps) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    info.Name,
		Version: info.Version,
	}, nil)

	clock := deps.Clock
	if clock == nil {
		clock = orchestra.SystemClock{}
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	runStore := deps.RunStore
	if runStore == nil {
		runStore = storage.NewRunStore()
	}

	session := NewDeviceSession(DeviceSessionDeps{Factory: deps.Factory, Clock: clock, Logger: logger})
	tc := &ToolContext{
		Session:  session,
		Logger:   logger,
		Storage:  deps.Storage,
		RunStore: runStore,
		Clock:    clock,
	}

	// Single choke point for every tools/call request: value-constraint
	// validation plus the per-call timeout wrapper apply uniformly to
	// all registered tools without each tool's handler knowing about
	// either. Registered before any AddTool call so it is in place for
	// every tool the loop below adds.
	server.AddReceivingMiddleware(newToolCallMiddleware(logger))

	// Registration order matches DEFAULT_TOOLS in
	// packages/mcp/src/tools/index.ts: device + app lifecycle first,
	// then orientation/discovery, core actions, wait/verification, run
	// lifecycle + reporting, run/bug queries, run/bug CRUD, guidance,
	// then script execution. check_health has no TypeScript
	// counterpart; it is registered here, alongside the other
	// device-session tools it reports on.
	registerListDevicesTool(server, deps.Probe)
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

	return server
}

// Serve runs server over stdio until ctx is done or the client
// disconnects.
func Serve(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}
