package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/wire"
)

const (
	leakSecret = "plant-7c1e9d42-secret"

	// leakScript captures a request whose URL, header and body carry the
	// secret, extracts the header, and asserts against the body with an
	// expected value built from the secret.
	leakScript = "appId: com.example.app\nname: Leak\nsteps:\n" +
		"  - capture: { pattern: \"GET /items/${TOKEN}\", as: req }\n" +
		"  - extract: { from: req, values: { fromHeader: headers.X-Auth } }\n" +
		"  - assertApi: { from: req, body: { token: \"${TOKEN}-mismatch\" } }\n"

	// leakUnparsableScript fails while it is parsed, and the parse error
	// quotes the interpolated value.
	leakUnparsableScript = "appId: com.example.app\nname: Bad\nsteps:\n  - sleep: \"${TOKEN}\"\n"
)

func leakCaptureFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	line := `{"method":"GET","url":"https://api.example.com/items/` + leakSecret + `","status":200,` +
		`"headers":{"X-Auth":"` + leakSecret + `"},"body":{"token":"` + leakSecret + `"},"timestamp":1}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireNoSecret(t *testing.T, name, text string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if strings.Contains(text, leakSecret) {
			t.Errorf("%s carries the planted secret:\n%s", name, text)
		}
	})
}

// runFolderText joins the step stream and every other file under root,
// separately, so a failure names the boundary.
func runFolderText(t *testing.T, root string) (steps, other string) {
	t.Helper()
	var stepsBuf, otherBuf strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if filepath.Base(path) == "steps.jsonl" {
			stepsBuf.Write(body)
			return nil
		}
		otherBuf.WriteString(path + "\n")
		otherBuf.Write(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return stepsBuf.String(), otherBuf.String()
}

func TestPlantedEnvArgumentNeverReachesAWriter(t *testing.T) {
	for _, openRun := range []bool{false, true} {
		name := "own run"
		if openRun {
			name = "open run"
		}
		t.Run(name, func(t *testing.T) {
			var log bytes.Buffer
			r := newRunServerWith(t, func(deps *ServerDeps) {
				deps.Logger = logging.NewStructuredLogger(&log, logging.LevelDebug)
			})
			selectMockDevice(t, r.cs, t.Context())
			if openRun {
				r.call("start_run", map[string]any{"name": "leak"})
			}
			args := map[string]any{
				"script": leakScript,
				"env":    map[string]any{"TOKEN": leakSecret},
				"proxy":  "file:" + leakCaptureFile(t),
			}
			result, _ := callTool(t, r.cs, t.Context(), "run_script", args)
			parsed, _ := callTool(t, r.cs, t.Context(), "run_script", map[string]any{
				"script": leakUnparsableScript,
				"env":    map[string]any{"TOKEN": leakSecret},
			})
			if openRun {
				r.call("finish_run", map[string]any{"verdict": "passed"})
			}

			text := textOf(t, result)
			if !strings.Contains(text, "Captured") && !strings.Contains(text, "capture") {
				t.Fatalf("the script never reached the capture step:\n%s", text)
			}
			steps, other := runFolderText(t, r.root)
			requireNoSecret(t, "run_script result", text)
			requireNoSecret(t, "parse error result", textOf(t, parsed))
			requireNoSecret(t, "step stream", steps)
			requireNoSecret(t, "other run folder files", other)
			requireNoSecret(t, "log output", log.String())
		})
	}
}

// TestRunScriptMasksAValueReadFromASecureField proves run_script passes
// its secret set to the runner. The value is not in the env argument and is
// not token-shaped, so only the runner marking the read secret masks it.
func TestRunScriptMasksAValueReadFromASecureField(t *testing.T) {
	const deviceHeld = "device-held-pw-81f3"
	drv := mockdriver.New()
	drv.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root", "bounds": "0,0,400,800"},
		Children: []wire.TreeNodeWire{{Attributes: map[string]string{
			"id": "pw", "value": deviceHeld, "bounds": "0,0,100,50", wire.AttrSecure: wire.AttrSecureTrue,
		}}},
	})
	r := newRunServerWith(t, func(deps *ServerDeps) { deps.Factory = singleMockDriverFactory{driver: drv} })
	selectMockDevice(t, r.cs, t.Context())
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	line := `{"method":"GET","url":"https://api.example.com/use/` + deviceHeld + `","status":200,"headers":{},"body":{},"timestamp":1}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	result, _ := callTool(t, r.cs, t.Context(), "run_script", map[string]any{
		"script": "appId: com.example.app\nname: Held\nsteps:\n" +
			"  - read: { selector: { id: pw }, field: value, as: pwd }\n" +
			"  - capture: { pattern: \"GET /use/" + deviceHeld + "\", as: used }\n",
		"proxy": "file:" + path,
	})

	text := textOf(t, result)
	if !strings.Contains(text, "Captured") {
		t.Fatalf("the capture step never completed:\n%s", text)
	}
	steps, other := runFolderText(t, r.root)
	for name, body := range map[string]string{"result": text, "step stream": steps, "run folder": other} {
		if strings.Contains(body, deviceHeld) {
			t.Errorf("%s carries the value read from the secure field", name)
		}
	}
}
