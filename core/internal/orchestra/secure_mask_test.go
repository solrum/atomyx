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

func TestMaskSecure_EmptyValuesStayEmpty(t *testing.T) {
	got := MaskSecure(secureNode("", "")).Attributes
	if got[AttrText] != "" || got[AttrValue] != "" {
		t.Fatalf("empty secure field = %v, want it left empty", got)
	}
}

func TestMaskSecure_ReadsSecrecyThroughIsSecure(t *testing.T) {
	node := secureNode("hunter2", "hunter2")
	node.Attributes[AttrSecure] = "false"

	if got := MaskSecure(node).Attributes[AttrText]; got != "hunter2" {
		t.Fatalf("node with secure=false was masked: %q", got)
	}
}

func TestTreeNodeToCompactElements_SecureSelectorNeverCarriesText(t *testing.T) {
	root := wire.TreeNodeWire{Children: []wire.TreeNodeWire{secureNode("hunter2", "hunter2")}}

	elements := TreeNodeToCompactElements(root)

	if len(elements) != 1 {
		t.Fatalf("elements = %d, want only the node with bounds", len(elements))
	}
	pw := elements[0]
	if !pw.Secure {
		t.Fatalf("secure node not flagged: %+v", pw)
	}
	if _, has := pw.Selector["text"]; has {
		t.Fatalf("secure selector carries text: %v", pw.Selector)
	}
	if pw.Selector["resourceId"] != "pw" || pw.Selector["contentDesc"] != "Password" {
		t.Fatalf("secure selector = %v, want id and label kept", pw.Selector)
	}
}

func TestDiffAppeared_SecureElementSelectorResolvesWithoutText(t *testing.T) {
	before := TreeNodeToCompactElements(wire.TreeNodeWire{})
	after := TreeNodeToCompactElements(wire.TreeNodeWire{Children: []wire.TreeNodeWire{secureNode("hunter2", "hunter2")}})

	appeared := DiffAppeared(before, after)

	if len(appeared) != 1 || !appeared[0].Secure {
		t.Fatalf("appeared = %+v, want one secure element", appeared)
	}
	if text, has := appeared[0].Selector["text"]; has {
		t.Fatalf("appeared selector carries text %q", text)
	}
	if appeared[0].Selector["resourceId"] != "pw" {
		t.Fatalf("appeared selector = %v, want the id", appeared[0].Selector)
	}
}
