package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

const testKeyboardBounds = "0,650,430,932"

func keyboardScreen(field wire.TreeNodeWire, keyboardBounds string) wire.TreeNodeWire {
	children := []wire.TreeNodeWire{field}
	if keyboardBounds != "" {
		children = append(children, wire.TreeNodeWire{
			Attributes: map[string]string{AttrRole: RoleKeyboard, AttrBounds: keyboardBounds},
		})
	}
	return wire.TreeNodeWire{Attributes: map[string]string{AttrID: "root"}, Children: children}
}

// coveredFieldDriver models a field under the keyboard that scrolling
// cannot move. HideKeyboard swaps in the layout without the keyboard.
func coveredFieldDriver(field func(bounds string) wire.TreeNodeWire) *mock.Driver {
	d := mock.New()
	d.SetHierarchy(keyboardScreen(field("100,700,200,750"), testKeyboardBounds))
	d.OnHideKeyboard = func() *driver.KeyResult {
		d.SetHierarchy(keyboardScreen(field("100,700,200,750"), ""))
		return nil
	}
	return d
}

func plainField(bounds string) wire.TreeNodeWire { return node("target", "Submit", bounds) }

func focusedField(bounds string) wire.TreeNodeWire { return focusedNode("target", "", bounds) }

func TestKeyboardCover_TapDismissesWhenScrollCannotReveal(t *testing.T) {
	d := coveredFieldDriver(plainField)
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if got := countCalls(d, "HideKeyboard"); got != 1 {
		t.Fatalf("got %d HideKeyboard calls, want 1", got)
	}
	if countCalls(d, "Swipe") == 0 {
		t.Fatal("expected a scroll attempt before dismissing the keyboard")
	}
	if countCalls(d, "Tap") != 1 {
		t.Fatalf("got calls %+v, want exactly one Tap", d.Calls)
	}
}

func TestKeyboardCover_TapWithoutScrollDismisses(t *testing.T) {
	d := coveredFieldDriver(plainField)
	o, _ := newTestOrchestra(d)
	noScroll := false

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{ScrollToFind: &noScroll})
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if got := countCalls(d, "HideKeyboard"); got != 1 {
		t.Fatalf("got %d HideKeyboard calls, want 1", got)
	}
	if countCalls(d, "Swipe") != 0 {
		t.Fatal("scrollToFind=false must not swipe")
	}
}

func TestKeyboardCover_ScrollRevealsWithoutDismissing(t *testing.T) {
	d := mock.New()
	d.SetHierarchy(keyboardScreen(plainField("100,700,200,750"), testKeyboardBounds))
	d.OnSwipe = func(from, to wire.Point) {
		d.SetHierarchy(keyboardScreen(plainField("100,300,200,350"), testKeyboardBounds))
	}
	o, _ := newTestOrchestra(d)

	result, err := o.Tap(context.Background(), Selector{ID: lit("target")}, TapOptions{})
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if got := countCalls(d, "HideKeyboard"); got != 0 {
		t.Fatalf("got %d HideKeyboard calls, want 0 when scrolling reveals the target", got)
	}
}

func TestKeyboardCover_UncoveredTargetKeepsKeyboardOpen(t *testing.T) {
	d := mock.New()
	d.SetHierarchy(keyboardScreen(focusedField("100,200,300,250"), testKeyboardBounds))
	o, _ := newTestOrchestra(d)

	result, err := o.InputText(context.Background(), Selector{ID: lit("target")}, "hi", InputTextOptions{})
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if countCalls(d, "HideKeyboard") != 0 || countCalls(d, "Swipe") != 0 {
		t.Fatalf("got calls %+v, want no HideKeyboard and no Swipe", d.Calls)
	}
}

func TestKeyboardCover_InputTextDismissesAndTypes(t *testing.T) {
	d := coveredFieldDriver(focusedField)
	o, _ := newTestOrchestra(d)

	result, err := o.InputText(context.Background(), Selector{ID: lit("target")}, "hi", InputTextOptions{})
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if got := countCalls(d, "HideKeyboard"); got != 1 {
		t.Fatalf("got %d HideKeyboard calls, want 1", got)
	}
	if countCalls(d, "InputText") != 1 {
		t.Fatalf("got calls %+v, want one InputText", d.Calls)
	}
}

func TestKeyboardCover_LongPressDismisses(t *testing.T) {
	d := coveredFieldDriver(plainField)
	o, _ := newTestOrchestra(d)

	result, err := o.LongPress(context.Background(), Selector{ID: lit("target")}, 500)
	if err != nil || !result.Ok {
		t.Fatalf("got result %+v err %v, want Ok", result, err)
	}
	if got := countCalls(d, "HideKeyboard"); got != 1 {
		t.Fatalf("got %d HideKeyboard calls, want 1", got)
	}
}

func TestKeyboardCover_UntrustedFrameDoesNotReduceViewport(t *testing.T) {
	d := mock.New()
	d.SetHierarchy(keyboardScreen(plainField("100,300,200,350"), "0,200,430,1900"))
	o, _ := newTestOrchestra(d)

	if _, err := o.scroll.EnsureVisible(context.Background(), CompileSelector(Selector{ID: lit("target")})); err != nil {
		t.Fatalf("EnsureVisible returned error: %v", err)
	}
	if countCalls(d, "Swipe") != 0 || countCalls(d, "HideKeyboard") != 0 {
		t.Fatalf("got calls %+v, want the untrusted frame ignored", d.Calls)
	}
}

func TestKeyboardRegion_RejectsFramesOutsideScreen(t *testing.T) {
	screen := wire.Size{Width: 430, Height: 932}
	cases := map[string]bool{
		"0,650,430,932":   true,
		"0,650,430,1900":  false,
		"0,650,900,932":   false,
		"-10,650,430,932": false,
		"0,-5,430,200":    false,
		"0,700,430,700":   false,
		"":                false,
	}
	for bounds, wantTrusted := range cases {
		got := keyboardRegion(keyboardScreen(plainField("0,0,10,10"), bounds), screen) != nil
		if got != wantTrusted {
			t.Errorf("frame %q: trusted=%t, want %t", bounds, got, wantTrusted)
		}
	}
}
