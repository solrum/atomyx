package shard

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeLineRoundTripsEveryKind(t *testing.T) {
	failure := "assertion failed"
	lines := []Line{
		{Kind: LineReady, RunID: "run-1"},
		{Kind: LineEntry, RunID: "run-1", DeviceLost: true, Entry: &Entry{
			Path: "/flows/a.yml", Kind: "script", Status: "failed", Error: &failure, DurationMs: 40, Quarantined: true,
			Attempts: []Attempt{{Number: 1, Status: "failed", Error: &failure, DurationMs: 40, Videos: []string{"video-entry-1-1.mp4"}}},
		}},
		{Kind: LineError, Code: "DEVICE_HELD", Message: "held"},
	}
	for _, want := range lines {
		var buf bytes.Buffer
		if err := EncodeLine(&buf, want); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(buf.String(), "\n") || strings.Count(buf.String(), "\n") != 1 {
			t.Fatalf("encoded %q, want exactly one newline-ended line", buf.String())
		}
		got, err := DecodeLine(buf.Bytes())
		if err != nil {
			t.Fatalf("DecodeLine(%q) error = %v", buf.String(), err)
		}
		want.V = ProtocolVersion
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip = %+v, want %+v", got, want)
		}
	}
}

func TestDecodeLineRejectsWhatTheParentCannotTrust(t *testing.T) {
	cases := map[string]string{
		"other version":   `{"v":2,"kind":"ready"}`,
		"missing version": `{"kind":"ready"}`,
		"unknown kind":    `{"v":1,"kind":"progress"}`,
		"entry no entry":  `{"v":1,"kind":"entry"}`,
		"not json":        `starting up`,
	}
	for name, raw := range cases {
		if _, err := DecodeLine([]byte(raw)); err == nil {
			t.Errorf("%s: DecodeLine(%q) accepted the line", name, raw)
		}
	}
}
