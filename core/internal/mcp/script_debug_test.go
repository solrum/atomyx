package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// blockingDriver holds its first screenshot until released, so a test can
// set a breakpoint while a script is mid-run.
type blockingDriver struct {
	*mockdriver.Driver
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *blockingDriver) Screenshot(ctx context.Context) ([]byte, error) {
	b.once.Do(func() {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
	return b.Driver.Screenshot(ctx)
}

type blockingFactory struct{ drv *blockingDriver }

func (f blockingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f blockingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

// debugScript has its steps on lines 4 and 5.
const debugScript = "appId: com.example.app\nname: Debug\nsteps:\n  - screenshot: first\n  - screenshot: second\n"

type debugHarness struct {
	t        *testing.T
	srv      *Server
	cs       *mcp.ClientSession
	ctx      context.Context
	clk      *clock.ManualClock
	drv      *blockingDriver
	progress chan recordedPhase
	result   chan map[string]any
}

func newDebugHarness(t *testing.T) *debugHarness {
	t.Helper()
	h := &debugHarness{
		t: t, ctx: context.Background(), clk: clock.NewManualClock(0),
		drv:      &blockingDriver{Driver: mockdriver.New(), entered: make(chan struct{}), release: make(chan struct{})},
		progress: make(chan recordedPhase, 32), result: make(chan map[string]any, 1),
	}
	h.drv.StageHierarchy(sampleTree())
	h.srv = mustNewServer(t, ServerDeps{
		Probe: device.NewStaticProbe(), Factory: blockingFactory{drv: h.drv}, Storage: storage.NewInMemoryStorage(),
		TimerClock: h.clk,
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := h.srv.Connect(h.ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "atomyx-test-client", Version: "0.0.0-test"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			h.progress <- recordedPhase{progress: req.Params.Progress, total: req.Params.Total, message: req.Params.Message}
		},
	})
	h.cs, err = client.Connect(h.ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.cs.Close() })
	selectMockDevice(t, h.cs, h.ctx)
	t.Cleanup(func() {
		if sess := h.held(); sess != nil {
			_, _ = sess.Resume(h.ctx, session.ResumeOptions{Force: true})
		}
		select {
		case <-h.drv.release:
		default:
			close(h.drv.release)
		}
	})
	return h
}

func (h *debugHarness) held() *session.Session {
	h.srv.session.mu.Lock()
	defer h.srv.session.mu.Unlock()
	return h.srv.session.sess
}

// startScript runs script as a run_script call that asks for progress.
func (h *debugHarness) startScript(script string) {
	h.t.Helper()
	params := &mcp.CallToolParams{Name: "run_script", Arguments: map[string]any{"script": script}}
	params.SetProgressToken("debug-run")
	go func() {
		res, err := h.cs.CallTool(h.ctx, params)
		decoded := map[string]any{}
		if err != nil {
			decoded["transportError"] = err.Error()
		} else if text, ok := res.Content[0].(*mcp.TextContent); ok {
			_ = json.Unmarshal([]byte(text.Text), &decoded)
		}
		h.result <- decoded
	}()
}

// pauseBeforeLine starts script, sets an inline breakpoint on line while the
// first step is held in the driver, and returns once the run is paused.
func (h *debugHarness) pauseBeforeLine(script string, line string) session.PauseLocation {
	h.t.Helper()
	h.startScript(script)
	h.awaitEntered()
	if err := h.held().SetBreakpoints([]string{"inline:" + line}); err != nil {
		h.t.Fatalf("SetBreakpoints: %v", err)
	}
	close(h.drv.release)
	var loc session.PauseLocation
	eventually(h.t, "the run to pause", func() bool {
		var paused bool
		loc, paused = h.held().Paused()
		return paused
	})
	return loc
}

func (h *debugHarness) awaitEntered() {
	h.t.Helper()
	select {
	case <-h.drv.entered:
	case <-time.After(yieldWait):
		h.t.Fatal("the script never reached its first step")
	}
}

func (h *debugHarness) awaitResult() map[string]any {
	h.t.Helper()
	select {
	case r := <-h.result:
		return r
	case <-time.After(yieldWait):
		h.t.Fatal("run_script did not return")
		return nil
	}
}

func TestScriptDebug_AnInlineScriptPausesAtItsInlineLocation(t *testing.T) {
	h := newDebugHarness(t)
	loc := h.pauseBeforeLine(debugScript, "5")
	if loc.Source != "inline" || loc.Line != 5 || loc.Command != "screenshot" || loc.Depth != 0 {
		t.Fatalf("paused at %+v, want inline line 5 before the screenshot", loc)
	}
	if got := h.held().Info().State; got != session.StatePaused {
		t.Fatalf("state = %q, want paused", got)
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result after resume = %v, want ok", got)
	}
}

func TestScriptDebug_ABreakpointThatMatchesNoStepIsRefusedAndBetweenScriptsNothingIsRunning(t *testing.T) {
	h := newDebugHarness(t)
	if got := codeOfErr(h.held().SetBreakpoints([]string{"inline:5"})); got != diag.CodeHolderBusy {
		t.Fatalf("breakpoints between scripts: %q, want %s", got, diag.CodeHolderBusy)
	}
	h.startScript(debugScript)
	h.awaitEntered()
	if got := codeOfErr(h.held().SetBreakpoints([]string{"inline:9"})); got != diag.CodeInvalidArgument {
		t.Fatalf("breakpoint on a line with no step: %q, want %s", got, diag.CodeInvalidArgument)
	}
	close(h.drv.release)
	h.awaitResult()
}
