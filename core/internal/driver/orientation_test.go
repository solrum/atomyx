package driver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

func TestParseOrientation(t *testing.T) {
	for _, o := range AllOrientations() {
		got, ok := ParseOrientation(string(o))
		if !ok || got != o {
			t.Errorf("ParseOrientation(%q) = %q, %v", o, got, ok)
		}
	}
	for _, name := range []string{"", "Portrait", "portraitUpsideDown", "sideways"} {
		if _, ok := ParseOrientation(name); ok {
			t.Errorf("ParseOrientation(%q) accepted an unknown name", name)
		}
	}
}

func TestValidateOrientation(t *testing.T) {
	if err := ValidateOrientation(OrientationLandscapeLeft); err != nil {
		t.Fatalf("ValidateOrientation(landscapeLeft) = %v, want nil", err)
	}
	err := ValidateOrientation("sideways")
	if err == nil {
		t.Fatal("ValidateOrientation(sideways) returned no error")
	}
	code, msg, _ := diag.Describe(err)
	if code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", code, diag.CodeInvalidArgument)
	}
	for _, want := range []string{"sideways", "portrait", "landscapeLeft", "landscapeRight"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestSizeMatchesOrientation(t *testing.T) {
	portrait := wire.Size{Width: 400, Height: 800}
	landscape := wire.Size{Width: 800, Height: 400}
	square := wire.Size{Width: 500, Height: 500}
	tests := []struct {
		o    Orientation
		size wire.Size
		want bool
	}{
		{OrientationPortrait, portrait, true},
		{OrientationPortrait, landscape, false},
		{OrientationLandscapeLeft, landscape, true},
		{OrientationLandscapeLeft, portrait, false},
		{OrientationLandscapeRight, landscape, true},
		{OrientationLandscapeRight, portrait, false},
		{OrientationPortrait, square, false},
		{OrientationLandscapeLeft, square, false},
	}
	for _, tt := range tests {
		if got := SizeMatchesOrientation(tt.o, tt.size); got != tt.want {
			t.Errorf("SizeMatchesOrientation(%s, %+v) = %v, want %v", tt.o, tt.size, got, tt.want)
		}
	}
}

func TestAwaitOrientation_ReturnsWithoutSleepingWhenAlreadyMatching(t *testing.T) {
	clk := clock.NewFakeClock(0)
	reads := 0
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		reads++
		return wire.Size{Width: 800, Height: 400}, nil
	})
	if err != nil {
		t.Fatalf("AwaitOrientation = %v", err)
	}
	if reads != 1 || clk.NowMs() != 0 {
		t.Fatalf("reads = %d, elapsed = %dms, want one read and no sleep", reads, clk.NowMs())
	}
}

func TestAwaitOrientation_PollsUntilTheSizeFlips(t *testing.T) {
	clk := clock.NewFakeClock(0)
	reads := 0
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeRight, func(context.Context) (wire.Size, error) {
		reads++
		if reads < 4 {
			return wire.Size{Width: 400, Height: 800}, nil
		}
		return wire.Size{Width: 800, Height: 400}, nil
	})
	if err != nil {
		t.Fatalf("AwaitOrientation = %v", err)
	}
	if reads != 4 {
		t.Fatalf("reads = %d, want 4", reads)
	}
	if want := int64(3 * OrientationPollIntervalMs); clk.NowMs() != want {
		t.Fatalf("elapsed = %dms, want %dms", clk.NowMs(), want)
	}
}

func TestAwaitOrientation_FailsWithOrientationNotSetAtTheBound(t *testing.T) {
	clk := clock.NewFakeClock(0)
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		return wire.Size{Width: 400, Height: 800}, nil
	})
	if err == nil {
		t.Fatal("AwaitOrientation returned no error for a screen that never rotates")
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationNotSet {
		t.Fatalf("code = %q, want %q", code, diag.CodeOrientationNotSet)
	}
	if clk.NowMs() < OrientationSettleTimeoutMs {
		t.Fatalf("gave up after %dms, before the %dms bound", clk.NowMs(), OrientationSettleTimeoutMs)
	}
	if clk.NowMs() > OrientationSettleTimeoutMs+OrientationPollIntervalMs {
		t.Fatalf("waited %dms, past the bound plus one poll", clk.NowMs())
	}
}

func transientReadError() error {
	return diag.New(diag.CodeAgentCommunicationFailed, "could not complete the request", "", errors.New("eof"))
}

func TestAwaitOrientation_KeepsWaitingThroughTransientReadErrors(t *testing.T) {
	clk := clock.NewFakeClock(0)
	reads := 0
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		reads++
		if reads < 3 {
			return wire.Size{}, transientReadError()
		}
		return wire.Size{Width: 800, Height: 400}, nil
	})
	if err != nil {
		t.Fatalf("AwaitOrientation = %v, want nil", err)
	}
	if reads != 3 {
		t.Fatalf("reads = %d, want 3", reads)
	}
}

func TestAwaitOrientation_ReturnsTheLastTransientErrorAtTheBound(t *testing.T) {
	clk := clock.NewFakeClock(0)
	want := transientReadError()
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		return wire.Size{}, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("AwaitOrientation = %v, want %v", err, want)
	}
	if clk.NowMs() < OrientationSettleTimeoutMs {
		t.Fatalf("gave up after %dms, before the %dms bound", clk.NowMs(), OrientationSettleTimeoutMs)
	}
}

func TestAwaitOrientation_ReturnsOrientationNotSetWhenTheLastReadSucceeded(t *testing.T) {
	clk := clock.NewFakeClock(0)
	reads := 0
	err := AwaitOrientation(context.Background(), clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		reads++
		if reads == 1 {
			return wire.Size{}, transientReadError()
		}
		return wire.Size{Width: 400, Height: 800}, nil
	})
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationNotSet {
		t.Fatalf("code = %q, want %q", code, diag.CodeOrientationNotSet)
	}
}

func TestAwaitOrientation_FailsFastOnAPermanentReadError(t *testing.T) {
	for name, want := range map[string]error{
		"invalid argument": diag.New(diag.CodeInvalidArgument, "bad", "", nil),
		"device not found": diag.New(diag.CodeDeviceNotFound, "gone", "", nil),
		"uncoded":          errors.New("device gone"),
	} {
		t.Run(name, func(t *testing.T) {
			clk := clock.NewFakeClock(0)
			reads := 0
			err := AwaitOrientation(context.Background(), clk, OrientationPortrait, func(context.Context) (wire.Size, error) {
				reads++
				return wire.Size{}, want
			})
			if !errors.Is(err, want) {
				t.Fatalf("AwaitOrientation = %v, want %v", err, want)
			}
			if reads != 1 || clk.NowMs() != 0 {
				t.Fatalf("reads = %d, elapsed = %dms, want one read and no sleep", reads, clk.NowMs())
			}
		})
	}
}

func TestAwaitOrientation_ReturnsPromptlyWhenCancelledDuringTransientErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clk := clock.NewFakeClock(0)
	reads := 0
	err := AwaitOrientation(ctx, clk, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		reads++
		if reads == 2 {
			cancel()
		}
		return wire.Size{}, transientReadError()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitOrientation = %v, want context.Canceled", err)
	}
	if reads != 2 {
		t.Fatalf("reads = %d, want 2", reads)
	}
}

func TestAwaitOrientation_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := AwaitOrientation(ctx, clock.SystemClock{}, OrientationLandscapeLeft, func(context.Context) (wire.Size, error) {
		return wire.Size{Width: 400, Height: 800}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitOrientation = %v, want context.Canceled", err)
	}
}
