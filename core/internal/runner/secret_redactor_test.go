package runner

import (
	"strings"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script/expression"
)

func secretSnapshot() VariableSnapshot {
	return VariableSnapshot{
		Values: map[string]ScriptValue{
			"password": expression.StringValue("hunter2hunter2"),
			"pin":      expression.StringValue("42"),
			"user":     expression.StringValue("alice"),
			"copy":     expression.StringValue("pw is hunter2hunter2"),
			"count":    expression.NumberValue(3),
		},
		Secret: map[string]bool{"password": true, "pin": true},
	}
}

func TestRedactor_MasksSecretValuesInFreeText(t *testing.T) {
	got := secretSnapshot().Redactor().String("typed hunter2hunter2 for alice, 42 times")
	want := "typed " + orchestra.SecureMask + " for alice, " + orchestra.SecureMask + " times"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactor_MasksAShortSecretOnlyAsAWholeWord(t *testing.T) {
	got := secretSnapshot().Redactor().String("a142b 42x _42 42, (42) 420")
	want := "a142b 42x _42 " + orchestra.SecureMask + ", (" + orchestra.SecureMask + ") 420"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactor_MaskNeverRevealsLength(t *testing.T) {
	short := VariableSnapshot{Values: map[string]ScriptValue{"a": expression.StringValue("abcdefg")}, Secret: map[string]bool{"a": true}}
	long := VariableSnapshot{Values: map[string]ScriptValue{"a": expression.StringValue(strings.Repeat("x", 40))}, Secret: map[string]bool{"a": true}}
	if short.Redactor().String("abcdefg") != long.Redactor().String(strings.Repeat("x", 40)) {
		t.Fatal("mask output differs by secret length")
	}
}

func TestRedactor_MasksTokenShapesWithoutTaggedValues(t *testing.T) {
	got := VariableSnapshot{}.Redactor().String("Authorization: Bearer abc.def-123_XYZ")
	want := "Authorization: Bearer " + orchestra.SecureMask
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactor_IgnoresUntaggedValues(t *testing.T) {
	snap := secretSnapshot()
	snap.Secret = map[string]bool{"password": true, "user": false}
	if got := snap.Redactor().String("alice"); got != "alice" {
		t.Fatalf("masked an untagged value: %q", got)
	}
}

func TestVariableSnapshotRedacted(t *testing.T) {
	got := secretSnapshot().Redacted()
	want := map[string]string{
		"password": orchestra.SecureMask,
		"pin":      orchestra.SecureMask,
		"user":     "alice",
		"copy":     "pw is " + orchestra.SecureMask,
		"count":    "3",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d", len(got), len(want))
	}
}
