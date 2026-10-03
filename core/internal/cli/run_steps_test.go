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

func stepsDeps(stdout, stderr *bytes.Buffer) Deps {
	deps := mockDeps(stdout, stderr)
	deps.Factory = stagedMockFactory{tree: focusedInputTree()}
	return deps
}

func TestRunWritesOneStepLinePerPhaseAndNeverTheTypedText(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), stepsDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": writeScript(t, "flow.yml", secretFlow), "--report-dir": root,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	lines, body := readStepLines(t, singleRunDir(t, root))
	requireOnePhasePairPerStep(t, lines, 2)
	if lines[0]["command"] != "screenshot" || lines[2]["command"] != "type" {
		t.Errorf("commands = %v, %v, want screenshot then type", lines[0]["command"], lines[2]["command"])
	}
	if strings.Contains(body, typedSecret) {
		t.Errorf("steps file carries the typed text:\n%s", body)
	}
}

func TestScenarioRunWritesStepLinesForItsChildren(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "first.yml", "- screenshot: one\n")
	writeFile(t, dir, "second.yml", secretFlow)
	writeFile(t, dir, "scenario.yml", "name: Nested\nscripts:\n  - first.yml\n  - second.yml\n")
	root := filepath.Join(dir, "reports")
	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), stepsDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": filepath.Join(dir, "scenario.yml"), "--report-dir": root,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	lines, body := readStepLines(t, singleRunDir(t, root))
	requireOnePhasePairPerStep(t, lines, 3)
	var commands []any
	for i := 0; i < len(lines); i += 2 {
		commands = append(commands, lines[i]["command"])
	}
	want := []any{"screenshot", "screenshot", "type"}
	for i := range want {
		if commands[i] != want[i] {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
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
	code := RunSuite(context.Background(), stepsDeps(&stdout, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report-dir": root,
	}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	lines, body := readStepLines(t, singleRunDir(t, root))
	requireOnePhasePairPerStep(t, lines, 3)
	if strings.Contains(body, typedSecret) {
		t.Errorf("steps file carries the typed text:\n%s", body)
	}
}

func TestRunKeepsAnEnvValueOutOfTheStepFileWhenAnExpressionCannotConvertIt(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	flow := "appId: com.test\nenv:\n  TOKEN: " + typedSecret + "\n---\n- assertExpr:\n    expr: number(TOKEN) > 1\n"

	code := RunScript(context.Background(), stepsDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": writeScript(t, "flow.yml", flow), "--report-dir": root,
	})
	if code == 0 {
		t.Fatal("the run passed, want a failed assertion")
	}

	lines, body := readStepLines(t, singleRunDir(t, root))
	if strings.Contains(body, typedSecret) {
		t.Errorf("steps file carries the env value:\n%s", body)
	}
	if detail, _ := lines[len(lines)-1]["detail"].(string); !strings.Contains(detail, "cannot convert a string of length") {
		t.Errorf("completed detail = %q, want the conversion failure described by length", detail)
	}
}

const envSecretValue = "cli-env-s3cr3t-5f2c"

const envSecretFlow = "env:\n  SECRET: " + envSecretValue + "\n---\n" +
	"- assertExpr: { expr: \"\\\"${SECRET}\\\" == \\\"${SECRET}\\\"\" }\n" +
	"- tap: { text: \"${SECRET}\" }\n"

func TestRunRecordsNeverCarryAnInterpolatedEnvValue(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), stepsDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": writeScript(t, "flow.yml", envSecretFlow), "--report-dir": root,
	})
	if code == 0 {
		t.Fatal("a tap on an absent element passed")
	}

	runDir := singleRunDir(t, root)
	_, steps := readStepLines(t, runDir)
	if strings.Contains(steps, envSecretValue) {
		t.Errorf("steps file carries the expanded value:\n%s", steps)
	}
	if !strings.Contains(steps, `\"${SECRET}\" == \"${SECRET}\"`) {
		t.Errorf("steps file does not quote the assertion as written:\n%s", steps)
	}
	for _, out := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(out, envSecretValue) {
			t.Errorf("console output carries the expanded value:\n%s", out)
		}
	}
	_ = filepath.WalkDir(runDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) == runfolder.MetaFileName {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(body), envSecretValue) && strings.HasSuffix(path, ".jsonl") {
			t.Errorf("%s carries the expanded value", path)
		}
		return nil
	})
}
