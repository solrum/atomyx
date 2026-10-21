package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

// noopLogger discards every log entry, matching a host that has not
// wired a real logging sink for a test run.
type noopLogger struct{}

func (noopLogger) Debug(string, logging.LogContext)        {}
func (noopLogger) Info(string, logging.LogContext)         {}
func (noopLogger) Warn(string, logging.LogContext)         {}
func (noopLogger) Error(string, error, logging.LogContext) {}
func (noopLogger) Child(logging.LogContext) logging.Logger { return noopLogger{} }

// capturingLogger records every Error and Debug call it receives,
// merging its own Child-derived context onto each entry the same way a
// real logging.Logger implementation does, so a test can assert a
// recovered panic was logged at the right severity instead of merely
// swallowed, that the stack trace landed on the debug entry rather
// than the error entry, and that a field carried through Child (for
// example "runId") reached the logged entry rather than being dropped.
type capturingLogger struct {
	ctx    logging.LogContext
	errors *[]capturedError
	debugs *[]capturedError
}

func newCapturingLogger() *capturingLogger {
	return &capturingLogger{errors: &[]capturedError{}, debugs: &[]capturedError{}}
}

type capturedError struct {
	msg string
	err error
	ctx logging.LogContext
}

func (c *capturingLogger) Debug(msg string, ctx logging.LogContext) {
	*c.debugs = append(*c.debugs, capturedError{msg: msg, ctx: mergeLogContext(c.ctx, ctx)})
}
func (c *capturingLogger) Info(string, logging.LogContext) {}
func (c *capturingLogger) Warn(string, logging.LogContext) {}
func (c *capturingLogger) Error(msg string, err error, ctx logging.LogContext) {
	*c.errors = append(*c.errors, capturedError{msg: msg, err: err, ctx: mergeLogContext(c.ctx, ctx)})
}
func (c *capturingLogger) Child(ctx logging.LogContext) logging.Logger {
	return &capturingLogger{ctx: mergeLogContext(c.ctx, ctx), errors: c.errors, debugs: c.debugs}
}

// recordedLog is one Info call a recordingLogger captured, with its
// own context merged over whatever context the Child chain that
// produced the logger carried.
type recordedLog struct {
	msg string
	ctx logging.LogContext
}

// recordingLogger records every Info call, merging Child's context
// down the chain the way a real logging.Logger implementation does,
// so a test can assert a specific field (for example "runId") reached
// a logged entry through Child rather than being dropped.
type recordingLogger struct {
	ctx  logging.LogContext
	logs *[]recordedLog
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{logs: &[]recordedLog{}}
}

func (r *recordingLogger) Debug(string, logging.LogContext) {}
func (r *recordingLogger) Info(msg string, ctx logging.LogContext) {
	*r.logs = append(*r.logs, recordedLog{msg: msg, ctx: mergeLogContext(r.ctx, ctx)})
}
func (r *recordingLogger) Warn(string, logging.LogContext)         {}
func (r *recordingLogger) Error(string, error, logging.LogContext) {}
func (r *recordingLogger) Child(ctx logging.LogContext) logging.Logger {
	return &recordingLogger{ctx: mergeLogContext(r.ctx, ctx), logs: r.logs}
}

func mergeLogContext(base, overlay logging.LogContext) logging.LogContext {
	merged := make(logging.LogContext, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

// fakeFlowLoader resolves a fixed set of paths to inline YAML source,
// parsing on every call the way a filesystem-backed loader would.
type fakeFlowLoader struct {
	flows map[string]string
	calls map[string]int
}

func newFakeFlowLoader() *fakeFlowLoader {
	return &fakeFlowLoader{flows: map[string]string{}, calls: map[string]int{}}
}

func (f *fakeFlowLoader) stage(path, yaml string) *fakeFlowLoader {
	f.flows[path] = yaml
	return f
}

func (f *fakeFlowLoader) LoadFlow(path string, env map[string]string) (*script.ScriptDefinition, error) {
	f.calls[path]++
	yaml, ok := f.flows[path]
	if !ok {
		return nil, fmt.Errorf("no flow staged for %q", path)
	}
	return script.ParseScript(yaml, env, script.WithoutRunTimeResolution())
}

// fakeNetworkCapture serves a queue of pre-staged captured requests in
// order, one per WaitForRequest call.
type fakeNetworkCapture struct {
	queue []CapturedRequest
}

func (f *fakeNetworkCapture) Start(context.Context) error { return nil }
func (f *fakeNetworkCapture) Stop(context.Context) error  { return nil }
func (f *fakeNetworkCapture) All() []CapturedRequest      { return nil }
func (f *fakeNetworkCapture) Reset()                      { f.queue = nil }

func (f *fakeNetworkCapture) WaitForRequest(_ context.Context, pattern string, _ int) (CapturedRequest, error) {
	if len(f.queue) == 0 {
		return CapturedRequest{}, fmt.Errorf("no request staged for pattern %q", pattern)
	}
	req := f.queue[0]
	f.queue = f.queue[1:]
	return req, nil
}

// panicCommand always panics with a fixed error, used to exercise the
// runner's dispatch panic recovery independent of any real command.
// It claims the "back" command name so a test can trigger it through
// an ordinary "- back" step: newTestRunner registers extra commands
// after the default registry, so this overrides BackCommand in the
// map NewScriptRunner builds.
type panicCommand struct{}

func (panicCommand) Command() string { return script.CommandBack }
func (panicCommand) Execute(script.ScriptStep, *CommandContext) CommandResult {
	panic(errors.New("boom"))
}

// cancelingFailureCommand cancels the run's own context before
// failing its step, modeling a step that fails at the exact moment a
// caller stops the run or a run-level timeout elapses. It claims the
// "back" command name the same way panicCommand does.
type cancelingFailureCommand struct {
	cancel context.CancelFunc
}

func (c cancelingFailureCommand) Command() string { return script.CommandBack }
func (c cancelingFailureCommand) Execute(script.ScriptStep, *CommandContext) CommandResult {
	c.cancel()
	return CommandResult{Ok: false, Detail: strPtr("step failed because the run was canceled")}
}

// testRunner bundles a ScriptRunner with the fakes a test wants to
// inspect afterward.
type testRunner struct {
	runner  *ScriptRunner
	driver  *mock.Driver
	clock   *orchestra.FakeClock
	orch    *orchestra.Orchestra
	capture *fakeNetworkCapture
	loader  *fakeFlowLoader
}

func newTestRunner(extraCommands ...CommandDefinition) *testRunner {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	capture := &fakeNetworkCapture{}
	loader := newFakeFlowLoader()

	commands := append(append([]CommandDefinition{}, DefaultCommands()...), extraCommands...)

	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra:      orch,
		Clock:          clock,
		Logger:         noopLogger{},
		NetworkCapture: capture,
		FlowLoader:     loader,
		Commands:       commands,
	})
	return &testRunner{runner: r, driver: d, clock: clock, orch: orch, capture: capture, loader: loader}
}

func mustParse(t *testing.T, yaml string) *script.ScriptDefinition {
	t.Helper()
	def, err := script.ParseScript(yaml, nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	return def
}

func run(t *testing.T, tr *testRunner, yaml string) ScriptResult {
	t.Helper()
	return runWithEnv(t, tr, yaml, nil)
}

// runWithEnv runs yaml with env as the external variables, which are
// secret, unlike the literals the script's own env block declares.
func runWithEnv(t *testing.T, tr *testRunner, yaml string, env map[string]string) ScriptResult {
	t.Helper()
	def, err := script.ParseScript(yaml, env)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func lastStep(t *testing.T, result ScriptResult) StepResult {
	t.Helper()
	if len(result.Steps) == 0 {
		t.Fatalf("no steps recorded")
	}
	return result.Steps[len(result.Steps)-1]
}

func detail(s StepResult) string {
	if s.Detail == nil {
		return ""
	}
	return *s.Detail
}

// debugSteps renders a ScriptResult's steps with their Detail pointer
// dereferenced, for a test failure message that shows the actual
// failure reason instead of a pointer address.
func debugSteps(steps []StepResult) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = fmt.Sprintf("{StepIndex:%d Command:%s Ok:%t Detail:%q DurationMs:%d}",
			s.StepIndex, s.Command, s.Ok, detail(s), s.DurationMs)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func TestScriptRunner_Run_SequentialSuccess(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n- screenshot: first\n")

	if !result.Ok {
		t.Fatalf("got Ok=false, steps=%s", debugSteps(result.Steps))
	}
	if result.PassedSteps != 2 || result.TotalSteps != 2 {
		t.Fatalf("got passed=%d total=%d, want 2/2", result.PassedSteps, result.TotalSteps)
	}
	if result.FailedAtStep != nil {
		t.Fatalf("got FailedAtStep=%v, want nil", *result.FailedAtStep)
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "first" {
		t.Fatalf("got screenshots %+v, want one labeled \"first\"", shots)
	}
}

func TestScriptRunner_Run_EmitsStepTraceLogCarryingRunID(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	logger := newRecordingLogger()
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra: orch,
		Clock:     clock,
		Logger:    logger,
		RunID:     "run-abc123",
		Commands:  DefaultCommands(),
	})
	d.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	def := mustParse(t, "appId: com.test\n---\n- launchApp\n")
	if _, err := r.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(*logger.logs) == 0 {
		t.Fatalf("got no step trace log entries, want at least one")
	}
	entry := (*logger.logs)[0]
	if entry.msg == "" {
		t.Fatalf("got an empty log message for the step trace entry")
	}
	if entry.ctx["runId"] != "run-abc123" {
		t.Fatalf("got runId=%v, want %q carried from ScriptRunnerDeps.RunID via Child", entry.ctx["runId"], "run-abc123")
	}
	if entry.ctx["command"] != "launchApp" {
		t.Fatalf("got command=%v, want %q", entry.ctx["command"], "launchApp")
	}
	if entry.ctx["ok"] != true {
		t.Fatalf("got ok=%v, want true", entry.ctx["ok"])
	}
}

func TestScriptRunner_Run_PreconditionAlwaysPolicyFailsWhenCheckStillFailsAfterSetup(t *testing.T) {
	tr := newTestRunner()
	// No node exposes text or label "Home", so the check can never
	// hold no matter when it runs, modeling setup that silently fails
	// to establish the precondition it is supposed to guarantee.
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	yaml := "appId: com.test\n" +
		"preconditions:\n" +
		"  - check: { visible: \"Home\" }\n" +
		"    setup:\n" +
		"      - sleep: 0\n" +
		"    policy: always\n" +
		"---\n" +
		"- launchApp\n"

	result := run(t, tr, yaml)

	if result.Ok {
		t.Fatalf("got Ok=true, want false: an always-policy precondition whose setup did not satisfy its check must fail the step, steps=%s", debugSteps(result.Steps))
	}
	if len(result.Steps) != 1 || result.Steps[0].Command != "preconditions" {
		t.Fatalf("got steps=%s, want a single failed \"preconditions\" step", debugSteps(result.Steps))
	}
	if !strings.Contains(detail(result.Steps[0]), "Precondition not met after setup") {
		t.Fatalf("got detail=%q, want it to report the precondition still unmet after setup", detail(result.Steps[0]))
	}
}

func TestScriptRunner_Run_ProxyRequiredFailsImmediatelyWithNilCapture(t *testing.T) {
	d := mock.New()
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra: orch,
		Clock:     clock,
		Logger:    noopLogger{},
		Commands:  DefaultCommands(),
	})

	def := mustParse(t, "appId: com.test\nproxy: required\n---\n- screenshot: never\n- screenshot: never\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatal("got Ok=true, want false: proxy is required but no capture is configured")
	}
	if len(result.Steps) != 0 {
		t.Fatalf("got %d steps executed, want 0", len(result.Steps))
	}
	if result.TotalSteps != 2 || result.PassedSteps != 0 {
		t.Fatalf("got totalSteps=%d passedSteps=%d, want 2 and 0", result.TotalSteps, result.PassedSteps)
	}
	assertProxyRequiredAbort(t, result)
}

// assertProxyRequiredAbort asserts result carries the structured
// refusal a `proxy: required` script reports when no network capture
// adapter is configured: a non-nil Abort naming the stable diagnostic
// code, an operator-facing message, and a remedy pointing at both
// surfaces that can supply an adapter, without leaking any route,
// host, or port text.
func assertProxyRequiredAbort(t *testing.T, result ScriptResult) {
	t.Helper()
	if result.Abort == nil {
		t.Fatal("got Abort=nil, want a structured proxy-required abort")
	}
	if result.Abort.Reason != RunAbortProxyRequired {
		t.Errorf("got Abort.Reason=%q, want %q", result.Abort.Reason, RunAbortProxyRequired)
	}
	if result.Abort.Code != "PROXY_REQUIRED" {
		t.Errorf("got Abort.Code=%q, want %q", result.Abort.Code, "PROXY_REQUIRED")
	}
	if result.Abort.Message != "This script needs network capture, but no proxy is configured." {
		t.Errorf("got Abort.Message=%q", result.Abort.Message)
	}
	if !strings.Contains(result.Abort.Remedy, "--proxy") || !strings.Contains(result.Abort.Remedy, "run_script") {
		t.Errorf("got Abort.Remedy=%q, want it to name both the --proxy flag and the run_script tool", result.Abort.Remedy)
	}
	for _, leak := range []string{"http://", "localhost", "8889", "8765", ":22087"} {
		if strings.Contains(result.Abort.Message, leak) || strings.Contains(result.Abort.Remedy, leak) {
			t.Errorf("Abort text leaks route or transport detail %q: message=%q remedy=%q", leak, result.Abort.Message, result.Abort.Remedy)
		}
	}
}

func TestScriptRunner_Run_ProxyRequiredFailsImmediatelyWithDefaultNoOpCapture(t *testing.T) {
	d := mock.New()
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra:      orch,
		Clock:          clock,
		Logger:         noopLogger{},
		Commands:       DefaultCommands(),
		NetworkCapture: alwaysNoCapture{},
	})

	def := mustParse(t, "appId: com.test\nproxy: required\n---\n- screenshot: never\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatal("got Ok=true, want false: a wired no-op capture adapter must not satisfy proxy: required")
	}
	if len(result.Steps) != 0 {
		t.Fatalf("got %d steps executed, want 0", len(result.Steps))
	}
	assertProxyRequiredAbort(t, result)
}

func TestScriptRunner_Run_ProxyRequiredPassesWithRealCapture(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result := run(t, tr, "appId: com.test\nproxy: required\n---\n- screenshot: first\n")
	if !result.Ok {
		t.Fatalf("got Ok=false, steps=%s", debugSteps(result.Steps))
	}
}

func TestScriptRunner_Run_FailsFastOnFirstFailure(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result := run(t, tr, "appId: com.test\n---\n"+
		"- assertVisible: \"Does not exist\"\n"+
		"- screenshot: never\n")

	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if result.FailedAtStep == nil || *result.FailedAtStep != 0 {
		t.Fatalf("got FailedAtStep=%v, want 0", result.FailedAtStep)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("got %d steps recorded, want 1 (fail fast)", len(result.Steps))
	}
	for _, c := range tr.driver.Calls {
		if c.Method == "Screenshot" {
			t.Fatalf("screenshot step ran after failure: %+v", tr.driver.Calls)
		}
	}
}

func TestScriptRunner_Run_UnknownCommandFails(t *testing.T) {
	// A registry with no commands at all: dispatch must fail with
	// "Unknown command" rather than panicking on a nil map lookup.
	d := mock.New()
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	r := NewScriptRunner(ScriptRunnerDeps{Orchestra: orch, Clock: clock, Logger: noopLogger{}})

	def := mustParse(t, "appId: com.test\n---\n- back\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := `Unknown command: "back"`
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestScriptRunner_Run_PanicRecoveredAsException(t *testing.T) {
	tr := newTestRunner(panicCommand{})

	result := run(t, tr, "appId: com.test\n---\n- back\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "Exception: boom"
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestScriptRunner_Run_PanicRecoveredAsException_LogsTheRecoveredValue(t *testing.T) {
	d := mock.New()
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	logger := newCapturingLogger()
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra: orch,
		Clock:     clock,
		Logger:    logger,
		Commands:  append(append([]CommandDefinition{}, DefaultCommands()...), panicCommand{}),
	})

	def := mustParse(t, "appId: com.test\n---\n- back\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}

	if len(*logger.errors) != 1 {
		t.Fatalf("logged errors = %+v, want exactly one", *logger.errors)
	}
	errEntry := (*logger.errors)[0]
	if !strings.Contains(fmt.Sprint(errEntry.ctx["panic"]), "boom") {
		t.Fatalf("logged panic value = %v, want it to contain %q", errEntry.ctx["panic"], "boom")
	}
	if _, hasStack := errEntry.ctx["stack"]; hasStack {
		t.Fatalf("error-level entry carries a stack trace, want it kept out of default-visible output: %+v", errEntry.ctx)
	}

	if len(*logger.debugs) != 1 {
		t.Fatalf("logged debug entries = %+v, want exactly one", *logger.debugs)
	}
	debugEntry := (*logger.debugs)[0]
	if !strings.Contains(fmt.Sprint(debugEntry.ctx["panic"]), "boom") {
		t.Fatalf("logged panic value = %v, want it to contain %q", debugEntry.ctx["panic"], "boom")
	}
	if stack, _ := debugEntry.ctx["stack"].(string); stack == "" {
		t.Fatalf("logged stack trace is empty, want a non-empty stack trace on the debug entry")
	}
}

// TestEmitStep_HookPanic_LogsTheRecoveredValueInsteadOfDiscardingIt
// guards the panic-recovery log line itself: it must log through
// stepLog, the same run-correlated logger every other step log line
// uses, not the bare, uncorrelated deps.Logger. Constructing the
// ScriptRunner via NewScriptRunner with a RunID set is what makes
// stepLog carry that correlation in the first place.
func TestEmitStep_HookPanic_LogsTheRecoveredValueInsteadOfDiscardingIt(t *testing.T) {
	logger := newCapturingLogger()
	r := NewScriptRunner(ScriptRunnerDeps{
		Logger: logger,
		RunID:  "run-panic-1",
		OnStep: func(StepEvent) { panic("hook exploded") },
	})

	r.emitStep(StepEvent{Type: StepStarted, Command: "back"})

	if len(*logger.errors) != 1 {
		t.Fatalf("logged errors = %+v, want exactly one", *logger.errors)
	}
	entry := (*logger.errors)[0]
	if !strings.Contains(fmt.Sprint(entry.ctx["panic"]), "hook exploded") {
		t.Fatalf("logged panic value = %v, want it to contain %q", entry.ctx["panic"], "hook exploded")
	}
	if entry.ctx["runId"] != "run-panic-1" {
		t.Fatalf("logged panic entry runId = %v, want %q: the panic log must carry the same run correlation every other step log line does", entry.ctx["runId"], "run-panic-1")
	}
}

func TestScriptRunner_Run_RequiredFlowRunsAndDedups(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	reqPath := "flows/setup.yml"
	absPath, _ := filepath.Abs(reqPath)
	tr.loader.stage(absPath, "- screenshot: from-required\n")

	yaml := fmt.Sprintf("appId: com.test\nrequires:\n  - %s\n---\n- screenshot: main\n", reqPath)

	first := run(t, tr, yaml)
	if !first.Ok {
		t.Fatalf("first run: got Ok=false, steps=%s", debugSteps(first.Steps))
	}
	shots := first.Artifacts.Screenshots()
	if len(shots) != 2 || shots[0].Label != "from-required" || shots[1].Label != "main" {
		t.Fatalf("got screenshots %+v, want [from-required, main]", shots)
	}

	if got := tr.loader.calls[absPath]; got != 1 {
		t.Fatalf("got %d LoadFlow calls after first run, want 1", got)
	}

	second := run(t, tr, yaml)
	if !second.Ok {
		t.Fatalf("second run: got Ok=false, steps=%s", debugSteps(second.Steps))
	}
	if got := tr.loader.calls[absPath]; got != 1 {
		t.Fatalf("got %d LoadFlow calls after second run, want 1 (deduped)", got)
	}
	secondShots := second.Artifacts.Screenshots()
	if len(secondShots) != 1 || secondShots[0].Label != "main" {
		t.Fatalf("got screenshots %+v, want only [main] once deduped", secondShots)
	}
}

// unreachableAgentError models a driver liveness failure the same way
// a real driver's coded error does (an Error, a Code, and a Remedy),
// without this test importing the diagnostics package directly: the
// health check recognizes any error matching that method set,
// structurally, regardless of which package defines it.
type unreachableAgentError struct {
	msg    string
	remedy string
}

func (e unreachableAgentError) Error() string  { return e.msg }
func (e unreachableAgentError) Code() string   { return "AGENT_UNREACHABLE" }
func (e unreachableAgentError) Remedy() string { return e.remedy }

func TestScriptRunner_Run_StepFailureWithUnreachableAgentAbortsWithDeviceLost(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	tr.driver.FailNextPingForContract(unreachableAgentError{
		msg:    "could not reach the agent on the device",
		remedy: "Confirm the agent is running on the device, then try again.",
	})

	result := run(t, tr, "appId: com.test\n---\n"+
		"- assertVisible: \"Does not exist\"\n"+
		"- screenshot: never\n")

	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if result.Abort == nil {
		t.Fatalf("got Abort=nil, want a device-loss abort")
	}
	if result.Abort.Reason != RunAbortDeviceLost {
		t.Fatalf("got Abort.Reason=%q, want %q", result.Abort.Reason, RunAbortDeviceLost)
	}
	if result.Abort.Code != "AGENT_UNREACHABLE" {
		t.Fatalf("got Abort.Code=%q, want %q", result.Abort.Code, "AGENT_UNREACHABLE")
	}
	if result.FailedAtStep == nil || *result.FailedAtStep != 0 {
		t.Fatalf("got FailedAtStep=%v, want 0", result.FailedAtStep)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("got %d steps recorded, want 1 (fail fast)", len(result.Steps))
	}
}

// TestScriptRunner_Run_DeviceLostStepReportsTheClassifiedMessageNotTheRawError
// guards the tool-facing contract: once a step failure is classified
// as the device having gone away, the step's reported detail is the
// neutral classified message, never the raw driver or transport error
// text the failing step itself produced.
func TestScriptRunner_Run_DeviceLostStepReportsTheClassifiedMessageNotTheRawError(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	tr.driver.FailNextPingForContract(unreachableAgentError{
		msg:    "could not reach the agent on the device",
		remedy: "Confirm the agent is running on the device, then try again.",
	})

	result := run(t, tr, "appId: com.test\n---\n"+
		"- assertVisible: \"Does not exist\"\n")

	if result.Abort == nil {
		t.Fatalf("got Abort=nil, want a device-loss abort")
	}
	if len(result.Steps) != 1 {
		t.Fatalf("got %d steps recorded, want 1", len(result.Steps))
	}
	got := detail(result.Steps[0])
	want := "could not reach the agent on the device. Confirm the agent is running on the device, then try again."
	if got != want {
		t.Fatalf("got failed step detail %q, want the classified message %q", got, want)
	}
	for _, leak := range []string{"http", "127.0.0.1", "GET /"} {
		if strings.Contains(got, leak) {
			t.Fatalf("failed step detail %q leaks %q", got, leak)
		}
	}
}

func TestScriptRunner_Run_StepFailureWithHealthyAgentKeepsOrdinaryFailure(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result := run(t, tr, "appId: com.test\n---\n"+
		"- assertVisible: \"Does not exist\"\n"+
		"- screenshot: never\n")

	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if result.Abort != nil {
		t.Fatalf("got Abort=%+v, want nil: the agent answered healthy, so this is an ordinary step failure", result.Abort)
	}
	if result.FailedAtStep == nil || *result.FailedAtStep != 0 {
		t.Fatalf("got FailedAtStep=%v, want 0", result.FailedAtStep)
	}
}

func TestScriptRunner_Run_HappyPathNeverProbesLiveness(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n- screenshot: first\n")

	if !result.Ok {
		t.Fatalf("got Ok=false, steps=%s", debugSteps(result.Steps))
	}
	if result.Abort != nil {
		t.Fatalf("got Abort=%+v, want nil", result.Abort)
	}
	pings := 0
	for _, c := range tr.driver.Calls {
		if c.Method == "Ping" {
			pings++
		}
	}
	if pings != 0 {
		t.Fatalf("got %d Ping calls on the happy path, want 0", pings)
	}
}

// TestScriptRunner_Run_StepFailureAfterContextCanceledIsNotDeviceLost
// guards the cancellation path classifyStepFailure must not
// misclassify: a step that fails because its own context was already
// canceled (a user stop, or a run-level timeout) is an ordinary
// cancellation, not a device-lost abort, even though the same
// context is what a liveness probe would otherwise be run against.
func TestScriptRunner_Run_StepFailureAfterContextCanceledIsNotDeviceLost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := newTestRunner(cancelingFailureCommand{cancel: cancel})
	tr.driver.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	def := mustParse(t, "appId: com.test\n---\n- back\n")
	result, err := tr.runner.Run(ctx, def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Abort != nil {
		t.Fatalf("got Abort=%+v, want nil: a canceled context is a cancellation, not a device loss", result.Abort)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false: the step itself reported failure")
	}
}

func TestScriptRunner_Run_RequiredFlowCircularDependency(t *testing.T) {
	tr := newTestRunner()
	reqPath := "flows/self.yml"
	absPath, _ := filepath.Abs(reqPath)

	// Simulate an in-progress resolution of reqPath, the only way the
	// cycle branch is reachable given required flows do not recurse
	// into their own `requires` field: two concurrent Run calls on
	// the same ScriptRunner racing to require the same flow, or (as
	// here) a caller already resolving it further up a call stack
	// this test constructs directly.
	tr.runner.callStack[absPath] = struct{}{}

	yaml := fmt.Sprintf("appId: com.test\nrequires:\n  - %s\n---\n- back\n", reqPath)
	result := run(t, tr, yaml)

	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := fmt.Sprintf("Circular dependency detected: %s", reqPath)
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
	if got := result.Steps[0].Command; got != "requires" {
		t.Fatalf("got command %q, want \"requires\"", got)
	}
}

func TestDeviceLostDetail(t *testing.T) {
	cases := []struct {
		name string
		in   RunAbort
		want string
	}{
		{
			name: "message ends with a period",
			in: RunAbort{
				Message: "Re-enable it on the device.",
				Remedy:  "Re-enable the accessibility service on the device, then try again.",
			},
			want: "Re-enable it on the device. Re-enable the accessibility service on the device, then try again.",
		},
		{
			name: "message has no trailing punctuation",
			in: RunAbort{
				Message: "could not reach the agent on the device",
				Remedy:  "Confirm the agent is running on the device, then try again.",
			},
			want: "could not reach the agent on the device. Confirm the agent is running on the device, then try again.",
		},
		{
			name: "no remedy returns the message unchanged",
			in: RunAbort{
				Message: "could not reach the agent on the device.",
			},
			want: "could not reach the agent on the device.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deviceLostDetail(&tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScriptResultFailureCode(t *testing.T) {
	failed := 1
	cases := []struct {
		name   string
		result ScriptResult
		want   string
	}{
		{"passed", ScriptResult{Ok: true}, ""},
		{"step without code", ScriptResult{FailedAtStep: &failed, Steps: []StepResult{{StepIndex: 0, Ok: true}, {StepIndex: 1}}}, ""},
		{"step with code", ScriptResult{FailedAtStep: &failed, Steps: []StepResult{{StepIndex: 0, Ok: true}, {StepIndex: 1, Code: diag.CodeToolTimeout}}}, diag.CodeToolTimeout},
		{"abort wins", ScriptResult{FailedAtStep: &failed, Abort: &RunAbort{Code: diag.CodeAgentUnreachable}, Steps: []StepResult{{StepIndex: 1, Code: diag.CodeToolTimeout}}}, diag.CodeAgentUnreachable},
	}
	for _, c := range cases {
		if got := c.result.FailureCode(); got != c.want {
			t.Errorf("%s: FailureCode() = %q, want %q", c.name, got, c.want)
		}
	}
}
