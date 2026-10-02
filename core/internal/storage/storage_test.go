package storage

import "testing"

func TestInMemoryStorageRoundTrips(t *testing.T) {
	s := NewInMemoryStorage()
	if err := s.Save("k", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var out map[string]int
	found, err := s.Load("k", &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load: expected key to be found")
	}
	if out["v"] != 1 {
		t.Fatalf("Load: got %v, want v=1", out)
	}
}

func TestInMemoryStorageListAndDelete(t *testing.T) {
	s := NewInMemoryStorage()
	for _, k := range []string{"a/1", "a/2", "b/1"} {
		if err := s.Save(k, map[string]int{}); err != nil {
			t.Fatalf("Save(%q): %v", k, err)
		}
	}

	keys, err := s.List("a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"a/1", "a/2"}; !equalStrings(keys, want) {
		t.Fatalf("List: got %v, want %v", keys, want)
	}

	if err := s.Delete("a/1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	keys, err = s.List("a")
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if want := []string{"a/2"}; !equalStrings(keys, want) {
		t.Fatalf("List after Delete: got %v, want %v", keys, want)
	}
}

func TestInMemoryStorageLoadMissingKey(t *testing.T) {
	s := NewInMemoryStorage()
	var out map[string]int
	found, err := s.Load("nope", &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("Load: expected key to be missing")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInMemoryStorageListChildrenIgnoresNestedRecords(t *testing.T) {
	m := NewInMemoryStorage()
	for _, key := range []string{"runs/a", "runs/b", "runs/v1/x/meta", "runsx/c"} {
		if err := m.Save(key, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.ListChildren("runs")
	if err != nil || len(got) != 2 || got[0] != "runs/a" || got[1] != "runs/b" {
		t.Fatalf("got %v %v", got, err)
	}
}
