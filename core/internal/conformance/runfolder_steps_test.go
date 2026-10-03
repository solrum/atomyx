package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func TestRunfolderStepLineFormat(t *testing.T) {
	w := writeCorpusRun(t, t.TempDir())
	detail := "Element exists"
	lines := []runfolder.StepLine{
		{StepIndex: 0, Command: "assertExists", Phase: runfolder.StepPhaseStarted},
		{StepIndex: 0, Command: "assertExists", Phase: runfolder.StepPhaseCompleted, Ok: true, DurationMs: 12, Detail: &detail},
		{StepIndex: 1, Command: "screenshot", Phase: runfolder.StepPhaseCompleted, Ok: true, DurationMs: 40, Artifacts: []string{"step-002-home.png"}},
	}
	for _, line := range lines {
		if _, err := w.AppendStep(line); err != nil {
			t.Fatal(err)
		}
	}

	body, err := os.ReadFile(filepath.Join(w.Dir(), runfolder.StepsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var decoded []json.RawMessage
	for _, raw := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		decoded = append(decoded, json.RawMessage(raw))
	}
	got, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "runfolder/step-lines.json", got, DiffJSON)
}
