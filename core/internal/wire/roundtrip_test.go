package wire

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTrip encodes v, decodes the result into a fresh value of the
// same type, and returns the decoded value alongside the raw JSON so
// callers can additionally inspect which keys were emitted.
func roundTrip[T any](t *testing.T, v T) (T, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshaling %T: %v", v, err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshaling %T into a map: %v", v, err)
	}
	return decoded, asMap
}

func ptr[T any](v T) *T { return &v }

func TestRoundTrip_Point(t *testing.T) {
	in := Point{X: 12, Y: 34}
	out, _ := roundTrip(t, in)
	if out != in {
		t.Fatalf("Point round trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestRoundTrip_Size(t *testing.T) {
	in := Size{Width: 1080, Height: 2400}
	out, _ := roundTrip(t, in)
	if out != in {
		t.Fatalf("Size round trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestRoundTrip_Bounds(t *testing.T) {
	in := Bounds{Left: 10, Top: 20, Right: 110, Bottom: 60}
	out, _ := roundTrip(t, in)
	if out != in {
		t.Fatalf("Bounds round trip mismatch: got %+v, want %+v", out, in)
	}
}

// TestRoundTrip_TreeNodeWire_TriState confirms that Go-side encoding
// preserves the tri-state contract: an unset pointer is omitted from
// the JSON (decoding back to nil, the "unknown" state) while an
// explicit true or false round-trips as that same explicit value.
func TestRoundTrip_TreeNodeWire_TriState(t *testing.T) {
	in := TreeNodeWire{
		Attributes: map[string]string{"id": "x"},
		Children:   []TreeNodeWire{},
		Clickable:  ptr(true),
		Enabled:    ptr(false),
	}
	out, asMap := roundTrip(t, in)
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("TreeNodeWire round trip mismatch: got %+v, want %+v", out, in)
	}
	for _, key := range []string{"focused", "selected", "checked", "visible"} {
		if _, ok := asMap[key]; ok {
			t.Fatalf("encoded JSON unexpectedly contains key %q for an unset tri-state field", key)
		}
	}
	if out.Focused != nil {
		t.Fatalf("Focused = %v, want nil after round trip of an unset field", *out.Focused)
	}
}

func TestIsCanonicalAttribute(t *testing.T) {
	for _, key := range []string{"id", "text", "label", "hint", "value", "role", "class", "package", "bounds", AttrSecure} {
		if !IsCanonicalAttribute(key) {
			t.Errorf("IsCanonicalAttribute(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"ext:ios-title", "enabled", ""} {
		if IsCanonicalAttribute(key) {
			t.Errorf("IsCanonicalAttribute(%q) = true, want false", key)
		}
	}
}
