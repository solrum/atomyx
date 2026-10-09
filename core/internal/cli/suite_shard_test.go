package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shard"
)

// workerEntry is the line a worker reports for path. An entry whose file
// name starts with "fail" fails.
func workerEntry(path string) shard.Line {
	status, failure := "passed", ""
	if strings.HasPrefix(filepath.Base(path), "fail") {
		status, failure = "failed", "step 1 (assertExpr) failed"
	}
	return shard.Line{Kind: shard.LineEntry, Entry: &shard.Entry{
		Path: path, Kind: "script", Status: status, DurationMs: 7,
		Attempts: []shard.Attempt{{Number: 1, Status: status, Failure: failure, DurationMs: 7}},
	}}
}

type shardRun struct {
	code     int
	stdout   string
	stderr   string
	root     string
	launcher *shard.FakeLauncher
}

// runShardedFake runs `atomyx suite` over the fake workers scripted for
// each device id.
func runShardedFake(ctx context.Context, t *testing.T, flags map[string]FlagValue, descriptors []device.Descriptor, scripts map[string]*shard.FakeScript) shardRun {
	t.Helper()
	run := shardRun{root: t.TempDir(), launcher: shard.NewFakeLauncher(scripts)}
	var stdout, stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(descriptors), Stdout: &stdout, Stderr: &stderr, Shards: run.launcher}
	all := map[string]FlagValue{"--glob": []string{"*.yml"}, "--report-dir": run.root}
	for k, v := range flags {
		all[k] = v
	}
	run.code = RunSuite(ctx, deps, all, nil)
	run.stdout, run.stderr = stdout.String(), stderr.String()
	return run
}

func twoAndroidDevices() []device.Descriptor {
	return []device.Descriptor{
		{ID: "phone", Platform: device.PlatformAndroid, State: device.StateOnline},
		{ID: "emu", Platform: device.PlatformAndroid, State: device.StateOnline},
	}
}

func answeringWorkers() map[string]*shard.FakeScript {
	return map[string]*shard.FakeScript{"phone": {Handle: workerEntry}, "emu": {Handle: workerEntry}}
}

func writeShardFlows(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range names {
		writeFile(t, dir, name, passingEntry)
	}
}

func TestShardedSuiteRunsEveryEntryOnceAndMergesIndexJUnitAndJSON(t *testing.T) {
	writeShardFlows(t, "a_one.yml", "b_two.yml", "fail_three.yml", "d_four.yml", "e_five.yml")

	run := runShardedFake(context.Background(), t, map[string]FlagValue{"--shard": "auto", "--json": true, "--report": "junit"}, twoAndroidDevices(), answeringWorkers())

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1 for one failed entry; stderr: %s", run.code, run.stderr)
	}
	ran := append(run.launcher.Handled("phone"), run.launcher.Handled("emu")...)
	if len(ran) != 5 || len(slices.Compact(slices.Sorted(slices.Values(ran)))) != 5 {
		t.Fatalf("workers ran %v, want each of the 5 entries exactly once", ran)
	}
	runDir := singleRunDir(t, run.root)
	index := readIndex(t, runDir)
	if len(index.Entries) != 5 {
		t.Fatalf("index has %d entries, want 5", len(index.Entries))
	}
	for _, entry := range index.Entries {
		attempt := entry.Attempts[0]
		if (attempt.DeviceID != "phone" && attempt.DeviceID != "emu") || attempt.WorkerRunID != "run-"+attempt.DeviceID {
			t.Errorf("entry %s attempt = %+v, want the device and worker run named", entry.Path, attempt)
		}
	}
	var payload suiteJSONResult
	if err := json.Unmarshal([]byte(run.stdout), &payload); err != nil || payload.Ok || payload.RunFolder != runDir || !reflect.DeepEqual(payload.Entries, index.Entries) {
		t.Errorf("--json printed %q (%v), want the index entries of %s", run.stdout, err, runDir)
	}
	body, err := os.ReadFile(filepath.Join(runfolder.ReportDir(runDir), runfolder.JUnitFileName))
	if err != nil {
		t.Fatalf("no junit report: %v", err)
	}
	var suite junitTestSuite
	if err := xml.Unmarshal(body, &suite); err != nil || suite.Tests != 5 || suite.Failures != 1 {
		t.Errorf("junit = %+v (%v), want 5 cases with 1 failure", suite, err)
	}
	if meta := readMeta(t, runDir); meta.Kind != runfolder.KindSuite || meta.Outcome != runfolder.OutcomeFailed || meta.Device != nil {
		t.Errorf("meta = %+v, want a failed suite run with no single device", meta)
	}
	for _, spec := range run.launcher.Specs() {
		if !slices.Contains(spec.Args, "--worker") || !slices.Contains(spec.Args, filepath.Base(runDir)) {
			t.Errorf("worker command line = %v, want --worker and the parent run id", spec.Args)
		}
	}
}

func TestShardedSuiteExitsZeroWhenEveryEntryPassed(t *testing.T) {
	writeShardFlows(t, "a.yml", "b.yml", "c.yml")

	run := runShardedFake(context.Background(), t, map[string]FlagValue{"--devices": "phone,emu"}, twoAndroidDevices(), answeringWorkers())

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", run.code, run.stderr)
	}
	if meta := readMeta(t, singleRunDir(t, run.root)); meta.Outcome != runfolder.OutcomePassed {
		t.Errorf("outcome = %q, want passed", meta.Outcome)
	}
}

func TestShardedSuiteUsageErrorsExitTwoBeforeAnyWorkerStarts(t *testing.T) {
	cases := map[string]map[string]FlagValue{
		"--shard auto with no ready device": {"--shard": "auto"},
		"--device with --devices":           {"--device": "phone", "--devices": "phone,emu"},
		"--devices with --shard":            {"--devices": "phone", "--shard": "auto"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			writeShardFlows(t, "a.yml")
			run := runShardedFake(context.Background(), t, flags, nil, answeringWorkers())

			if run.code != 2 {
				t.Errorf("exit code = %d, want 2; stderr: %s", run.code, run.stderr)
			}
			if len(run.launcher.Specs()) != 0 {
				t.Errorf("workers were started: %v", run.launcher.Specs())
			}
		})
	}
}
