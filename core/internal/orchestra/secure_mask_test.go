package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func secureNode(text, value string) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{
		AttrID: "pw", AttrRole: RoleTextField, AttrLabel: "Password", AttrHint: "Enter password",
		AttrText: text, AttrValue: value, AttrSecure: "true", AttrBounds: "0,0,10,10",
	}}
}

func TestMaskSecure_ReplacesTextAndValueOnly(t *testing.T) {
	root := wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: "root"},
		Children: []wire.TreeNodeWire{
			{Attributes: map[string]string{AttrID: "user", AttrText: "alice", AttrValue: "alice"}},
			secureNode("hunter2", "hunter2"),
		},
	}

	masked := MaskSecure(root)

	user := masked.Children[0].Attributes
	if user[AttrText] != "alice" || user[AttrValue] != "alice" {
		t.Fatalf("non-secure node changed: %v", user)
	}
	pw := masked.Children[1].Attributes
	if pw[AttrText] != SecureMask || pw[AttrValue] != SecureMask {
		t.Fatalf("secure text and value not masked: %v", pw)
	}
	for key, want := range map[string]string{
		AttrID: "pw", AttrRole: RoleTextField, AttrLabel: "Password", AttrHint: "Enter password",
		AttrSecure: "true", AttrBounds: "0,0,10,10",
	} {
		if pw[key] != want {
			t.Fatalf("attribute %s = %q, want %q kept", key, pw[key], want)
		}
	}
}

func TestMaskSecure_LeavesTheInputTreeIntact(t *testing.T) {
	root := wire.TreeNodeWire{Children: []wire.TreeNodeWire{secureNode("hunter2", "hunter2")}}

	_ = MaskSecure(root)

	if got := root.Children[0].Attributes[AttrText]; got != "hunter2" {
		t.Fatalf("input tree text = %q, want the real value kept for selector resolution", got)
	}
}

func TestMaskSecureAttributes_EmptyValuesStayEmpty(t *testing.T) {
	got := MaskSecureAttributes(secureNode("", "").Attributes)
	if got[AttrText] != "" || got[AttrValue] != "" {
		t.Fatalf("empty secure field = %v, want it left empty", got)
	}
}

func TestTreeNodeToCompactElements_MasksSecureText(t *testing.T) {
	root := wire.TreeNodeWire{Children: []wire.TreeNodeWire{secureNode("hunter2", "hunter2")}}

	elements := TreeNodeToCompactElements(root)

	for _, el := range elements {
		if el.Text == "hunter2" || el.Selector["text"] == "hunter2" {
			t.Fatalf("compact element carries the secret: %+v", el)
		}
	}
}
