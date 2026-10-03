package lease

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHolderJSON_OmitsSocketErrorUnlessSet(t *testing.T) {
	plain, err := json.Marshal(Holder{PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "socketError") {
		t.Fatalf("holder JSON %s carries socketError, want it omitted when empty", plain)
	}
	failed, err := json.Marshal(Holder{PID: 1, SocketError: "bind failed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(failed), `"socketError":"bind failed"`) {
		t.Fatalf("holder JSON %s lacks the socketError field", failed)
	}
}

func TestHolderJSON_ProcessStartedAtIsOptional(t *testing.T) {
	var legacy Holder
	if err := json.Unmarshal([]byte(`{"pid":7,"command":"atomyx run","role":"cli","startedAt":"2026-10-03T10:00:00Z"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.PID != 7 || legacy.ProcessStartedAt != nil {
		t.Fatalf("holder = %+v, want metadata without the field to read with no process start time", legacy)
	}
	plain, err := json.Marshal(Holder{PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "processStartedAt") {
		t.Fatalf("holder JSON %s carries processStartedAt, want it omitted when unset", plain)
	}
	started := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	data, err := json.Marshal(Holder{PID: 1, ProcessStartedAt: &started})
	if err != nil {
		t.Fatal(err)
	}
	var back Holder
	if err := json.Unmarshal(data, &back); err != nil || back.ProcessStartedAt == nil || !back.ProcessStartedAt.Equal(started) {
		t.Fatalf("round trip = %+v, %v, want %v", back, err, started)
	}
}
