package cli

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
	"time"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// Result rendering and report assembly: mapping a run's result to its
// JSON or text form and writing the on-disk report bundle for it.

// jsonStepResult, jsonArtifacts, jsonScreenshot, and jsonScriptResult
// define the --json run output's exact field names and order, which
// is a stable contract callers depend on byte-for-byte.
// runner.ScriptResult and runner.StepResult carry the same data under
// Go-idiomatic names, so these types are a pure rename, not a
// reshape.
type jsonStepResult struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
}

type jsonScreenshot struct {
	Label     string `json:"label"`
	SizeBytes int    `json:"sizeBytes"`
}

type jsonArtifacts struct {
	Screenshots []jsonScreenshot `json:"screenshots"`
}

type jsonScriptResult struct {
	ScriptName   string           `json:"scriptName"`
	Ok           bool             `json:"ok"`
	Steps        []jsonStepResult `json:"steps"`
	TotalSteps   int              `json:"totalSteps"`
	PassedSteps  int              `json:"passedSteps"`
	FailedAtStep *int             `json:"failedAtStep,omitempty"`
	DurationMs   int64            `json:"durationMs"`
	Artifacts    jsonArtifacts    `json:"artifacts"`
	// Abort is present only when the run stopped early for a reason
	// beyond an ordinary step failure (the device or agent going
	// unreachable, or a script refused outright for a declared
	// requirement this host cannot meet), consistent with how
	// runner.ScriptResult.Abort reports the same outcome.
	Abort *jsonAbort `json:"abort,omitempty"`
}

// jsonAbort is the --json run output's shape for runner.RunAbort.
type jsonAbort struct {
	Reason  string `json:"reason"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Remedy  string `json:"remedy,omitempty"`
}

func toJSONScriptResult(result runner.ScriptResult) jsonScriptResult {
	steps := make([]jsonStepResult, len(result.Steps))
	for i, s := range result.Steps {
		steps[i] = jsonStepResult{
			StepIndex:  s.StepIndex,
			Command:    s.Command,
			Ok:         s.Ok,
			Detail:     s.Detail,
			DurationMs: s.DurationMs,
		}
	}
	screenshots := result.Artifacts.Screenshots()
	shots := make([]jsonScreenshot, len(screenshots))
	for i, s := range screenshots {
		shots[i] = jsonScreenshot{Label: s.Label, SizeBytes: len(s.Data)}
	}
	var abort *jsonAbort
	if result.Abort != nil {
		abort = &jsonAbort{
			Reason:  string(result.Abort.Reason),
			Code:    result.Abort.Code,
			Message: result.Abort.Message,
			Remedy:  result.Abort.Remedy,
		}
	}
	return jsonScriptResult{
		ScriptName:   result.ScriptName,
		Ok:           result.Ok,
		Steps:        steps,
		TotalSteps:   result.TotalSteps,
		PassedSteps:  result.PassedSteps,
		FailedAtStep: result.FailedAtStep,
		DurationMs:   result.DurationMs,
		Artifacts:    jsonArtifacts{Screenshots: shots},
		Abort:        abort,
	}
}

// jsonScenarioScriptResult and jsonScenarioResult define the --json
// run output's exact shape for a scenario, which is a stable
// contract callers depend on byte-for-byte. Each child script's raw
// screenshot buffers are represented as size-only metadata through
// jsonScriptResult rather than the actual image bytes, keeping the
// JSON payload small even when a scenario captures many screenshots.
type jsonScenarioScriptResult struct {
	ScriptIndex int               `json:"scriptIndex"`
	ScriptPath  string            `json:"scriptPath"`
	Status      string            `json:"status"`
	Result      *jsonScriptResult `json:"result,omitempty"`
	Error       *string           `json:"error,omitempty"`
	DurationMs  int64             `json:"durationMs"`
}

type jsonScenarioResult struct {
	ScenarioName  string                     `json:"scenarioName"`
	Ok            bool                       `json:"ok"`
	Scripts       []jsonScenarioScriptResult `json:"scripts"`
	TotalScripts  int                        `json:"totalScripts"`
	PassedScripts int                        `json:"passedScripts"`
	DurationMs    int64                      `json:"durationMs"`
}

func toJSONScenarioResult(result scenario.ScenarioResult) jsonScenarioResult {
	scripts := make([]jsonScenarioScriptResult, len(result.Scripts))
	for i, s := range result.Scripts {
		var scriptResult *jsonScriptResult
		if s.Result != nil {
			converted := toJSONScriptResult(*s.Result)
			scriptResult = &converted
		}
		scripts[i] = jsonScenarioScriptResult{
			ScriptIndex: s.ScriptIndex,
			ScriptPath:  s.ScriptPath,
			Status:      string(s.Status),
			Result:      scriptResult,
			Error:       s.Error,
			DurationMs:  s.DurationMs,
		}
	}
	return jsonScenarioResult{
		ScenarioName:  result.ScenarioName,
		Ok:            result.Ok,
		Scripts:       scripts,
		TotalScripts:  result.TotalScripts,
		PassedScripts: result.PassedScripts,
		DurationMs:    result.DurationMs,
	}
}

// printScenarioResult renders result to w as fixed, human-readable
// text: exact wording, spacing, and icons are part of the output's
// contract and must not change incidentally.
func printScenarioResult(w io.Writer, result scenario.ScenarioResult) {
	status := "FAIL"
	if result.Ok {
		status = "PASS"
	}
	_, _ = fmt.Fprintf(w, "\n%s: scenario \"%s\"\n", status, result.ScenarioName)
	_, _ = fmt.Fprintf(w, "  %d/%d scripts passed", result.PassedScripts, result.TotalScripts)
	_, _ = fmt.Fprintf(w, " (%dms)\n\n", result.DurationMs)

	for _, s := range result.Scripts {
		icon := "✗"
		switch s.Status {
		case scenario.StatusPassed:
			icon = "✓"
		case scenario.StatusSkipped:
			icon = "·"
		case scenario.StatusErrored:
			icon = "!"
		}
		_, _ = fmt.Fprintf(w, "  %s [%d] %s: %s", icon, s.ScriptIndex+1, s.ScriptPath, s.Status)
		if s.DurationMs > 0 {
			_, _ = fmt.Fprintf(w, " (%dms)", s.DurationMs)
		}
		if s.Result != nil && s.Result.FailedAtStep != nil {
			_, _ = fmt.Fprintf(w, " (failed at step %d)", *s.Result.FailedAtStep+1)
		}
		if s.Error != nil {
			_, _ = fmt.Fprintf(w, ": %s", *s.Error)
		}
		_, _ = io.WriteString(w, "\n")
	}
	_, _ = io.WriteString(w, "\n")
}

// printTextResult renders result to w as fixed, human-readable text.
// Its exact wording, spacing, and icons are part of the output's
// contract: tests assert against it byte-for-byte, so any change here
// is a deliberate breaking change to that output.
func printTextResult(w io.Writer, result runner.ScriptResult) {
	status := "FAIL"
	if result.Ok {
		status = "PASS"
	}
	_, _ = fmt.Fprintf(w, "\n%s: %s\n", status, result.ScriptName)
	_, _ = fmt.Fprintf(w, "  %d/%d steps passed", result.PassedSteps, result.TotalSteps)
	_, _ = fmt.Fprintf(w, " (%dms)\n\n", result.DurationMs)

	for _, step := range result.Steps {
		icon := "✗"
		if step.Ok {
			icon = "✓"
		}
		detail := ""
		if step.Detail != nil {
			detail = fmt.Sprintf(": %s", *step.Detail)
		}
		_, _ = fmt.Fprintf(w, "  %s [%d] %s (%dms)%s\n", icon, step.StepIndex+1, step.Command, step.DurationMs, detail)
	}

	if result.FailedAtStep != nil {
		_, _ = fmt.Fprintf(w, "\n  Failed at step %d\n", *result.FailedAtStep+1)
	}

	if result.Abort != nil {
		_, _ = fmt.Fprintf(w, "\n  Aborted [%s]: %s\n", result.Abort.Code, result.Abort.Message)
		if result.Abort.Remedy != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", result.Abort.Remedy)
		}
	}

	screenshots := result.Artifacts.Screenshots()
	if len(screenshots) > 0 {
		labels := make([]string, len(screenshots))
		for i, s := range screenshots {
			labels[i] = s.Label
		}
		_, _ = fmt.Fprintf(w, "\n  Screenshots: %s\n", strings.Join(labels, ", "))
	}
	_, _ = io.WriteString(w, "\n")
}

// buildReportMetadata assembles a report.ReportMetadata from what a
// run command knows about itself. envName is the selected config
// environment's name, nil when none applied. deviceID is rendered as
// nil rather than an empty string when no device was targeted
// explicitly. abort is the run's device-lost (or other early-stop)
// outcome, nil for an ordinary run.
func buildReportMetadata(runID, label string, startedAt, finishedAt time.Time, durationMs int64, platform, deviceID string, envName *string, abort *runner.RunAbort) report.ReportMetadata {
	metadata := report.ReportMetadata{
		RunID:      runID,
		Label:      label,
		StartedAt:  formatReportTimestamp(startedAt),
		FinishedAt: formatReportTimestamp(finishedAt),
		DurationMs: durationMs,
		Command:    report.ReportCommandRun,
		Env:        envName,
		Platform:   &platform,
	}
	if deviceID != "" {
		metadata.Device = &deviceID
	}
	if abort != nil {
		metadata.Abort = &report.ReportAbort{
			Reason:  string(abort.Reason),
			Code:    abort.Code,
			Message: abort.Message,
			Remedy:  abort.Remedy,
		}
	}
	return metadata
}

// writeReport builds this run's report.ScriptReportInput and writes
// the bundle through a report.NewFsReporter for a "run" command
// script result. runID is the caller's already-generated run
// identifier, so the report bundle's directory name matches the id
// this run's live log trace carried.
func writeReport(
	runID string,
	result runner.ScriptResult,
	startedAt, finishedAt time.Time,
	platform, deviceID string,
	envName *string,
	options report.ReportOptions,
) (report.ReportBundle, error) {
	metadata := buildReportMetadata(runID, result.ScriptName, startedAt, finishedAt, result.DurationMs, platform, deviceID, envName, result.Abort)
	input := report.ScriptReportInput{
		Metadata:    metadata,
		Result:      result,
		Screenshots: toReportScreenshots(result.Artifacts.Screenshots()),
	}
	return report.NewFsReporter().Generate(input, options)
}

// writeScenarioReport builds this run's report.ScenarioReportInput
// and writes the bundle through a report.NewFsReporter for a "run"
// command scenario result. runID is the caller's already-generated
// run identifier, so the report bundle's directory name matches the
// id this run's live log trace carried.
func writeScenarioReport(
	runID string,
	result scenario.ScenarioResult,
	startedAt, finishedAt time.Time,
	platform, deviceID string,
	envName *string,
	options report.ReportOptions,
) (report.ReportBundle, error) {
	metadata := buildReportMetadata(runID, result.ScenarioName, startedAt, finishedAt, result.DurationMs, platform, deviceID, envName, nil)
	input := report.ScenarioReportInput{
		Metadata:    metadata,
		Result:      result,
		Screenshots: scenarioReportScreenshots(result),
	}
	return report.NewFsReporter().Generate(input, options)
}

// formatReportTimestamp renders t as a UTC timestamp with millisecond
// precision (e.g. "2026-09-14T03:59:31.064Z"). This exact format is
// part of the report bundle's on-disk contract, so consumers that
// parse report metadata can rely on it byte-for-byte.
func formatReportTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// runIDSuffixAlphabet and runIDSuffixLength describe the random
// suffix generateRunID appends: 6 characters drawn from base36
// (0-9, a-z). The suffix's own value is never meaningful on its own,
// only its shape: consumers that parse a run id only need to know it
// ends in a fixed-length base36 suffix, not what value it holds.
const runIDSuffixAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
const runIDSuffixLength = 6

// generateRunID builds a run's report id from startedAt: a UTC,
// millisecond-precision timestamp with ":" and "." replaced by "-"
// and lowercased, then a random base36 suffix, e.g.
// "2026-09-14t03-59-31-064z-a1b2c3". This exact format is part of the
// report bundle's on-disk contract, so tooling that locates or sorts
// run directories can rely on it byte-for-byte.
func generateRunID(startedAt time.Time) string {
	stamp := strings.ToLower(formatReportTimestamp(startedAt))
	stamp = strings.NewReplacer(":", "-", ".", "-").Replace(stamp)
	return stamp + "-" + randomRunIDSuffix()
}

// randomRunIDSuffix draws runIDSuffixLength characters from
// runIDSuffixAlphabet using a cryptographic random source. The
// resulting distribution has a slight bias toward the alphabet's
// first 28 characters (256 is not an exact multiple of 36), which is
// immaterial here: the suffix only needs to disambiguate two runs
// that start within the same millisecond, not to be uniform.
func randomRunIDSuffix() string {
	raw := make([]byte, runIDSuffixLength)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	suffix := make([]byte, runIDSuffixLength)
	for i, v := range raw {
		suffix[i] = runIDSuffixAlphabet[int(v)%len(runIDSuffixAlphabet)]
	}
	return string(suffix)
}

// toReportScreenshots converts a script run's flat screenshot list
// into report.ReportScreenshot values, all tagged with the root
// script scope: a standalone script run has only one script in its
// result, so there is no scenario or suite position to key on.
func toReportScreenshots(screenshots []runner.Screenshot) []report.ReportScreenshot {
	out := make([]report.ReportScreenshot, len(screenshots))
	for i, s := range screenshots {
		out[i] = report.ReportScreenshot{Scope: report.RootScriptScope, Label: s.Label, Data: s.Data, MimeType: s.MimeType}
	}
	return out
}

// scenarioReportScreenshots flattens every child script's screenshots
// out of a scenario result for a standalone scenario run. Each
// child's scope carries its position in the scenario's scripts list
// (via report.ScenarioChildScope), so two children that reference the
// same script path still get distinct scopes.
func scenarioReportScreenshots(result scenario.ScenarioResult) []report.ReportScreenshot {
	var out []report.ReportScreenshot
	for _, child := range result.Scripts {
		if child.Result == nil {
			continue
		}
		scope := report.ScenarioChildScope(report.RootScriptScope, child.ScriptIndex, child.ScriptPath)
		for _, shot := range child.Result.Artifacts.Screenshots() {
			out = append(out, report.ReportScreenshot{Scope: scope, Label: shot.Label, Data: shot.Data, MimeType: shot.MimeType})
		}
	}
	return out
}
