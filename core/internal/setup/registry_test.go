package setup

import "testing"

func TestRegistryRejectsBadRegistrations(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	if err := reg.Add(&fakeStep{id: "", calls: &calls}); err == nil {
		t.Error("empty id was accepted")
	}
	if err := reg.Add(&fakeStep{id: "a", calls: &calls}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&fakeStep{id: "a", calls: &calls}); err == nil {
		t.Error("duplicate id was accepted")
	}
	if err := reg.Add(&fakeStep{id: "b", calls: &calls}, "later"); err == nil {
		t.Error("a requirement that is not registered yet was accepted")
	}
}
