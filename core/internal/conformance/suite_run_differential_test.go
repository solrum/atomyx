package conformance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

type suiteEntryResultComparable struct {
	Path       string          `json:"path"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *string         `json:"error,omitempty"`
	DurationMs int64           `json:"durationMs"`
}

type suiteResultComparable struct {
	SuiteName  *string                      `json:"suiteName,omitempty"`
	Ok         bool                         `json:"ok"`
	Entries    []suiteEntryResultComparable `json:"entries"`
	Total      int                          `json:"total"`
	Passed     int                          `json:"passed"`
	Failed     int                          `json:"failed"`
	DurationMs int64                        `json:"durationMs"`
}

func goSuiteEntryComparable(e scenario.SuiteEntryResult) (suiteEntryResultComparable, error) {
	out := suiteEntryResultComparable{
		Path: e.Path, Kind: string(e.Kind), Status: string(e.Status), Error: e.Error, DurationMs: e.DurationMs,
	}
	if e.Result == nil {
		return out, nil
	}
	var raw []byte
	var err error
	switch {
	case e.Result.Script != nil:
		raw, err = json.Marshal(goScriptResultComparable(*e.Result.Script))
	case e.Result.Scenario != nil:
		raw, err = json.Marshal(goScenarioResultComparable(*e.Result.Scenario))
	}
	if err != nil {
		return suiteEntryResultComparable{}, err
	}
	out.Result = raw
	return out, nil
}

func goSuiteResultComparable(r scenario.SuiteResult) (suiteResultComparable, error) {
	entries := make([]suiteEntryResultComparable, len(r.Entries))
	for i, e := range r.Entries {
		comparable, err := goSuiteEntryComparable(e)
		if err != nil {
			return suiteResultComparable{}, err
		}
		entries[i] = comparable
	}
	return suiteResultComparable{
		SuiteName: r.SuiteName, Ok: r.Ok, Entries: entries,
		Total: r.Total, Passed: r.Passed, Failed: r.Failed, DurationMs: r.DurationMs,
	}, nil
}

// flatCreateLoadScript resolves a scenario entry's child scripts
// against scripts, keyed by "<dir>/<childPath>" (or bare childPath
// when dir is "" or "."), the same key convention suiteRunCorpus uses
// to build each case's Scripts map.
func flatCreateLoadScript(scripts map[string]string) scenario.CreateLoadScript {
	return func(dir string) scenario.LoadScript {
		return func(path string) (string, error) {
			key := path
			if dir != "" && dir != "." {
				key = dir + "/" + path
			}
			content, ok := scripts[key]
			if !ok {
				return "", &suiteUnknownScriptError{key: key}
			}
			return content, nil
		}
	}
}

type suiteUnknownScriptError struct{ key string }

func (e *suiteUnknownScriptError) Error() string {
	return "suite corpus referenced unknown child script: " + e.key
}

// runGoSuiteCase runs c's entries through the Go suite runner against
// a fresh scenarioRunFixture, for the caller to turn into a golden
// comparable.
func runGoSuiteCase(t *testing.T, c SuiteRunCase) scenario.SuiteResult {
	t.Helper()

	entries := make([]scenario.SuiteEntry, len(c.Entries))
	for i, e := range c.Entries {
		entries[i] = scenario.SuiteEntry{Path: e.Path, Content: e.Content}
	}

	orch, clock := newScenarioRunFixture()
	r := scenario.NewSuiteRunner(scenario.SuiteRunnerDeps{
		Orchestra:        orch,
		Clock:            clock,
		Logger:           noopLogger{},
		Commands:         runner.DefaultCommands(),
		CreateLoadScript: flatCreateLoadScript(c.Scripts),
	})

	var options *scenario.SuiteRunOptions
	if c.SuiteName != nil {
		options = &scenario.SuiteRunOptions{SuiteName: c.SuiteName}
	}

	result, runErr := r.Run(context.Background(), entries, options)
	if runErr != nil {
		t.Fatalf("Go Run: %v", runErr)
	}
	return result
}

func TestSuiteRunDifferential(t *testing.T) {
	for _, c := range suiteRunCorpus() {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			result := runGoSuiteCase(t, c)

			goComp, err := goSuiteResultComparable(result)
			if err != nil {
				t.Fatalf("building Go comparable: %v", err)
			}
			goJSON, err := json.Marshal(goComp)
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("suite_run", c.Name+".json"), goJSON, DiffRunnerJSON)
		})
	}
}
