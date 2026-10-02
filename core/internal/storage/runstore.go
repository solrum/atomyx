package storage

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNoActiveRun is returned by RunStore.RecordBug when no run is
// active. A bug finding without a run to attach it to is a
// programming error at the call site: the caller must start a run
// before recording findings against it.
var ErrNoActiveRun = errors.New("storage: recordBug: no active run; call Start before RecordBug")

// RunStatus is the verdict of a test run.
type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusPassed  RunStatus = "passed"
	RunStatusFailed  RunStatus = "failed"
	RunStatusError   RunStatus = "error"
	// RunStatusCancelled marks a run the server closed because its
	// session sat idle past the open-run idle timeout, or lost its
	// device. A caller never submits it as a finish_run verdict.
	RunStatusCancelled RunStatus = "cancelled"
)

// RunBugReport is one bug finding recorded against a run.
type RunBugReport struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ScreenshotPath string `json:"screenshotPath,omitempty"`
	Timestamp      int64  `json:"timestamp"`
}

// RunRecord is a bounded test session: its identity, verdict, and the
// bug findings recorded against it while it was active.
type RunRecord struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	StartedAt   int64          `json:"startedAt"`
	FinishedAt  *int64         `json:"finishedAt,omitempty"`
	Status      RunStatus      `json:"status"`
	ActionCount int            `json:"actionCount"`
	Findings    []RunBugReport `json:"findings"`
}

// StartRunResult is returned by RunStore.Start. Run is the new active
// run. ErroredPredecessor is set when a previously active run was
// force-errored to make room for this one (the caller forgot to call
// Finish). A caller that ignores it silently drops the old run's
// history, so it should persist ErroredPredecessor before discarding
// it.
type StartRunResult struct {
	Run                *RunRecord
	ErroredPredecessor *RunRecord
}

// Clock is the time source RunStore uses for run ids and timestamps.
// Injecting it lets a test assert exact ids and timestamps without
// depending on wall-clock time.
type Clock interface {
	NowMs() int64
}

// SystemClock is the production Clock, backed by the host runtime.
type SystemClock struct{}

func (SystemClock) NowMs() int64 { return time.Now().UnixMilli() }

// StartOptions configures RunStore.Start. Source is a pointer so an
// explicitly empty string and an omitted value stay distinguishable:
// omitting Source defaults to "unknown", while an explicit empty
// string is stored as given. A non-empty ID becomes the run's id, so a
// caller that already owns the run's durable identity keeps one id for
// both. An empty ID takes the next generated one.
type StartOptions struct {
	ID     string
	Name   string
	Source *string
}

// RecordBugOptions configures RunStore.RecordBug.
type RecordBugOptions struct {
	Title          string
	Description    string
	ScreenshotPath *string
}

// RunStore tracks the single in-flight test run for a process. It
// holds no persistence of its own. A caller that wants a finished
// run to outlive the process saves the returned RunRecord through a
// Storage instance, keyed by RunKey. RunStore is safe for concurrent
// use.
type RunStore struct {
	mu     sync.Mutex
	clock  Clock
	active *RunRecord
	nextID int
}

// NewRunStore constructs a RunStore backed by the system clock.
func NewRunStore() *RunStore {
	return NewRunStoreWithClock(SystemClock{})
}

// NewRunStoreWithClock constructs a RunStore backed by clock, for
// deterministic tests.
func NewRunStoreWithClock(clock Clock) *RunStore {
	return &RunStore{clock: clock, nextID: 1}
}

// Start begins a new run. If a run is already active, it is marked
// RunStatusError (an implicit finish) and returned as
// ErroredPredecessor alongside the new run.
func (r *RunStore) Start(opts StartOptions) StartRunResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	var erroredPredecessor *RunRecord
	if r.active != nil && r.active.Status == RunStatusRunning {
		r.active.Status = RunStatusError
		finishedAt := r.clock.NowMs()
		r.active.FinishedAt = &finishedAt
		erroredPredecessor = r.active
	}

	source := "unknown"
	if opts.Source != nil {
		source = *opts.Source
	}

	startedAt := r.clock.NowMs()
	id := opts.ID
	if id == "" {
		id = fmt.Sprintf("run-%d-%d", startedAt, r.nextID)
	}
	run := &RunRecord{
		ID:          id,
		Name:        opts.Name,
		Source:      source,
		StartedAt:   startedAt,
		Status:      RunStatusRunning,
		ActionCount: 0,
		Findings:    []RunBugReport{},
	}
	r.nextID++
	r.active = run

	return StartRunResult{Run: run, ErroredPredecessor: erroredPredecessor}
}

// Finish finishes the currently active run with the given verdict and
// returns it, or returns nil when no run is active. Passing "" for
// status defaults to RunStatusPassed.
func (r *RunStore) Finish(status RunStatus) *RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil
	}
	if status == "" {
		status = RunStatusPassed
	}
	r.active.Status = status
	finishedAt := r.clock.NowMs()
	r.active.FinishedAt = &finishedAt
	finished := r.active
	r.active = nil
	return finished
}

// Current returns the currently active run, or nil when none is
// active.
func (r *RunStore) Current() *RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// IsLive reports whether runID is the active run, which is the run this
// process is writing. It satisfies runfolder.Liveness for a catalog
// that reads in the same process as the writer.
func (r *RunStore) IsLive(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active != nil && r.active.ID == runID
}

// IncrementActions increments the action counter on the active run.
// It is a no-op when no run is active.
func (r *RunStore) IncrementActions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active != nil {
		r.active.ActionCount++
	}
}

// RecordBug records a bug finding against the active run and returns
// it, or returns ErrNoActiveRun when no run is active. The bug id is
// derived from the run id and the run's current finding count, so it
// is deterministic within a run and does not depend on timestamp
// uniqueness.
func (r *RunStore) RecordBug(opts RecordBugOptions) (RunBugReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return RunBugReport{}, ErrNoActiveRun
	}
	bug := RunBugReport{
		ID:          fmt.Sprintf("%s-bug-%d", r.active.ID, len(r.active.Findings)+1),
		Title:       opts.Title,
		Description: opts.Description,
		Timestamp:   r.clock.NowMs(),
	}
	if opts.ScreenshotPath != nil {
		bug.ScreenshotPath = *opts.ScreenshotPath
	}
	r.active.Findings = append(r.active.Findings, bug)
	return bug, nil
}
