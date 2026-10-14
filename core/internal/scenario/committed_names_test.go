package scenario

import "testing"

func TestCommittedChildNames(t *testing.T) {
	got := committedChildNames(
		map[string]string{"KEPT": "a", "OVERRIDDEN": "b"},
		map[string]string{"OVERRIDDEN": "c", "FLAG": "d", "CONFIG": "e"},
		map[string]bool{"CONFIG": true},
	)
	want := map[string]bool{"KEPT": true, "CONFIG": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("got %v, missing %q", got, name)
		}
	}
}
