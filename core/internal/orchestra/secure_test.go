package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func TestIsSecure(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		want  bool
	}{
		{"marked-text-field", map[string]string{AttrRole: RoleTextField, AttrSecure: "true"}, true},
		{"marked-secure-role", map[string]string{AttrRole: RoleSecureTextField, AttrSecure: "true"}, true},
		{"secure-role-without-attribute", map[string]string{AttrRole: RoleSecureTextField}, false},
		{"unmarked", map[string]string{AttrRole: RoleTextField}, false},
		{"other-attribute-value", map[string]string{AttrSecure: "false"}, false},
		{"nil-attributes", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSecure(wire.TreeNodeWire{Attributes: tc.attrs}); got != tc.want {
				t.Fatalf("IsSecure() = %v, want %v", got, tc.want)
			}
		})
	}
}
