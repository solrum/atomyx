package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/shard"
)

const (
	passingEntry = "- sleep: 1\n"
	failingEntry = "- assertExpr: { expr: '1 == 2' }\n"
)

type workerRun struct {
	code   int
	lines  []shard.Line
	stderr string
	root   string
}

func runWorker(t *testing.T, factory device.WrappingFactory, input string, extra map[string]FlagValue) workerRun {
	t.Helper()
	return runWorkerReading(t, factory, strings.NewReader(input), extra)
}

func runWorkerReading(t *testing.T, factory device.WrappingFactory, input io.Reader, extra map[string]FlagValue) workerRun {
	t.Helper()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: factory, Stdin: input, Stdout: &stdout, Stderr: &stderr}
	flags := map[string]FlagValue{"--worker": true, "--device": "emulator-5556", "--report-dir": root}
	for k, v := range extra {
		flags[k] = v
	}
	code := RunSuite(context.Background(), deps, flags, nil)
	var lines []shard.Line
	for _, raw := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if raw == "" {
			continue
		}
		line, err := shard.DecodeLine([]byte(raw))
		if err != nil {
			t.Fatalf("stdout line %q is not a protocol line: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return workerRun{code: code, lines: lines, stderr: stderr.String(), root: root}
}

func TestSuiteWorkerReportsReadyThenOneLinePerEntryAndWritesItsOwnRunFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a_pass.yml", passingEntry)
	writeFile(t, dir, "b_fail.yml", failingEntry)
	input := filepath.Join(dir, "a_pass.yml") + "\n\n" + filepath.Join(dir, "b_fail.yml") + "\n"

	run := runWorker(t, screenshotMockFactory{}, input, map[string]FlagValue{"--parent-run": "parent-1"})

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", run.code, run.stderr)
	}
	if len(run.lines) != 3 || run.lines[0].Kind != shard.LineReady || run.lines[1].Kind != shard.LineEntry || run.lines[2].Kind != shard.LineEntry {
		t.Fatalf("lines = %+v, want ready then two entries", run.lines)
	}
	runDir := singleRunDir(t, run.root)
	if run.lines[0].RunID != filepath.Base(runDir) || run.lines[1].RunID != filepath.Base(runDir) {
		t.Errorf("lines name run %q and %q, want the worker folder %q", run.lines[0].RunID, run.lines[1].RunID, filepath.Base(runDir))
	}
	first, second := run.lines[1].Entry, run.lines[2].Entry
	if first.Status != "passed" || second.Status != "failed" || first.Path != filepath.Join(dir, "a_pass.yml") {
		t.Errorf("entries = %+v and %+v", first, second)
	}
	if len(second.Attempts) != 1 || second.Attempts[0].Status != "failed" || run.lines[1].DeviceLost || run.lines[2].DeviceLost {
		t.Errorf("second entry attempts = %+v", second.Attempts)
	}
	meta := readMeta(t, runDir)
	if meta.Kind != runfolder.KindSuite || meta.ParentRunID != "parent-1" || meta.Outcome != runfolder.OutcomeFailed {
		t.Errorf("meta = %+v, want a failed suite run of parent-1", meta)
	}
	index := readIndex(t, runDir)
	if len(index.Entries) != 2 || index.Entries[0].Path != first.Path || index.Entries[1].Status != runfolder.ResultFailed {
		t.Errorf("index = %+v, want both entries", index.Entries)
	}
}

func TestSuiteWorkerStopsAfterReportingALostDevice(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yml", "- screenshot: home\n- sleep: 1\n")
	writeFile(t, dir, "b.yml", passingEntry)
	input := filepath.Join(dir, "a.yml") + "\n" + filepath.Join(dir, "b.yml") + "\n"

	run := runWorker(t, lossFactory{}, input, nil)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", run.code, run.stderr)
	}
	if len(run.lines) != 2 || !run.lines[1].DeviceLost || run.lines[1].Entry.Status != "failed" {
		t.Fatalf("lines = %+v, want ready then one device-lost entry and nothing after it", run.lines)
	}
	if meta := readMeta(t, singleRunDir(t, run.root)); meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Errorf("outcome = %q, want deviceLost", meta.Outcome)
	}
}

func TestSuiteWorkerReportsTheErrorCodeWhenItCannotAcquireItsDevice(t *testing.T) {
	run := runWorker(t, heldFactory{}, "", nil)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1", run.code)
	}
	if len(run.lines) != 1 || run.lines[0].Kind != shard.LineError || run.lines[0].Code != diag.CodeDeviceHeld {
		t.Fatalf("lines = %+v, want one DEVICE_HELD error and no ready line", run.lines)
	}
}

func TestSuiteWorkerReportsAnUnreadableEntryAsFailed(t *testing.T) {
	run := runWorker(t, screenshotMockFactory{}, filepath.Join(t.TempDir(), "missing.yml")+"\n", nil)

	if run.code != 0 || len(run.lines) != 2 {
		t.Fatalf("exit code %d, lines %+v", run.code, run.lines)
	}
	entry := run.lines[1].Entry
	if entry.Status != "failed" || entry.Error == nil || len(entry.Attempts) != 1 {
		t.Errorf("entry = %+v, want a failed entry carrying the read error", entry)
	}
}

func TestSuiteWorkerWithoutADeviceIsAUsageError(t *testing.T) {
	var stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stderr: &stderr}

	code := RunSuite(context.Background(), deps, map[string]FlagValue{"--worker": true}, nil)

	if code != 2 || !strings.Contains(stderr.String(), "--worker requires --device") {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
}

type heldFactory struct{}

func (f heldFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (heldFactory) Build(context.Context, device.Descriptor) (device.Built, error) {
	return device.Built{}, diag.New(diag.CodeDeviceHeld, "the device is held by another process", "", nil)
}

func TestSuiteWorkerKeepsTheArtifactsOfEveryEntryApart(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yml", failingEntry)
	writeFile(t, dir, "b.yml", failingEntry)
	input := filepath.Join(dir, "a.yml") + "\n" + filepath.Join(dir, "b.yml") + "\n"

	run := runWorker(t, screenshotMockFactory{}, input, nil)

	names := artifactNames(t, runfolder.ArtifactsDir(singleRunDir(t, run.root)))
	var first, second int
	for _, name := range names {
		switch {
		case strings.HasPrefix(name, "0-"):
			first++
		case strings.HasPrefix(name, "1-"):
			second++
		}
	}
	if first == 0 || second == 0 {
		t.Errorf("artifacts %v, want files named for entry positions 0 and 1", names)
	}
}

// idleLossFactory builds a device that reports its runner gone as soon as
// it is opened, so the loss reaches a worker that holds no entry.
type idleLossFactory struct{}

func (f idleLossFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (idleLossFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	lost := make(chan error, 1)
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	lost <- &device.ProcessExitError{Component: device.LostRunner}
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
		Lost:      lost,
	}, nil
}

func TestSuiteWorkerThatLosesItsDeviceWhileIdleReportsTheLossAndEndsAsDeviceLost(t *testing.T) {
	idle, release := io.Pipe()
	t.Cleanup(func() { _ = release.Close() })

	run := runWorkerReading(t, idleLossFactory{}, idle, nil)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", run.code, run.stderr)
	}
	if len(run.lines) != 2 || run.lines[1].Kind != shard.LineError || run.lines[1].Code != shard.DeviceLostCode {
		t.Fatalf("lines = %+v, want ready then a device-lost error line", run.lines)
	}
	if meta := readMeta(t, singleRunDir(t, run.root)); meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Errorf("outcome = %q, want deviceLost", meta.Outcome)
	}
}
