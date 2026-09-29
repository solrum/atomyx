package conformance

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/script"
)

// scriptComparable is the subset of a script parse result the
// differential test diffs against the frozen golden: the
// valid/invalid verdict and, when valid, the parsed
// ScriptDefinition. Error message text is excluded here, since zod's
// and this package's messages are not expected to match word for word
// in general, only the outcome they produce. ScriptCase.ExpectErrorText
// marks a case where this package deliberately mirrors zod's own
// issue-message format, verified byte-exact when the golden was
// recorded.
type scriptComparable struct {
	Valid  bool            `json:"valid"`
	Parsed json.RawMessage `json:"parsed"`
}

// TestScriptDifferential diffs the Go script parser's output over
// every corpus/script case against a frozen golden fixture. It also
// confirms every ExpectErrorText case is one Go itself rejects: the
// case is marked ExpectErrorText because, when the golden was
// recorded, Go's rejection message matched the reference
// implementation's byte for byte.
func TestScriptDifferential(t *testing.T) {
	cases, err := LoadScriptCorpus("corpus/script")
	if err != nil {
		t.Fatalf("loading script corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goDef, goErr := script.ParseScript(c.Yaml, c.ExternalEnv)
			goValid := goErr == nil
			var goParsed json.RawMessage
			if goValid {
				goParsed, err = json.Marshal(goDef)
				if err != nil {
					t.Fatalf("marshaling Go parsed script: %v", err)
				}
			}

			goComparable, err := json.Marshal(scriptComparable{Valid: goValid, Parsed: goParsed})
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("script", c.Name+".json"), goComparable, DiffJSON)

			if !goValid && testing.Verbose() {
				t.Logf("script rejected by Go: %v", goErr)
			}
			if c.ExpectErrorText && goValid {
				t.Fatalf("%s: expectErrorText is set but Go accepted the script", c.Description)
			}
		})
	}
}
