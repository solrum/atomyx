package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/orchestra"
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

type debugRun struct {
	deps   Deps
	stdout *lockedBuffer
	stderr *lockedBuffer
	stdin  *io.PipeWriter
}

func newDebugRun(t *testing.T, terminal bool) *debugRun {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	clock := orchestra.NewFakeClock(0)
	factory := device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock})
	r := &debugRun{stdout: &lockedBuffer{}, stderr: &lockedBuffer{}, stdin: pw}
	r.deps = Deps{
		Probe:         device.NewStaticProbe(nil),
		Factory:       factory,
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
