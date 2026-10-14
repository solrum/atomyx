package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/shard"
)

const (
	leakSecret    = "plant-7c1e9d42-secret"
	leakEnvVar    = "ATOMYX_LEAK_SECRET"
	leakLiteralID = "com.example.literalapp"
)

// leakCapture is the single request the fake proxy file holds. The
// secret sits in the URL, in a response header and in the response body,
// the three places a captured request carries a value the script
// interpolated.
func leakCapture(secret string) string {
	return fmt.Sprintf(`{"method":"GET","url":"https://api.example.com/items/%[1]s","status":200,`+
		`"headers":{"X-Auth":"%[1]s"},"body":{"token":"%[1]s"},"timestamp":1}`+"\n"+
		`{"method":"GET","url":"https://api.example.com/apps/%[2]s","status":200,"headers":{},"body":{},"timestamp":2}`+"\n",
		secret, leakLiteralID)
}

// leakScript uses TOKEN in a capture URL pattern, an extract of the
// response header, and an assertion expected value that fails, so the
// failure text carries the response body value too.
const leakScript = "appId: com.example.app\n---\n" +
	"- capture: { pattern: \"GET /items/${TOKEN}\", as: req }\n" +
	"- screenshot: \"shot-${TOKEN}\"\n" +
	"- extract: { from: req, values: { fromHeader: headers.X-Auth } }\n" +
	"- assertApi: { from: req, body: { token: \"${TOKEN}-mismatch\" } }\n"

// leakLogScript types the secret on a device that rejects the text with an
// error quoting it. The command layer logs that error as a handler panic,
// so the secret reaches the log boundary through the run's own logging.
const leakLogScript = "appId: com.example.app\n---\n" +
	"- capture: { pattern: \"GET /items/${TOKEN}\", as: req }\n" +
	"- type: { text: \"${TOKEN}\" }\n"

const panicLogMessage = "command handler panicked"

// echoingDriver rejects typed text with an error that quotes it.
type echoingDriver struct{ driver.Driver }

func (echoingDriver) InputText(_ context.Context, text string) error {
	return fmt.Errorf("the device rejected the text %q", text)
}

func echoingFactory() device.WrappingFactory {
	wrapped := sessiontest.Wrapping(stagedMockFactory{tree: focusedInputTree()}).WithDriverWrap(func(inner driver.Driver) driver.Driver {
		return echoingDriver{Driver: inner}
	})
	return sessiontest.Wrapping(wrapped)
}

// leakLiteralScript holds an app id in a literal env entry and puts it in
// a capture URL, so the id reaches step detail and the reports.
const leakLiteralScript = "env:\n  APP: " + leakLiteralID + "\n---\n" +
	"- capture: { pattern: \"GET /apps/${APP}\", as: app }\n"

// leakUnparsableScript fails while it is parsed, and the parse error
// quotes the interpolated value. A suite records that error on the entry.
const leakUnparsableScript = "- sleep: \"${TOKEN}\"\n"

const leakScenario = "name: Leak\nscripts:\n  - leak.yml\n"

// leakSource names where a run receives the planted secret from.
type leakSource struct {
	name  string
	flags func(t *testing.T, dir string) map[string]FlagValue
}

func leakSources() []leakSource {
	return []leakSource{
		{"env reference in config", func(t *testing.T, dir string) map[string]FlagValue {
			t.Setenv(leakEnvVar, leakSecret)
			config := "project: Demo\napps:\n  - id: main\n    platform: android\n    appId: com.example.app\n" +
				"environments:\n  staging:\n    app: main\n    vars:\n      TOKEN: \"${ENV:" + leakEnvVar + "}\"\n"
			writeFile(t, dir, "atomyx.config.yaml", config)
			return map[string]FlagValue{"--config": filepath.Join(dir, "atomyx.config.yaml"), "--env": "staging"}
		}},
		{"variable flag", func(t *testing.T, dir string) map[string]FlagValue {
			return map[string]FlagValue{"--var": []string{"TOKEN=" + leakSecret}}
		}},
		{"variable file", func(t *testing.T, dir string) map[string]FlagValue {
			writeFile(t, dir, "vars.json", `{"TOKEN":"`+leakSecret+`"}`)
			return map[string]FlagValue{"--var-file": filepath.Join(dir, "vars.json")}
		}},
	}
}

// leakRun is what one host wrote while running the leak scripts.
type leakRun struct {
	runDir string
	log    string
	lines  string
	stdout string
	stderr string
}

func leakDeps(stdout, stderr *bytes.Buffer, log *bytes.Buffer) Deps {
	deps := stepsDeps(stdout, stderr)
	deps.Factory = echoingFactory()
	deps.Logger = logging.NewStructuredLogger(log, logging.LevelDebug)
	return deps
}

func prepareLeakFiles(t *testing.T, script, secret string) (dir string, flags map[string]FlagValue) {
	t.Helper()
	dir = t.TempDir()
	writeFile(t, dir, "capture.jsonl", leakCapture(secret))
	writeFile(t, dir, "leak.yml", script)
	writeFile(t, dir, "bad.yml", leakUnparsableScript)
	writeFile(t, dir, "log.yml", leakLogScript)
	writeFile(t, dir, "scenario.yml", leakScenario)
	return dir, map[string]FlagValue{
		"--proxy":      "file:" + filepath.Join(dir, "capture.jsonl"),
		"--report":     "html,json,junit",
		"--report-dir": filepath.Join(dir, "reports"),
	}
}

// feedCapture appends the capture line to the proxy file until the test
// ends. A suite resets its capture before each entry and only reads
// lines written after that, so a file written once would never match.
func feedCapture(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "capture.jsonl")
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600); err == nil {
				_, _ = f.WriteString(leakCapture(leakSecret))
				_ = f.Close()
			}
			select {
			case <-done:
				return
			case <-time.After(captureFeedInterval):
			}
		}
	}()
	t.Cleanup(func() {
		close(done)
		<-finished
	})
}

const captureFeedInterval = 50 * time.Millisecond

func mergeFlags(base, extra map[string]FlagValue) map[string]FlagValue {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func runLeakScript(t *testing.T, source leakSource, kind, script string, jsonOutput bool) leakRun {
	t.Helper()
	dir, flags := prepareLeakFiles(t, script, leakSecret)
	mergeFlags(flags, source.flags(t, dir))
	flags["--file"] = filepath.Join(dir, "leak.yml")
	if kind == "scenario" {
		flags["--file"] = filepath.Join(dir, "scenario.yml")
	}
	flags["--json"] = jsonOutput
	var stdout, stderr, log bytes.Buffer
	RunScript(context.Background(), leakDeps(&stdout, &stderr, &log), flags)
	return leakRun{runDir: singleRunDir(t, filepath.Join(dir, "reports")), log: log.String(), stdout: stdout.String(), stderr: stderr.String()}
}

func runLeakSuite(t *testing.T, source leakSource, jsonOutput bool) leakRun {
	t.Helper()
	dir, flags := prepareLeakFiles(t, leakScript, leakSecret)
	mergeFlags(flags, source.flags(t, dir))
	t.Chdir(dir)
	feedCapture(t, dir)
	flags["--glob"] = []string{"leak.yml", "bad.yml", "log.yml"}
	flags["--json"] = jsonOutput
	var stdout, stderr, log bytes.Buffer
	RunSuite(context.Background(), leakDeps(&stdout, &stderr, &log), flags, nil)
	return leakRun{runDir: singleRunDir(t, filepath.Join(dir, "reports")), log: log.String(), stdout: stdout.String(), stderr: stderr.String()}
}

func runLeakWorker(t *testing.T, source leakSource) leakRun {
	t.Helper()
	dir, flags := prepareLeakFiles(t, leakScript, leakSecret)
	mergeFlags(flags, source.flags(t, dir))
	feedCapture(t, dir)
	flags["--worker"] = true
	flags["--device"] = "emulator-5556"
	var stdout, stderr, log bytes.Buffer
	deps := leakDeps(&stdout, &stderr, &log)
	deps.Stdin = strings.NewReader(filepath.Join(dir, "bad.yml") + "\n" + filepath.Join(dir, "leak.yml") + "\n" + filepath.Join(dir, "log.yml") + "\n")
	RunSuite(context.Background(), deps, flags, nil)
	if _, err := shard.DecodeLine([]byte(strings.SplitN(stdout.String(), "\n", 2)[0])); err != nil {
		t.Fatalf("worker stdout is not a protocol stream: %v\n%s", err, stdout.String())
	}
	return leakRun{runDir: singleRunDir(t, filepath.Join(dir, "reports")), log: log.String(), lines: stdout.String(), stderr: stderr.String()}
}

// boundaryFiles groups the files of a run folder by the writer that
// produced them, so a leak names the boundary that failed.
var boundaryFiles = []struct {
	name string
	path string
}{
	{"step stream", runfolder.StepsFileName},
	{"html report", "report/report.html"},
	{"json report", "report/report.json"},
	{"junit report", "report/junit.xml"},
	{"suite index", runfolder.IndexFileName},
}

func readIfPresent(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// otherRunFolderText joins every file under runDir that no named
// boundary covers.
func otherRunFolderText(t *testing.T, runDir string) string {
	t.Helper()
	named := map[string]bool{}
	for _, b := range boundaryFiles {
		named[filepath.Join(runDir, filepath.FromSlash(b.path))] = true
	}
	var all strings.Builder
	err := filepath.WalkDir(runDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || named[path] {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		all.WriteString(path + "\n")
		all.Write(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.String()
}

// runFolderFileNames lists every path under runDir, relative to it, so a
// secret interpolated into an artifact name is found even when the file
// body is clean.
func runFolderFileNames(t *testing.T, runDir string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(runDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(runDir, path)
		if relErr != nil {
			return relErr
		}
		names = append(names, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(names, "\n")
}

// requireNoFragment fails when text holds the leading part of the planted
// secret. A file name is cut to a fixed length, so the whole secret may
// never fit while a prefix of it still gives the value away.
func requireNoFragment(t *testing.T, name, text string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if strings.Contains(text, leakSecret[:leakFragmentLen]) {
			t.Errorf("%s carries the start of the planted secret:\n%s", name, text)
		}
	})
}

const leakFragmentLen = 12

func requireNoSecret(t *testing.T, name, text string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if strings.Contains(text, leakSecret) {
			t.Errorf("%s carries the planted secret:\n%s", name, text)
		}
	})
}

// requireCaptured fails when the run never reached the step that holds
// the secret, which would make every leak check below pass vacuously.
func requireCaptured(t *testing.T, run leakRun) {
	t.Helper()
	steps := readIfPresent(t, filepath.Join(run.runDir, runfolder.StepsFileName))
	if !strings.Contains(steps, `"command":"capture","phase":"completed","ok":true`) {
		t.Fatalf("the capture step did not complete, so the run never held the secret:\n%s", steps)
	}
}

// requireLeakFreeRun fails when the planted secret appears in any output of
// run. wantPanicLog also requires the run to have logged a handler panic
// that quoted the secret, which proves the log check can fail.
func requireLeakFreeRun(t *testing.T, run leakRun, wantPanicLog bool) {
	t.Helper()
	requireCaptured(t, run)
	if wantPanicLog && !strings.Contains(run.log, panicLogMessage) {
		t.Fatalf("the run never logged a handler panic, so the log check passes vacuously:\n%s", run.log)
	}
	requireNoFragment(t, "run folder file names", runFolderFileNames(t, run.runDir))
	requireNoSecret(t, "standard output", run.stdout)
	requireNoSecret(t, "standard error", run.stderr)
	for _, b := range boundaryFiles {
		requireNoSecret(t, b.name, readIfPresent(t, filepath.Join(run.runDir, filepath.FromSlash(b.path))))
	}
	requireNoSecret(t, "other run folder files", otherRunFolderText(t, run.runDir))
	requireNoSecret(t, "log output", run.log)
	if run.lines != "" {
		requireNoSecret(t, "worker lines", run.lines)
	}
}

func TestPlantedSecretNeverReachesAWriter(t *testing.T) {
	for _, source := range leakSources() {
		t.Run(source.name, func(t *testing.T) {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("json=%t", jsonOutput), func(t *testing.T) {
					t.Run("script", func(t *testing.T) {
						requireLeakFreeRun(t, runLeakScript(t, source, "script", leakScript, jsonOutput), false)
					})
					t.Run("script log", func(t *testing.T) {
						requireLeakFreeRun(t, runLeakScript(t, source, "script", leakLogScript, jsonOutput), true)
					})
					t.Run("scenario", func(t *testing.T) {
						requireLeakFreeRun(t, runLeakScript(t, source, "scenario", leakScript, jsonOutput), false)
					})
					t.Run("suite", func(t *testing.T) { requireLeakFreeRun(t, runLeakSuite(t, source, jsonOutput), true) })
				})
			}
			t.Run("sharded worker", func(t *testing.T) { requireLeakFreeRun(t, runLeakWorker(t, source), true) })
		})
	}
}

// TestParseErrorNeverQuotesAnExternalValue runs a script whose parse error
// quotes the interpolated secret, before any run folder exists.
func TestParseErrorNeverQuotesAnExternalValue(t *testing.T) {
	for _, source := range leakSources() {
		t.Run(source.name, func(t *testing.T) {
			dir, flags := prepareLeakFiles(t, leakScript, leakSecret)
			mergeFlags(flags, source.flags(t, dir))
			flags["--file"] = filepath.Join(dir, "bad.yml")
			var stdout, stderr, log bytes.Buffer
			if code := RunScript(context.Background(), leakDeps(&stdout, &stderr, &log), flags); code != 1 {
				t.Fatalf("exit code %d, want 1, stderr: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "error [") {
				t.Fatalf("the parse failure was not reported, so the check is vacuous: %q", stderr.String())
			}
			requireNoSecret(t, "standard error", stderr.String())
			requireNoSecret(t, "standard output", stdout.String())
		})
	}
}

// leakMixedConfig holds a var that mixes a literal with an environment
// reference, so the secret the environment supplies is only part of the
// var's value.
func leakMixedConfig() string {
	return "project: Demo\napps:\n  - id: main\n    platform: android\n    appId: com.example.app\n" +
		"environments:\n  staging:\n    app: main\n    vars:\n      AUTH: \"Bearer ${ENV:" + leakEnvVar + "}\"\n"
}

// TestMixedEnvReferenceMasksTheResolvedValueAlone fails a response body
// assertion, whose failure text quotes the body value the environment
// supplied, without the literal part that sits next to it in the var.
func TestMixedEnvReferenceMasksTheResolvedValueAlone(t *testing.T) {
	t.Setenv(leakEnvVar, leakSecret)
	script := "appId: com.example.app\n---\n" +
		"- capture: { pattern: \"GET /items/*\", as: req }\n" +
		"- assertApi: { from: req, body: { token: \"wrong\" } }\n"
	dir, flags := prepareLeakFiles(t, script, leakSecret)
	writeFile(t, dir, "atomyx.config.yaml", leakMixedConfig())
	flags["--config"] = filepath.Join(dir, "atomyx.config.yaml")
	flags["--env"] = "staging"
	flags["--file"] = filepath.Join(dir, "leak.yml")
	var stdout, stderr, log bytes.Buffer
	RunScript(context.Background(), leakDeps(&stdout, &stderr, &log), flags)
	run := leakRun{runDir: singleRunDir(t, filepath.Join(dir, "reports")), log: log.String(), stdout: stdout.String(), stderr: stderr.String()}
	requireLeakFreeRun(t, run, false)
}

func TestLiteralEnvEntryStaysReadable(t *testing.T) {
	dir, flags := prepareLeakFiles(t, leakLiteralScript, leakSecret)
	flags["--file"] = filepath.Join(dir, "leak.yml")
	var stdout, stderr, log bytes.Buffer
	if code := RunScript(context.Background(), leakDeps(&stdout, &stderr, &log), flags); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	runDir := singleRunDir(t, filepath.Join(dir, "reports"))
	for _, b := range boundaryFiles[:3] {
		t.Run(b.name, func(t *testing.T) {
			if body := readIfPresent(t, filepath.Join(runDir, filepath.FromSlash(b.path))); !strings.Contains(body, leakLiteralID) {
				t.Errorf("%s hides the literal app id:\n%s", b.name, body)
			}
		})
	}
}
