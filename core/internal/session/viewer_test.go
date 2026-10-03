package session_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

func openMock(t *testing.T) (*session.Session, *mockdriver.Driver) {
	t.Helper()
	s, err := session.Open(context.Background(), session.Deps{Factory: device.NewMockDriverFactory()}, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	mock, ok := driver.As[*mockdriver.Driver](s.Built().Driver)
	if !ok {
		t.Fatal("the built driver does not reach the mock")
	}
	return s, mock
}

func TestSession_ViewersOnlySeeCapturesTheRunnerFetchedAndTheirEpochs(t *testing.T) {
	s, mock := openMock(t)
	ctx := context.Background()
	observed := s.Built().Driver.(session.ObservedDriver)
	mock.StageHierarchy(rootTree("before"))
	mock.StageHierarchy(rootTree("after"))
	mock.ScreenshotData = encodedPNG(t, 860, 1864)

	if snap := s.Latest(); snap.HasTree || snap.HasScreenshot {
		t.Fatalf("a session nobody drove shows %+v, want nothing", snap)
	}

	beforeEpoch := observed.Epoch()
	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if err := observed.Tap(ctx, wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	afterEpoch := observed.Epoch()
	if _, err := s.Orchestra().Screenshot(ctx); err != nil {
		t.Fatal(err)
	}

	snap := s.Latest()
	if snap.Tree.Attributes["id"] != "before" || snap.TreeEpoch != beforeEpoch {
		t.Fatalf("tree %v at epoch %d, want the fetched tree at epoch %d", snap.Tree.Attributes, snap.TreeEpoch, beforeEpoch)
	}
	if snap.ScreenshotEpoch != afterEpoch || snap.ScreenshotFormat != session.ImagePNG {
		t.Fatalf("screenshot at epoch %d in %q, want epoch %d and png", snap.ScreenshotEpoch, snap.ScreenshotFormat, afterEpoch)
	}
	if snap.Paired || snap.Highlightable {
		t.Fatalf("a tree and a screenshot from different epochs report paired %v, highlightable %v", snap.Paired, snap.Highlightable)
	}

	if _, err := s.Orchestra().Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if snap := s.Latest(); !snap.Paired || snap.Tree.Attributes["id"] != "after" {
		t.Fatalf("after fetching the tree again in the screenshot's epoch, paired = %v, tree = %v", snap.Paired, snap.Tree.Attributes)
	}
}

func TestSession_ASubscriberThatNeverReadsDoesNotChangeTheRunnerCalls(t *testing.T) {
	bare := stagedMock()
	orchestraRun(t, bare)

	s, mock := openMock(t)
	stageRun(mock)
	if _, err := s.Subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	orchestraRun(t, s.Built().Driver)

	if !reflect.DeepEqual(callMethods(bare), callMethods(mock)) {
		t.Fatalf("calls with an unread subscriber = %v, without = %v", callMethods(mock), callMethods(bare))
	}
}

func TestSession_LatestMasksSecretsWhileTheOrchestraStillSeesThem(t *testing.T) {
	s, mock := openMock(t)
	mock.StageHierarchy(secureTree("hunter2"))
	ctx := context.Background()

	raw, err := s.Orchestra().Hierarchy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Children[1].Attributes["text"] != "hunter2" {
		t.Fatal("the Orchestra no longer sees the real value")
	}
	if got := s.Latest().Tree.Children[1].Attributes["text"]; got != orchestra.SecureMask {
		t.Fatalf("viewer text = %q, want the mask", got)
	}
}

func TestSession_InfoReportsTheHolderAndTheSessionsOwnState(t *testing.T) {
	m := newManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), baseDeps(t, device.NewMockDriverFactory(), m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if info.Holder.Role != "cli" || info.Holder.Command != "atomyx run" || info.Device.ID != "A" {
		t.Fatalf("Info() = %+v, want the opener's role, command and device", info)
	}
	if info.State != session.StateRunning {
		t.Fatalf("state = %q, want a freshly opened session to be running", info.State)
	}
	if info.Holder.IdleSince != nil {
		t.Fatalf("holder idleSince = %v, want unset while the session is running", info.Holder.IdleSince)
	}
	if err := s.SetRunning("run-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != session.StateRunning {
		t.Fatalf("state = %q, want running", got)
	}
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != session.StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != session.StateClosed {
		t.Fatalf("state = %q, want closed", got)
	}
}

func TestSession_InfoWithoutALeaseStartsRunningAndOnlyItsOwnerSetsItIdle(t *testing.T) {
	s, _ := openMock(t)
	if got := s.Info().State; got != session.StateRunning {
		t.Fatalf("state = %q, want running for an open session with no lease", got)
	}
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != session.StateIdle {
		t.Fatalf("state = %q, want idle after SetIdle with no lease", got)
	}
}

// lostMockFactory builds like the mock factory and hands its test the
// channel the session reads a lost control plane from.
type lostMockFactory struct{ lost chan error }

func (f lostMockFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	built, err := device.NewMockDriverFactory().Build(ctx, desc)
	built.Lost = f.lost
	return built, err
}

func (f lostMockFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func TestSession_InfoReportsLostAndThenClosed(t *testing.T) {
	lost := make(chan error, 1)
	s, err := session.Open(context.Background(), session.Deps{Factory: lostMockFactory{lost: lost}}, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	lost <- &device.ProcessExitError{Component: device.LostRunner, Cause: errors.New("signal: killed")}
	<-s.Lost()
	if got := s.Info().State; got != session.StateLost {
		t.Fatalf("state = %q, want lost", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != session.StateClosed {
		t.Fatalf("state = %q, want closed to win over lost", got)
	}
}

func TestSession_CloseEndsSubscriptions(t *testing.T) {
	s, _ := openMock(t)
	ch, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.Kind != session.ObservedState || got.State != session.StateClosed {
		t.Fatalf("a subscription got %+v at Close, want the closed state first", got)
	}
	if got := <-ch; got.Kind != session.ObservedClosed {
		t.Fatalf("a subscription got %+v at Close, want ObservedClosed", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("a subscription survived Close")
	}
}

func TestOpen_RefusesDepsWithoutAFactoryAndHoldsNothing(t *testing.T) {
	m := newManager(t, clock.SystemClock{})

	_, err := session.Open(context.Background(), baseDeps(t, nil, m), session.Request{Descriptor: androidDesc("A")})
	if !errors.Is(err, session.ErrNoFactory) {
		t.Fatalf("Open() error = %v, want ErrNoFactory", err)
	}
	status, err := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil || status.Held {
		t.Fatalf("Probe() = %+v, %v, want the device free", status, err)
	}
}

func TestSession_ALostDeviceIsAnnouncedAsAStateTransition(t *testing.T) {
	lost := make(chan error, 1)
	s, err := session.Open(context.Background(), session.Deps{Factory: lostMockFactory{lost: lost}}, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	feed, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	lost <- &device.ProcessExitError{Component: device.LostRunner, Cause: errors.New("signal: killed")}
	<-s.Lost()

	select {
	case got := <-feed:
		if got.Kind != session.ObservedState || got.State != session.StateLost {
			t.Fatalf("a subscription got %+v after the loss, want the lost state", got)
		}
	case <-time.After(remoteGuard):
		t.Fatalf("no lost state within %s", remoteGuard)
	}
}
