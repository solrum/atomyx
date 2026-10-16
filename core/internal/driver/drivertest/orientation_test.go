package drivertest

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestFindPrefixedTextStopsAtFirstLineBreak(t *testing.T) {
	cases := []struct {
		name  string
		label string
		want  string
	}{
		{"merged semantics label", "Tap count: 3\nTap\nCount: 3", "3"},
		{"plain label", "Tap count: 3", "3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := wire.TreeNodeWire{
				Attributes: map[string]string{},
				Children: []wire.TreeNodeWire{
					{Attributes: map[string]string{"label": tc.label}},
				},
			}
			got, ok := findPrefixedText(root, tapCountPrefix)
			if !ok || got != tc.want {
				t.Fatalf("findPrefixedText = %q, %v, want %q, true", got, ok, tc.want)
			}
		})
	}
}
