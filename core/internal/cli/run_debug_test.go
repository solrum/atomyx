package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/debugrun"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/shutdown"
)

const debugWait = 5 * time.Second

// lockedBuffer is a writer the test can read while a command still
// writes to it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) waitFor(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(debugWait)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), text) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never saw %q in %q", text, b.String())
}

// countingFactory counts how many devices a command builds.
type countingFactory struct {
	device.WrappingFactory
	builds atomic.Int32
}

func (f *countingFactory) Build(ctx context.Context, d device.Descriptor) (device.Built, error) {
	f.builds.Add(1)
	return f.WrappingFactory.Build(ctx, d)
}

type debugRun struct {
	deps   Deps
	stdout *lockedBuffer
	stderr *lockedBuffer
	stdin  *io.PipeWriter
	count  *countingFactory
}

func newDebugRun(t *testing.T, terminal bool) *debugRun {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	clock := orchestra.NewFakeClock(0)
	count := &countingFactory{WrappingFactory: device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock})}
	r := &debugRun{stdout: &lockedBuffer{}, stderr: &lockedBuffer{}, stdin: pw, count: count}
	r.deps = Deps{
		Probe:         device.NewStaticProbe(nil),
		Factory:       count,
		Clock:         clock,
		Stdout:        r.stdout,
		Stderr:        r.stderr,
		Stdin:         pr,
		StdinTerminal: terminal,
	}
	return r
}

// start runs the command on a goroutine and returns its exit code.
func (r *debugRun) start(flags map[string]FlagValue) <-chan int {
	done := make(chan int, 1)
	go func() { done <- RunScript(context.Background(), r.deps, flags) }()
	return done
}

func (r *debugRun) pressEnter(t *testing.T) {
	t.Helper()
	if _, err := r.stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
}

func awaitExit(t *testing.T, done <-chan int) int {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(debugWait):
		t.Fatal("the command never finished")
		return -1
	}
}

const sleepFlow = "- sleep: 1\n- sleep: 2\n- sleep: 3\n"

func breakFlags(path string, extra ...string) map[string]FlagValue {
	return map[string]FlagValue{"--file": path, "--break": append([]string{path + ":2"}, extra...)}
}

func TestRunBreakPausesAtTheLocationAndEnterResumes(t *testing.T) {
	r := newDebugRun(t, true)
	path := writeScript(t, "flow.yml", sleepFlow)
	flags := breakFlags(path)
	flags["--report-dir"] = t.TempDir()

	done := r.start(flags)
	r.stdout.waitFor(t, "Press Enter to resume")
	if out := r.stdout.String(); !strings.Contains(out, path+":2") || strings.Contains(out, "PASS") {
		t.Fatalf("stdout while paused = %q, want the location and no result yet", out)
	}
	r.pressEnter(t)
	if code := awaitExit(t, done); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, r.stderr.String())
	}
	if !strings.Contains(r.stdout.String(), "PASS") {
		t.Fatalf("stdout = %q, want the finished run", r.stdout.String())
	}
}

func TestRunBreakWithJSONPutsThePromptOnStderr(t *testing.T) {
	r := newDebugRun(t, true)
	path := writeScript(t, "flow.yml", sleepFlow)
	flags := breakFlags(path)
	flags["--json"] = true
	flags["--report-dir"] = t.TempDir()

	done := r.start(flags)
	r.stderr.waitFor(t, "Press Enter to resume")
	r.pressEnter(t)
	if code := awaitExit(t, done); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(r.stdout.String()), &payload); err != nil {
		t.Fatalf("stdout is not one JSON document (%v): %q", err, r.stdout.String())
	}
}

func TestRunBreakRefusesWithoutATerminalBeforeTakingTheDevice(t *testing.T) {
	r := newDebugRun(t, false)
	path := writeScript(t, "flow.yml", sleepFlow)

	if code := awaitExit(t, r.start(breakFlags(path))); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(r.stderr.String(), "--break needs a terminal") || r.count.builds.Load() != 0 {
		t.Fatalf("stderr = %q builds = %d", r.stderr.String(), r.count.builds.Load())
	}
}

func TestRunBreakRejectsALocationThatMatchesNoStepBeforeTakingTheDevice(t *testing.T) {
	r := newDebugRun(t, true)
	path := writeScript(t, "flow.yml", sleepFlow)
	flags := map[string]FlagValue{"--file": path, "--break": []string{path + ":99"}}

	if code := awaitExit(t, r.start(flags)); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(r.stderr.String(), "no top-level step starts at line 99") || r.count.builds.Load() != 0 {
		t.Fatalf("stderr = %q builds = %d", r.stderr.String(), r.count.builds.Load())
	}
}

func TestSuiteDoesNotAcceptBreak(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"suite", "--break", "a.yml:1"}, testDeps(&stdout, &stderr)); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr: %s", code, stderr.String())
	}
}

func TestRunBreakReportsABreakpointTheRunNeverReached(t *testing.T) {
	r := newDebugRun(t, true)
	path := writeScript(t, "flow.yml", "- assertVisible: nothing\n- sleep: 2\n")
	flags := breakFlags(path)
	flags["--report-dir"] = t.TempDir()

	if code := awaitExit(t, r.start(flags)); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "breakpoint at "+path+":2 was never reached") {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
}

func TestRunBreakInAScenarioChildPausesAtTheChildFile(t *testing.T) {
	r := newDebugRun(t, true)
	dir := t.TempDir()
	writeFile(t, dir, "first.yml", "- sleep: 1\n")
	writeFile(t, dir, "second.yml", "- sleep: 1\n- sleep: 2\n")
	writeFile(t, dir, "all.yml", "name: all\nscripts:\n  - first.yml\n  - second.yml\n")
	scenarioPath := filepath.Join(dir, "all.yml")
	child := filepath.Join(dir, "second.yml")
	flags := map[string]FlagValue{"--file": scenarioPath, "--break": []string{child + ":2"}, "--report-dir": t.TempDir()}

	done := r.start(flags)
	r.stdout.waitFor(t, "Press Enter to resume")
	if out := r.stdout.String(); !strings.Contains(out, child+":2") {
		t.Fatalf("stdout = %q, want the pause at the second child", out)
	}
	r.pressEnter(t)
	if code := awaitExit(t, done); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, r.stderr.String())
	}
}

func TestRunBreakResumeAnywayNeedsASecondEnterWhenTheTargetIsMissing(t *testing.T) {
	r := newDebugRun(t, true)
	r.deps.Factory = stagedMockFactory{tree: focusedInputTree()}
	path := writeScript(t, "flow.yml", "- sleep: 1\n- tap: Nowhere\n")
	flags := breakFlags(path)
	flags["--report-dir"] = t.TempDir()

	done := r.start(flags)
	r.stdout.waitFor(t, "Press Enter to resume")
	r.pressEnter(t)
	r.stdout.waitFor(t, "Press Enter again to resume anyway")
	select {
	case code := <-done:
		t.Fatalf("the run finished with %d after the first Enter", code)
	default:
	}
	r.pressEnter(t)
	if code := awaitExit(t, done); code != 1 {
		t.Fatalf("exit code = %d, want 1 (the tap fails once forced)", code)
	}
}

func TestRunBreakInASkippedScenarioChildIsReportedUnhit(t *testing.T) {
	r := newDebugRun(t, true)
	dir := t.TempDir()
	writeFile(t, dir, "bad.yml", "- assertVisible: nothing\n")
	writeFile(t, dir, "later.yml", "- sleep: 1\n")
	writeFile(t, dir, "all.yml", "name: all\nscripts:\n  - bad.yml\n  - later.yml\n")
	later := filepath.Join(dir, "later.yml")
	flags := map[string]FlagValue{"--file": filepath.Join(dir, "all.yml"), "--break": []string{later + ":1"}, "--report-dir": t.TempDir()}

	if code := awaitExit(t, r.start(flags)); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "breakpoint at "+later+":1 was never reached") {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
}

func TestReadEnterKeysStampsEachLineWhenItIsRead(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	var current atomic.Uint64
	current.Store(5)
	stamped := make(chan struct{}, 4)
	keys, stop := readEnterKeys(pr, func() uint64 {
		at := current.Load()
		stamped <- struct{}{}
		return at
	})
	t.Cleanup(stop)
	go func() { _, _ = pw.Write([]byte("\n\n")) }()

	if first := <-keys; first != 5 {
		t.Fatalf("first key stamped %d, want 5", first)
	}
	<-stamped
	<-stamped
	current.Store(6)
	if second := <-keys; second != 5 {
		t.Fatalf("second key stamped %d, want 5, the pause number when it was read", second)
	}
}

// fakePausedSession reports a fixed pause state and counts resumes.
type fakePausedSession struct {
	resumes int
}

func (*fakePausedSession) Paused() (session.PauseLocation, bool) {
	return session.PauseLocation{}, true
}

func (f *fakePausedSession) Resume(context.Context, session.ResumeOptions) (session.ResumeResult, error) {
	f.resumes++
	return session.ResumeResult{Resumed: true}, nil
}

// quietPauser is a debugrun.Pauser that ignores every call.
type quietPauser struct{ debugrun.Pauser }

func (quietPauser) EnterPause(session.PauseLocation) {}

func TestResumeOnEnterDropsAKeyReadBeforeTheLatestPause(t *testing.T) {
	sess := &fakePausedSession{}
	pauser := &promptPauser{Pauser: quietPauser{}, out: io.Discard}
	d := &runDebug{sess: sess, pauser: pauser, prompt: io.Discard}
	pauser.EnterPause(session.PauseLocation{})
	typedAhead := pauser.currentPause()
	pauser.EnterPause(session.PauseLocation{})

	d.resumeOnEnter(context.Background(), typedAhead)
	if sess.resumes != 0 {
		t.Fatalf("a key read before the latest pause resumed the run %d times", sess.resumes)
	}
	d.resumeOnEnter(context.Background(), pauser.currentPause())
	if sess.resumes != 1 {
		t.Fatalf("a key read during the pause resumed %d times, want 1", sess.resumes)
	}
}

func TestRunBreakStopsOnCtrlCWhilePausedAndLeavesNoKeyReaderBehind(t *testing.T) {
	r := newDebugRun(t, true)
	path := writeScript(t, "flow.yml", sleepFlow)
	flags := breakFlags(path)
	flags["--report-dir"] = t.TempDir()
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	done := make(chan int, 1)
	go func() { done <- RunScript(ctx, r.deps, flags) }()

	r.stdout.waitFor(t, "Press Enter to resume")
	cancel(shutdown.InterruptCause(syscall.SIGINT))
	if code := awaitExit(t, done); code != shutdown.ExitCodeSigint {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, shutdown.ExitCodeSigint, r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "interrupted") {
		t.Fatalf("stderr = %q, want the interrupted line", r.stderr.String())
	}

	// A reader blocked on stdin returns only when a line arrives. Once it
	// has consumed one, it must see that the command stopped and exit.
	go func() { _, _ = r.stdin.Write([]byte("\n")) }()
	deadline := time.Now().Add(debugWait)
	for keyReaderRunning() {
		if time.Now().After(deadline) {
			t.Fatal("a goroutine is still reading Enter keys after the command ended")
		}
		runtime.Gosched()
	}
}

func keyReaderRunning() bool {
	buf := make([]byte, 1<<20)
	return strings.Contains(string(buf[:runtime.Stack(buf, true)]), "cli.readEnterKeys")
}
