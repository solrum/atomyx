package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
)

// pruneNow is the instant the prune tests run at.
var pruneNow = runsEpoch.Add(100 * 24 * time.Hour)

func execRunsAt(t *testing.T, now time.Time, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	deps := Deps{Stdout: &out, Stderr: &errOut, Clock: orchestra.NewFakeClock(now.UnixMilli())}
	code = ExecuteRuns(context.Background(), deps, args)
	return code, out.String(), errOut.String()
}

func daysBeforePruneNow(days int) time.Time {
	return pruneNow.Add(-time.Duration(days) * 24 * time.Hour)
}

func runFolderExists(root, id string) bool {
	_, err := os.Stat(runfolder.RunDir(root, id))
	return err == nil
}

func seedPruneStore(t *testing.T) (root string, ids map[string]string) {
	root = runsStorageDir(t)
	ids = map[string]string{
		"ancient": seedRecordedRun(t, root, "ancient", daysBeforePruneNow(90), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"}),
		"old":     seedRecordedRun(t, root, "old", daysBeforePruneNow(40), time.Second, runfolder.OutcomeFailed, runfolder.Meta{TestKey: "a"}),
		"recent":  seedRecordedRun(t, root, "recent", daysBeforePruneNow(2), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"}),
	}
	return root, ids
}

func TestRunsPruneMisuseExitsTwo(t *testing.T) {
	root, ids := seedPruneStore(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no criteria", []string{"prune"}, "needs --keep, --older-than, or both"},
		{"dry run alone", []string{"prune", "--dry-run"}, "needs --keep, --older-than, or both"},
		{"zero keep", []string{"prune", "--keep", "0"}, "must be a whole number of at least 1"},
		{"text keep", []string{"prune", "--keep", "some"}, "must be a whole number of at least 1"},
		{"negative days", []string{"prune", "--older-than=-3"}, "must be a whole number of at least 1"},
		{"unknown flag", []string{"prune", "--test", "a"}, `does not accept "--test"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := execRunsAt(t, pruneNow, tt.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
		})
	}
	for name, id := range ids {
		if !runFolderExists(root, id) {
			t.Errorf("misuse deleted run %s", name)
		}
	}
}

func TestRunsPruneDryRunReportsAndDeletesNothing(t *testing.T) {
	root, ids := seedPruneStore(t)

	code, stdout, stderr := execRunsAt(t, pruneNow, "prune", "--older-than", "30", "--dry-run")

	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if got := strings.Count(stdout, "would delete"); got != 2 || !strings.Contains(stdout, "older than 30 days") {
		t.Errorf("output:\n%s", stdout)
	}
	for name, id := range ids {
		if !runFolderExists(root, id) {
			t.Errorf("dry run deleted run %s", name)
		}
	}
}

func TestRunsPruneOlderThanDeletesAgainstTheInjectedClock(t *testing.T) {
	root, ids := seedPruneStore(t)

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--older-than", "30")

	if code != 0 || strings.Count(stdout, "deleted") != 2 {
		t.Fatalf("code = %d, output:\n%s", code, stdout)
	}
	if runFolderExists(root, ids["ancient"]) || runFolderExists(root, ids["old"]) || !runFolderExists(root, ids["recent"]) {
		t.Errorf("wrong runs remain: ancient=%v old=%v recent=%v",
			runFolderExists(root, ids["ancient"]), runFolderExists(root, ids["old"]), runFolderExists(root, ids["recent"]))
	}
}

func TestRunsPruneKeepJSON(t *testing.T) {
	root, ids := seedPruneStore(t)

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--keep", "2", "--json")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	var doc runPruneJSON
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if doc.DryRun || doc.Keep != 2 || doc.RunsDeleted != 1 || len(doc.Units) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	unit := doc.Units[0]
	if unit.RunID != ids["ancient"] || unit.Action != "deleted" || unit.Reason != "outside the newest 2 runs" {
		t.Errorf("unit = %+v", unit)
	}
	if runFolderExists(root, ids["ancient"]) || !runFolderExists(root, ids["old"]) {
		t.Error("keep 2 did not delete exactly the oldest run")
	}
}

func TestRunsPruneNothingSelected(t *testing.T) {
	seedPruneStore(t)

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--keep", "10")
	if code != 0 || !strings.Contains(stdout, "No runs match the prune criteria.") {
		t.Errorf("code = %d, output = %q", code, stdout)
	}
	_, js, _ := execRunsAt(t, pruneNow, "prune", "--keep", "10", "--json")
	if !strings.Contains(js, `"units": []`) {
		t.Errorf("json = %s", js)
	}
}

func TestRunsPruneDeletesAShardedSuiteWithItsWorkersAsOneUnit(t *testing.T) {
	root := runsStorageDir(t)
	worker := seedRecordedRun(t, root, "worker", daysBeforePruneNow(60), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"})
	suite := seedSuiteRun(t, root, daysBeforePruneNow(60), nil)
	rewriteParent(t, root, worker, suite)
	fresh := seedRecordedRun(t, root, "fresh", daysBeforePruneNow(1), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"})

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--keep", "1")

	if code != 0 {
		t.Fatalf("code = %d, output:\n%s", code, stdout)
	}
	if runFolderExists(root, suite) || runFolderExists(root, worker) || !runFolderExists(root, fresh) {
		t.Errorf("suite=%v worker=%v fresh=%v, want the unit gone and the fresh run kept",
			runFolderExists(root, suite), runFolderExists(root, worker), runFolderExists(root, fresh))
	}
	if !strings.Contains(stdout, suite) || strings.Contains(stdout, worker) {
		t.Errorf("the report should name the unit by its root run only:\n%s", stdout)
	}
}

func TestRunsPruneRefusesALiveRunAndExitsOne(t *testing.T) {
	root, ids := seedPruneStore(t)
	live, err := runfolder.NewStarter(root, func() time.Time { return daysBeforePruneNow(80) }, bytes.NewReader([]byte{9, 8})).
		Start("live", runfolder.Meta{Kind: runfolder.KindScript, TestKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Close() })

	code, stdout, stderr := execRunsAt(t, pruneNow, "prune", "--older-than", "30")

	if code != 1 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q, output:\n%s", code, stderr, stdout)
	}
	if !runFolderExists(root, live.ID()) {
		t.Error("the live run was deleted")
	}
	if !strings.Contains(stdout, "still being written") || !strings.Contains(stdout, "skipped") {
		t.Errorf("the report should explain the skip:\n%s", stdout)
	}
	if runFolderExists(root, ids["ancient"]) || runFolderExists(root, ids["old"]) {
		t.Error("the other selected runs were not deleted")
	}
}

func TestRunsPruneRefusesAGroupWithALiveWorker(t *testing.T) {
	root := runsStorageDir(t)
	suite := seedSuiteRun(t, root, daysBeforePruneNow(60), nil)
	live, err := runfolder.NewStarter(root, func() time.Time { return daysBeforePruneNow(60) }, bytes.NewReader([]byte{5, 6})).
		Start("worker", runfolder.Meta{Kind: runfolder.KindScript, TestKey: "a", ParentRunID: suite})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Close() })

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--older-than", "30", "--json")

	if code != 1 {
		t.Fatalf("code = %d, output:\n%s", code, stdout)
	}
	if !runFolderExists(root, suite) || !runFolderExists(root, live.ID()) {
		t.Error("a group with a live worker lost a member")
	}
	var doc runPruneJSON
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || len(doc.Units) != 1 || doc.Units[0].Action != "skippedLive" {
		t.Errorf("doc = %+v, err = %v", doc, err)
	}
}

func TestRunsPruneDryRunOverALiveRunExitsZero(t *testing.T) {
	root := runsStorageDir(t)
	live, err := runfolder.NewStarter(root, func() time.Time { return daysBeforePruneNow(80) }, bytes.NewReader([]byte{7, 7})).
		Start("live", runfolder.Meta{Kind: runfolder.KindScript, TestKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Close() })

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--older-than", "30", "--dry-run")

	if code != 0 || !strings.Contains(stdout, "still being written") {
		t.Errorf("code = %d, output:\n%s", code, stdout)
	}
}

func TestRunsPruneLeavesUnreadableRecordsAlone(t *testing.T) {
	root, _ := seedPruneStore(t)
	writeUnreadableRun(t, root)

	code, stdout, _ := execRunsAt(t, pruneNow, "prune", "--older-than", "1")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !runFolderExists(root, "broken-run") {
		t.Error("the unreadable record was deleted")
	}
	if !strings.Contains(stdout, "1 run record could not be read and was left untouched.") {
		t.Errorf("output:\n%s", stdout)
	}
}

func writeRetentionConfig(t *testing.T, retention string) {
	t.Helper()
	dir := t.TempDir()
	content := "project: Demo\napps:\n  - id: main\n    platform: android\n    appId: com.example.app\nenvironments:\n  staging:\n    app: main\n" + retention
	if err := os.WriteFile(dir+"/atomyx.config.yaml", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func TestRunsPruneWithoutFlagsUsesTheConfiguredRetention(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "retention:\n  keepLast: 1\n")

	code, stdout, stderr := execRunsAt(t, pruneNow, "prune", "--json")

	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, `"keep": 1`) {
		t.Errorf("output does not show the configured keep:\n%s", stdout)
	}
	if runFolderExists(root, ids["ancient"]) || runFolderExists(root, ids["old"]) || !runFolderExists(root, ids["recent"]) {
		t.Error("the configured keepLast 1 was not applied")
	}
}

func TestRunsPruneFlagsReplaceTheConfiguredRetentionEntirely(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "retention:\n  keepLast: 1\n  olderThanDays: 1\n")

	code, stdout, stderr := execRunsAt(t, pruneNow, "prune", "--older-than", "60", "--json")

	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, `"keep"`) {
		t.Errorf("a configured keepLast leaked into a prune that gave only --older-than:\n%s", stdout)
	}
	if runFolderExists(root, ids["ancient"]) || !runFolderExists(root, ids["old"]) || !runFolderExists(root, ids["recent"]) {
		t.Error("only the run older than 60 days should be deleted")
	}
}

func TestRunsPruneWithoutFlagsOrRetentionExitsTwo(t *testing.T) {
	seedPruneStore(t)
	writeRetentionConfig(t, "")

	code, _, stderr := execRunsAt(t, pruneNow, "prune")

	if code != 2 || !strings.Contains(stderr, "retention policy") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestRunsPruneReportsAnInvalidProjectConfig(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "retention:\n  keepLast: 0\n")

	code, _, stderr := execRunsAt(t, pruneNow, "prune", "--keep", "1")

	if code != 1 || !strings.Contains(stderr, "retention") {
		t.Fatalf("code = %d, stderr = %q, want exit 1 naming the retention problem", code, stderr)
	}
	for name, id := range ids {
		if !runFolderExists(root, id) {
			t.Errorf("run %s was deleted despite the config error", name)
		}
	}
}
