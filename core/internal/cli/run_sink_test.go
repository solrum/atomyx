package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

const deviceHeldSecret = "device-held-pw-81f3"

// TestRunMasksAValueReadFromASecureField proves the run passes its secret
// set to the runner. The value is neither an external one nor token-shaped,
// so only the runner marking the read secret can get it masked when a later
// step prints it.
func TestRunMasksAValueReadFromASecureField(t *testing.T) {
	tree := wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root", "bounds": "0,0,400,800"},
		Children: []wire.TreeNodeWire{{Attributes: map[string]string{
			"id": "pw", "value": deviceHeldSecret, "bounds": "0,0,100,50", wire.AttrSecure: wire.AttrSecureTrue,
		}}},
	}
	const script = "appId: com.example.app\n---\n" +
		"- read: { selector: { id: pw }, field: value, as: pwd }\n" +
		"- capture: { pattern: \"GET /use/" + deviceHeldSecret + "\", as: used }\n"
	for _, kind := range []string{"script", "scenario"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "leak.yml", script)
			writeFile(t, dir, "scenario.yml", leakScenario)
			capture := `{"method":"GET","url":"https://api.example.com/use/` + deviceHeldSecret + `","status":200,"headers":{},"body":{},"timestamp":1}` + "\n"
			writeFile(t, dir, "capture.jsonl", capture)
			flags := map[string]FlagValue{
				"--file":       filepath.Join(dir, "leak.yml"),
				"--proxy":      "file:" + filepath.Join(dir, "capture.jsonl"),
				"--report":     "html,json",
				"--report-dir": filepath.Join(dir, "reports"),
			}
			if kind == "scenario" {
				flags["--file"] = filepath.Join(dir, "scenario.yml")
			}
			var stdout, stderr bytes.Buffer
			deps := stepsDeps(&stdout, &stderr)
			deps.Factory = stagedMockFactory{tree: tree}
			if code := RunScript(context.Background(), deps, flags); code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
			}

			runDir := singleRunDir(t, filepath.Join(dir, "reports"))
			steps := readIfPresent(t, filepath.Join(runDir, runfolder.StepsFileName))
			if !strings.Contains(steps, `"command":"capture","phase":"completed","ok":true`) {
				t.Fatalf("the capture step never completed:\n%s", steps)
			}
			for _, file := range []string{runfolder.StepsFileName, "report/report.html", "report/report.json"} {
				body, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(file)))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(body), deviceHeldSecret) {
					t.Errorf("%s carries the value read from the secure field", file)
				}
			}
		})
	}
}
