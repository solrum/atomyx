package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// SuiteEntryKind identifies whether a suite entry is a plain script
// or a scenario.
type SuiteEntryKind string

const (
	KindScript   SuiteEntryKind = "script"
	KindScenario SuiteEntryKind = "scenario"
)

// SuiteEntryStatus is one suite entry's outcome. Unlike
// ScriptRunStatus, a suite entry never reports "skipped": SuiteRunner
// runs every entry regardless of earlier failures.
type SuiteEntryStatus string

const (
	EntryPassed SuiteEntryStatus = "passed"
	EntryFailed SuiteEntryStatus = "failed"
)

// SuiteEntry is one resolved test file. SuiteRunner decides script
// versus scenario from Content via script.IsScenarioYaml. Callers
// never tag an entry's kind themselves.
type SuiteEntry struct {
	Path    string
	Content string
}

// CreateLoadScript builds a LoadScript for one scenario entry, scoped
// to that entry's own directory (scenarioBaseDir). SuiteRunner calls
// it once per scenario entry with filepath.Dir(entry.Path), so a
// scenario's `scripts:` list resolves relative to the scenario file
// itself, the same rule a standalone scenario run uses. Only
// required when at least one entry in a given Run call is a scenario;
// a suite made up of script entries alone never calls it.
type CreateLoadScript func(scenarioBaseDir string) LoadScript

// ResolvePreconditions resolves the precondition override for one
// entry, given that entry's own scenario-level preconditions field
// (nil for a plain script entry, since only a scenario carries that
// field). Returning runner.PreconditionsNoOverride leaves each
// precondition's own policy in effect.
type ResolvePreconditions func(flowLevel *string) runner.PreconditionsOverride

// SuiteRunnerDeps constructs a SuiteRunner. Orchestra, Clock, and
// Logger are required; every other field is optional.
type SuiteRunnerDeps struct {
	Orchestra      *orchestra.Orchestra
	Clock          orchestra.Clock
	Logger         logging.Logger
	NetworkCapture runner.NetworkCapture
	Commands       []runner.CommandDefinition
	// RunID identifies this suite run for log correlation. It is
	// forwarded unchanged to every child runner.ScriptRunner or
	// nested ScenarioRunner this SuiteRunner constructs, so every
	// entry's own "script.step" (and, for a scenario entry,
	// "scenario.run") log entries carry it under the "runId" field,
	// and it also tags the suite-level outcome entry this SuiteRunner
	// logs on Run's completion. Empty omits the field from all of
	// them.
	RunID string
	// CreateLoadScript is required only when a given Run call
	// includes a scenario entry; see CreateLoadScript's own doc.
	CreateLoadScript CreateLoadScript
	// ExternalEnv is forwarded to every entry the same way
	// ScenarioRunnerDeps.ExternalEnv forwards it to every child
	// script.
	ExternalEnv      map[string]string
	LaunchNoReset    *bool
	ClearDataOnFresh bool
	AppArtifact      *string
	// OnStep, when non-nil, is forwarded to every script the suite runs,
	// directly or through a scenario entry.
	OnStep func(runner.StepEvent)
	// ChildArtifacts, when non-nil, supplies the artifacts of every
	// script the suite runs, directly or through a scenario entry, and
	// enables failure evidence capture for them.
	ChildArtifacts ArtifactsFactory
	// OnEntryStart, when non-nil, is called before every entry. A host
	// uses it to scope work such as a screen recording to one entry.
	OnEntryStart EntryStart
}

// SuiteRunOptions configures one SuiteRunner.Run call.
type SuiteRunOptions struct {
	// SuiteName is carried through unchanged onto SuiteResult.SuiteName.
	SuiteName *string
	// ResolvePreconditions, when non-nil, is called once per entry to
	// resolve that entry's precondition override. Nil leaves every
	// precondition's own policy in effect for every entry.
	ResolvePreconditions ResolvePreconditions
	// Retries is how many more times a failed entry runs after its first
	// attempt, limited to MaxRetries. Only a test failure is retried, see
	// retryable. A negative value counts as zero.
	Retries int
	// Quarantined, when non-nil, reports whether an entry path is
	// quarantined. A quarantined entry still runs and is still retried,
	// but its failure does not fail the suite.
	Quarantined func(entryPath string) bool
	// EntryOffset is the position of the first entry of this call in the
	// host's whole entry list. A host that feeds a suite one entry at a
	// time sets it to the number of entries already run, so artifact and
	// video names, which derive from the entry position, stay unique
	// across calls.
	EntryOffset int
}

// SuiteEntryOutcome holds the outcome of one suite entry that
// completed with a result: a Script result for a "script" entry, or a
// Scenario result for a "scenario" entry. Exactly one is populated,
// chosen by the owning SuiteEntryResult.Kind. MarshalJSON renders
// whichever is set with no wrapper object, matching the ScriptResult
// or ScenarioResult union this type stands in for.
type SuiteEntryOutcome struct {
	Script   *runner.ScriptResult
	Scenario *ScenarioResult
}

func (o SuiteEntryOutcome) MarshalJSON() ([]byte, error) {
	if o.Script != nil {
		return json.Marshal(o.Script)
	}
	if o.Scenario != nil {
		return json.Marshal(o.Scenario)
	}
	return []byte("null"), nil
}

// SuiteAttempt is one run of a suite entry. Number counts from 1, and
// Videos lists the files the host kept for the attempt, as the host's
// EntryEnd callback reported them.
type SuiteAttempt struct {
	Number     int                `json:"number"`
	Status     SuiteEntryStatus   `json:"status"`
	Error      *string            `json:"error,omitempty"`
	DurationMs int64              `json:"durationMs"`
	Videos     []string           `json:"videos,omitempty"`
	Result     *SuiteEntryOutcome `json:"-"`
	// Failure describes a failed test for an attempt whose Result the host
	// did not keep, such as an attempt a sharding worker reported. It makes
	// the attempt read as a test that ran and failed, not as an entry that
	// could not run.
	Failure string `json:"-"`
}

// SuiteEntryResult is one suite entry's outcome. Status, Result and
// Error describe the last attempt, and DurationMs spans every attempt.
// Attempts lists them all in order, so it holds one element for an entry
// that ran once.
//
// Flaky marks an entry that failed and then passed, which counts as
// passed. Quarantined marks an entry matched by the quarantine list,
// whatever its outcome. A quarantined entry that failed is a quarantined
// failure and does not fail the suite.
type SuiteEntryResult struct {
	Path        string             `json:"path"`
	Kind        SuiteEntryKind     `json:"kind"`
	Status      SuiteEntryStatus   `json:"status"`
	Result      *SuiteEntryOutcome `json:"result,omitempty"`
	Error       *string            `json:"error,omitempty"`
	DurationMs  int64              `json:"durationMs"`
	Attempts    []SuiteAttempt     `json:"attempts,omitempty"`
	Flaky       bool               `json:"flaky,omitempty"`
	Quarantined bool               `json:"quarantined,omitempty"`
}

// QuarantinedFailure reports whether the entry failed while quarantined.
func (e SuiteEntryResult) QuarantinedFailure() bool {
	return e.Quarantined && e.Status == EntryFailed
}

// SuiteResult is the outcome of running a whole suite. Total counts every
// entry. Passed counts entries that passed, flaky ones included. Failed
// counts failures that are not quarantined, and QuarantinedFailures counts
// the rest, so the three add up to Total. Ok is true when Failed is zero.
type SuiteResult struct {
	SuiteName           *string            `json:"suiteName,omitempty"`
	Ok                  bool               `json:"ok"`
	Entries             []SuiteEntryResult `json:"entries"`
	Total               int                `json:"total"`
	Passed              int                `json:"passed"`
	Failed              int                `json:"failed"`
	Flaky               int                `json:"flaky,omitempty"`
	QuarantinedFailures int                `json:"quarantinedFailures,omitempty"`
	DurationMs          int64              `json:"durationMs"`
}

// SuiteRunner runs an ordered set of independent script or scenario
// entries against one shared Orchestra and, when configured, one
// shared NetworkCapture session. Every entry gets its own fresh
// runner.ScriptRunner or ScenarioRunner, so variables and precondition
// dedup state never leak between entries; each entry is responsible
// for its own launchApp and preconditions, since SuiteRunner never
// launches, resets, or reconnects the device itself.
//
// Unlike ScenarioRunner (fail-fast across its scripts), a failing
// entry here, whether a failed run or a load/parse error, does not
// stop the suite. Every entry runs and is recorded, so a full
// regression sweep reports every failure in one pass instead of
// stopping at the first one.
type SuiteRunner struct {
	deps SuiteRunnerDeps
	// runLog is deps.Logger extended with deps.RunID, computed once so
	// the suite-level outcome entry Run logs shares the same
	// correlation field as every entry's own step trace, without
	// rebuilding it on every call.
	runLog logging.Logger
}

// NewSuiteRunner constructs a SuiteRunner from deps.
func NewSuiteRunner(deps SuiteRunnerDeps) *SuiteRunner {
	runLog := deps.Logger
	if deps.RunID != "" {
		runLog = deps.Logger.Child(logging.LogContext{"runId": deps.RunID})
	}
	return &SuiteRunner{deps: deps, runLog: runLog}
}

// Run executes every entry in order. It returns a non-nil error only
// when ctx is canceled between entries; every other failure is
// reported through the returned SuiteResult's per-entry "failed"
// status, never through the error. A canceled suite still returns the
// result of the entries that ran, with Ok false, so a host can record
// them.
func (r *SuiteRunner) Run(ctx context.Context, entries []SuiteEntry, options *SuiteRunOptions) (SuiteResult, error) {
	startedAt := r.deps.Clock.NowMs()
	results := make([]SuiteEntryResult, 0, len(entries))

	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			partial := r.summarize(results, options, startedAt)
			partial.Ok = false
			return partial, err
		}
		// Each entry is an independent test case, unlike a scenario's
		// own scripts, so it must launch its own app rather than
		// inherit whatever the previous entry already launched.
		r.deps.Orchestra.ResetLaunchedApps()
		results = append(results, r.runEntry(ctx, index+entryOffset(options), entry, options))
	}

	result := r.summarize(results, options, startedAt)
	name := "suite"
	if result.SuiteName != nil {
		name = *result.SuiteName
	}
	r.runLog.Info("suite.run", logging.LogContext{
		"name":       name,
		"ok":         result.Ok,
		"durationMs": result.DurationMs,
	})
	return result, nil
}

func entryOffset(options *SuiteRunOptions) int {
	if options == nil || options.EntryOffset < 0 {
		return 0
	}
	return options.EntryOffset
}

// summarize counts the finished entries into a SuiteResult.
func (r *SuiteRunner) summarize(results []SuiteEntryResult, options *SuiteRunOptions, startedAt int64) SuiteResult {
	var suiteName *string
	if options != nil {
		suiteName = options.SuiteName
	}
	return Summarize(results, suiteName, r.deps.Clock.NowMs()-startedAt)
}

// Summarize counts finished entries into a SuiteResult, the same way a
// SuiteRunner does. A host that gathers entries from several runs uses it
// to build one result.
func Summarize(results []SuiteEntryResult, suiteName *string, durationMs int64) SuiteResult {
	var passed, failed, flaky, quarantinedFailures int
	for _, e := range results {
		switch {
		case e.Status == EntryPassed:
			passed++
		case e.QuarantinedFailure():
			quarantinedFailures++
		default:
			failed++
		}
		if e.Flaky {
			flaky++
		}
	}
	return SuiteResult{
		SuiteName:           suiteName,
		Ok:                  failed == 0,
		Entries:             results,
		Total:               len(results),
		Passed:              passed,
		Failed:              failed,
		Flaky:               flaky,
		QuarantinedFailures: quarantinedFailures,
		DurationMs:          durationMs,
	}
}

// runEntry runs one entry and, while its attempt failed with a test
// failure and retries remain, runs it again. Before each retry it clears
// the launchApp dedup exactly as Run does before every entry, so the
// entry's own launchApp runs again. A retry clears no app data, and the
// entry's preconditions follow their own policy.
func (r *SuiteRunner) runEntry(ctx context.Context, index int, entry SuiteEntry, options *SuiteRunOptions) SuiteEntryResult {
	entryStart := r.deps.Clock.NowMs()
	kind := KindScript
	if script.IsScenarioYaml(entry.Content) {
		kind = KindScenario
	}
	maxAttempts := 1
	quarantined := false
	if options != nil {
		maxAttempts += clampRetries(options.Retries)
		quarantined = options.Quarantined != nil && options.Quarantined(entry.Path)
	}

	var attempts []SuiteAttempt
	for number := 1; ; number++ {
		if number > 1 {
			r.deps.Orchestra.ResetLaunchedApps()
		}
		attempt := r.runAttempt(ctx, index, number, entry, kind, options)
		attempts = append(attempts, attempt)
		if attempt.Status == EntryPassed || number >= maxAttempts || !retryable(ctx.Err(), attempt) {
			break
		}
	}

	last := attempts[len(attempts)-1]
	return SuiteEntryResult{
		Path:        entry.Path,
		Kind:        kind,
		Status:      last.Status,
		Result:      last.Result,
		Error:       last.Error,
		DurationMs:  r.deps.Clock.NowMs() - entryStart,
		Attempts:    attempts,
		Flaky:       last.Status == EntryPassed && len(attempts) > 1,
		Quarantined: quarantined,
	}
}

// runAttempt runs one attempt of entry, resetting the injected
// NetworkCapture first. There is one capture session for the whole suite
// run, not one per attempt, so each attempt otherwise starts with
// whatever the previous attempt's traffic left behind. A load, parse, or
// dispatch failure is reported as a "failed" attempt with Error set,
// never propagated to Run.
func (r *SuiteRunner) runAttempt(ctx context.Context, index, number int, entry SuiteEntry, kind SuiteEntryKind, options *SuiteRunOptions) SuiteAttempt {
	attemptStart := r.deps.Clock.NowMs()

	var entryEnd EntryEnd
	if r.deps.OnEntryStart != nil {
		entryEnd = r.deps.OnEntryStart(ctx, EntryTarget{EntryIndex: index, EntryPath: entry.Path, Attempt: number})
	}

	if r.deps.NetworkCapture != nil {
		r.deps.NetworkCapture.Reset()
	}

	var outcome SuiteEntryOutcome
	var status SuiteEntryStatus
	var errMsg *string

	if kind == KindScenario {
		result, err := r.runScenarioEntry(ctx, index, number, entry, options)
		if err != nil {
			msg := err.Error()
			errMsg = &msg
		} else {
			outcome.Scenario = &result
			status = statusFor(result.Ok)
		}
	} else {
		result, err := r.runScriptEntry(ctx, index, number, entry, options)
		if err != nil {
			msg := err.Error()
			errMsg = &msg
		} else {
			outcome.Script = &result
			status = statusFor(result.Ok)
		}
	}

	attempt := SuiteAttempt{Number: number, DurationMs: r.deps.Clock.NowMs() - attemptStart}
	if errMsg != nil {
		attempt.Status = EntryFailed
		attempt.Error = errMsg
	} else {
		attempt.Status = status
		attempt.Result = &outcome
	}
	if entryEnd != nil {
		attempt.Videos = entryEnd(SuiteEntryResult{
			Path:       entry.Path,
			Kind:       kind,
			Status:     attempt.Status,
			Result:     attempt.Result,
			Error:      attempt.Error,
			DurationMs: attempt.DurationMs,
		})
	}
	return attempt
}

// stepHook returns the step hook of one attempt. A retried attempt stamps
// its number on every event, so the step stream tells attempts apart. The
// first attempt forwards events unchanged.
func (r *SuiteRunner) stepHook(number int) func(runner.StepEvent) {
	hook := r.deps.OnStep
	if hook == nil || number < 2 {
		return hook
	}
	return func(event runner.StepEvent) {
		event.Attempt = number
		hook(event)
	}
}

func statusFor(ok bool) SuiteEntryStatus {
	if ok {
		return EntryPassed
	}
	return EntryFailed
}

func (r *SuiteRunner) runScriptEntry(ctx context.Context, index, attempt int, entry SuiteEntry, options *SuiteRunOptions) (runner.ScriptResult, error) {
	def, err := script.ParseScript(entry.Content, r.deps.ExternalEnv)
	if err != nil {
		return runner.ScriptResult{}, err
	}

	var override runner.PreconditionsOverride
	if options != nil && options.ResolvePreconditions != nil {
		override = options.ResolvePreconditions(nil)
	}

	artifacts, failureCapture := r.deps.ChildArtifacts.childEvidence(
		ChildTarget{EntryIndex: index, EntryPath: entry.Path, Attempt: attempt, ScriptIndex: NoPosition},
		r.deps.Orchestra, r.deps.Clock, r.deps.Logger)
	childRunner := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:             r.deps.Orchestra,
		Clock:                 r.deps.Clock,
		Logger:                r.deps.Logger,
		RunID:                 r.deps.RunID,
		NetworkCapture:        r.deps.NetworkCapture,
		Commands:              r.deps.Commands,
		LaunchNoReset:         r.deps.LaunchNoReset,
		ClearDataOnFresh:      r.deps.ClearDataOnFresh,
		AppArtifact:           r.deps.AppArtifact,
		PreconditionsOverride: override,
		OnStep:                r.stepHook(attempt),
		Artifacts:             artifacts,
		FailureCapture:        failureCapture,
	})
	return childRunner.Run(ctx, def)
}

func (r *SuiteRunner) runScenarioEntry(ctx context.Context, index, attempt int, entry SuiteEntry, options *SuiteRunOptions) (ScenarioResult, error) {
	if r.deps.CreateLoadScript == nil {
		return ScenarioResult{}, fmt.Errorf(
			"entry %q is a scenario, but no createLoadScript was configured to resolve its child scripts", entry.Path)
	}
	def, err := script.ParseScenario(entry.Content)
	if err != nil {
		return ScenarioResult{}, err
	}
	loadScript := r.deps.CreateLoadScript(filepath.Dir(entry.Path))

	var override runner.PreconditionsOverride
	if options != nil && options.ResolvePreconditions != nil {
		override = options.ResolvePreconditions(def.Preconditions)
	}

	scenarioRunner := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra:             r.deps.Orchestra,
		Clock:                 r.deps.Clock,
		Logger:                r.deps.Logger,
		RunID:                 r.deps.RunID,
		NetworkCapture:        r.deps.NetworkCapture,
		Commands:              r.deps.Commands,
		LoadScript:            loadScript,
		ExternalEnv:           r.deps.ExternalEnv,
		LaunchNoReset:         r.deps.LaunchNoReset,
		ClearDataOnFresh:      r.deps.ClearDataOnFresh,
		AppArtifact:           r.deps.AppArtifact,
		PreconditionsOverride: override,
		OnStep:                r.stepHook(attempt),
		ChildArtifacts:        r.deps.ChildArtifacts.forAttempt(index, entry.Path, attempt),
	})
	return scenarioRunner.Run(ctx, def)
}
