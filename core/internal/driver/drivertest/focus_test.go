package drivertest

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestHasFocusedInput(t *testing.T) {
	yes, no := true, false
	field := func(id string, focused *bool) wire.TreeNodeWire {
		return wire.TreeNodeWire{Attributes: map[string]string{"id": id}, Focused: focused}
	}
	tree := func(children ...wire.TreeNodeWire) wire.TreeNodeWire {
		return wire.TreeNodeWire{Attributes: map[string]string{}, Children: children}
	}
	cases := []struct {
		name string
		root wire.TreeNodeWire
		want bool
	}{
		{"focused field", tree(field("focus-input", &yes)), true},
		{"prefixed id", tree(field("dev.atomyx.demo:id/focus-input", &yes)), true},
		{"unfocused field", tree(field("focus-input", &no)), false},
		{"focus not reported", tree(field("focus-input", nil)), false},
		{"other field focused", tree(field("other", &yes)), false},
		{"nested", tree(tree(field("focus-input", &yes))), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasFocusedInput(tc.root, "focus-input"); got != tc.want {
				t.Fatalf("hasFocusedInput = %v, want %v", got, tc.want)
			}
		})
	}
}
