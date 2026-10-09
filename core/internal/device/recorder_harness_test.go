package device

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const harnessTimeout = 5 * time.Second

// recorderHarness stands in for the host tools a recorder drives. It
// wraps the fake runners, keeps one ordered log of every command so a test
// can assert what happened before what, and plays the device side: an
// interrupt aimed at a device process id ends the matching fake process,
// a pull writes the local file, and starting a simulator recording creates
// its output file.
type recorderHarness struct {
	t      *testing.T
	runner *process.Fake
	bg     *process.FakeBackground

	mu         sync.Mutex
	events     []string
	devicePids map[int]*process.FakeBackgroundProcess
	procs      []*process.FakeBackgroundProcess
	pullErr    error

	simRecorders []*process.FakeBackgroundProcess

	tick chan struct{}
	idle chan struct{}
}

func newRecorderHarness(t *testing.T) *recorderHarness {
	return &recorderHarness{
		t:          t,
		runner:     process.NewFake(),
		bg:         process.NewFakeBackground(),
		devicePids: map[int]*process.FakeBackgroundProcess{},
		tick:       make(chan struct{}),
		idle:       make(chan struct{}, 8),
	}
}

func (h *recorderHarness) deps() RecorderDeps {
	return RecorderDeps{Runner: h, Background: harnessBackground{h}}
}

func (h *recorderHarness) log(entry string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, entry)
}

// stageAndroid queues one segment process per device process id. Each
// prints its id the way the real shell does.
func (h *recorderHarness) stageAndroid(pids ...int) {
	for _, pid := range pids {
		proc := process.NewFakeBackgroundProcess()
		_, _ = proc.Write([]byte(strconv.Itoa(pid) + "\n"))
		h.mu.Lock()
		h.devicePids[pid] = proc
		h.procs = append(h.procs, proc)
		h.mu.Unlock()
		h.bg.Stub(proc)
	}
}

// stageSimulator queues n recorder processes.
func (h *recorderHarness) stageSimulator(n int) {
	for i := 0; i < n; i++ {
		proc := process.NewFakeBackgroundProcess()
		h.mu.Lock()
		h.procs = append(h.procs, proc)
		h.mu.Unlock()
		h.bg.Stub(proc)
	}
}

func (h *recorderHarness) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	res, err := h.runner.Run(ctx, name, args...)
	h.log(strings.Join(append([]string{name}, args...), " "))
	if err != nil || len(args) == 0 {
		return res, err
	}
	last := args[len(args)-1]
	switch {
	case containsArg(args, "pull"):
		if h.pullErr != nil {
			return process.Result{Stderr: "pull failed"}, h.pullErr
		}
		if werr := os.WriteFile(last, []byte("video"), 0o600); werr != nil {
			h.t.Fatalf("write pulled file: %v", werr)
		}
	case strings.HasPrefix(last, "kill -INT "):
		pid, _ := strconv.Atoi(strings.TrimPrefix(last, "kill -INT "))
		h.mu.Lock()
		proc := h.devicePids[pid]
		h.mu.Unlock()
		if proc != nil {
			proc.Exit(process.FakeInterruptExitCode)
		}
	}
	return res, nil
}

type harnessBackground struct{ h *recorderHarness }

// Start refuses a second simulator recording while one is still running,
// the way simctl does.
func (b harnessBackground) Start(ctx context.Context, name string, args ...string) (process.BackgroundProcess, error) {
	if containsArg(args, "recordVideo") {
		for _, running := range b.h.simRecorders {
			if !running.Interrupted() && !running.Killed() {
				return nil, errors.New("Host recording is already in progress")
			}
		}
	}
	proc, err := b.h.bg.Start(ctx, name, args...)
	if fake, ok := proc.(*process.FakeBackgroundProcess); ok && containsArg(args, "recordVideo") {
		b.h.simRecorders = append(b.h.simRecorders, fake)
	}
	b.h.log("start " + strings.Join(append([]string{name}, args...), " "))
	if err == nil && containsArg(args, "recordVideo") {
		if werr := os.WriteFile(args[len(args)-1], []byte("video"), 0o600); werr != nil {
			b.h.t.Fatalf("write recording file: %v", werr)
		}
	}
	return proc, err
}

// wait is the rotationWaiter a test injects. It reports idle each time
// the loop is ready for the next interval, then blocks until the test
// ticks.
func (h *recorderHarness) wait(ctx context.Context) bool {
	select {
	case h.idle <- struct{}{}:
	default:
	}
	select {
	case <-h.tick:
		return true
	case <-ctx.Done():
		return false
	}
}

func (h *recorderHarness) awaitIdle() {
	h.t.Helper()
	select {
	case <-h.idle:
	case <-time.After(harnessTimeout):
		h.t.Fatal("the rotation loop never became idle")
	}
}

// rotateOnce triggers one rotation and returns after it finished.
func (h *recorderHarness) rotateOnce() {
	h.t.Helper()
	select {
	case h.tick <- struct{}{}:
	case <-time.After(harnessTimeout):
		h.t.Fatal("the rotation loop did not accept a tick")
	}
	h.awaitIdle()
}

func (h *recorderHarness) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

// indexOf returns the position of the first logged command containing
// every part, or fails the test.
func (h *recorderHarness) indexOf(parts ...string) int {
	h.t.Helper()
	if i := h.find(0, parts...); i >= 0 {
		return i
	}
	h.t.Fatalf("no command containing %q in %q", parts, h.snapshot())
	return -1
}

func (h *recorderHarness) find(from int, parts ...string) int {
	events := h.snapshot()
	for i := from; i < len(events); i++ {
		if containsAll(events[i], parts) {
			return i
		}
	}
	return -1
}

func (h *recorderHarness) count(parts ...string) int {
	n := 0
	for _, e := range h.snapshot() {
		if containsAll(e, parts) {
			n++
		}
	}
	return n
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
