package cli

import (
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/shard"
)

func wireAttempt(number int, status, device, run string) shard.Attempt {
	return shard.Attempt{Number: number, Status: status, DurationMs: 5, DeviceID: device, WorkerRunID: run}
}

func TestShardedEntriesKeepSuiteOrderAndNameTheDeviceAndWorkerRunOfEveryAttempt(t *testing.T) {
	paths := []string{"/f/a.yml", "/f/b.yml", "/f/c.yml"}
	outcome := shard.Outcome{Entries: []shard.Entry{
		{Path: "/f/c.yml", Kind: "script", Status: "passed", Flaky: true, Attempts: []shard.Attempt{wireAttempt(1, "failed", "sim", "run-sim"), wireAttempt(2, "passed", "sim", "run-sim")}},
		{Path: "/f/a.yml", Kind: "script", Status: "passed", Attempts: []shard.Attempt{wireAttempt(1, "failed", "phone", "run-phone"), wireAttempt(2, "passed", "sim", "run-sim")}},
		{Path: "/f/b.yml", Kind: "scenario", Status: "failed", Quarantined: true, Attempts: []shard.Attempt{{Number: 1, Status: "failed", Videos: []string{"video-entry-1-1.mp4"}, DeviceID: "phone", WorkerRunID: "run-phone"}}},
	}}

	entries, index := shardedEntries(paths, outcome)

	if len(entries) != 3 || entries[0].Path != "/f/a.yml" || entries[1].Path != "/f/b.yml" || entries[2].Path != "/f/c.yml" {
		t.Fatalf("entries = %+v, want suite order", entries)
	}
	requeued := index[0]
	if len(requeued.Attempts) != 2 || requeued.Attempts[0].DeviceID != "phone" || requeued.Attempts[0].WorkerRunID != "run-phone" ||
		requeued.Attempts[1].DeviceID != "sim" || requeued.Attempts[1].WorkerRunID != "run-sim" || requeued.Attempts[0].Result != runfolder.ResultFailed {
		t.Errorf("requeued entry = %+v, want attempts in two workers", requeued)
	}
	if !index[1].Quarantined || index[1].Status != runfolder.ResultFailed || index[1].Attempts[0].Videos[0] != "video-entry-1-1.mp4" || !index[2].Flaky {
		t.Errorf("index = %+v", index)
	}
	result := scenario.Summarize(entries, nil, 0)
	if !result.Ok || result.Passed != 2 || result.Flaky != 1 || result.QuarantinedFailures != 1 || entries[1].Kind != scenario.KindScenario {
		t.Errorf("result = %+v, want a passing suite with one flaky entry and one quarantined failure", result)
	}
}

func TestShardedEntriesCountAnUnrunEntryAsFailedInPlace(t *testing.T) {
	paths := []string{"/f/a.yml", "/f/b.yml"}
	outcome := shard.Outcome{
		Entries: []shard.Entry{{Path: "/f/a.yml", Kind: "script", Status: "passed", Attempts: []shard.Attempt{wireAttempt(1, "passed", "sim", "run-sim")}}},
		Unrun:   []string{"/f/b.yml"},
	}

	entries, index := shardedEntries(paths, outcome)

	if len(entries) != 2 || entries[1].Status != scenario.EntryFailed || entries[1].Error == nil || len(entries[1].Attempts) != 0 {
		t.Fatalf("entries = %+v, want the unrun entry failed with no attempts", entries)
	}
	if index[1].Status != runfolder.ResultFailed || index[1].Attempts == nil || len(index[1].Attempts) != 0 {
		t.Errorf("index entry = %+v, want a failed entry with an empty attempt list", index[1])
	}
	if result := scenario.Summarize(entries, nil, 0); result.Ok || result.Failed != 1 {
		t.Errorf("result = %+v, want a failed suite", result)
	}
}
