package session_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

func collectKinds(t *testing.T, ch <-chan session.Observation) []session.ObservationKind {
	t.Helper()
	var kinds []session.ObservationKind
	for {
		select {
		case o, ok := <-ch:
			if !ok {
				return kinds
			}
			kinds = append(kinds, o.Kind)
		default:
			return kinds
		}
	}
}

func TestFreshTree_CapturesAPairedHighlightableSnapshotWithMaskedSecrets(t *testing.T) {
	s, mock := openMock(t)
	mock.StageHierarchy(secureTree("hunter2"))
	mock.ScreenshotData = encodedPNG(t, 860, 1864)

	snap, err := s.FreshTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Paired || !snap.Highlightable || snap.Scale != 2 {
		t.Fatalf("paired = %v, highlightable = %v, scale = %v, want a paired pair at scale 2", snap.Paired, snap.Highlightable, snap.Scale)
	}
	if snap.TreeEpoch != snap.ScreenshotEpoch {
		t.Fatalf("epochs %d and %d differ", snap.TreeEpoch, snap.ScreenshotEpoch)
	}
	if got := snap.Tree.Children[1].Attributes["text"]; got != orchestra.SecureMask {
		t.Fatalf("password text = %q, want the mask", got)
	}
}

func TestFreshTree_ReadsTheScreenSizeOncePerActionEpoch(t *testing.T) {
	s, mock := openMock(t)
	ctx := context.Background()
	for range 3 {
		mock.StageHierarchy(rootTree("r"))
	}
	mock.ScreenshotData = encodedPNG(t, 430, 932)
	events, err := s.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.FreshTree(ctx); err != nil {
		t.Fatal(err)
	}
	want := []session.ObservationKind{session.ObservedTree, session.ObservedScreenshot, session.ObservedScreenSize}
	if got := collectKinds(t, events); !equalKinds(got, want) {
		t.Fatalf("first capture published %v, want %v", got, want)
	}

	if _, err := s.FreshTree(ctx); err != nil {
		t.Fatal(err)
	}
	if got := collectKinds(t, events); !equalKinds(got, want[:2]) {
		t.Fatalf("second capture in the same epoch published %v, want %v", got, want[:2])
	}

	if err := s.Built().Driver.Tap(ctx, wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	collectKinds(t, events)
	if _, err := s.FreshTree(ctx); err != nil {
		t.Fatal(err)
	}
	if got := collectKinds(t, events); !equalKinds(got, want) {
		t.Fatalf("capture after an action published %v, want %v", got, want)
	}
}

func equalKinds(a, b []session.ObservationKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFreshTree_PairsACaptureTheRunnerLeftUnpaired(t *testing.T) {
	s, mock := openMock(t)
	ctx := context.Background()
	mock.StageHierarchy(rootTree("old"))
	mock.StageHierarchy(rootTree("new"))
	mock.ScreenshotData = encodedPNG(t, 430, 932)
	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Built().Driver.Tap(ctx, wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Screenshot(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Latest().Paired {
		t.Fatal("the runner's own captures were already paired, so the test proves nothing")
	}

	snap, err := s.FreshTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Paired || snap.Tree.Attributes["id"] != "new" {
		t.Fatalf("paired = %v, tree = %v, want the new tree paired", snap.Paired, snap.Tree.Attributes)
	}
}

func openWithGate(t *testing.T) (*session.Session, *session.ActivityGate) {
	t.Helper()
	gate := session.NewActivityGate()
	s, err := session.Open(context.Background(), session.Deps{Factory: device.NewMockDriverFactory()}, session.Request{Descriptor: androidDesc("A"), Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	mock, _ := driver.As[*mockdriver.Driver](s.Built().Driver)
	mock.StageHierarchy(rootTree("r"))
	if s.Gate() != gate {
		t.Fatal("the session ignored the gate it was given")
	}
	return s, gate
}

func TestFreshTree_WaitsWhileAToolCallHoldsTheGate(t *testing.T) {
	s, gate := openWithGate(t)
	events, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	endCall, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.FreshTree(context.Background())
		done <- err
	}()
	awaitWaiting(gate, 1)
	if got := collectKinds(t, events); len(got) != 0 {
		t.Fatalf("FreshTree captured %v while a tool call held the gate", got)
	}

	endCall()
	if err := <-done; err != nil {
		t.Fatalf("FreshTree after the call ended: %v", err)
	}
	if got := collectKinds(t, events); len(got) == 0 {
		t.Fatal("FreshTree captured nothing after the gate opened")
	}
}

func TestFreshTree_HonorsContextCancellationWhileWaiting(t *testing.T) {
	s, gate := openWithGate(t)
	events, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	endCall, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer endCall()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.FreshTree(ctx)
		done <- err
	}()
	awaitWaiting(gate, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("FreshTree error = %v, want context.Canceled", err)
	}
	if got := collectKinds(t, events); len(got) != 0 {
		t.Fatalf("a canceled FreshTree captured %v", got)
	}
}

func TestFreshTree_OnAClosedSessionReturnsErrClosed(t *testing.T) {
	s, _ := openMock(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FreshTree(context.Background()); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("FreshTree error = %v, want ErrClosed", err)
	}
}
