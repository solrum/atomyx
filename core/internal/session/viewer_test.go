package session_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
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

func TestSession_InfoReportsTheHolderRoleAndState(t *testing.T) {
	m := newManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), baseDeps(t, device.NewMockDriverFactory(), m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if info.Role != "cli" || info.Holder.Command != "atomyx run" || info.Device.ID != "A" {
		t.Fatalf("Info() = %+v, want the opener's role, command and device", info)
	}
	if err := s.SetRunning("run-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != "running (run open)" {
		t.Fatalf("state = %q, want %q", got, "running (run open)")
	}
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != "idle" {
		t.Fatalf("state = %q, want idle", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().State; got != "closed" {
		t.Fatalf("state = %q, want closed", got)
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
	if got := <-ch; got.Kind != session.ObservedClosed {
		t.Fatalf("a subscription got %+v at Close, want ObservedClosed", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("a subscription survived Close")
	}
}

type plainFactory struct{ builds int }

func (f *plainFactory) Build(context.Context, device.Descriptor) (device.Built, error) {
	f.builds++
	return device.Built{}, errors.New("must not build")
}

func TestOpen_RefusesAFactoryThatCannotWrapItsDriverAndHoldsNothing(t *testing.T) {
	factory := &plainFactory{}
	m := newManager(t, clock.SystemClock{})

	_, err := session.Open(context.Background(), baseDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")})
	if !errors.Is(err, session.ErrNotWrappable) {
		t.Fatalf("Open() error = %v, want ErrNotWrappable", err)
	}
	if factory.builds != 0 {
		t.Fatalf("the factory built %d times, want none", factory.builds)
	}
	status, err := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil || status.Held {
		t.Fatalf("Probe() = %+v, %v, want the device free", status, err)
	}
}
