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

	hub.ObserveTree(rootTree("one"), 2)
	hub.ObserveTree(rootTree("two"), 4)
	hub.ObserveScreenshot([]byte("first"), 4)
	hub.ObserveScreenshot([]byte("second"), 6)

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
	hub.ObserveTree(rootTree("a"), 2)
	if hub.Latest().Paired {
		t.Fatal("a tree alone reports paired")
	}
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 4)
	if hub.Latest().Paired {
		t.Fatal("a tree and a screenshot from different epochs report paired")
	}
	hub.ObserveTree(rootTree("b"), 4)
	if !hub.Latest().Paired {
		t.Fatal("a tree and a screenshot from one epoch report unpaired")
	}
	hub.ObserveScreenshot(encodedPNG(t, 10, 20), 6)
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
			hub.ObserveTree(rootTree("r"), 2)
			hub.ObserveScreenshot(tc.image, 2)
			if tc.size != nil {
				hub.ObserveScreenSize(*tc.size, 0)
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
	hub.ObserveScreenSize(wire.Size{Width: 100, Height: 200}, 2)
	hub.ObserveScreenshot(encodedPNG(t, 200, 400), 4)
	hub.ObserveTree(rootTree("r"), 2)
	snap := hub.Latest()
	if snap.Scale != 2 || snap.Paired || snap.Highlightable {
		t.Fatalf("scale = %v, paired = %v, highlightable = %v, want a known scale but no pairing", snap.Scale, snap.Paired, snap.Highlightable)
	}
}

func TestHub_LatestMasksSecretsAndLeavesTheStoredTreeUntouched(t *testing.T) {
	hub := session.NewHub()
	tree := secureTree("hunter2")
	hub.ObserveTree(tree, 2)

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
	hub.ObserveTree(secureTree("hunter2"), 2)

	first := hub.Latest().Tree.Children[1].Attributes
	second := hub.Latest().Tree.Children[1].Attributes
	if reflect.ValueOf(first).Pointer() != reflect.ValueOf(second).Pointer() {
		t.Fatal("two reads of one stored tree built two masked copies")
	}

	hub.ObserveTree(secureTree("other"), 2)
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
				hub.ObserveTree(shared, uint64(i))
				hub.ObserveScreenshot(encodedPNG(t, 4, 4), uint64(i))
				hub.ObserveScreenSize(wire.Size{Width: float64(p + 1), Height: 1}, uint64(i))
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
		hub.ObserveTree(rootTree("r"), uint64(i))
		got, _ := drain(healthy)
		healthyGot += got
	}

	if healthyGot != publishes {
		t.Fatalf("the healthy subscriber got %d of %d notifications", healthyGot, publishes)
	}
	received, closed := drain(slow)
	if !closed {
		t.Fatalf("the slow subscriber was not dropped after %d unread notifications", publishes)
	}
	if received == 0 || received >= publishes {
		t.Fatalf("the slow subscriber kept %d notifications, want a bounded buffer", received)
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
	hub.ObserveTree(rootTree("r"), 2)
	hub.ObserveScreenshot([]byte("x"), 4)
	hub.ObserveScreenSize(wire.Size{Width: 1, Height: 1}, 6)
	want := []session.Observation{
		{Kind: session.ObservedTree, Epoch: 2},
		{Kind: session.ObservedScreenshot, Epoch: 4},
		{Kind: session.ObservedScreenSize, Epoch: 6},
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
