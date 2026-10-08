package debugrun

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

const waitTimeout = 5 * time.Second

type fakePauser struct {
	entered chan session.PauseLocation
	closed  chan struct{}
	lost    chan struct{}
	left    atomic.Int32
}

func newFakePauser() *fakePauser {
	return &fakePauser{entered: make(chan session.PauseLocation, 16), closed: make(chan struct{}), lost: make(chan struct{})}
}

func (p *fakePauser) EnterPause(loc session.PauseLocation) { p.entered <- loc }
func (p *fakePauser) LeavePause()                          { p.left.Add(1) }
func (p *fakePauser) Closed() <-chan struct{}              { return p.closed }
func (p *fakePauser) Lost() <-chan struct{}                { return p.lost }

type outcome struct {
	result runner.ScriptResult
	err    error
}

type harness struct {
	t       *testing.T
	adapter *Adapter
	pauser  *fakePauser
	driver  *mock.Driver
	orch    *orchestra.Orchestra
	runner  *runner.ScriptRunner
	started atomic.Int32
	done    chan outcome
	cancel  context.CancelFunc
}

const sourcePath = "/work/flow.yaml"

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	return newHarnessWith(t, opts, runner.DefaultCommands())
}

func newHarnessWith(t *testing.T, opts Options, commands []runner.CommandDefinition) *harness {
	t.Helper()
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	clock := orchestra.NewFakeClock(0)
	h := &harness{t: t, driver: d, pauser: newFakePauser(), done: make(chan outcome, 1)}
	h.orch = orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	h.adapter = New(opts)
	h.adapter.Bind(h.pauser)
	h.runner = runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:  h.orch,
		Clock:      clock,
		Logger:     logging.NoopLogger{},
		Commands:   commands,
		StepGate:   h.adapter,
		SourcePath: sourcePath,
		OnStep: func(e runner.StepEvent) {
			if e.Type == runner.StepStarted {
				h.started.Add(1)
			}
		},
	})
	return h
}

// start runs yaml on a goroutine and returns at once.
func (h *harness) start(yaml string) {
	h.t.Helper()
	def, err := script.ParseScript(yaml, nil)
	if err != nil {
		h.t.Fatalf("ParseScript: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.t.Cleanup(cancel)
	go func() {
		result, err := h.runner.Run(ctx, def)
		h.done <- outcome{result, err}
	}()
}

func (h *harness) awaitPause() session.PauseLocation {
	h.t.Helper()
	select {
	case loc := <-h.pauser.entered:
		return loc
	case <-time.After(waitTimeout):
		h.t.Fatal("the run never paused")
		return session.PauseLocation{}
	}
}

func (h *harness) awaitDone() outcome {
	h.t.Helper()
	select {
	case o := <-h.done:
		return o
	case <-time.After(waitTimeout):
		h.t.Fatal("the run never finished")
		return outcome{}
	}
}

func (h *harness) resume(opts session.ResumeOptions) session.ResumeResult {
	h.t.Helper()
	res, err := h.adapter.Resume(context.Background(), opts)
	if err != nil {
		h.t.Fatalf("Resume: %v", err)
	}
	return res
}

func loc(line int) string { return sourcePath + ":" + strconv.Itoa(line) }

func goButtonTree() wire.TreeNodeWire {
	button := wire.TreeNodeWire{Attributes: map[string]string{"id": "go", "text": "Go", "label": "Go", "bounds": "0,0,10,10"}}
	return wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}, Children: []wire.TreeNodeWire{button}}
}

func threeShotScripts(t *testing.T) []runner.BreakScript {
	t.Helper()
	def, err := script.ParseScript(threeShots, nil)
	if err != nil {
		t.Fatal(err)
	}
	return []runner.BreakScript{{Path: sourcePath, Definition: def}}
}
