package runfolder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTestKeyIsOmittedWhenEmptyAndRoundTripsWhenSet(t *testing.T) {
	bare, err := json.Marshal(Meta{SchemaVersion: 1, RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "testKey") {
		t.Fatalf("meta without a key encodes testKey: %s", bare)
	}
	bareEntry, err := json.Marshal(IndexEntry{Path: "a.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bareEntry), "testKey") {
		t.Fatalf("entry without a key encodes testKey: %s", bareEntry)
	}

	data, err := json.Marshal(Meta{RunID: "r", TestKey: "flows/a.yml"})
	if err != nil {
		t.Fatal(err)
	}
	var back Meta
	if err := json.Unmarshal(data, &back); err != nil || back.TestKey != "flows/a.yml" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}
