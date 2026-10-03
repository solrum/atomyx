package runfolder

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readInterventionFile(t *testing.T, w *Writer) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(w.Dir(), InterventionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
}

func TestAppendInterventionWritesOneJSONLinePerEntryInOrder(t *testing.T) {
	w := startOne(t, t.TempDir())
	entries := []InterventionLine{
		{Kind: "viewerAttached", AtMs: 1000, Client: "atomyx inspect"},
		{Kind: "viewerDetached", AtMs: 2500, Client: "atomyx inspect"},
	}
	for _, e := range entries {
		if err := w.AppendIntervention(e); err != nil {
			t.Fatal(err)
		}
	}

	lines := readInterventionFile(t, w)
	if len(lines) != len(entries) {
		t.Fatalf("got %d lines, want %d", len(lines), len(entries))
	}
	for i, raw := range lines {
		var got InterventionLine
		if err := json.Unmarshal([]byte(raw), &got); err != nil || got != entries[i] {
			t.Fatalf("line %d = %q (%v), want %+v", i, raw, err, entries[i])
		}
	}
	if lines[0] != `{"kind":"viewerAttached","atMs":1000,"client":"atomyx inspect"}` {
		t.Errorf("line 0 = %s, want the pinned field names and order", lines[0])
	}
}

func TestAppendInterventionStopsAtTheBoundAndRecordsOneMarker(t *testing.T) {
	w := startOne(t, t.TempDir())
	w.maxInterventionBytes = 100
	for range 20 {
		if err := w.AppendIntervention(InterventionLine{Kind: "viewerAttached", AtMs: 1, Client: "c"}); err != nil {
			t.Fatal(err)
		}
	}

	lines := readInterventionFile(t, w)
	last := lines[len(lines)-1]
	if !strings.Contains(last, `"truncated":true`) {
		t.Fatalf("last line = %s, want the truncation marker", last)
	}
	markers := 0
	for _, l := range lines {
		if strings.Contains(l, "truncated") {
			markers++
		}
	}
	if markers != 1 || len(lines) >= 20 {
		t.Fatalf("got %d lines with %d markers, want fewer lines and exactly one marker", len(lines), markers)
	}
}

func TestAppendInterventionAfterTheWriterClosedFails(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.AppendIntervention(InterventionLine{Kind: "viewerAttached"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("AppendIntervention() error = %v, want ErrClosed", err)
	}
}
