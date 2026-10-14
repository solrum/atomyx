package runfolder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadIndexRoundTripsWriteIndex(t *testing.T) {
	w := startOne(t, t.TempDir())
	entries := sampleIndexEntries()
	entries[0].TestKey = "flows/a.yml"
	if err := w.WriteIndex(entries); err != nil {
		t.Fatal(err)
	}

	got, err := ReadIndex(w.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != IndexSchemaVersion || got.RunID != w.ID() {
		t.Fatalf("header = %d %q, want %d %q", got.SchemaVersion, got.RunID, IndexSchemaVersion, w.ID())
	}
	if !reflect.DeepEqual(got.Entries, entries) {
		t.Fatalf("entries = %+v\nwant %+v", got.Entries, entries)
	}
}

func TestReadIndexReportsAMissingFileAsNotExist(t *testing.T) {
	_, err := ReadIndex(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadIndexRejectsBadContent(t *testing.T) {
	tests := map[string]string{
		"malformed":      `{`,
		"future version": `{"schemaVersion":99,"runId":"r","entries":[]}`,
		"zero version":   `{"runId":"r","entries":[]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, IndexFileName), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadIndex(dir); err == nil {
				t.Fatal("ReadIndex accepted bad content")
			}
		})
	}
}

func TestReadIndexReadsNullEntriesAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, IndexFileName), []byte(`{"schemaVersion":1,"runId":"r","entries":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadIndex(dir)
	if err != nil || got.Entries == nil || len(got.Entries) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
