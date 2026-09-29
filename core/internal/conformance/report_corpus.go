package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// reportCaseMetadata matches ReportMetadata in report.contract.ts.
type reportCaseMetadata struct {
	RunID      string  `json:"runId"`
	Label      string  `json:"label"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt string  `json:"finishedAt"`
	DurationMs int64   `json:"durationMs"`
	Command    string  `json:"command"`
	Env        *string `json:"env,omitempty"`
	Platform   *string `json:"platform,omitempty"`
	Device     *string `json:"device,omitempty"`
}

// reportCaseStepResult matches the subset of a ScriptResult step that
// report.render-json.ts's renderJson reads.
type reportCaseStepResult struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
}

// reportCaseScriptResult matches the subset of a ScriptResult that
// report.render-json.ts's renderJson reads.
type reportCaseScriptResult struct {
	ScriptName   string                 `json:"scriptName"`
	Ok           bool                   `json:"ok"`
	Steps        []reportCaseStepResult `json:"steps"`
	TotalSteps   int                    `json:"totalSteps"`
	PassedSteps  int                    `json:"passedSteps"`
	FailedAtStep *int                   `json:"failedAtStep,omitempty"`
	DurationMs   int64                  `json:"durationMs"`
}

// reportCaseScenarioChild matches the subset of a
// ScenarioScriptResult that report.render-json.ts's renderJson reads.
// Result is nil for a skipped or errored child that never produced a
// script result.
type reportCaseScenarioChild struct {
	ScriptIndex int                     `json:"scriptIndex"`
	ScriptPath  string                  `json:"scriptPath"`
	Status      string                  `json:"status"`
	Error       *string                 `json:"error,omitempty"`
	DurationMs  int64                   `json:"durationMs"`
	Result      *reportCaseScriptResult `json:"result,omitempty"`
}

// reportCaseScenarioResult matches the subset of a ScenarioResult
// that report.render-json.ts's renderJson reads.
type reportCaseScenarioResult struct {
	ScenarioName  string                    `json:"scenarioName"`
	Ok            bool                      `json:"ok"`
	TotalScripts  int                       `json:"totalScripts"`
	PassedScripts int                       `json:"passedScripts"`
	DurationMs    int64                     `json:"durationMs"`
	Scripts       []reportCaseScenarioChild `json:"scripts"`
}

// reportCaseSuiteEntry matches the subset of a SuiteEntryResult that
// report.render-json.ts's renderJson reads. Result is kept as raw
// JSON because its shape depends on Kind: a reportCaseScriptResult
// for "script", a reportCaseScenarioResult for "scenario".
type reportCaseSuiteEntry struct {
	Path       string          `json:"path"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Error      *string         `json:"error,omitempty"`
	DurationMs int64           `json:"durationMs"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// reportCaseSuiteResult matches the subset of a SuiteResult that
// report.render-json.ts's renderJson reads.
type reportCaseSuiteResult struct {
	SuiteName  *string                `json:"suiteName,omitempty"`
	Ok         bool                   `json:"ok"`
	Total      int                    `json:"total"`
	Passed     int                    `json:"passed"`
	Failed     int                    `json:"failed"`
	DurationMs int64                  `json:"durationMs"`
	Entries    []reportCaseSuiteEntry `json:"entries"`
}

// reportCaseScreenshot matches ReportScreenshot in report.contract.ts,
// minus its data field: renderJson only reads scope and label.
type reportCaseScreenshot struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
}

// ReportCase is one differential renderJson case read from a corpus
// file under corpus/report/. It carries a canonical ReportInput
// fixture, of kind "script", "scenario", or "suite", that the Go
// report package and the reference renderJson oracle both
// deserialize into an identical shape. Result is kept as raw JSON
// because its shape depends on Kind; see toReportInput in
// report_differential_test.go for the kind-dispatched decode.
type ReportCase struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Kind        string                 `json:"kind"`
	Metadata    reportCaseMetadata     `json:"metadata"`
	Result      json.RawMessage        `json:"result"`
	Screenshots []reportCaseScreenshot `json:"screenshots"`
}

func loadReportCase(path string) (*ReportCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading report corpus case %s: %w", path, err)
	}
	var c ReportCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing report corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("report corpus case %s: missing name", path)
	}
	return &c, nil
}

// LoadReportCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadReportCorpus(dir string) ([]*ReportCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading report corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*ReportCase, 0, len(names))
	for _, name := range names {
		c, err := loadReportCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
