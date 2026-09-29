package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestWalk_PreOrderWithParentAndDepth(t *testing.T) {
	leaf1 := wire.TreeNodeWire{Attributes: map[string]string{AttrID: "leaf1"}}
	leaf2 := wire.TreeNodeWire{Attributes: map[string]string{AttrID: "leaf2"}}
	child := wire.TreeNodeWire{Attributes: map[string]string{AttrID: "child"}, Children: []wire.TreeNodeWire{leaf1, leaf2}}
	root := wire.TreeNodeWire{Attributes: map[string]string{AttrID: "root"}, Children: []wire.TreeNodeWire{child}}

	cursors := Walk(root)
	if len(cursors) != 4 {
		t.Fatalf("got %d cursors, want 4", len(cursors))
	}

	order := []string{"root", "child", "leaf1", "leaf2"}
	for i, want := range order {
		if got := cursors[i].Node.Attributes[AttrID]; got != want {
			t.Fatalf("cursor %d: got id %q, want %q", i, got, want)
		}
	}

	if cursors[0].Parent != nil {
		t.Fatal("root cursor must have a nil parent")
	}
	if cursors[0].Depth != 0 {
		t.Fatalf("root depth = %d, want 0", cursors[0].Depth)
	}
	if cursors[1].Parent != cursors[0] {
		t.Fatal("child's parent must be the root cursor")
	}
	if cursors[1].Depth != 1 {
		t.Fatalf("child depth = %d, want 1", cursors[1].Depth)
	}
	if cursors[2].ChildIndex != 0 || cursors[3].ChildIndex != 1 {
		t.Fatalf("leaf child indexes = %d, %d, want 0, 1", cursors[2].ChildIndex, cursors[3].ChildIndex)
	}
}
