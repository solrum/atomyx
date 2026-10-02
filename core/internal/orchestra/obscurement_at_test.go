package orchestra

import (
	"reflect"
	"testing"
)

func boundedNode(role, id, bounds string) map[string]string {
	attrs := map[string]string{AttrRole: role, AttrBounds: bounds}
	if id != "" {
		attrs[AttrID] = id
	}
	return attrs
}

func TestObscurementAt_AnOverlayOnTopOfTheTargetObscuresIt(t *testing.T) {
	root := suggestRoot(
		suggestNode(boundedNode("button", "pay", "0,0,100,100")),
		suggestNode(boundedNode("dialog", "promo", "0,0,200,200")),
	)
	got, err := ObscurementAt(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Obscured || got.Obscurer == nil || got.Obscurer.ID != "promo" || got.Obscurer.Role != "dialog" {
		t.Fatalf("ObscurementAt = %+v, want obscured by the promo dialog", got)
	}
}

func TestObscurementAt_ATopmostTargetIsNotObscured(t *testing.T) {
	root := suggestRoot(
		suggestNode(boundedNode("dialog", "promo", "0,0,200,200")),
		suggestNode(boundedNode("button", "pay", "0,0,100,100")),
	)
	got, err := ObscurementAt(root, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Obscured {
		t.Fatalf("ObscurementAt = %+v, want not obscured", got)
	}
}

func TestObscurementAt_MatchesDetectObscurementOnTheSameNode(t *testing.T) {
	root := suggestRoot(
		suggestNode(boundedNode("button", "pay", "0,0,100,100")),
		suggestNode(boundedNode("sheet", "menu", "0,0,300,300")),
	)
	want := DetectObscurement(root, cursorAt(FromTree(root), []int{0}))
	got, err := ObscurementAt(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ObscurementAt = %+v, DetectObscurement = %+v", got, want)
	}
}

func TestObscurementAt_FailsOpenWithoutBoundsAndRejectsAnUnknownPath(t *testing.T) {
	root := suggestRoot(suggestNode(map[string]string{AttrRole: "button"}))
	got, err := ObscurementAt(root, []int{0})
	if err != nil || got.Obscured {
		t.Fatalf("no bounds: got %+v, %v, want not obscured and no error", got, err)
	}
	if _, err := ObscurementAt(root, []int{5}); err == nil {
		t.Fatal("a path outside the tree returned no error")
	}
}
