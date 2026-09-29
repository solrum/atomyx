package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestIsSystemOverlay(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"samsung-edge-panel-prefix", "com.samsung.android.app.cocktailbarservice:handle", true},
		{"unrelated-id", "com.example.app:button", false},
		{"empty-id", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := node(tc.id, "", "0,0,10,10")
			if got := IsSystemOverlay(n); got != tc.want {
				t.Fatalf("IsSystemOverlay(id=%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestIsSystemOverlay_NoIDAttribute(t *testing.T) {
	n := wire.TreeNodeWire{Attributes: map[string]string{AttrText: "no id here"}}
	if IsSystemOverlay(n) {
		t.Fatalf("IsSystemOverlay(no id attribute) = true, want false")
	}
}

func TestFilterSystemOverlays_RemovesOverlayNodesAndTheirSubtrees(t *testing.T) {
	overlayHandle := node("com.samsung.android.app.cocktailbarservice:handle", "", "0,0,5,5")
	overlayTrigger := node("com.samsung.android.app.cocktailbarservice:trigger", "", "0,0,5,5")
	btn2 := node("btn2", "Cancel", "0,60,100,110")
	group := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "group"},
		Children:   []wire.TreeNodeWire{overlayTrigger, btn2},
	}
	btn1 := node("btn1", "Login", "0,0,100,50")
	root := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children:   []wire.TreeNodeWire{overlayHandle, btn1, group},
	}

	filtered, ok := FilterSystemOverlays(root)
	if !ok {
		t.Fatalf("FilterSystemOverlays reported root itself as filtered, want ok=true")
	}
	if len(filtered.Children) != 2 {
		t.Fatalf("got %d top-level children, want 2 (overlayHandle removed): %+v", len(filtered.Children), filtered.Children)
	}
	if filtered.Children[0].Attributes[AttrID] != "btn1" {
		t.Fatalf("got first child id %q, want btn1", filtered.Children[0].Attributes[AttrID])
	}
	filteredGroup := filtered.Children[1]
	if filteredGroup.Attributes[AttrID] != "group" {
		t.Fatalf("got second child id %q, want group", filteredGroup.Attributes[AttrID])
	}
	if len(filteredGroup.Children) != 1 || filteredGroup.Children[0].Attributes[AttrID] != "btn2" {
		t.Fatalf("group's children = %+v, want exactly [btn2] (overlayTrigger and its subtree removed)", filteredGroup.Children)
	}
}

func TestFilterSystemOverlays_RootItselfIsOverlay(t *testing.T) {
	root := node("com.samsung.android.app.cocktailbarservice:handle", "", "0,0,5,5",
		node("btn1", "Login", "0,0,100,50"),
	)

	filtered, ok := FilterSystemOverlays(root)
	if ok {
		t.Fatalf("FilterSystemOverlays reported ok=true for an overlay root, want false")
	}
	if len(filtered.Children) != 0 || filtered.Attributes != nil {
		t.Fatalf("got %+v, want the zero value when root itself is filtered", filtered)
	}
}

func TestFilterSystemOverlays_LeavesNonOverlayTreeUnchanged(t *testing.T) {
	root := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			node("btn1", "Login", "0,0,100,50"),
			node("btn2", "Cancel", "0,60,100,110"),
		},
	}

	filtered, ok := FilterSystemOverlays(root)
	if !ok {
		t.Fatalf("FilterSystemOverlays reported ok=false for a tree with no overlays")
	}
	if len(filtered.Children) != 2 {
		t.Fatalf("got %d children, want 2 (nothing should be removed)", len(filtered.Children))
	}
}
