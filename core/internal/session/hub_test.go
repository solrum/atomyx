package session_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"reflect"
	"sync"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

func encodedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodedJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rootTree(id string) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{"id": id}}
}

func secureTree(secret string) wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root"},
		Children: []wire.TreeNodeWire{
			{Attributes: map[string]string{"id": "user", "text": "alice"}},
			{Attributes: map[string]string{"id": "password", "label": "Password", "text": secret, "value": secret, "secure": "true"}},
		},
	}
}

func TestHub_StartsEmptyAndKeepsOnlyTheLatestValueOfEachKind(t *testing.T) {
	hub := session.NewHub()
	if snap := hub.Latest(); snap.HasTree || snap.HasScreenshot || snap.Paired || snap.Highlightable || snap.Scale != 0 {
		t.Fatalf("empty hub Latest() = %+v, want nothing known", snap)
	}

	hub.ObserveTree(rootTree("one"), 2, true)
	hub.ObserveTree(rootTree("two"), 4, true)
	hub.ObserveScreenshot([]byte("first"), 4, true)
	hub.ObserveScreenshot([]byte("second"), 6, true)

	snap := hub.Latest()
	if snap.Tree.Attributes["id"] != "two" || snap.TreeEpoch != 4 || !snap.HasTree {
		t.Fatalf("tree = %v at epoch %d, want the second tree at epoch 4", snap.Tree.Attributes, snap.TreeEpoch)
	}
	if string(snap.Screenshot) != "second" || snap.ScreenshotEpoch != 6 || !snap.HasScreenshot {
		t.Fatalf("screenshot = %q at epoch %d, want the second at epoch 6", snap.Screenshot, snap.ScreenshotEpoch)
	}
}

func TestHub_PairedMeansTheTreeAndTheScreenshotShareAnEpoch(t *testing.T) {
	hub := session.NewHub()
	hub.ObserveTree(rootTree("a"), 2, true)
	if hub.Latest().Paired {
		t.Fatal("a tree alone reports paired")
	}
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 4, true)
	if hub.Latest().Paired {
		t.Fatal("a tree and a screenshot from different epochs report paired")
	}
	hub.ObserveTree(rootTree("b"), 4, true)
	if !hub.Latest().Paired {
		t.Fatal("a tree and a screenshot from one epoch report unpaired")
	}
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 6, true)
	if hub.Latest().Paired {
		t.Fatal("a newer screenshot from another epoch still reports paired")
	}
}

func TestHub_ScaleIsScreenshotWidthOverScreenWidth(t *testing.T) {
	tests := []struct {
		name       string
		image      []byte
		size       *wire.Size
		wantScale  float64
		wantFormat session.ImageFormat
	}{
		{"png at twice the point width", encodedPNG(t, 860, 1864), &wire.Size{Width: 430, Height: 932}, 2, session.ImagePNG},
		{"jpeg at the point width", encodedJPEG(t, 430, 932), &wire.Size{Width: 430, Height: 932}, 1, session.ImageJPEG},
		{"unknown screen size", encodedPNG(t, 860, 1864), nil, 0, session.ImagePNG},
		{"zero screen width", encodedPNG(t, 860, 1864), &wire.Size{}, 0, session.ImagePNG},
		{"bytes that are no image", []byte("not an image"), &wire.Size{Width: 430, Height: 932}, 0, session.ImageUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub := session.NewHub()
			hub.ObserveTree(rootTree("r"), 2, true)
			hub.ObserveScreenshot(tc.image, 2, true)
			if tc.size != nil {
				hub.ObserveScreenSize(*tc.size, 0, true)
			}
			snap := hub.Latest()
			if snap.Scale != tc.wantScale || snap.ScreenshotFormat != tc.wantFormat {
				t.Fatalf("scale = %v, format = %q, want %v and %q", snap.Scale, snap.ScreenshotFormat, tc.wantScale, tc.wantFormat)
			}
			if want := tc.wantScale > 0; snap.Highlightable != want {
				t.Fatalf("highlightable = %v, want %v for a paired capture", snap.Highlightable, want)
			}
		})
	}
}

func TestHub_AnUnpairedCaptureIsNeverHighlightableEvenWithAScale(t *testing.T) {
	hub := session.NewHub()
	hub.ObserveScreenSize(wire.Size{Width: 100, Height: 200}, 2, true)
	hub.ObserveScreenshot(encodedPNG(t, 200, 400), 4, true)
	hub.ObserveTree(rootTree("r"), 2, true)
	snap := hub.Latest()
	if snap.Scale != 2 || snap.Paired || snap.Highlightable {
		t.Fatalf("scale = %v, paired = %v, highlightable = %v, want a known scale but no pairing", snap.Scale, snap.Paired, snap.Highlightable)
	}
}

func TestHub_LatestMasksSecretsAndLeavesTheStoredTreeUntouched(t *testing.T) {
	hub := session.NewHub()
	tree := secureTree("hunter2")
	hub.ObserveTree(tree, 2, true)

	password := hub.Latest().Tree.Children[1].Attributes
	if password["text"] != orchestra.SecureMask || password["value"] != orchestra.SecureMask || password["label"] != "Password" {
		t.Fatalf("password node = %v, want text and value masked and the label kept", password)
	}
	if tree.Children[1].Attributes["text"] != "hunter2" {
		t.Fatal("masking changed the tree the driver returned")
	}
}

func TestHub_MasksOncePerStoredTreeAndSharesTheCopyAcrossReads(t *testing.T) {
	hub := session.NewHub()
	hub.ObserveTree(secureTree("hunter2"), 2, true)

	first := hub.Latest().Tree.Children[1].Attributes
	second := hub.Latest().Tree.Children[1].Attributes
	if reflect.ValueOf(first).Pointer() != reflect.ValueOf(second).Pointer() {
		t.Fatal("two reads of one stored tree built two masked copies")
	}

	hub.ObserveTree(secureTree("other"), 2, true)
	third := hub.Latest().Tree.Children[1].Attributes
	if reflect.ValueOf(first).Pointer() == reflect.ValueOf(third).Pointer() || third["text"] != orchestra.SecureMask {
		t.Fatalf("a new tree with the same epoch reused the old masked copy: %v", third)
	}
}

func TestHub_ConcurrentPublishersAndReadersShareTreeMapsWithoutARace(t *testing.T) {
	hub := session.NewHub()
	shared := secureTree("hunter2")
	const rounds = 200
	var wg sync.WaitGroup
	for p := range 4 {
		wg.Go(func() {
			for i := range rounds {
				hub.ObserveTree(shared, uint64(i), true)
				hub.ObserveScreenshot(encodedPNG(t, 4, 4), uint64(i), true)
				hub.ObserveScreenSize(wire.Size{Width: float64(p + 1), Height: 1}, uint64(i), true)
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range rounds {
				snap := hub.Latest()
				if snap.HasTree {
					for _, child := range snap.Tree.Children {
						for range child.Attributes {
						}
					}
				}
			}
		})
	}
	wg.Wait()
	if got := hub.Latest().Tree.Children[1].Attributes["text"]; got != orchestra.SecureMask {
		t.Fatalf("password text = %q after concurrent use, want the mask", got)
	}
}

func drain(ch <-chan session.Observation) (received int, closed bool) {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return received, true
			}
			received++
		default:
			return received, false
		}
	}
}

func drainObservations(ch <-chan session.Observation) (got []session.Observation, closed bool) {
	for {
		select {
		case o, ok := <-ch:
			if !ok {
				return got, true
			}
			got = append(got, o)
		default:
			return got, false
		}
	}
}

func TestHub_ASubscriberThatNeverReadsIsDroppedAndNeverBlocksThePublisher(t *testing.T) {
	hub := session.NewHub()
	slow, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	const publishes = 200
	healthyGot := 0
	for i := range publishes {
		hub.ObserveTree(rootTree("r"), uint64(i), true)
		got, _ := drain(healthy)
		healthyGot += got
	}

	if healthyGot != publishes {
		t.Fatalf("the healthy subscriber got %d of %d notifications", healthyGot, publishes)
	}
	kept, closed := drainObservations(slow)
	if !closed {
		t.Fatalf("the slow subscriber was not dropped after %d unread notifications", publishes)
	}
	if len(kept) == 0 || len(kept) >= publishes {
		t.Fatalf("the slow subscriber kept %d notifications, want a bounded buffer", len(kept))
	}
	if last := kept[len(kept)-1].Kind; last != session.ObservedDropped {
		t.Fatalf("the slow subscriber ended with kind %v, want ObservedDropped", last)
	}
	if hub.Latest().TreeEpoch != publishes-1 {
		t.Fatal("the hub does not hold the newest tree after dropping a subscriber")
	}
}

func TestHub_NotificationsCarryTheKindAndEpochOfTheCapture(t *testing.T) {
	hub := session.NewHub()
	ch, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hub.ObserveTree(rootTree("r"), 2, true)
	hub.ObserveScreenshot([]byte("x"), 4, true)
	hub.ObserveScreenSize(wire.Size{Width: 1, Height: 1}, 6, true)
	want := []session.Observation{
		{Kind: session.ObservedTree, Epoch: 2, Revision: 1, Stable: true},
		{Kind: session.ObservedScreenshot, Epoch: 4, Revision: 1, Stable: true},
		{Kind: session.ObservedScreenSize, Epoch: 6, Revision: 1, Stable: true},
	}
	for _, w := range want {
		if got := <-ch; got != w {
			t.Fatalf("notification = %+v, want %+v", got, w)
		}
	}
}

func TestHub_SubscriptionsEndWhenTheirContextEndsOrTheHubCloses(t *testing.T) {
	hub := session.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	canceled, err := hub.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	open, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cancel()
	if _, ok := <-canceled; ok {
		t.Fatal("a canceled subscription delivered a value instead of closing")
	}
	hub.Close()
	if got := <-open; got.Kind != session.ObservedClosed {
		t.Fatalf("a subscription on a closed hub got %+v, want ObservedClosed", got)
	}
	if _, ok := <-open; ok {
		t.Fatal("a subscription on a closed hub stayed open")
	}
	if _, err := hub.Subscribe(context.Background()); !errors.Is(err, session.ErrHubClosed) {
		t.Fatalf("Subscribe on a closed hub error = %v, want ErrHubClosed", err)
	}
	endedCtx, endNow := context.WithCancel(context.Background())
	endNow()
	if _, err := session.NewHub().Subscribe(endedCtx); err == nil {
		t.Fatal("Subscribe with an ended context succeeded")
	}
}

func TestHub_TwoTreesStoredInOneEpochHaveDifferentRevisions(t *testing.T) {
	hub := session.NewHub()
	if got := hub.Latest().TreeRevision; got != 0 {
		t.Fatalf("empty hub tree revision = %d, want 0", got)
	}
	hub.ObserveTree(rootTree("one"), 2, true)
	first := hub.Latest()
	hub.ObserveTree(rootTree("two"), 2, true)
	second := hub.Latest()

	if first.TreeEpoch != second.TreeEpoch {
		t.Fatalf("epochs %d and %d differ, want one shared epoch", first.TreeEpoch, second.TreeEpoch)
	}
	if first.TreeRevision == 0 || second.TreeRevision <= first.TreeRevision {
		t.Fatalf("revisions %d then %d, want a strictly increasing identity", first.TreeRevision, second.TreeRevision)
	}
	hub.ObserveScreenshot([]byte("a"), 2, true)
	one := hub.Latest().ScreenshotRevision
	hub.ObserveScreenshot([]byte("b"), 2, true)
	if two := hub.Latest().ScreenshotRevision; one == 0 || two <= one {
		t.Fatalf("screenshot revisions %d then %d, want a strictly increasing identity", one, two)
	}
}

func TestHub_AnUnstableCaptureNeverPairs(t *testing.T) {
	hub := session.NewHub()
	hub.ObserveTree(rootTree("r"), 2, false)
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 2, true)
	if snap := hub.Latest(); snap.Paired || snap.Highlightable {
		t.Fatalf("an unstable tree paired: %+v", snap)
	}
	hub.ObserveTree(rootTree("r"), 2, true)
	hub.ObserveScreenSize(wire.Size{Width: 5, Height: 10}, 2, false)
	snap := hub.Latest()
	if !snap.Paired || snap.Scale != 0 || snap.Highlightable {
		t.Fatalf("paired = %v, scale = %v, highlightable = %v, want a pair with no trusted scale", snap.Paired, snap.Scale, snap.Highlightable)
	}
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 2, false)
	if hub.Latest().Paired {
		t.Fatal("an unstable screenshot paired")
	}
}

func TestHub_AScaleNeedsAScreenSizeWithTheScreenshotsAspectRatio(t *testing.T) {
	hub := session.NewHub()
	hub.ObserveTree(rootTree("r"), 2, true)
	hub.ObserveScreenSize(wire.Size{Width: 430, Height: 932}, 2, true)
	hub.ObserveScreenshot(encodedPNG(t, 860, 1864), 2, true)
	if snap := hub.Latest(); snap.Scale != 2 || !snap.Highlightable {
		t.Fatalf("scale = %v, highlightable = %v, want a portrait size to scale a portrait screenshot", snap.Scale, snap.Highlightable)
	}

	hub.ObserveScreenshot(encodedPNG(t, 1864, 860), 2, true)
	if snap := hub.Latest(); snap.Scale != 0 || snap.Highlightable {
		t.Fatalf("scale = %v, highlightable = %v, want a portrait size not to scale a landscape screenshot", snap.Scale, snap.Highlightable)
	}

	hub.ObserveScreenSize(wire.Size{Width: 932, Height: 430}, 2, true)
	if snap := hub.Latest(); snap.Scale != 2 || !snap.Highlightable {
		t.Fatalf("scale = %v, highlightable = %v, want a landscape size to scale a landscape screenshot", snap.Scale, snap.Highlightable)
	}
}

func TestHub_ASubscriberCanTellAClosedHubFromBeingDropped(t *testing.T) {
	hub := session.NewHub()
	ch, err := hub.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		hub.ObserveTree(rootTree("r"), uint64(i), true)
	}
	hub.Close()
	got, closed := drainObservations(ch)
	if !closed || len(got) != 4 || got[3].Kind != session.ObservedClosed {
		t.Fatalf("got %+v closed = %v, want three captures then ObservedClosed then a closed channel", got, closed)
	}
}
