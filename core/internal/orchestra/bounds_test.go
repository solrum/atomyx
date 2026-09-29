package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestParseBounds_Valid(t *testing.T) {
	b, ok := ParseBounds("10,20,110,220")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := wire.Bounds{Left: 10, Top: 20, Right: 110, Bottom: 220}
	if b != want {
		t.Fatalf("got %+v, want %+v", b, want)
	}
}

func TestParseBounds_TrimsWhitespace(t *testing.T) {
	b, ok := ParseBounds(" 10 , 20 , 110 , 220 ")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := wire.Bounds{Left: 10, Top: 20, Right: 110, Bottom: 220}
	if b != want {
		t.Fatalf("got %+v, want %+v", b, want)
	}
}

func TestParseBounds_WrongPartCount(t *testing.T) {
	if _, ok := ParseBounds("10,20,110"); ok {
		t.Fatal("expected ok=false for three parts")
	}
}

func TestParseBounds_Empty(t *testing.T) {
	if _, ok := ParseBounds(""); ok {
		t.Fatal("expected ok=false for empty string")
	}
}

func TestParseBounds_NonNumeric(t *testing.T) {
	if _, ok := ParseBounds("a,b,c,d"); ok {
		t.Fatal("expected ok=false for non-numeric parts")
	}
}

func TestBoundsCenter(t *testing.T) {
	center := BoundsCenter(wire.Bounds{Left: 0, Top: 0, Right: 100, Bottom: 50})
	want := wire.Point{X: 50, Y: 25}
	if center != want {
		t.Fatalf("got %+v, want %+v", center, want)
	}
}
