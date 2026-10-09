package runner

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}

type memoryEvidence struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMemoryEvidence() *memoryEvidence {
	return &memoryEvidence{files: map[string][]byte{}}
}

func (m *memoryEvidence) WriteEvidence(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = data
	return nil
}

func (m *memoryEvidence) names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for n := range m.files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (m *memoryEvidence) file(name string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[name]
}

type captureRig struct {
	runner   *ScriptRunner
	driver   *mock.Driver
	evidence *memoryEvidence
}

func newCaptureRig(d driver.Driver, mockDriver *mock.Driver, bundleClock orchestra.Clock) *captureRig {
	evidence := newMemoryEvidence()
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)})
	bundle := NewFailureBundle(FailureBundleDeps{Orchestra: orch, Clock: bundleClock, Logger: noopLogger{}, Sink: evidence})
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra:      orch,
		Clock:          orchestra.NewFakeClock(0),
		Logger:         noopLogger{},
		Commands:       DefaultCommands(),
		FailureCapture: bundle,
	})
	return &captureRig{runner: r, driver: mockDriver, evidence: evidence}
}

func newMockCaptureRig() *captureRig {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.ScreenshotData = pngBytes
	d.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	return newCaptureRig(d, d, clock.NewManualClock(0))
}

func (c *captureRig) run(t *testing.T, yaml string) ScriptResult {
	t.Helper()
	result, err := c.runner.Run(context.Background(), mustParse(t, yaml))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func TestFailureBundle_FailedStepWritesScreenshotTreeAndLog(t *testing.T) {
	rig := newMockCaptureRig()

	result := rig.run(t, "appId: com.test\n---\n- launchApp\n- assertVisible: \"Does not exist\"\n")

	if result.Ok || result.FailedAtStep == nil || *result.FailedAtStep != 1 {
		t.Fatalf("want a failure at index 1, got Ok=%t FailedAtStep=%v", result.Ok, result.FailedAtStep)
	}
	want := []string{"step-002-failure.png", "step-002-log.jsonl", "step-002-tree.json"}
	if got := rig.evidence.names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if string(rig.evidence.file("step-002-failure.png")) != string(pngBytes) {
		t.Fatalf("screenshot bytes were not written verbatim")
	}
	var tree wire.TreeNodeWire
	if err := json.Unmarshal(rig.evidence.file("step-002-tree.json"), &tree); err != nil || tree.Attributes["id"] != "root" {
		t.Fatalf("tree file = %q, err %v", rig.evidence.file("step-002-tree.json"), err)
	}
	log := string(rig.evidence.file("step-002-log.jsonl"))
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 2 {
		t.Fatalf("log has %d lines, want the started and completed event of the failed step only: %q", len(lines), log)
	}
	if !strings.Contains(lines[0], `"type":"stepStarted"`) || !strings.Contains(lines[1], `"ok":false`) ||
		!strings.Contains(lines[1], `"command":"assertVisible"`) {
		t.Fatalf("log lines do not describe the failed step: %q", log)
	}
}

func TestFailureBundle_JPEGScreenshotUsesJpgExtension(t *testing.T) {
	rig := newMockCaptureRig()
	rig.driver.ScreenshotData = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 1, 2}

	rig.run(t, "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n")

	if rig.evidence.file("step-001-failure.jpg") == nil || rig.evidence.file("step-001-failure.png") != nil {
		t.Fatalf("files = %v, want a jpg screenshot", rig.evidence.names())
	}
}

func TestFailureBundle_PassedRunWritesNothing(t *testing.T) {
	rig := newMockCaptureRig()

	result := rig.run(t, "appId: com.test\n---\n- launchApp\n- screenshot: ok\n")

	if !result.Ok {
		t.Fatalf("run failed: %s", debugSteps(result.Steps))
	}
	if got := rig.evidence.names(); len(got) != 0 {
		t.Fatalf("passed run wrote %v", got)
	}
}

func TestFailureBundle_DeviceLossWritesNothing(t *testing.T) {
	rig := newMockCaptureRig()
	rig.driver.FailNextPingForContract(unreachableAgentError{msg: "agent gone"})

	result := rig.run(t, "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n")

	if result.Abort == nil || result.Abort.Reason != RunAbortDeviceLost {
		t.Fatalf("want a device-lost abort, got %+v", result.Abort)
	}
	if got := rig.evidence.names(); len(got) != 0 {
		t.Fatalf("device loss wrote %v", got)
	}
}

type blockedScreenshotDriver struct{ *mock.Driver }

func (blockedScreenshotDriver) Screenshot(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestFailureBundle_BlockedScreenshotEndsAtTheBoundWithoutChangingTheRun(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	bundleClock := clock.NewManualClock(0)
	rig := newCaptureRig(blockedScreenshotDriver{d}, d, bundleClock)

	type outcome struct {
		result ScriptResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := rig.runner.Run(context.Background(), mustParse(t, "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n"))
		done <- outcome{result, err}
	}()
	bundleClock.AwaitSleepers(1)
	bundleClock.Advance(FailureCaptureBoundMs)
	got := <-done

	if got.err != nil {
		t.Fatalf("Run: %v", got.err)
	}
	if got.result.Ok || got.result.FailedAtStep == nil || *got.result.FailedAtStep != 0 || got.result.Abort != nil {
		t.Fatalf("run outcome changed: Ok=%t FailedAtStep=%v Abort=%v", got.result.Ok, got.result.FailedAtStep, got.result.Abort)
	}
	if rig.evidence.file("step-001-failure.png") != nil {
		t.Fatalf("a screenshot was written for a blocked capture")
	}
	log := string(rig.evidence.file("step-001-log.jsonl"))
	if !strings.Contains(log, `"type":"truncated"`) || !strings.Contains(log, "5000 ms") || !strings.Contains(log, "screenshot") {
		t.Fatalf("log lacks a truncation note: %q", log)
	}
}

type panickingCapture struct{}

func (panickingCapture) CaptureFailure(context.Context, StepFailure) { panic("capture broke") }

func TestScriptRunner_PanickingFailureCaptureDoesNotChangeTheResult(t *testing.T) {
	tr := newTestRunner()
	tr.runner.deps.FailureCapture = panickingCapture{}

	result := run(t, tr, "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n")

	if result.Ok || result.FailedAtStep == nil || *result.FailedAtStep != 0 || result.Abort != nil {
		t.Fatalf("outcome changed: Ok=%t FailedAtStep=%v Abort=%v", result.Ok, result.FailedAtStep, result.Abort)
	}
}

func TestFailureBundle_TreeFileMasksSecureNodeValues(t *testing.T) {
	const secret = "hunter2-sentinel"
	rig := newMockCaptureRig()
	rig.driver.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root"},
		Children: []wire.TreeNodeWire{{Attributes: map[string]string{
			"id": "pw", "label": "Password", "text": secret, "value": secret, "secure": "true",
		}}},
	})

	rig.run(t, "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n")

	data := rig.evidence.file("step-001-tree.json")
	if strings.Contains(string(data), secret) {
		t.Fatalf("tree file carries the secret: %s", data)
	}
	var tree wire.TreeNodeWire
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("tree file: %v", err)
	}
	pw := tree.Children[0].Attributes
	if pw["text"] != orchestra.SecureMask || pw["value"] != orchestra.SecureMask || pw["label"] != "Password" {
		t.Fatalf("secure node attributes = %v, want masked text and value with the label kept", pw)
	}
}

// gatedDriver counts the Screenshot and Hierarchy calls still inside
// the driver. Each call waits for cancellation, announces it, and then
// stays inside the driver until the test releases it.
type gatedDriver struct {
	*mock.Driver
	inFlight atomic.Int32
	canceled chan struct{}
	release  chan struct{}
}

func (g *gatedDriver) hold(ctx context.Context) error {
	g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	<-ctx.Done()
	g.canceled <- struct{}{}
	<-g.release
	return ctx.Err()
}

func (g *gatedDriver) Screenshot(ctx context.Context) ([]byte, error) {
	return nil, g.hold(ctx)
}

func (g *gatedDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	return wire.TreeNodeWire{}, g.hold(ctx)
}

// inFlightProbe records how many driver calls are still running when
// the bundle first writes evidence, which happens after the bundle has
// stopped waiting for its capture calls.
type inFlightProbe struct {
	EvidenceWriter
	driver *gatedDriver
	once   sync.Once
	seen   int32
}

func (p *inFlightProbe) WriteEvidence(name string, data []byte) error {
	p.once.Do(func() { p.seen = p.driver.inFlight.Load() })
	return p.EvidenceWriter.WriteEvidence(name, data)
}

func TestFailureBundle_StopsUsingTheDriverBeforeItWritesEvidence(t *testing.T) {
	g := &gatedDriver{Driver: mock.New(), canceled: make(chan struct{}, 2), release: make(chan struct{})}
	bundleClock := clock.NewManualClock(0)
	probe := &inFlightProbe{EvidenceWriter: newMemoryEvidence(), driver: g}
	bundle := NewFailureBundle(FailureBundleDeps{
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: g, Clock: orchestra.NewFakeClock(0)}),
		Clock:     bundleClock, Logger: noopLogger{}, Sink: probe,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		bundle.CaptureFailure(context.Background(), StepFailure{StepNumber: 1})
	}()
	bundleClock.AwaitSleepers(1)
	bundleClock.Advance(FailureCaptureBoundMs)
	<-g.canceled
	<-g.canceled
	close(g.release)
	<-done

	if probe.seen != 0 {
		t.Fatalf("%d driver call(s) were still running when the bundle wrote evidence", probe.seen)
	}
	if left := g.inFlight.Load(); left != 0 {
		t.Fatalf("%d driver call(s) were still running after CaptureFailure returned", left)
	}
}
