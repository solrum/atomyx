package scenario

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/wire"
)

var emptyHierarchy = wire.TreeNodeWire{Attributes: map[string]string{}, Children: []wire.TreeNodeWire{}}

// newEmptyScreenFixture builds an orchestra whose screen shows nothing,
// so passYaml fails until the test stages testHierarchy on the driver.
func newEmptyScreenFixture(t *testing.T) (*orchestra.Orchestra, *mock.Driver) {
	t.Helper()
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(emptyHierarchy)
	return orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)}), d
}

func launchCalls(d *mock.Driver) int {
	n := 0
	for _, c := range d.Calls {
		if c.Method == "LaunchApp" {
			n++
		}
	}
	return n
}

// attemptRecorder is an OnEntryStart hook that records every attempt it
// sees and runs an optional action when an attempt starts.
type attemptRecorder struct {
	targets []EntryTarget
	onStart func(EntryTarget)
	videos  map[int][]string
}

func (a *attemptRecorder) start(_ context.Context, target EntryTarget) EntryEnd {
	a.targets = append(a.targets, target)
	if a.onStart != nil {
		a.onStart(target)
	}
	return func(SuiteEntryResult) []string { return a.videos[target.Attempt] }
}

func (a *attemptRecorder) attempts() string {
	parts := make([]string, len(a.targets))
	for i, target := range a.targets {
		parts[i] = fmt.Sprintf("%d.%d", target.EntryIndex, target.Attempt)
	}
	return strings.Join(parts, ",")
}

func newRetrySuite(orch *orchestra.Orchestra, hook EntryStart, extra func(*SuiteRunnerDeps)) *SuiteRunner {
	deps := SuiteRunnerDeps{
		Orchestra:    orch,
		Clock:        orchestra.NewFakeClock(0),
		Logger:       noopLogger{},
		Commands:     runner.DefaultCommands(),
		OnEntryStart: hook,
	}
	if extra != nil {
		extra(&deps)
	}
	return NewSuiteRunner(deps)
}

func TestSuiteRunner_FailThenPassIsFlakyAndCountsAsPassed(t *testing.T) {
	orch, d := newEmptyScreenFixture(t)
	rec := &attemptRecorder{
		videos: map[int][]string{1: {"video-entry-1-1.mp4"}, 2: nil},
		onStart: func(target EntryTarget) {
			if target.Attempt == 2 {
				d.StageHierarchy(testHierarchy)
			}
		},
	}
	r := newRetrySuite(orch, rec.start, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}

	entry := result.Entries[0]
	if !result.Ok || entry.Status != EntryPassed || !entry.Flaky {
		t.Fatalf("ok=%v status=%s flaky=%v, want a flaky pass", result.Ok, entry.Status, entry.Flaky)
	}
	if result.Passed != 1 || result.Failed != 0 || result.Flaky != 1 {
		t.Errorf("passed=%d failed=%d flaky=%d, want 1, 0, 1", result.Passed, result.Failed, result.Flaky)
	}
	if len(entry.Attempts) != 2 || entry.Attempts[0].Status != EntryFailed || entry.Attempts[1].Status != EntryPassed {
		t.Fatalf("attempts = %+v, want failed then passed", entry.Attempts)
	}
	if got := entry.Attempts[0].Videos; len(got) != 1 || got[0] != "video-entry-1-1.mp4" {
		t.Errorf("attempt 1 videos = %v, want the kept video", got)
	}
	if rec.attempts() != "0.1,0.2" {
		t.Errorf("hook attempts = %s, want 0.1,0.2", rec.attempts())
	}
}

func TestSuiteRunner_RetryClearsTheLaunchDedupLikeANewEntry(t *testing.T) {
	orch, d := newEmptyScreenFixture(t)
	rec := &attemptRecorder{onStart: func(target EntryTarget) {
		if target.Attempt == 2 {
			d.StageHierarchy(testHierarchy)
		}
	}}
	r := newRetrySuite(orch, rec.start, nil)

	if _, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 1}); err != nil {
		t.Fatal(err)
	}

	if got := launchCalls(d); got != 2 {
		t.Errorf("launchApp reached the driver %d times, want once per attempt", got)
	}
}

func TestSuiteRunner_AlwaysFailingEntryExhaustsItsRetriesAndFails(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}

	entry := result.Entries[0]
	if result.Ok || entry.Status != EntryFailed || entry.Flaky {
		t.Errorf("ok=%v status=%s flaky=%v, want a plain failure", result.Ok, entry.Status, entry.Flaky)
	}
	if rec.attempts() != "0.1,0.2,0.3" || len(entry.Attempts) != 3 || result.Failed != 1 {
		t.Errorf("attempts = %s (%d recorded), failed=%d, want three attempts and one failure", rec.attempts(), len(entry.Attempts), result.Failed)
	}
}

func TestSuiteRunner_RetriesAreLimitedToTheMaximum(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)

	if _, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 50}); err != nil {
		t.Fatal(err)
	}

	if len(rec.targets) != 1+MaxRetries {
		t.Errorf("attempts = %d, want %d", len(rec.targets), 1+MaxRetries)
	}
}

func TestSuiteRunner_WithoutRetriesAnEntryRunsOnce(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if rec.attempts() != "0.1" || len(result.Entries[0].Attempts) != 1 {
		t.Errorf("attempts = %s, want a single attempt", rec.attempts())
	}
}

func TestSuiteRunner_CanceledContextIsNotRetried(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &attemptRecorder{onStart: func(EntryTarget) { cancel() }}
	r := newRetrySuite(orch, rec.start, nil)

	if _, err := r.Run(ctx, []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 2}); err != nil {
		t.Fatal(err)
	}

	if rec.attempts() != "0.1" {
		t.Errorf("attempts = %s, want the canceled attempt only", rec.attempts())
	}
}

func TestSuiteRunner_DeviceLossIsNotRetried(t *testing.T) {
	orch, d := newEmptyScreenFixture(t)
	d.FailNextPingForContract(fmt.Errorf("device unplugged"))
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}

	abort := result.Entries[0].Result.Script.Abort
	if abort == nil || abort.Reason != runner.RunAbortDeviceLost {
		t.Fatalf("abort = %+v, want a device loss", abort)
	}
	if rec.attempts() != "0.1" {
		t.Errorf("attempts = %s, want one", rec.attempts())
	}
}

func TestSuiteRunner_LoadErrorIsNotRetried(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: "not: [valid"}}, &SuiteRunOptions{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}

	if rec.attempts() != "0.1" || result.Entries[0].Error == nil {
		t.Errorf("attempts = %s, error = %v, want one failed load", rec.attempts(), result.Entries[0].Error)
	}
}

func TestSuiteRunner_QuarantinedFailureDoesNotFailTheSuite(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	r := newRetrySuite(orch, nil, nil)
	entries := []SuiteEntry{
		{Path: "flaky/a.yml", Content: passYaml},
		{Path: "stable/b.yml", Content: "appId: com.test\nname: ok\n---\n- launchApp\n"},
	}

	result, err := r.Run(context.Background(), entries, &SuiteRunOptions{
		Quarantined: func(path string) bool { return strings.HasPrefix(path, "flaky/") },
	})
	if err != nil {
		t.Fatal(err)
	}

	quarantined := result.Entries[0]
	if !result.Ok || quarantined.Status != EntryFailed || !quarantined.Quarantined || !quarantined.QuarantinedFailure() {
		t.Fatalf("ok=%v status=%s quarantined=%v, want a quarantined failure that keeps the suite green", result.Ok, quarantined.Status, quarantined.Quarantined)
	}
	if result.Passed != 1 || result.Failed != 0 || result.QuarantinedFailures != 1 || result.Total != 2 {
		t.Errorf("passed=%d failed=%d quarantined=%d total=%d", result.Passed, result.Failed, result.QuarantinedFailures, result.Total)
	}
	if result.Entries[1].Quarantined {
		t.Error("an entry outside the quarantine list is marked quarantined")
	}
}

func TestSuiteRunner_FailureOutsideQuarantineStillFailsTheSuite(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	r := newRetrySuite(orch, nil, nil)

	result, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{
		Quarantined: func(string) bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Ok || result.Failed != 1 {
		t.Errorf("ok=%v failed=%d, want a failed suite", result.Ok, result.Failed)
	}
}

func TestSuiteRunner_RetriedAttemptsGetTheirOwnArtifactTargetsAndStepEvents(t *testing.T) {
	orch, d := newEmptyScreenFixture(t)
	recorder := &childFactoryRecorder{}
	var events []runner.StepEvent
	rec := &attemptRecorder{onStart: func(target EntryTarget) {
		if target.Attempt == 2 {
			d.StageHierarchy(testHierarchy)
		}
	}}
	r := newRetrySuite(orch, rec.start, func(deps *SuiteRunnerDeps) {
		deps.ChildArtifacts = recorder.factory
		deps.OnStep = func(e runner.StepEvent) { events = append(events, e) }
	})

	if _, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, &SuiteRunOptions{Retries: 1}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.targets) != 2 || recorder.targets[0].Attempt != 1 || recorder.targets[1].Attempt != 2 {
		t.Errorf("child targets = %+v, want attempts 1 and 2", recorder.targets)
	}
	var first, second int
	for _, e := range events {
		switch e.Attempt {
		case 0:
			first++
		case 2:
			second++
		default:
			t.Errorf("unexpected step attempt %d", e.Attempt)
		}
	}
	if first == 0 || second == 0 {
		t.Errorf("step events: %d for the first attempt, %d for the second, want both", first, second)
	}
}

func TestSuiteRunner_CanceledSuiteReturnsTheEntriesThatRan(t *testing.T) {
	orch, _ := newEmptyScreenFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	rec := &attemptRecorder{onStart: func(target EntryTarget) {
		if target.EntryIndex == 1 {
			cancel()
		}
	}}
	r := newRetrySuite(orch, rec.start, nil)
	entries := []SuiteEntry{
		{Path: "a.yml", Content: passYaml},
		{Path: "b.yml", Content: passYaml},
		{Path: "c.yml", Content: passYaml},
	}

	result, err := r.Run(ctx, entries, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if result.Ok || result.Total != 2 || len(result.Entries) != 2 || result.Entries[1].Path != "b.yml" {
		t.Errorf("result ok=%v total=%d entries=%d, want the two entries that started and Ok false", result.Ok, result.Total, len(result.Entries))
	}
}

func TestSuiteRunner_CanceledSuiteIsNotOkEvenWhenEveryEntryThatRanPassed(t *testing.T) {
	orch := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	hook := func(_ context.Context, target EntryTarget) EntryEnd {
		return func(SuiteEntryResult) []string {
			if target.EntryIndex == 0 {
				cancel()
			}
			return nil
		}
	}
	r := newRetrySuite(orch, hook, nil)
	entries := []SuiteEntry{{Path: "a.yml", Content: passYaml}, {Path: "b.yml", Content: passYaml}}

	result, err := r.Run(ctx, entries, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if result.Ok || result.Passed != 1 || result.Total != 1 {
		t.Errorf("ok=%v passed=%d total=%d, want one passed entry and Ok false", result.Ok, result.Passed, result.Total)
	}
}
