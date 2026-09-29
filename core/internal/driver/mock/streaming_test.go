package mock

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

func TestStreamingDriver_SatisfiesStreamingTouchCapable(t *testing.T) {
	d := NewStreamingTouchCapable()

	streaming, ok := driver.AsStreamingTouchCapable(d)
	if !ok {
		t.Fatal("AsStreamingTouchCapable() = false, want StreamingDriver to satisfy StreamingTouchCapable")
	}

	ctx := context.Background()
	if err := streaming.StreamingTouchDown(ctx, wire.Point{X: 1, Y: 1}, 7); err != nil {
		t.Fatalf("StreamingTouchDown() error = %v", err)
	}
	if err := streaming.StreamingTouchMove(ctx, wire.Point{X: 2, Y: 2}, 7); err != nil {
		t.Fatalf("StreamingTouchMove() error = %v", err)
	}
	if err := streaming.StreamingTouchUp(ctx, wire.Point{X: 2, Y: 2}, 7); err != nil {
		t.Fatalf("StreamingTouchUp() error = %v", err)
	}

	want := []string{"StreamingTouchDown", "StreamingTouchMove", "StreamingTouchUp"}
	if len(d.StreamingCalls) != len(want) {
		t.Fatalf("StreamingCalls = %+v, want %d entries", d.StreamingCalls, len(want))
	}
	for i, method := range want {
		if d.StreamingCalls[i].Method != method {
			t.Fatalf("StreamingCalls[%d].Method = %q, want %q", i, d.StreamingCalls[i].Method, method)
		}
	}
}

func TestNew_DoesNotSatisfyStreamingTouchCapable(t *testing.T) {
	if _, ok := driver.AsStreamingTouchCapable(New()); ok {
		t.Fatal("AsStreamingTouchCapable(New()) = true, want the base Driver to not implement it")
	}
}
