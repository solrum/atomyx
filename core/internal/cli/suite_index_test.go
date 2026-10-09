package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shutdown"
)

func readIndex(t *testing.T, runDir string) runfolder.Index {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(runDir, runfolder.IndexFileName))
	if err != nil {
		t.Fatalf("the suite wrote no index: %v", err)
	}
	var index runfolder.Index
	if err := json.Unmarshal(body, &index); err != nil {
		t.Fatal(err)
	}
	return index
}

func rawMeta(t *testing.T, runDir string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(runDir, runfolder.MetaFileName))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestSuiteIndexOfAPlainSuiteListsEveryEntryWithOneAttempt(t *testing.T) {
	writeSuiteFlows(t)
	run := runRetrySuite(t, screenshotMockFactory{}, nil, map[string]FlagValue{"--glob": []string{"*.yml"}}, nil)

	index := readIndex(t, run.runDir)

	if index.SchemaVersion != runfolder.IndexSchemaVersion || index.RunID != filepath.Base(run.runDir) {
		t.Errorf("index header = %d %q, want version %d and the run id", index.SchemaVersion, index.RunID, runfolder.IndexSchemaVersion)
	}
	want := []struct {
		file   string
		status runfolder.Result
	}{{"a_fail.yml", runfolder.ResultFailed}, {"b_pass.yml", runfolder.ResultPassed}, {"c_fail.yml", runfolder.ResultFailed}}
	if len(index.Entries) != len(want) {
		t.Fatalf("entries = %+v, want %d", index.Entries, len(want))
	}
	for i, w := range want {
		entry := index.Entries[i]
		if !strings.HasSuffix(entry.Path, w.file) || entry.Status != w.status || entry.Flaky || entry.Quarantined {
			t.Errorf("entry %d = %+v, want %s %s", i, entry, w.file, w.status)
		}
		if len(entry.Attempts) != 1 || entry.Attempts[0].Number != 1 || entry.Attempts[0].Result != w.status {
			t.Errorf("entry %d attempts = %+v, want one attempt with the entry's result", i, entry.Attempts)
		}
	}
	meta := rawMeta(t, run.runDir)
	for _, key := range []string{"flakyEntries", "quarantinedFailures"} {
		if _, present := meta[key]; present {
			t.Errorf("meta.json carries %s although it is zero", key)
		}
	}
}

func TestSuiteIndexRecordsAFlakyEntryItsAttemptsAndVideos(t *testing.T) {
	writeReadyFlow(t)
	factory := &flakyFactory{}
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	rec.onStart = func() {
		if rec.starts == 2 {
			factory.show()
		}
	}
	run := runRetrySuite(t, factory, rec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "2", "--video": true}, nil)

	index := readIndex(t, run.runDir)

	entry := index.Entries[0]
	if entry.Status != runfolder.ResultPassed || !entry.Flaky || len(entry.Attempts) != 2 {
		t.Fatalf("entry = %+v, want a flaky pass with two attempts", entry)
	}
	if entry.Attempts[0].Result != runfolder.ResultFailed || entry.Attempts[1].Result != runfolder.ResultPassed {
		t.Errorf("attempt results = %+v", entry.Attempts)
	}
	if !reflect.DeepEqual(entry.Attempts[0].Videos, []string{"video-entry-1-1.mp4"}) || len(entry.Attempts[1].Videos) != 0 {
		t.Errorf("attempt videos = %+v, want the failed attempt's video only", entry.Attempts)
	}
	if _, err := os.Stat(filepath.Join(runfolder.ArtifactsDir(run.runDir), entry.Attempts[0].Videos[0])); err != nil {
		t.Errorf("the indexed video is not in artifacts: %v", err)
	}
	meta := rawMeta(t, run.runDir)
	if meta["flakyEntries"] != float64(1) {
		t.Errorf("meta flakyEntries = %v, want 1", meta["flakyEntries"])
	}
	if _, present := meta["quarantinedFailures"]; present {
		t.Error("meta carries quarantinedFailures although it is zero")
	}
}

func TestSuiteIndexRecordsAQuarantinedFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "b_pass.yml", "- sleep: 1\n")
	writeFile(t, dir, "atomyx.config.yaml", suiteConfig("    quarantine: [\"a_*.yml\"]\n"))
	name := "nightly"
	run := runRetrySuite(t, screenshotMockFactory{}, nil, map[string]FlagValue{"--config": filepath.Join(dir, "atomyx.config.yaml")}, &name)

	index := readIndex(t, run.runDir)

	if !index.Entries[0].Quarantined || index.Entries[0].Status != runfolder.ResultFailed || index.Entries[1].Quarantined {
		t.Errorf("entries = %+v, want only the first quarantined and failed", index.Entries)
	}
	meta := rawMeta(t, run.runDir)
	if meta["quarantinedFailures"] != float64(1) || meta["result"] != "passed" {
		t.Errorf("meta = %v, want one quarantined failure and result passed", meta)
	}
	if _, present := meta["flakyEntries"]; present {
		t.Error("meta carries flakyEntries although it is zero")
	}
}

func TestSuiteIndexOfACanceledSuiteListsTheEntriesThatRan(t *testing.T) {
	writeSuiteFlows(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	rec.onStart = func() { cancel(shutdown.InterruptCause(syscall.SIGINT)) }
	root := t.TempDir()
	var stderr bytes.Buffer

	RunSuite(ctx, suiteDeps(rec, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report-dir": root, "--video": true,
	}, nil)

	index := readIndex(t, singleRunDir(t, root))
	if len(index.Entries) != 1 || !strings.HasSuffix(index.Entries[0].Path, "a_fail.yml") || len(index.Entries[0].Attempts) != 1 {
		t.Errorf("entries = %+v, want only the entry that started", index.Entries)
	}
}

func TestSuiteReportAndIndexAgreeOnEntriesAttemptsAndVideos(t *testing.T) {
	writeReadyFlow(t)
	factory := &flakyFactory{}
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4", ".tmp-b.mp4"}}
	rec.onStart = func() {
		if rec.starts == 3 {
			factory.show()
		}
	}
	run := runRetrySuite(t, factory, rec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "2", "--video": true}, nil)
	index := readIndex(t, run.runDir)

	var decoded struct {
		Result struct {
			Entries []struct {
				Status      string   `json:"status"`
				Flaky       bool     `json:"flaky"`
				Quarantined bool     `json:"quarantined"`
				Videos      []string `json:"videos"`
				Attempts    []struct {
					Number int      `json:"number"`
					Status string   `json:"status"`
					Videos []string `json:"videos"`
				} `json:"attempts"`
			} `json:"entries"`
		} `json:"result"`
	}
	if err := json.Unmarshal(findReportJSON(t, run.root), &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Result.Entries) != len(index.Entries) {
		t.Fatalf("report has %d entries, index has %d", len(decoded.Result.Entries), len(index.Entries))
	}
	for i, got := range decoded.Result.Entries {
		want := index.Entries[i]
		if got.Status != string(want.Status) || got.Flaky != want.Flaky || got.Quarantined != want.Quarantined || len(got.Attempts) != len(want.Attempts) {
			t.Fatalf("entry %d: report %+v, index %+v", i, got, want)
		}
		var allVideos []string
		for j, attempt := range got.Attempts {
			if attempt.Number != want.Attempts[j].Number || attempt.Status != string(want.Attempts[j].Result) || !reflect.DeepEqual(attempt.Videos, want.Attempts[j].Videos) {
				t.Errorf("entry %d attempt %d: report %+v, index %+v", i, j, attempt, want.Attempts[j])
			}
			allVideos = append(allVideos, want.Attempts[j].Videos...)
		}
		if !reflect.DeepEqual(got.Videos, allVideos) {
			t.Errorf("entry %d videos: report %v, index attempts %v", i, got.Videos, allVideos)
		}
	}
}
