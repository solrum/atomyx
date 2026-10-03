package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// remoteGuard bounds every wait on a handle, so a hang fails by assertion
// before the test binary's own timeout.
const remoteGuard = 10 * time.Second

func dialRemote(t *testing.T, path string) *session.RemoteHandle {
	t.Helper()
	return dialRemoteWith(t, session.RemoteConfig{Path: path, Client: "viewer"})
}

func dialRemoteWith(t *testing.T, cfg session.RemoteConfig) *session.RemoteHandle {
	t.Helper()
	h, err := session.DialRemote(context.Background(), cfg, androidDesc("A"))
	if err != nil {
		t.Fatalf("DialRemote() error = %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// awaitObservation reads the feed until an observation of kind arrives
// whose revision is at least minRevision.
func awaitObservation(t *testing.T, feed <-chan session.Observation, kind session.ObservationKind, minRevision uint64) session.Observation {
	t.Helper()
	guard := time.After(remoteGuard)
	for {
		select {
		case o, open := <-feed:
			if !open {
				t.Fatalf("feed closed before kind %d reached revision %d", kind, minRevision)
			}
			if o.Kind == kind && o.Revision >= minRevision {
				return o
			}
		case <-guard:
			t.Fatalf("no observation of kind %d at revision %d within %s", kind, minRevision, remoteGuard)
		}
	}
}

func TestRemoteHandle_FollowsTheHoldersStepsAndCapturesWithSecretsMasked(t *testing.T) {
	s, mock, path := socketHolder(t)
	h := dialRemote(t, path)
	feed, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if snap := h.Latest(); snap.HasTree || snap.HasScreenshot {
		t.Fatalf("Latest() before any capture = %+v, want nothing", snap)
	}

	s.PublishStep(stepLine(1))
	awaitObservation(t, feed, session.ObservedStep, 1)
	mock.StageHierarchy(secureTree(typedPassword))
	image := encodedPNG(t, 430, 932)
	mock.ScreenshotData = image
	if _, err := s.Orchestra().Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orchestra().Screenshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := s.Latest()
	awaitObservation(t, feed, session.ObservedTree, want.TreeRevision)
	awaitObservation(t, feed, session.ObservedScreenshot, want.ScreenshotRevision)

	got := h.Latest()
	if !got.HasTree || got.TreeRevision != want.TreeRevision || got.ScreenshotRevision != want.ScreenshotRevision {
		t.Fatalf("Latest() = tree revision %d, screenshot revision %d, want %d and %d",
			got.TreeRevision, got.ScreenshotRevision, want.TreeRevision, want.ScreenshotRevision)
	}
	if text := got.Tree.Children[1].Attributes["text"]; text == typedPassword || text == "" {
		t.Fatalf("password text = %q, want the mask", text)
	}
	if got.ScreenshotFormat != session.ImagePNG || string(got.Screenshot) != string(image) {
		t.Fatalf("screenshot = format %q, %d bytes, want the holder's png", got.ScreenshotFormat, len(got.Screenshot))
	}
	lines, gap, err := h.StepsSince("", 0)
	if err != nil || gap || len(lines) != 1 || lines[0].Seq != 1 || lines[0].RunID != runA {
		t.Fatalf("StepsSince() = %+v, gap %t, %v, want step 1 of %s", lines, gap, err, runA)
	}
	info := h.Info()
	if info.State != session.StateRunning || info.Holder.Command != "atomyx run" || info.Device.ID != "A" {
		t.Fatalf("Info() = %+v, want the running holder of device A", info)
	}
}

func TestRemoteHandle_EachConnectionStampsItsOwnGeneration(t *testing.T) {
	_, _, path := socketHolder(t)
	first := dialRemote(t, path)
	second := dialRemote(t, path)

	if first.Info().Generation == 0 || first.Info().Generation == second.Info().Generation {
		t.Fatalf("generations = %d and %d, want two different non-zero values", first.Info().Generation, second.Info().Generation)
	}
	if first.Latest().Generation != first.Info().Generation {
		t.Fatalf("Latest().Generation = %d, want the handle's %d", first.Latest().Generation, first.Info().Generation)
	}
}

func TestRemoteHandle_FreshTreeIsRefusedWhileTheHolderRunsAndSucceedsWhileItIsIdle(t *testing.T) {
	s, mock, path := socketHolder(t)
	h := dialRemote(t, path)
	ctx := context.Background()
	for range 2 {
		mock.StageHierarchy(secureTree(typedPassword))
	}
	mock.ScreenshotData = encodedPNG(t, 430, 932)

	if _, err := h.FreshTree(ctx); codeOfRemote(err) != diag.CodeHolderBusy {
		t.Fatalf("FreshTree() while running: %v, want %s", err, diag.CodeHolderBusy)
	}

	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	snap, err := h.FreshTree(ctx)
	if err != nil {
		t.Fatalf("FreshTree() while idle: %v", err)
	}
	if !snap.HasTree || !snap.HasScreenshot || !snap.Paired || snap.Generation != h.Info().Generation {
		t.Fatalf("FreshTree() = %+v, want a paired capture stamped with the handle's generation", snap)
	}
	if text := snap.Tree.Children[1].Attributes["text"]; text == typedPassword {
		t.Fatal("FreshTree() returned the typed secret")
	}
	if latest := h.Latest(); latest.TreeRevision != snap.TreeRevision || !latest.Paired {
		t.Fatalf("Latest() = revision %d paired %t, want the fresh pair kept", latest.TreeRevision, latest.Paired)
	}
}

func TestRemoteHandle_ASubscriberThatStopsReadingIsDroppedAndTheHandleKeepsFollowing(t *testing.T) {
	s, _, path := socketHolder(t)
	h := dialRemote(t, path)
	slow, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	following, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	const published = 40
	for seq := int64(1); seq <= published; seq++ {
		s.PublishStep(stepLine(seq))
	}
	awaitObservation(t, following, session.ObservedStep, published)

	var last session.Observation
	guard := time.After(remoteGuard)
drain:
	for {
		select {
		case o, open := <-slow:
			if !open {
				break drain
			}
			last = o
		case <-guard:
			t.Fatal("the slow subscriber was never closed")
		}
	}
	if last.Kind != session.ObservedDropped {
		t.Fatalf("last observation = %+v, want ObservedDropped", last)
	}
}

func TestRemoteHandle_AHolderThatClosesEndsEverySubscriptionAndTheState(t *testing.T) {
	s, _, path := socketHolder(t)
	h := dialRemote(t, path)
	feed, err := h.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	guard := time.After(remoteGuard)
	var last session.Observation
	for open := true; open; {
		select {
		case o, ok := <-feed:
			open = ok
			if ok {
				last = o
			}
		case <-guard:
			t.Fatal("the subscription never ended after the holder closed")
		}
	}
	if last.Kind != session.ObservedClosed {
		t.Fatalf("last observation = %+v, want ObservedClosed", last)
	}
	select {
	case <-h.Done():
	case <-guard:
		t.Fatal("Done() never closed after the holder closed")
	}
	if state := h.Info().State; state != session.StateClosed {
		t.Fatalf("Info().State = %q, want closed", state)
	}
	if _, err := h.Subscribe(context.Background()); !errors.Is(err, session.ErrRemoteClosed) {
		t.Fatalf("Subscribe() after the holder closed: %v, want ErrRemoteClosed", err)
	}
}

// blockingRefresher parks the holder's fresh capture until released, so a
// test can hold the viewer's connection busy while the holder keeps
// publishing.
type blockingRefresher struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingRefresher) FreshTree(context.Context) (session.Snapshot, error) {
	close(r.entered)
	<-r.release
	return session.Snapshot{}, nil
}

func TestRemoteHandle_ResumesAfterTheLastStoredLineWhenNoticesWereDropped(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	factory := &recordingFactory{events: &eventLog{}}
	refresher := &blockingRefresher{entered: make(chan struct{}), release: make(chan struct{})}
	s, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A"), SocketRefresher: refresher})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h := dialRemoteWith(t, session.RemoteConfig{Path: socketPathFor(m), Client: "viewer", NoticeBuffer: 1})
	feed, err := h.SubscribeSince(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	freshDone := make(chan struct{})
	go func() {
		defer close(freshDone)
		_, _ = h.FreshTree(context.Background())
	}()
	select {
	case <-refresher.entered:
	case <-time.After(remoteGuard):
		t.Fatal("the holder never started the fresh capture")
	}
	factory.drivers[0].StageHierarchy(rootTree("r"))
	if _, err := s.Orchestra().Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	const published = 5
	for seq := int64(1); seq <= published; seq++ {
		s.PublishStep(stepLine(seq))
	}
	close(refresher.release)
	<-freshDone

	awaitObservation(t, feed, session.ObservedStep, published)
	lines, gap, err := h.StepsSince("", 0)
	if err != nil || gap || len(lines) != published {
		t.Fatalf("StepsSince() = %d lines, gap %t, %v, want %d lines and no gap", len(lines), gap, err, published)
	}
	for i, line := range lines {
		if line.Seq != int64(i+1) {
			t.Fatalf("line %d has seq %d, want %d", i, line.Seq, i+1)
		}
	}
	attached := 0
	for _, entry := range s.Interventions() {
		if entry.Kind == "viewerAttached" && strings.HasPrefix(entry.Client, "viewer") {
			attached++
		}
	}
	if attached < 2 {
		t.Fatalf("viewerAttached entries = %d, want the handle to have reconnected once", attached)
	}
}
