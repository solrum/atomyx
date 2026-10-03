package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

// newShortLeaseManager returns a lease manager whose runtime directory
// sits under a short path, so the holder socket path fits the unix limit.
func newShortLeaseManager(t *testing.T) *lease.Manager {
	t.Helper()
	return newShortLeaseManagerWithClock(t, clock.SystemClock{})
}

// newShortLeaseManagerWithClock is newShortLeaseManager over a clock the
// manager waits on while it retries a contended lease.
func newShortLeaseManagerWithClock(t *testing.T, clk clock.Clock) *lease.Manager {
	t.Helper()
	root, err := os.MkdirTemp("", "a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	manager, err := lease.NewManager(lease.Environment{
		GOOS:          "linux",
		Getenv:        func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, clk)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

// holderSocket returns the one socket a command's session bound under the
// manager's runtime directory.
func holderSocket(t *testing.T, manager *lease.Manager) string {
	t.Helper()
	sockets, err := filepath.Glob(filepath.Join(manager.Dir(), "*.sock"))
	if err != nil || len(sockets) != 1 {
		t.Fatalf("runtime directory holds %d sockets (%v), want exactly one", len(sockets), err)
	}
	return sockets[0]
}

func countFileLines(runDir string) int {
	body, err := os.ReadFile(filepath.Join(runDir, runfolder.StepsFileName))
	if err != nil {
		return 0
	}
	return strings.Count(string(body), "\n")
}

// stdoutHook is a command's stdout. A command prints its result only
// after every step ran, so the first write is the moment a test connects
// a viewer and waits for it to catch up with the steps file.
type stdoutHook struct {
	buf     bytes.Buffer
	onWrite func()
}

func (h *stdoutHook) Write(p []byte) (int, error) {
	n, err := h.buf.Write(p)
	h.onWrite()
	return n, err
}

// stepLinesOverTheSocket runs command with a viewer that connects once the
// command has printed its result, and returns the lines the viewer read.
func stepLinesOverTheSocket(t *testing.T, root string, command func(Deps) int) []sessiontest.StepLine {
	t.Helper()
	manager := newShortLeaseManager(t)
	var stderr bytes.Buffer
	deps := stepsDeps(&bytes.Buffer{}, &stderr)
	deps.Session = session.Deps{Leases: manager, Sockets: manager, Clock: clock.SystemClock{}}
	var viewer *sessiontest.Viewer
	deps.Stdout = &stdoutHook{onWrite: func() {
		if viewer == nil {
			viewer = sessiontest.DialViewer(t, holderSocket(t, manager), "atomyx inspect")
			viewer.FollowSteps(t)
		}
		if err := viewer.WaitForLines(countFileLines(singleRunDir(t, root))); err != nil {
			t.Errorf("waiting for step lines: %v", err)
		}
	}}

	if code := command(deps); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if viewer == nil {
		t.Fatal("the command never printed, so no viewer connected")
	}
	return viewer.Lines()
}

func requireWireMatchesFile(t *testing.T, received []sessiontest.StepLine, runDir string) {
	t.Helper()
	written, _ := readStepLines(t, runDir)
	if len(received) != len(written) || len(received) == 0 {
		t.Fatalf("viewer read %d lines, steps.jsonl has %d, want the same non-zero count", len(received), len(written))
	}
	for i, line := range received {
		if int(line.Seq) != i+1 || float64(line.Seq) != written[i]["seq"] || line.Command != written[i]["command"] || line.RunID != written[i]["runId"] {
			t.Errorf("wire line %d = seq %d command %q run %q, file has %v %v %v", i, line.Seq, line.Command, line.RunID, written[i]["seq"], written[i]["command"], written[i]["runId"])
		}
	}
	body, err := json.Marshal(received)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), typedSecret) {
		t.Errorf("the step lines on the socket carry the typed text:\n%s", body)
	}
}

func TestRunSendsEveryStepLineToAViewerOnTheHolderSocket(t *testing.T) {
	root := t.TempDir()
	script := writeScript(t, "flow.yml", secretFlow)

	received := stepLinesOverTheSocket(t, root, func(deps Deps) int {
		return RunScript(context.Background(), deps, map[string]FlagValue{"--file": script, "--report-dir": root})
	})

	requireWireMatchesFile(t, received, singleRunDir(t, root))
}

func TestSuiteSendsEveryStepLineToAViewerOnTheHolderSocket(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a.yml", secretFlow)
	root := filepath.Join(dir, "reports")

	received := stepLinesOverTheSocket(t, root, func(deps Deps) int {
		return RunSuite(context.Background(), deps, map[string]FlagValue{"--glob": []string{"*.yml"}, "--report-dir": root}, nil)
	})

	requireWireMatchesFile(t, received, singleRunDir(t, root))
}

func TestScenarioRunSendsItsChildrensStepLinesToAViewerOnTheHolderSocket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "child.yml", "- screenshot: one\n")
	writeFile(t, dir, "scenario.yml", "name: Nested\nscripts:\n  - child.yml\n")
	root := filepath.Join(dir, "reports")

	received := stepLinesOverTheSocket(t, root, func(deps Deps) int {
		return RunScript(context.Background(), deps, map[string]FlagValue{"--file": filepath.Join(dir, "scenario.yml"), "--report-dir": root})
	})

	requireWireMatchesFile(t, received, singleRunDir(t, root))
}

// hookedDriver runs hook before its first hierarchy read, which is inside
// the run, after the run folder exists.
type hookedDriver struct {
	*mock.Driver
	once sync.Once
	hook func()
}

func (d *hookedDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	d.once.Do(d.hook)
	return d.Driver.Hierarchy(ctx)
}

// hookedFactory builds a staged mock whose first hierarchy read calls
// hook, and keeps the mock so a test can count its calls.
type hookedFactory struct {
	hook  func()
	built *mock.Driver
}

func (f *hookedFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *hookedFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.StageHierarchy(focusedInputTree())
	f.built = drv
	hooked := &hookedDriver{Driver: drv, hook: f.hook}
	return device.Built{
		Driver:    hooked,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: hooked, Clock: orchestra.SystemClock{}}),
	}, nil
}

func runWithHook(t *testing.T, root string, hook func(manager *lease.Manager)) (calls int, warnings string) {
	t.Helper()
	manager := newShortLeaseManager(t)
	factory := &hookedFactory{hook: func() { hook(manager) }}
	var stdout, stderr, logs bytes.Buffer
	deps := mockDeps(&stdout, &stderr)
	deps.Factory = factory
	deps.Logger = logging.NewStructuredLogger(&logs, logging.LevelWarn)
	deps.Session = session.Deps{Leases: manager, Sockets: manager, Clock: clock.SystemClock{}}

	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": writeScript(t, "flow.yml", secretFlow), "--report-dir": root,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	return len(factory.built.Calls), logs.String()
}

func TestRunMakesTheSameDriverCallsWithAViewerAttachedAsWithout(t *testing.T) {
	baseline, _ := runWithHook(t, t.TempDir(), func(*lease.Manager) {})

	root := t.TempDir()
	attached := false
	withViewer, warnings := runWithHook(t, root, func(manager *lease.Manager) {
		viewer := sessiontest.DialViewer(t, holderSocket(t, manager), "atomyx inspect")
		for _, request := range []struct {
			name string
			call func() error
		}{{"status", viewer.Status}, {"tree.latest", viewer.TreeLatest}, {"screenshot.latest", viewer.ScreenshotLatest}} {
			if err := request.call(); err != nil {
				t.Errorf("%s: %v", request.name, err)
			}
		}
		if got := freshCode(viewer.TreeFresh()); got != diag.CodeHolderBusy {
			t.Errorf("fresh capture during a run = %q, want %s", got, diag.CodeHolderBusy)
		}
		attached = true
	})

	if !attached {
		t.Fatal("the viewer never attached")
	}
	if withViewer != baseline {
		t.Fatalf("driver calls with a viewer = %d, without = %d, want the same", withViewer, baseline)
	}
	if strings.Contains(warnings, "intervention") {
		t.Errorf("the run logged an intervention failure:\n%s", warnings)
	}
}

func TestRunRecordsAViewerThatAttachesDuringItInTheRunFolder(t *testing.T) {
	root := t.TempDir()
	runWithHook(t, root, func(manager *lease.Manager) {
		sessiontest.DialViewer(t, holderSocket(t, manager), "atomyx inspect")
	})

	body, err := os.ReadFile(filepath.Join(singleRunDir(t, root), runfolder.InterventionsFileName))
	if err != nil {
		t.Fatalf("reading the interventions file: %v", err)
	}
	var line runfolder.InterventionLine
	if err := json.Unmarshal(bytes.TrimSpace(body), &line); err != nil {
		t.Fatalf("decoding %q: %v", body, err)
	}
	if line.Kind != session.InterventionViewerAttached || line.Client != "atomyx inspect" || line.AtMs <= 0 {
		t.Fatalf("intervention line = %+v, want viewerAttached by atomyx inspect with a time", line)
	}
}

func freshCode(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}
