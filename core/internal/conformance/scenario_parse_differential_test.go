package conformance

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/script"
)

// scenarioComparable is the subset of a scenario parse result the
// differential test diffs against the frozen golden: the
// valid/invalid verdict and, when valid, the parsed
// ScenarioDefinition. Error message text is excluded here for the
// same reason scriptComparable excludes it. ScenarioCase.ExpectErrorText
// marks a case verified byte-exact against the reference
// implementation's message when the golden was recorded.
type scenarioComparable struct {
	Valid  bool            `json:"valid"`
	Parsed json.RawMessage `json:"parsed"`
}

// TestScenarioParseDifferential diffs the Go scenario parser's output
// over every corpus/scenario_parse case against a frozen golden
// fixture. It also confirms every ExpectErrorText case is one Go
// itself rejects.
func TestScenarioParseDifferential(t *testing.T) {
	cases, err := LoadScenarioCorpus("corpus/scenario_parse")
	if err != nil {
		t.Fatalf("loading scenario corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goDef, goErr := script.ParseScenario(c.Yaml)
			goValid := goErr == nil
			var goParsed json.RawMessage
			if goValid {
				goParsed, err = json.Marshal(goDef)
				if err != nil {
					t.Fatalf("marshaling Go parsed scenario: %v", err)
				}
			}

			goComparable, err := json.Marshal(scenarioComparable{Valid: goValid, Parsed: goParsed})
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("scenario_parse", c.Name+".json"), goComparable, DiffJSON)

			if !goValid && testing.Verbose() {
				t.Logf("scenario rejected by Go: %v", goErr)
			}
			if c.ExpectErrorText && goValid {
				t.Fatalf("%s: expectErrorText is set but Go accepted the scenario", c.Description)
			}
		})
	}
}
