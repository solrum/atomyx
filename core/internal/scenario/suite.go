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
}

// SuiteRunOptions configures one SuiteRunner.Run call.
type SuiteRunOptions struct {
	// SuiteName is carried through unchanged onto SuiteResult.SuiteName.
	SuiteName *string
	// ResolvePreconditions, when non-nil, is called once per entry to
	// resolve that entry's precondition override. Nil leaves every
	// precondition's own policy in effect for every entry.
	ResolvePreconditions ResolvePreconditions
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

// SuiteEntryResult is one suite entry's outcome.
type SuiteEntryResult struct {
	Path       string             `json:"path"`
	Kind       SuiteEntryKind     `json:"kind"`
	Status     SuiteEntryStatus   `json:"status"`
	Result     *SuiteEntryOutcome `json:"result,omitempty"`
	Error      *string            `json:"error,omitempty"`
	DurationMs int64              `json:"durationMs"`
}

// SuiteResult is the outcome of running a whole suite.
type SuiteResult struct {
	SuiteName  *string            `json:"suiteName,omitempty"`
	Ok         bool               `json:"ok"`
	Entries    []SuiteEntryResult `json:"entries"`
	Total      int                `json:"total"`
	Passed     int                `json:"passed"`
	Failed     int                `json:"failed"`
	DurationMs int64              `json:"durationMs"`
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
// status, never through the error.
func (r *SuiteRunner) Run(ctx context.Context, entries []SuiteEntry, options *SuiteRunOptions) (SuiteResult, error) {
	startedAt := r.deps.Clock.NowMs()
	results := make([]SuiteEntryResult, 0, len(entries))

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return SuiteResult{}, err
		}
		// Each entry is an independent test case, unlike a scenario's
		// own scripts, so it must launch its own app rather than
		// inherit whatever the previous entry already launched.
		r.deps.Orchestra.ResetLaunchedApps()
		results = append(results, r.runEntry(ctx, entry, options))
	}

	passed := 0
	for _, e := range results {
		if e.Status == EntryPassed {
			passed++
		}
	}

	var suiteName *string
	if options != nil {
		suiteName = options.SuiteName
	}
	name := "suite"
	if suiteName != nil {
		name = *suiteName
	}

	ok := passed == len(results)
	durationMs := r.deps.Clock.NowMs() - startedAt

	r.runLog.Info("suite.run", logging.LogContext{
		"name":       name,
		"ok":         ok,
		"durationMs": durationMs,
	})

	return SuiteResult{
		SuiteName:  suiteName,
		Ok:         ok,
		Entries:    results,
		Total:      len(results),
		Passed:     passed,
		Failed:     len(results) - passed,
		DurationMs: durationMs,
	}, nil
}

// runEntry dispatches entry to a script or scenario run by
// script.IsScenarioYaml(entry.Content), resetting the injected
// NetworkCapture first. There is one capture session for the whole
// suite run, not one per entry, so each entry otherwise starts with
// whatever the previous entry's traffic left behind. A load, parse,
// or dispatch failure is reported as a "failed" entry with Error set,
// never propagated to Run.
func (r *SuiteRunner) runEntry(ctx context.Context, entry SuiteEntry, options *SuiteRunOptions) SuiteEntryResult {
	entryStart := r.deps.Clock.NowMs()
	kind := KindScript
	if script.IsScenarioYaml(entry.Content) {
		kind = KindScenario
	}

	if r.deps.NetworkCapture != nil {
		r.deps.NetworkCapture.Reset()
	}

	var outcome SuiteEntryOutcome
	var status SuiteEntryStatus
	var errMsg *string

	if kind == KindScenario {
		result, err := r.runScenarioEntry(ctx, entry, options)
		if err != nil {
			msg := err.Error()
			errMsg = &msg
		} else {
			outcome.Scenario = &result
			status = statusFor(result.Ok)
		}
	} else {
		result, err := r.runScriptEntry(ctx, entry, options)
		if err != nil {
			msg := err.Error()
			errMsg = &msg
		} else {
			outcome.Script = &result
			status = statusFor(result.Ok)
		}
	}

	res := SuiteEntryResult{
		Path:       entry.Path,
		Kind:       kind,
		DurationMs: r.deps.Clock.NowMs() - entryStart,
	}
	if errMsg != nil {
		res.Status = EntryFailed
		res.Error = errMsg
	} else {
		res.Status = status
		res.Result = &outcome
	}
	return res
}

func statusFor(ok bool) SuiteEntryStatus {
	if ok {
		return EntryPassed
	}
	return EntryFailed
}

func (r *SuiteRunner) runScriptEntry(ctx context.Context, entry SuiteEntry, options *SuiteRunOptions) (runner.ScriptResult, error) {
	def, err := script.ParseScript(entry.Content, r.deps.ExternalEnv)
	if err != nil {
		return runner.ScriptResult{}, err
	}

	var override runner.PreconditionsOverride
	if options != nil && options.ResolvePreconditions != nil {
		override = options.ResolvePreconditions(nil)
	}

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
	})
	return childRunner.Run(ctx, def)
}

func (r *SuiteRunner) runScenarioEntry(ctx context.Context, entry SuiteEntry, options *SuiteRunOptions) (ScenarioResult, error) {
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
	})
	return scenarioRunner.Run(ctx, def)
}
