package runfolder

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func sampleIndexEntries() []IndexEntry {
	return []IndexEntry{
		{Path: "flows/a.yml", Status: ResultPassed, Attempts: []IndexAttempt{{Number: 1, Result: ResultPassed}}},
		{Path: "flows/b.yml", Status: ResultPassed, Flaky: true, Attempts: []IndexAttempt{
			{Number: 1, Result: ResultFailed, Videos: []string{"video-entry-2-1.mp4"}},
			{Number: 2, Result: ResultPassed},
		}},
		{Path: "flows/c.yml", Status: ResultFailed, Quarantined: true, Attempts: []IndexAttempt{
			{Number: 1, Result: ResultFailed, DeviceID: "emulator-5554", WorkerRunID: "20261002-070309-worker-abcd"},
		}},
	}
}

func TestIndexMatchesGolden(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.WriteIndex(sampleIndexEntries()); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(w.Dir(), IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "index-full.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte(w.ID()), []byte("RUN_ID")), want) {
		t.Fatalf("index.json = %s\nwant the golden %s", got, want)
	}
}

func TestIndexOmitsAbsentFieldsAndNeverEncodesNullEntries(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.WriteIndex(nil); err != nil {
		t.Fatal(err)
	}
	empty, _ := os.ReadFile(filepath.Join(w.Dir(), IndexFileName))
	if !strings.Contains(string(empty), `"entries": []`) {
		t.Errorf("an empty index = %s, want an empty entries array", empty)
	}

	plain, err := json.Marshal(IndexEntry{Path: "a.yml", Status: ResultPassed, Attempts: []IndexAttempt{{Number: 1, Result: ResultPassed}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"flaky"`, `"quarantined"`, `"videos"`, `"deviceId"`, `"workerRunId"`} {
		if bytes.Contains(plain, []byte(key)) {
			t.Errorf("a plain entry encoded %s: %s", key, plain)
		}
	}
}

func TestWriteIndexReplacesTheFileAtomicallyAndLeavesNoTemporaryFile(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.WriteIndex(sampleIndexEntries()[:1]); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteIndex(sampleIndexEntries()); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(w.Dir(), IndexFileName))
	var got Index
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != IndexSchemaVersion || got.RunID != w.ID() || len(got.Entries) != 3 {
		t.Errorf("index = %+v", got)
	}
	for _, name := range dirNames(t, w.Dir()) {
		if strings.HasSuffix(name, ".tmp") {
			t.Errorf("left %s behind", name)
		}
	}
}

func TestWriteIndexOnAFinalizedWriterFails(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteIndex(nil); !errors.Is(err, ErrFinalized) {
		t.Fatalf("WriteIndex after Finalize = %v, want ErrFinalized", err)
	}
}

func TestIndexSchemaMatchesJSONTags(t *testing.T) {
	defs := GenerateIndexJSONSchema()["definitions"].(map[string]any)
	types := map[string]reflect.Type{
		"RunIndex":     reflect.TypeOf(Index{}),
		"IndexEntry":   reflect.TypeOf(IndexEntry{}),
		"IndexAttempt": reflect.TypeOf(IndexAttempt{}),
	}
	if len(defs) != len(types) {
		t.Fatalf("schema has %d definitions, test covers %d", len(defs), len(types))
	}
	for name, typ := range types {
		def := defs[name].(map[string]any)
		var props []string
		for p := range def["properties"].(map[string]any) {
			props = append(props, p)
		}
		sort.Strings(props)
		var req []string
		if r, ok := def["required"].([]string); ok {
			req = append(req, r...)
		}
		sort.Strings(req)
		wantAll, wantReq := jsonFields(typ)
		if !reflect.DeepEqual(props, wantAll) {
			t.Errorf("%s properties = %v, json tags = %v", name, props, wantAll)
		}
		if !reflect.DeepEqual(req, wantReq) {
			t.Errorf("%s required = %v, tags without omitempty = %v", name, req, wantReq)
		}
	}
}
