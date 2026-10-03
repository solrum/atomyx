package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runfolder"
)

const typedSecret = "hunter2-correct-horse"

const secretFlow = "- screenshot: home\n- type: { text: " + typedSecret + " }\n"

func readStepLines(t *testing.T, runDir string) ([]map[string]any, string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(runDir, runfolder.StepsFileName))
	if err != nil {
		t.Fatalf("reading steps file: %v", err)
	}
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("decoding step line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines, string(body)
}

func requireOnePhasePairPerStep(t *testing.T, lines []map[string]any, wantSteps int) {
	t.Helper()
	if len(lines) != wantSteps*2 {
		t.Fatalf("got %d step lines, want %d (a started and a completed line per step)", len(lines), wantSteps*2)
	}
	for i, line := range lines {
		if seq, _ := line["seq"].(float64); int(seq) != i+1 {
			t.Errorf("line %d: seq = %v, want %d", i, line["seq"], i+1)
		}
		wantPhase := "started"
		if i%2 == 1 {
			wantPhase = "completed"
		}
		if line["phase"] != wantPhase {
			t.Errorf("line %d: phase = %v, want %s", i, line["phase"], wantPhase)
		}
		if line["schemaVersion"] != float64(1) {
			t.Errorf("line %d: schemaVersion = %v, want 1", i, line["schemaVersion"])
		}
	}
}

func TestRunWritesOneStepLinePerPhaseAndNeverTheTypedText(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	// The mock device has no focused field, so the type step fails. Its
	// lines are still written, which is the case worth checking.
	RunScript(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": writeScript(t, "flow.yml", secretFlow), "--report-dir": root,
	})

	lines, body := readStepLines(t, singleRunDir(t, root))
	requireOnePhasePairPerStep(t, lines, 2)
	if lines[0]["command"] != "screenshot" || lines[2]["command"] != "type" {
		t.Errorf("commands = %v, %v, want screenshot then type", lines[0]["command"], lines[2]["command"])
	}
	if strings.Contains(body, typedSecret) {
		t.Errorf("steps file carries the typed text:\n%s", body)
	}
}

func TestSuiteWritesStepLinesForEveryEntryInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a.yml", secretFlow)
	writeFile(t, dir, "b.yml", "- screenshot: second\n")
	root := filepath.Join(dir, "reports")
	var stdout, stderr bytes.Buffer
	RunSuite(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report-dir": root,
	}, nil)

	lines, body := readStepLines(t, singleRunDir(t, root))
	requireOnePhasePairPerStep(t, lines, 3)
	if strings.Contains(body, typedSecret) {
		t.Errorf("steps file carries the typed text:\n%s", body)
	}
}
