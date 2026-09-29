package device

import (
	"context"
	"sort"
	"testing"

	"dev.atomyx/internal/logging"
)

// capturingLogger records every Error call it receives, so a test can
// assert on what CompositeProbe logged without a real log sink.
type capturingLogger struct {
	errors []string
}

func (l *capturingLogger) Debug(string, logging.LogContext) {}
func (l *capturingLogger) Info(string, logging.LogContext)  {}
func (l *capturingLogger) Warn(string, logging.LogContext)  {}
func (l *capturingLogger) Error(msg string, _ error, _ logging.LogContext) {
	l.errors = append(l.errors, msg)
}
func (l *capturingLogger) Child(logging.LogContext) logging.Logger { return l }

type stubProbe struct {
	result []Descriptor
	panics bool
}

func (p stubProbe) Scan(context.Context) []Descriptor {
	if p.panics {
		panic("probe exploded")
	}
	return p.result
}

func TestCompositeProbe_Scan_ConcatenatesAllProbes(t *testing.T) {
	a := stubProbe{result: []Descriptor{{ID: "a"}}}
	b := stubProbe{result: []Descriptor{{ID: "b1"}, {ID: "b2"}}}

	got := NewCompositeProbe(nil, a, b).Scan(context.Background())
	if len(got) != 3 {
		t.Fatalf("Scan() = %+v, want 3 descriptors", got)
	}
	ids := make([]string, len(got))
	for i, d := range got {
		ids[i] = d.ID
	}
	sort.Strings(ids)
	want := []string{"a", "b1", "b2"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("Scan() ids = %v, want %v", ids, want)
		}
	}
}

func TestCompositeProbe_Scan_OnePanickingProbe_DoesNotHideTheOthers(t *testing.T) {
	broken := stubProbe{panics: true}
	healthy := stubProbe{result: []Descriptor{{ID: "ok"}}}

	got := NewCompositeProbe(nil, broken, healthy).Scan(context.Background())
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("Scan() = %+v, want [{ID: ok}]", got)
	}
}

func TestCompositeProbe_Scan_OnePanickingProbe_LogsThePanic(t *testing.T) {
	broken := stubProbe{panics: true}
	healthy := stubProbe{result: []Descriptor{{ID: "ok"}}}
	logger := &capturingLogger{}

	NewCompositeProbe(logger, broken, healthy).Scan(context.Background())

	if len(logger.errors) != 1 {
		t.Fatalf("logger.errors = %v, want exactly one entry for the broken probe", logger.errors)
	}
}

func TestCompositeProbe_Scan_NoProbes_ReturnsEmpty(t *testing.T) {
	got := NewCompositeProbe(nil).Scan(context.Background())
	if len(got) != 0 {
		t.Fatalf("Scan() = %+v, want empty", got)
	}
}

func TestStaticProbe_Scan_ReturnsSnapshotsInOrderThenEmpty(t *testing.T) {
	p := NewStaticProbe(
		[]Descriptor{{ID: "first"}},
		[]Descriptor{{ID: "second"}},
	)

	first := p.Scan(context.Background())
	if len(first) != 1 || first[0].ID != "first" {
		t.Fatalf("first Scan() = %+v, want [{ID: first}]", first)
	}
	second := p.Scan(context.Background())
	if len(second) != 1 || second[0].ID != "second" {
		t.Fatalf("second Scan() = %+v, want [{ID: second}]", second)
	}
	third := p.Scan(context.Background())
	if len(third) != 0 {
		t.Fatalf("third Scan() = %+v, want empty once the queue is exhausted", third)
	}
}
