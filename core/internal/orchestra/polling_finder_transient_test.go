package orchestra

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

func transientReadError() error {
	return diag.New(diag.CodeAgentNotReady, "the agent could not read the screen yet", "retry", errors.New("snapshot failed"))
}

func readyTree() wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{node("target", "Ready", "0,0,50,50")},
	}
}

func TestWaitFor_TransientReadErrorThenMatchSucceeds(t *testing.T) {
	d := mock.New()
	d.StageHierarchyError(transientReadError()).StageHierarchy(readyTree())
	o, _ := newTestOrchestra(d)

	results, err := o.WaitFor(context.Background(), Selector{Text: lit("Ready")}, WaitOptions{TimeoutMs: 1000, PollIntervalMs: 10})
	if err != nil {
		t.Fatalf("WaitFor returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestWaitFor_OnlyTransientErrorsTimeOutNamingLastError(t *testing.T) {
	d := mock.New()
	for i := 0; i < 10; i++ {
		d.StageHierarchyError(transientReadError())
	}
	o, _ := newTestOrchestra(d)

	_, err := o.WaitFor(context.Background(), Selector{Text: lit("Ready")}, WaitOptions{TimeoutMs: 100, PollIntervalMs: 50})
	var timeoutErr *FindTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("got %v, want *FindTimeoutError", err)
	}
	if timeoutErr.PollCount != 3 {
		t.Fatalf("PollCount = %d, want 3", timeoutErr.PollCount)
	}
	if !strings.Contains(err.Error(), "could not read the screen yet") {
		t.Fatalf("timeout message %q does not name the last read error", err.Error())
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("code = %q, want %q", code, diag.CodeToolTimeout)
	}
}

func TestWaitFor_RecoveredReadClearsLastError(t *testing.T) {
	d := mock.New()
	d.StageHierarchyError(transientReadError()).StageHierarchy(node("root", "", "0,0,10,10"))
	o, _ := newTestOrchestra(d)

	_, err := o.WaitFor(context.Background(), Selector{Text: lit("Ready")}, WaitOptions{TimeoutMs: 50, PollIntervalMs: 50})
	var timeoutErr *FindTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("got %v, want *FindTimeoutError", err)
	}
	if timeoutErr.LastReadErr != nil {
		t.Fatalf("LastReadErr = %v, want nil after a successful read", timeoutErr.LastReadErr)
	}
}

func TestWaitFor_NonTransientErrorEndsWaitAtOnce(t *testing.T) {
	permanent := diag.New(diag.CodeAgentRejected, "request rejected", "fix it", errors.New("rejected"))
	d := mock.New()
	d.StageHierarchyError(permanent).StageHierarchy(readyTree())
	o, clock := newTestOrchestra(d)

	_, err := o.WaitFor(context.Background(), Selector{Text: lit("Ready")}, WaitOptions{TimeoutMs: 1000, PollIntervalMs: 10})
	if !errors.Is(err, permanent) {
		t.Fatalf("got %v, want the permanent error", err)
	}
	if clock.NowMs() != 0 {
		t.Fatalf("clock advanced to %d, want the wait to end without sleeping", clock.NowMs())
	}
}
