package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// noopLogger discards every log entry, so a case's golden captures
// only the runner's own outcome and step events, never incidental
// log output.
type noopLogger struct{}

func (noopLogger) Debug(string, logging.LogContext)        {}
func (noopLogger) Info(string, logging.LogContext)         {}
func (noopLogger) Warn(string, logging.LogContext)         {}
func (noopLogger) Error(string, error, logging.LogContext) {}
func (noopLogger) Child(logging.LogContext) logging.Logger { return noopLogger{} }

// fsFlowLoader resolves a flow-file reference by reading it straight
// off disk and parsing it against env, the plainest possible
// FlowLoader implementation. The differential test's corpus cases
// always name flow files by the absolute path the {{DIR}} placeholder
// resolved to, so no further path resolution is needed here.
type fsFlowLoader struct{}

func (fsFlowLoader) LoadFlow(path string, env map[string]string) (*script.ScriptDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return script.ParseScript(string(data), env)
}

// fakeNetworkCapture serves a fixed, ordered list of requests: see
// corpus/runner/format.md for its exact matching semantics.
type fakeNetworkCapture struct {
	pending []runner.CapturedRequest
}

func newFakeNetworkCapture(reqs []RunnerNetworkRequest) *fakeNetworkCapture {
	pending := make([]runner.CapturedRequest, len(reqs))
	for i, r := range reqs {
		headers := r.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		pending[i] = runner.CapturedRequest{
			Method:      r.Method,
			URL:         r.URL,
			Status:      r.Status,
			Headers:     headers,
			Body:        r.Body,
			TimestampMs: r.TimestampMs,
		}
	}
	return &fakeNetworkCapture{pending: pending}
}

func (f *fakeNetworkCapture) Start(context.Context) error { return nil }
func (f *fakeNetworkCapture) Stop(context.Context) error  { return nil }
func (f *fakeNetworkCapture) All() []runner.CapturedRequest {
	return f.pending
}
func (f *fakeNetworkCapture) Reset() { f.pending = nil }

func (f *fakeNetworkCapture) WaitForRequest(_ context.Context, pattern string, _ int) (runner.CapturedRequest, error) {
	for i, r := range f.pending {
		if matchesCapturePattern(pattern, r.Method, r.URL) {
			req := r
			f.pending = append(f.pending[:i:i], f.pending[i+1:]...)
			return req, nil
		}
	}
	return runner.CapturedRequest{}, fmt.Errorf("No request matching %q was recorded.", pattern)
}

// matchesCapturePattern and globToRegexp implement the split-escape-
// rejoin glob matching documented in corpus/runner/format.md: this is
// test-harness scaffolding, not a production capture adapter, so
// matching that documented semantics is all that is required.
func matchesCapturePattern(pattern, method, url string) bool {
	patMethod, patPath := "*", pattern
	if spaceIdx := strings.IndexByte(pattern, ' '); spaceIdx != -1 {
		patMethod, patPath = pattern[:spaceIdx], pattern[spaceIdx+1:]
	}
	if patMethod != "*" && !strings.EqualFold(patMethod, method) {
		return false
	}
	return globToRegexp(patPath).MatchString(url)
}

func globToRegexp(glob string) *regexp.Regexp {
	parts := strings.Split(glob, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// runnerComparable is the shape the Go runner's outcome is marshaled
// through before diffing against the frozen golden, so no field can
// differ in presence from one case to the next.
type runnerComparable struct {
	ScriptName   string                  `json:"scriptName"`
	Ok           bool                    `json:"ok"`
	Steps        []runnerComparableStep  `json:"steps"`
	TotalSteps   int                     `json:"totalSteps"`
	PassedSteps  int                     `json:"passedSteps"`
	FailedAtStep *int                    `json:"failedAtStep"`
	DurationMs   int64                   `json:"durationMs"`
	Screenshots  []runnerComparableShot  `json:"screenshots"`
	Calls        []runnerComparableCall  `json:"calls"`
	Events       []runnerComparableEvent `json:"events"`
}

type runnerComparableStep struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail"`
	DurationMs int64   `json:"durationMs"`
}

type runnerComparableShot struct {
	Label      string `json:"label"`
	DataLength int    `json:"dataLength"`
}

type runnerComparableCall struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

type runnerComparableToken struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// runnerComparableEvent is one StepEvent flattened for comparison
// against the frozen golden: Ok, Detail, and DurationMs are nil for a
// "stepStarted" event.
type runnerComparableEvent struct {
	Type       string                  `json:"type"`
	StepIndex  int                     `json:"stepIndex"`
	Command    string                  `json:"command"`
	Summary    string                  `json:"summary"`
	Tokens     []runnerComparableToken `json:"tokens"`
	Depth      int                     `json:"depth"`
	Line       *int                    `json:"line"`
	Ok         *bool                   `json:"ok"`
	Detail     *string                 `json:"detail"`
	DurationMs *int64                  `json:"durationMs"`
}

// normalizeCallMethod renders a Go mock.Driver method name (Go's
// exported PascalCase convention, e.g. "LaunchApp") in the TypeScript
// MockDriver's lowerCamelCase convention (e.g. "launchApp"), so the
// diff surfaces a genuine behavioral divergence rather than the two
// languages' own method-naming conventions.
func normalizeCallMethod(m string) string {
	if m == "" {
		return m
	}
	return strings.ToLower(m[:1]) + m[1:]
}

// goComparableEvent flattens one Go runner.StepEvent to match the
// golden's shape: Ok, Detail, and DurationMs are nil for a
// StepStarted event, since runner.StepEvent carries those fields as
// always-present zero values regardless of Type.
func goComparableEvent(e runner.StepEvent) runnerComparableEvent {
	tokens := make([]runnerComparableToken, len(e.Tokens))
	for i, t := range e.Tokens {
		tokens[i] = runnerComparableToken{Kind: string(t.Kind), Text: t.Text}
	}
	out := runnerComparableEvent{
		Type: string(e.Type), StepIndex: e.StepIndex, Command: e.Command, Summary: e.Summary,
		Tokens: tokens, Depth: e.Depth, Line: e.Line,
	}
	if e.Type == runner.StepCompleted {
		ok := e.Ok
		out.Ok = &ok
		out.Detail = e.Detail
		durationMs := e.DurationMs
		out.DurationMs = &durationMs
	}
	return out
}

func goComparable(result runner.ScriptResult, calls []mock.Call, events []runner.StepEvent) (runnerComparable, error) {
	steps := make([]runnerComparableStep, len(result.Steps))
	for i, s := range result.Steps {
		steps[i] = runnerComparableStep{
			StepIndex: s.StepIndex, Command: s.Command, Ok: s.Ok, Detail: s.Detail, DurationMs: s.DurationMs,
		}
	}
	shots := make([]runnerComparableShot, 0, len(result.Artifacts.Screenshots()))
	for _, s := range result.Artifacts.Screenshots() {
		shots = append(shots, runnerComparableShot{Label: s.Label, DataLength: len(s.Data)})
	}
	callsOut := make([]runnerComparableCall, 0, len(calls))
	for i, c := range calls {
		method := normalizeCallMethod(c.Method)
		// The Go runner issues a liveness probe against the driver
		// after a step fails, to classify the failure before deciding
		// whether to abort the run. The recorded golden has no
		// equivalent call, since it never appears in a step's own
		// dispatched calls. Excluding it here keeps this comparison
		// scoped to driver calls a step itself dispatches.
		if method == "ping" {
			continue
		}

		// mock.Call.Args is nil for a call recorded with no variadic
		// arguments (record(method) with nothing after it), which
		// json.Marshal renders as the JSON literal null. The recorded
		// golden always holds an empty array in that case, so
		// normalize nil to "[]" here: this is an artifact of this test
		// harness's own marshaling, not a runner behavior this
		// differential exists to compare.
		args := c.Args
		if args == nil {
			args = []any{}
		}
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return runnerComparable{}, fmt.Errorf("marshaling call %d (%s) args: %w", i, c.Method, err)
		}
		callsOut = append(callsOut, runnerComparableCall{Method: method, Args: argsJSON})
	}
	eventsOut := make([]runnerComparableEvent, len(events))
	for i, e := range events {
		eventsOut[i] = goComparableEvent(e)
	}
	return runnerComparable{
		ScriptName: result.ScriptName, Ok: result.Ok, Steps: steps,
		TotalSteps: result.TotalSteps, PassedSteps: result.PassedSteps, FailedAtStep: result.FailedAtStep,
		DurationMs: result.DurationMs, Screenshots: shots, Calls: callsOut, Events: eventsOut,
	}, nil
}

// TestRunnerCorpusShape proves the corpus loads cleanly and is
// non-empty. LoadRunnerCorpus itself already rejects a malformed case
// file, so this test only needs to confirm the load succeeds, yields
// cases, and every case name is unique.
func TestRunnerCorpusShape(t *testing.T) {
	cases, err := LoadRunnerCorpus("corpus/runner")
	if err != nil {
		t.Fatalf("loading runner corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("runner corpus is empty")
	}

	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		if seen[c.Name] {
			t.Fatalf("duplicate runner corpus case name %q", c.Name)
		}
		seen[c.Name] = true
	}
}

// runGoRunnerCase stages c's driver, network, and flow files under
// dir, runs it through the Go script runner, and returns the parsed
// script, the run result, the driver calls recorded, and the step
// events emitted, for the caller to turn into a golden comparable.
func runGoRunnerCase(t *testing.T, c *RunnerCase, dir string) (result runner.ScriptResult, d *mock.Driver, events []runner.StepEvent, runErr error) {
	t.Helper()

	resolved := c.ResolveDir(dir)
	if err := resolved.WriteFlowFiles(dir); err != nil {
		t.Fatalf("writing flow files: %v", err)
	}

	def, err := script.ParseScript(resolved.Yaml, resolved.Env)
	if err != nil {
		t.Fatalf("Go ParseScript: %v", err)
	}

	d = mock.New()
	if def.AppID != "" {
		d.Apps = []driver.InstalledApp{{BundleID: def.AppID}}
	}
	if resolved.Driver.Capabilities != nil {
		applyCapabilitiesOverride(d, resolved.Driver.Capabilities)
	}
	if resolved.Driver.Screen != nil {
		d.Screen = *resolved.Driver.Screen
	}
	for _, h := range resolved.Driver.Hierarchies {
		d.StageHierarchy(h)
	}

	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})

	var netCapture runner.NetworkCapture
	if resolved.Network != nil {
		netCapture = newFakeNetworkCapture(resolved.Network.Requests)
	}

	r := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:      orch,
		Clock:          clock,
		Logger:         noopLogger{},
		NetworkCapture: netCapture,
		FlowLoader:     fsFlowLoader{},
		Commands:       runner.DefaultCommands(),
		OnStep:         func(e runner.StepEvent) { events = append(events, e) },
	})

	result, runErr = r.Run(context.Background(), def)
	return result, d, events, runErr
}

// tempDirToken replaces a runner case's own temporary directory root
// wherever the Go runner echoes an absolute flow-file path into a
// step's summary, tokens, or detail. Runner cases write their flow
// files under t.TempDir(), whose absolute path is both machine-
// specific and dependent on the ambient TMPDIR (for example macOS's
// "/var/folders/..." versus a CI Linux box's "/tmp/..."), so the
// golden fixture must carry this stable token instead of the literal
// path.
const tempDirToken = "{{TMPDIR}}"

// summaryTokenTruncateMax mirrors internal/runner/step_summary.go's own
// jsTruncate(text, 48) cap on a step summary or token's embedded flow-file
// path. A runner case's temp directory is always longer than that cap, so
// the truncated text that ends up in a golden never reaches past the temp
// directory into the flow-file name: reproducing the same cut here lets
// normalizeTempDir recognize and replace that truncated form too.
const summaryTokenTruncateMax = 48

// truncatedTempDirPrefix returns the same truncated-plus-ellipsis prefix
// of dir that internal/runner/step_summary.go's jsTruncate would produce
// when dir is long enough to be cut, and false otherwise (dir would then
// never appear truncated, so there is nothing extra to replace).
func truncatedTempDirPrefix(dir string) (string, bool) {
	units := utf16.Encode([]rune(dir))
	if len(units) < summaryTokenTruncateMax {
		return "", false
	}
	return string(utf16.Decode(units[:summaryTokenTruncateMax-1])) + "…", true
}

// normalizeTempDir replaces every occurrence of dir, the concrete
// temporary directory a runner case wrote its flow files under, with
// tempDirToken: the literal path, its symlink-resolved form when the OS
// reports one that differs from dir itself (macOS resolves "/var/..." to
// "/private/var/..." for some callers), and the truncated-plus-ellipsis
// prefix a step summary or token renders when the embedded path is longer
// than the runner's own display cap. Only the temp-dir root is touched;
// nothing else in the comparable is normalized here.
func normalizeTempDir(data []byte, dir string) []byte {
	candidates := []string{dir}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != dir {
		candidates = append(candidates, resolved)
	}
	for _, c := range candidates {
		if truncated, ok := truncatedTempDirPrefix(c); ok {
			candidates = append(candidates, truncated)
		}
	}
	for _, c := range candidates {
		data = bytes.ReplaceAll(data, []byte(c), []byte(tempDirToken))
	}
	return data
}

func TestRunnerDifferential(t *testing.T) {
	cases, err := LoadRunnerCorpus("corpus/runner")
	if err != nil {
		t.Fatalf("loading runner corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("runner corpus is empty")
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			result, d, events, runErr := runGoRunnerCase(t, c, dir)

			if c.ExpectRunError {
				// A required flow that cannot be loaded aborts the
				// TypeScript runner's own requires loop with an
				// uncaught exception, so it never produces a
				// ScriptResult for this class of case either; Go's Run
				// mirrors that by returning a non-nil error instead of
				// a graceful ScriptResult. Neither side has a
				// ScriptResult to freeze as a golden, so this case only
				// asserts Go failed to complete the same way.
				if runErr == nil {
					t.Fatalf("%s: Go Run succeeded, want an error (a required flow file cannot be loaded)", c.Description)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("Go Run: %v", runErr)
			}

			goComp, err := goComparable(result, d.Calls, events)
			if err != nil {
				t.Fatalf("building Go comparable: %v", err)
			}
			goJSON, err := json.Marshal(goComp)
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}
			goJSON = normalizeTempDir(goJSON, dir)

			checkGolden(t, filepath.Join("runner", c.Name+".json"), goJSON, DiffRunnerJSON)
		})
	}
}
