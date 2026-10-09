package cli

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shard"
	"dev.atomyx/internal/shutdown"
)

func TestShardedSuiteDropsAWorkerWhoseDeviceIsHeldAndRunsOnTheOthers(t *testing.T) {
	writeShardFlows(t, "a.yml", "b.yml", "c.yml")
	held := shard.Line{Kind: shard.LineError, Code: "DEVICE_HELD", Message: "held by atomyx inspect"}
	scripts := map[string]*shard.FakeScript{"phone": {ReadyLine: &held}, "emu": {Handle: workerEntry}}

	run := runShardedFake(context.Background(), t, map[string]FlagValue{"--shard": "auto"}, twoAndroidDevices(), scripts)

	if run.code != 0 || len(run.launcher.Handled("emu")) != 3 {
		t.Fatalf("exit code %d, emu ran %v; stderr: %s", run.code, run.launcher.Handled("emu"), run.stderr)
	}
	if !strings.Contains(run.stderr, "warning: device phone dropped: DEVICE_HELD") {
		t.Errorf("stderr = %q, want a warning naming the dropped device", run.stderr)
	}
}

func TestShardedSuiteWithNoWorkerLeftReportsTheEntriesAsNotRunAndExitsOne(t *testing.T) {
	writeShardFlows(t, "a.yml", "b.yml")
	scripts := map[string]*shard.FakeScript{
		"phone": {StartErr: errors.New("no such file")},
		"emu":   {ReadyLine: &shard.Line{Kind: shard.LineError, Code: "DEVICE_HELD"}},
	}

	run := runShardedFake(context.Background(), t, map[string]FlagValue{"--devices": "phone,emu", "--json": true}, twoAndroidDevices(), scripts)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1", run.code)
	}
	runDir := singleRunDir(t, run.root)
	index := readIndex(t, runDir)
	if len(index.Entries) != 2 {
		t.Fatalf("index = %+v, want both entries listed", index.Entries)
	}
	for _, entry := range index.Entries {
		if entry.Status != runfolder.ResultFailed || len(entry.Attempts) != 0 {
			t.Errorf("entry %s = %+v, want a failed entry that never ran", entry.Path, entry)
		}
	}
	if meta := readMeta(t, runDir); meta.Outcome != runfolder.OutcomeFailed {
		t.Errorf("outcome = %q, want failed", meta.Outcome)
	}
}

func TestShardedSuiteRequeuesTheEntryOfALostDeviceAndListsAttemptsInBothWorkers(t *testing.T) {
	writeShardFlows(t, "a.yml", "b.yml", "c.yml")
	phoneLost := make(chan struct{})
	lose := func(path string) shard.Line {
		line := workerEntry(path)
		line.Entry.Status, line.Entry.Attempts[0].Status = "failed", "failed"
		line.DeviceLost = true
		close(phoneLost)
		return line
	}
	scripts := map[string]*shard.FakeScript{"phone": {Handle: lose}, "emu": {ReadyAfter: phoneLost, Handle: workerEntry}}

	run := runShardedFake(context.Background(), t, map[string]FlagValue{"--devices": "phone,emu"}, twoAndroidDevices(), scripts)

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0 after the other worker passed the entry; stderr: %s", run.code, run.stderr)
	}
	first := readIndex(t, singleRunDir(t, run.root)).Entries[0]
	if len(first.Attempts) != 2 || first.Attempts[0].DeviceID != "phone" || first.Attempts[0].Result != runfolder.ResultFailed ||
		first.Attempts[1].DeviceID != "emu" || first.Attempts[1].Result != runfolder.ResultPassed || first.Attempts[0].WorkerRunID == first.Attempts[1].WorkerRunID {
		t.Errorf("first entry = %+v, want a failed attempt on phone then a passed attempt on emu in another worker run", first)
	}
}

func TestShardedSuiteInterruptStopsEveryWorkerAndExits130(t *testing.T) {
	writeShardFlows(t, "a.yml", "b.yml", "c.yml")
	started := make(chan struct{}, 2)
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	block := func(path string) shard.Line { started <- struct{}{}; <-gate; return workerEntry(path) }
	scripts := map[string]*shard.FakeScript{"phone": {Handle: block}, "emu": {Handle: block}}
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() { <-started; <-started; cancel(shutdown.InterruptCause(syscall.SIGINT)) }()

	run := runShardedFake(ctx, t, map[string]FlagValue{"--shard": "auto"}, twoAndroidDevices(), scripts)

	if run.code != shutdown.ExitCodeSigint {
		t.Errorf("exit code = %d, want %d", run.code, shutdown.ExitCodeSigint)
	}
	if !run.launcher.Stopped("phone") || !run.launcher.Stopped("emu") {
		t.Error("an interrupt left a worker running")
	}
	if got := run.launcher.StopGrace("phone"); got != workerStopGrace {
		t.Errorf("a worker was given %v to stop, want %v", got, workerStopGrace)
	}
	if meta := readMeta(t, singleRunDir(t, run.root)); meta.Outcome != runfolder.OutcomeCancelled {
		t.Errorf("outcome = %q, want cancelled", meta.Outcome)
	}
}
