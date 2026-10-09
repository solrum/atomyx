package shard

import (
	"context"
	"slices"
	"sync"
	"time"
)

// FakeScript describes how the fake worker of one device behaves.
type FakeScript struct {
	// StartErr makes Launch fail for the device.
	StartErr error
	// ReadyLine, when set, is the only line the worker sends. It stands in
	// for the ready line, for example to report an error.
	ReadyLine *Line
	// ReadyAfter, when set, holds the ready line back until it is closed.
	ReadyAfter <-chan struct{}
	// Handle answers one path. It may block to model a slow device.
	Handle func(path string) Line
	// Vanish, when set, makes the worker call it and exit on its first path
	// without answering, like a crashed process.
	Vanish func()
}

// FakeLauncher is a Launcher whose workers follow FakeScripts, so a test
// can drive a coordinator without starting processes. Its workers stamp
// every line with the run id "run-" followed by the device id. It is safe
// for concurrent use.
type FakeLauncher struct {
	mu      sync.Mutex
	scripts map[string]*FakeScript
	seen    map[string][]string
	stopped map[string]bool
	specs   []WorkerSpec
}

// NewFakeLauncher returns a FakeLauncher that scripts each device by id.
func NewFakeLauncher(scripts map[string]*FakeScript) *FakeLauncher {
	return &FakeLauncher{scripts: scripts, seen: map[string][]string{}, stopped: map[string]bool{}}
}

var _ Launcher = (*FakeLauncher)(nil)

// Launch implements Launcher.
func (l *FakeLauncher) Launch(_ context.Context, spec WorkerSpec) (Worker, error) {
	l.mu.Lock()
	l.specs = append(l.specs, spec)
	l.mu.Unlock()
	script := l.scripts[spec.Device.ID]
	if script.StartErr != nil {
		return nil, script.StartErr
	}
	w := &fakeWorker{id: spec.Device.ID, launcher: l, script: script, lines: make(chan Line), in: make(chan string), quit: make(chan struct{})}
	go w.run()
	return w, nil
}

// Specs returns the worker specs Launch received, in order.
func (l *FakeLauncher) Specs() []WorkerSpec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.specs)
}

// Handled returns the paths the worker of device id received, in order.
func (l *FakeLauncher) Handled(id string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen[id])
}

// Stopped reports whether the worker of device id was stopped.
func (l *FakeLauncher) Stopped(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stopped[id]
}

type fakeWorker struct {
	id       string
	launcher *FakeLauncher
	script   *FakeScript
	lines    chan Line
	in       chan string
	quit     chan struct{}
	once     sync.Once
}

func (w *fakeWorker) run() {
	defer close(w.lines)
	if w.script.ReadyAfter != nil {
		select {
		case <-w.script.ReadyAfter:
		case <-w.quit:
			return
		}
	}
	if w.script.ReadyLine != nil {
		w.lines <- *w.script.ReadyLine
		return
	}
	w.lines <- Line{Kind: LineReady, RunID: "run-" + w.id}
	for {
		select {
		case path, ok := <-w.in:
			if !ok {
				return
			}
			w.launcher.mu.Lock()
			w.launcher.seen[w.id] = append(w.launcher.seen[w.id], path)
			w.launcher.mu.Unlock()
			if w.script.Vanish != nil {
				w.script.Vanish()
				return
			}
			answer := make(chan Line, 1)
			go func() { answer <- w.script.Handle(path) }()
			var line Line
			select {
			case line = <-answer:
			case <-w.quit:
				return
			}
			line.RunID = "run-" + w.id
			select {
			case w.lines <- line:
			case <-w.quit:
				return
			}
			if line.DeviceLost {
				return
			}
		case <-w.quit:
			return
		}
	}
}

func (w *fakeWorker) Lines() <-chan Line { return w.lines }

func (w *fakeWorker) Send(path string) error {
	select {
	case w.in <- path:
		return nil
	case <-w.quit:
		return context.Canceled
	}
}

func (w *fakeWorker) CloseInput() error {
	w.once.Do(func() { close(w.in) })
	return nil
}

func (w *fakeWorker) Stop(time.Duration) {
	w.launcher.mu.Lock()
	w.launcher.stopped[w.id] = true
	w.launcher.mu.Unlock()
	close(w.quit)
}

func (w *fakeWorker) Wait() Exit { return Exit{Stderr: "worker stderr"} }
