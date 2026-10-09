package report

import (
	"fmt"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

const reportVersion = 1

// ReportJsonScreenshotRef is one screenshot reference inside a
// rendered ReportJson: its label and the file it was written under,
// relative to the run's artifacts directory.
type ReportJsonScreenshotRef struct {
	Label string `json:"label"`
	File  string `json:"file"`
}

// ReportJsonStepResult is one executed step inside a rendered
// ReportJsonScriptResult.
type ReportJsonStepResult struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
}

// ReportJsonScriptResult is the "script" kind of ReportJson.Result.
type ReportJsonScriptResult struct {
	ScriptName   string                    `json:"scriptName"`
	Ok           bool                      `json:"ok"`
	Steps        []ReportJsonStepResult    `json:"steps"`
	TotalSteps   int                       `json:"totalSteps"`
	PassedSteps  int                       `json:"passedSteps"`
	FailedAtStep *int                      `json:"failedAtStep,omitempty"`
	DurationMs   int64                     `json:"durationMs"`
	Screenshots  []ReportJsonScreenshotRef `json:"screenshots"`
}

// ReportJsonScenarioChild is one scenario child script's outcome
// inside a rendered ReportJsonScenarioResult.
type ReportJsonScenarioChild struct {
	ScriptIndex int                      `json:"scriptIndex"`
	ScriptPath  string                   `json:"scriptPath"`
	Status      scenario.ScriptRunStatus `json:"status"`
	Error       *string                  `json:"error,omitempty"`
	DurationMs  int64                    `json:"durationMs"`
	Result      *ReportJsonScriptResult  `json:"result,omitempty"`
}

// ReportJsonScenarioResult is the "scenario" kind of ReportJson.Result.
type ReportJsonScenarioResult struct {
	ScenarioName  string                    `json:"scenarioName"`
	Ok            bool                      `json:"ok"`
	TotalScripts  int                       `json:"totalScripts"`
	PassedScripts int                       `json:"passedScripts"`
	DurationMs    int64                     `json:"durationMs"`
	Scripts       []ReportJsonScenarioChild `json:"scripts"`
}

// ReportJsonSuiteEntry is one suite entry's outcome inside a rendered
// ReportJsonSuiteResult. Result holds a ReportJsonScriptResult for a
// "script" entry or a ReportJsonScenarioResult for a "scenario"
// entry, chosen by Kind.
type ReportJsonSuiteEntry struct {
	Path       string                    `json:"path"`
	Kind       scenario.SuiteEntryKind   `json:"kind"`
	Status     scenario.SuiteEntryStatus `json:"status"`
	Error      *string                   `json:"error,omitempty"`
	DurationMs int64                     `json:"durationMs"`
	Result     any                       `json:"result,omitempty"`
	// Videos are the kept video files of the entry, relative to the
	// artifacts directory. Omitted when none were kept.
	Videos []string `json:"videos,omitempty"`
	// Flaky marks an entry that failed and then passed, and Quarantined
	// marks an entry matched by the quarantine list. Both are omitted when
	// false.
	Flaky       bool `json:"flaky,omitempty"`
	Quarantined bool `json:"quarantined,omitempty"`
	// Attempts lists every attempt of an entry that ran more than once.
	Attempts []ReportJsonAttempt `json:"attempts,omitempty"`
}

// ReportJsonSuiteResult is the "suite" kind of ReportJson.Result.
type ReportJsonSuiteResult struct {
	SuiteName *string `json:"suiteName,omitempty"`
	Ok        bool    `json:"ok"`
	Total     int     `json:"total"`
	Passed    int     `json:"passed"`
	Failed    int     `json:"failed"`
	// Flaky counts entries that failed and then passed, and
	// QuarantinedFailures counts quarantined entries that failed. Both are
	// omitted when zero.
	Flaky               int                    `json:"flaky,omitempty"`
	QuarantinedFailures int                    `json:"quarantinedFailures,omitempty"`
	DurationMs          int64                  `json:"durationMs"`
	Entries             []ReportJsonSuiteEntry `json:"entries"`
}

// ReportJson is the versioned, machine-readable report RenderJSON
// produces. Downstream tools and agents read ReportVersion to know
// which shape to expect; a future breaking change to this shape
// increments it. Result holds a ReportJsonScriptResult,
// ReportJsonScenarioResult, or ReportJsonSuiteResult, chosen by Kind.
type ReportJson struct {
	ReportVersion int            `json:"reportVersion"`
	Kind          ReportKind     `json:"kind"`
	Metadata      ReportMetadata `json:"metadata"`
	Result        any            `json:"result"`
}

// RenderJSON renders a ReportJson for input: the full result plus
// the run's metadata, with every screenshot replaced by a reference
// to its file under the run's artifacts directory.
//
// byScope is the run's screenshot index grouped by owning scope (see
// BuildScreenshotIndex). Compute it once per run and pass the same
// value into RenderJSON, RenderHTML, and a Reporter, so every one of
// them resolves the same screenshots for the same scope.
func RenderJSON(input ReportInput, byScope ScreenshotsByScope) (ReportJson, error) {
	switch v := input.(type) {
	case ScriptReportInput:
		return ReportJson{
			ReportVersion: reportVersion,
			Kind:          ReportKindScript,
			Metadata:      v.Metadata,
			Result:        toJSONScript(v.Result, byScope, RootScriptScope),
		}, nil
	case ScenarioReportInput:
		return ReportJson{
			ReportVersion: reportVersion,
			Kind:          ReportKindScenario,
			Metadata:      v.Metadata,
			Result:        toJSONScenario(v.Result, byScope, RootScriptScope),
		}, nil
	case SuiteReportInput:
		return ReportJson{
			ReportVersion: reportVersion,
			Kind:          ReportKindSuite,
			Metadata:      v.Metadata,
			Result:        toJSONSuite(v.Result, v.EntryVideos, byScope),
		}, nil
	default:
		return ReportJson{}, fmt.Errorf("report: renderJson does not support report kind %q yet", input.ReportKind())
	}
}

func toJSONScreenshots(byScope ScreenshotsByScope, scope string) []ReportJsonScreenshotRef {
	refs := byScope[scope]
	out := make([]ReportJsonScreenshotRef, len(refs))
	for i, ref := range refs {
		out[i] = ReportJsonScreenshotRef{Label: ref.Label, File: ref.File}
	}
	return out
}

func toJSONScript(result runner.ScriptResult, byScope ScreenshotsByScope, scope string) ReportJsonScriptResult {
	steps := make([]ReportJsonStepResult, len(result.Steps))
	for i, step := range result.Steps {
		steps[i] = ReportJsonStepResult{
			StepIndex:  step.StepIndex,
			Command:    step.Command,
			Ok:         step.Ok,
			Detail:     step.Detail,
			DurationMs: step.DurationMs,
		}
	}
	return ReportJsonScriptResult{
		ScriptName:   result.ScriptName,
		Ok:           result.Ok,
		Steps:        steps,
		TotalSteps:   result.TotalSteps,
		PassedSteps:  result.PassedSteps,
		FailedAtStep: result.FailedAtStep,
		DurationMs:   result.DurationMs,
		Screenshots:  toJSONScreenshots(byScope, scope),
	}
}

func toJSONScenario(result scenario.ScenarioResult, byScope ScreenshotsByScope, parentScope string) ReportJsonScenarioResult {
	scripts := make([]ReportJsonScenarioChild, len(result.Scripts))
	for i, child := range result.Scripts {
		var jsonResult *ReportJsonScriptResult
		if child.Result != nil {
			scoped := toJSONScript(*child.Result, byScope, ScenarioChildScope(parentScope, child.ScriptIndex, child.ScriptPath))
			jsonResult = &scoped
		}
		scripts[i] = ReportJsonScenarioChild{
			ScriptIndex: child.ScriptIndex,
			ScriptPath:  child.ScriptPath,
			Status:      child.Status,
			Error:       child.Error,
			DurationMs:  child.DurationMs,
			Result:      jsonResult,
		}
	}
	return ReportJsonScenarioResult{
		ScenarioName:  result.ScenarioName,
		Ok:            result.Ok,
		TotalScripts:  result.TotalScripts,
		PassedScripts: result.PassedScripts,
		DurationMs:    result.DurationMs,
		Scripts:       scripts,
	}
}

func toJSONSuite(result scenario.SuiteResult, entryVideos [][]string, byScope ScreenshotsByScope) ReportJsonSuiteResult {
	entries := make([]ReportJsonSuiteEntry, len(result.Entries))
	for i, entry := range result.Entries {
		scope := SuiteAttemptScope(SuiteEntryScope(i, entry.Path), finalAttemptNumber(entry))
		var jsonResult any
		if entry.Result != nil {
			switch {
			case entry.Kind == scenario.KindScenario && entry.Result.Scenario != nil:
				jsonResult = toJSONScenario(*entry.Result.Scenario, byScope, scope)
			case entry.Result.Script != nil:
				jsonResult = toJSONScript(*entry.Result.Script, byScope, scope)
			}
		}
		entries[i] = ReportJsonSuiteEntry{
			Path:        entry.Path,
			Kind:        entry.Kind,
			Status:      entry.Status,
			Error:       entry.Error,
			DurationMs:  entry.DurationMs,
			Result:      jsonResult,
			Videos:      videosOf(entryVideos, i),
			Flaky:       entry.Flaky,
			Quarantined: entry.Quarantined,
			Attempts:    jsonAttempts(entry),
		}
	}
	return ReportJsonSuiteResult{
		SuiteName:           result.SuiteName,
		Ok:                  result.Ok,
		Total:               result.Total,
		Passed:              result.Passed,
		Failed:              result.Failed,
		Flaky:               result.Flaky,
		QuarantinedFailures: result.QuarantinedFailures,
		DurationMs:          result.DurationMs,
		Entries:             entries,
	}
}

func videosOf(entryVideos [][]string, index int) []string {
	if index >= len(entryVideos) || len(entryVideos[index]) == 0 {
		return nil
	}
	return entryVideos[index]
}
