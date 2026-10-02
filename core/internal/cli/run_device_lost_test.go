package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

// lossOnScreenshotDriver reports the runner gone from inside the first
// screenshot, then waits for the command to cancel the call, so the
// loss always arrives while a step is in flight.
type lossOnScreenshotDriver struct {
	*mock.Driver
	lost chan<- error
}

func (d *lossOnScreenshotDriver) Screenshot(ctx context.Context) ([]byte, error) {
	d.lost <- &device.ProcessExitError{Component: device.LostRunner}
	<-ctx.Done()
	return nil, ctx.Err()
}

type lossFactory struct{}

func (f lossFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (lossFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	lost := make(chan error, 1)
	drv := &lossOnScreenshotDriver{Driver: mock.New(), lost: lost}
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.ScreenshotData = failureScreenshot
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
		Lost:      lost,
	}, nil
}

func TestRunScriptWhoseRunnerExitsMidRunEndsAsDeviceLostWithoutAFailureBundle(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeScript(t, "flow.yml", "- screenshot: home\n- sleep: 1\n")
	var stdout, stderr bytes.Buffer
	deps := mockDeps(&stdout, &stderr)
	deps.Factory = lossFactory{}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath, "--report-dir": root})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	runDir := singleRunDir(t, root)
	meta := readMeta(t, runDir)
	if meta.Result != runfolder.ResultCancelled || meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Errorf("meta = result %q outcome %q, want cancelled deviceLost", meta.Result, meta.Outcome)
	}
	if !strings.Contains(stderr.String(), "device was lost") {
		t.Errorf("stderr = %q, want it to say the device was lost", stderr.String())
	}
	for _, name := range artifactNames(t, runfolder.ArtifactsDir(runDir)) {
		if strings.HasPrefix(name, "step-") {
			t.Errorf("a lost device wrote the failure artifact %s", name)
		}
	}
}

func TestRunScriptWhoseStepFailureClassifiesAsDeviceLostEndsAsDeviceLost(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeScript(t, "flow.yml", "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n")
	var stdout, stderr bytes.Buffer
	deps := mockDeps(&stdout, &stderr)
	deps.Factory = unreachableAgentFactory{}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath, "--report-dir": root})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	runDir := singleRunDir(t, root)
	meta := readMeta(t, runDir)
	if meta.Result != runfolder.ResultCancelled || meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Errorf("meta = result %q outcome %q, want cancelled deviceLost", meta.Result, meta.Outcome)
	}
	for _, name := range artifactNames(t, runfolder.ArtifactsDir(runDir)) {
		if strings.HasPrefix(name, "step-") {
			t.Errorf("a device-lost abort wrote the failure artifact %s", name)
		}
	}
}

type unreachableAgentFactory struct{}

func (f unreachableAgentFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (unreachableAgentFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	drv.FailNextPingForContract(diag.New(
		diag.CodeAgentUnreachable,
		"could not reach the agent on device mock-1",
		"Confirm the agent is running on the device, then try again.",
		nil,
	))
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}
