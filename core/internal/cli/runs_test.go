package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storageroot"
)

// runsStorageDir points the storage root at a fresh temp directory and
// returns it.
func runsStorageDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storageroot.EnvVar, root)
	return root
}

// seedRecordedRun records one finished run under root that started at start and
// lasted duration. mutate, when not nil, adjusts the meta before the run
// is finalized.
func seedRecordedRun(t *testing.T, root, label string, start time.Time, duration time.Duration, outcome runfolder.Outcome, base runfolder.Meta) string {
	t.Helper()
	ticks := []time.Time{start, start.Add(duration)}
	clock := func() time.Time {
		at := ticks[0]
		if len(ticks) > 1 {
			ticks = ticks[1:]
		}
		return at
	}
	if base.Kind == "" {
		base.Kind = runfolder.KindScript
	}
	w, err := runfolder.NewStarter(root, clock, rand.Reader).Start(label, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Finalize(outcome, nil); err != nil {
		t.Fatal(err)
	}
	return w.ID()
}

func execRuns(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = ExecuteRuns(context.Background(), Deps{Stdout: &out, Stderr: &errOut}, args)
	return code, out.String(), errOut.String()
}

var runsEpoch = time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

func seedListStore(t *testing.T) (oldID, midID, newID string) {
	root := runsStorageDir(t)
	oldID = seedRecordedRun(t, root, "a", runsEpoch, 2*time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "flows/a.yml"})
	midID = seedRecordedRun(t, root, "b", runsEpoch.Add(24*time.Hour), 90*time.Second, runfolder.OutcomeFailed, runfolder.Meta{ScriptPath: "/abs/b.yml"})
	newID = seedRecordedRun(t, root, "a", runsEpoch.Add(48*time.Hour), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "flows/a.yml"})
	return oldID, midID, newID
}

func TestRunsListTextNewestFirstWithKeyFallback(t *testing.T) {
	oldID, midID, newID := seedListStore(t)

	code, stdout, stderr := execRuns(t, "list")

	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want header and 3 rows:\n%s", len(lines), stdout)
	}
	for i, id := range []string{newID, midID, oldID} {
		if !strings.HasPrefix(lines[i+1], id) {
			t.Errorf("row %d = %q, want run %s", i, lines[i+1], id)
		}
	}
	for _, want := range []string{"RESULT", "2026-03-03T10:00:00Z", "/abs/b.yml", "flows/a.yml", "failed", "1m30s"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
}

func TestRunsListFilters(t *testing.T) {
	seedListStore(t)

	_, byTest, _ := execRuns(t, "list", "--test", "flows/a.yml")
	if got := strings.Count(byTest, "flows/a.yml"); got != 2 || strings.Contains(byTest, "/abs/b.yml") {
		t.Errorf("--test output:\n%s", byTest)
	}

	_, bySince, _ := execRuns(t, "list", "--since", "2026-03-02")
	if rows := strings.Count(bySince, "\n") - 1; rows != 2 {
		t.Errorf("--since date rows = %d:\n%s", rows, bySince)
	}

	_, byStamp, _ := execRuns(t, "list", "--since", "2026-03-03T00:00:00Z")
	if rows := strings.Count(byStamp, "\n") - 1; rows != 1 {
		t.Errorf("--since timestamp rows = %d:\n%s", rows, byStamp)
	}

	_, none, _ := execRuns(t, "list", "--test", "missing")
	if !strings.Contains(none, "No runs found.") {
		t.Errorf("empty output = %q", none)
	}
}

func TestRunsListJSON(t *testing.T) {
	_, midID, _ := seedListStore(t)

	code, stdout, _ := execRuns(t, "list", "--json")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	var doc runListJSON
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if len(doc.Runs) != 3 || doc.Skipped != 0 {
		t.Fatalf("doc = %+v", doc)
	}
	got := doc.Runs[1]
	if got.RunID != midID || got.TestKey != "/abs/b.yml" || got.Result != "failed" || got.Kind != "script" ||
		got.DurationMs == nil || *got.DurationMs != 90000 || got.StartedAt != "2026-03-02T10:00:00Z" {
		t.Errorf("middle run = %+v", got)
	}
}

func TestRunsListEmptyStoreJSONHasEmptyArray(t *testing.T) {
	runsStorageDir(t)

	_, stdout, _ := execRuns(t, "list", "--json")

	if !strings.Contains(stdout, `"runs": []`) {
		t.Errorf("stdout = %s", stdout)
	}
}

func TestRunsListReportsUnreadableRunsInFooter(t *testing.T) {
	root := runsStorageDir(t)
	seedRecordedRun(t, root, "a", runsEpoch, time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"})
	writeUnreadableRun(t, root)

	_, stdout, _ := execRuns(t, "list")
	if !strings.Contains(stdout, "1 run record could not be read and was skipped.") || strings.Count(stdout, "\n") != 3 {
		t.Errorf("text output:\n%s", stdout)
	}
	_, js, _ := execRuns(t, "list", "--json")
	var doc runListJSON
	if err := json.Unmarshal([]byte(js), &doc); err != nil || doc.Skipped != 1 || len(doc.Runs) != 1 {
		t.Errorf("json = %s, err = %v", js, err)
	}
}

func TestRunsMisuseExitsTwo(t *testing.T) {
	runsStorageDir(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"bogus"}, `unknown runs command "bogus"`},
		{"unknown flag", []string{"list", "--bogus"}, `does not accept "--bogus"`},
		{"bad since", []string{"list", "--since", "yesterday"}, "neither an RFC 3339 timestamp nor a YYYY-MM-DD date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := execRuns(t, tt.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
		})
	}
}

func TestRunsHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"list", "--help"}, {"stats", "--help"}} {
		code, stdout, stderr := execRuns(t, args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "atomyx runs") {
			t.Errorf("args %v: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
	}
}

// writeUnreadableRun leaves a run folder whose meta.json cannot be decoded.
func writeUnreadableRun(t *testing.T, root string) {
	t.Helper()
	dir := runfolder.RunDir(root, "broken-run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runfolder.MetaFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedSuiteRun records one finished suite run whose index holds entries.
func seedSuiteRun(t *testing.T, root string, start time.Time, entries []runfolder.IndexEntry) string {
	t.Helper()
	ticks := []time.Time{start, start.Add(time.Minute)}
	clock := func() time.Time {
		at := ticks[0]
		if len(ticks) > 1 {
			ticks = ticks[1:]
		}
		return at
	}
	w, err := runfolder.NewStarter(root, clock, rand.Reader).Start("suite", runfolder.Meta{Kind: runfolder.KindSuite})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteIndex(entries); err != nil {
		t.Fatal(err)
	}
	if err := w.Finalize(runfolder.OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	return w.ID()
}

func seedStatsStore(t *testing.T) {
	root := runsStorageDir(t)
	for i, outcome := range []runfolder.Outcome{runfolder.OutcomePassed, runfolder.OutcomePassed, runfolder.OutcomeFailed} {
		seedRecordedRun(t, root, "a", runsEpoch.Add(time.Duration(i)*time.Hour), 2*time.Second, outcome, runfolder.Meta{TestKey: "flows/a.yml"})
	}
	seedRecordedRun(t, root, "c", runsEpoch, time.Second, runfolder.OutcomePassed, runfolder.Meta{ScriptPath: "/abs/c.yml"})
	seedSuiteRun(t, root, runsEpoch.Add(5*time.Hour), []runfolder.IndexEntry{
		{Path: "x.yml", TestKey: "flows/x.yml", Status: runfolder.ResultPassed, Flaky: true},
		{Path: "a.yml", TestKey: "flows/a.yml", Status: runfolder.ResultPassed},
	})
	seedRecordedRun(t, root, "cancelled", runsEpoch, time.Second, runfolder.OutcomeCancelled, runfolder.Meta{TestKey: "flows/a.yml"})
}

func TestRunsStatsTextSortedByFlakyRateThenFailures(t *testing.T) {
	seedStatsStore(t)

	code, stdout, stderr := execRuns(t, "stats")

	if code != 0 || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d:\n%s", len(lines), stdout)
	}
	wantOrder := []string{"flows/x.yml", "flows/a.yml", "/abs/c.yml"}
	for i, key := range wantOrder {
		if !strings.HasPrefix(lines[i+1], key) {
			t.Errorf("row %d = %q, want key %s", i, lines[i+1], key)
		}
	}
	if !strings.Contains(lines[1], "100.0%") || !strings.HasSuffix(lines[1], "K") {
		t.Errorf("flaky row = %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "PFPP") {
		t.Errorf("flows/a.yml row = %q", lines[2])
	}
}

func TestRunsStatsFilters(t *testing.T) {
	seedStatsStore(t)

	_, byTest, _ := execRuns(t, "stats", "--test", "flows/a.yml")
	if rows := strings.Count(byTest, "\n") - 1; rows != 1 || !strings.Contains(byTest, "flows/a.yml") {
		t.Errorf("--test output:\n%s", byTest)
	}

	_, last, _ := execRuns(t, "stats", "--test", "flows/a.yml", "--last", "2", "--json")
	var doc runStatsJSON
	if err := json.Unmarshal([]byte(last), &doc); err != nil || len(doc.Tests) != 1 || doc.Tests[0].Runs != 2 {
		t.Errorf("--last json = %s, err = %v", last, err)
	}

	_, since, _ := execRuns(t, "stats", "--since", "2026-03-01T13:00:00Z", "--json")
	doc = runStatsJSON{}
	if err := json.Unmarshal([]byte(since), &doc); err != nil || len(doc.Tests) != 2 {
		t.Errorf("--since json = %s, err = %v", since, err)
	}
}

func TestRunsStatsJSON(t *testing.T) {
	seedStatsStore(t)

	_, stdout, _ := execRuns(t, "stats", "--json")

	var doc runStatsJSON
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if len(doc.Tests) != 3 || doc.Skipped != 0 {
		t.Fatalf("doc = %+v", doc)
	}
	a := doc.Tests[1]
	if a.TestKey != "flows/a.yml" || a.Runs != 4 || a.Passed != 3 || a.Failed != 1 || a.Flaky != 0 ||
		a.DurationP50Ms != 2000 || len(a.LastOutcomes) != 4 {
		t.Errorf("flows/a.yml = %+v", a)
	}
	x := doc.Tests[0]
	if x.TestKey != "flows/x.yml" || x.Flaky != 1 || x.FlakyRate != 1 || x.DurationP50Ms != 0 {
		t.Errorf("flows/x.yml = %+v", x)
	}
}

func TestRunsStatsEmptyStoreAndSkippedFooter(t *testing.T) {
	root := runsStorageDir(t)
	_, empty, _ := execRuns(t, "stats")
	if !strings.Contains(empty, "No finished runs found.") {
		t.Errorf("empty output = %q", empty)
	}
	_, emptyJSON, _ := execRuns(t, "stats", "--json")
	if !strings.Contains(emptyJSON, `"tests": []`) {
		t.Errorf("empty json = %s", emptyJSON)
	}
	writeUnreadableRun(t, root)
	_, withSkipped, _ := execRuns(t, "stats")
	if !strings.Contains(withSkipped, "1 run record could not be read and was skipped.") {
		t.Errorf("output = %q", withSkipped)
	}
}

func TestRunsStatsMisuseExitsTwo(t *testing.T) {
	runsStorageDir(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"zero last", []string{"stats", "--last", "0"}, "must be a whole number of at least 1"},
		{"text last", []string{"stats", "--last", "many"}, "must be a whole number of at least 1"},
		{"bad since", []string{"stats", "--since", "2026/03/01"}, "neither an RFC 3339 timestamp nor a YYYY-MM-DD date"},
		{"unknown flag", []string{"stats", "--keep", "2"}, `does not accept "--keep"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := execRuns(t, tt.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
		})
	}
}
