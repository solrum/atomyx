package runfolder

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLabelRules(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Login Flow", "login-flow"},
		{"already-ok-1", "already-ok-1"},
		{"  spaced   out  ", "spaced-out"},
		{"a__b..c", "a-b-c"},
		{"--lead-and-trail--", "lead-and-trail"},
		{"Ünïcode wörds", "n-code-w-rds"},
		{"", DefaultLabel},
		{"***", DefaultLabel},
		{strings.Repeat("a", 30), strings.Repeat("a", MaxLabelLen)},
		{strings.Repeat("a", MaxLabelLen-1) + " tail", strings.Repeat("a", MaxLabelLen-1)},
		{strings.Repeat("ab ", 20), "ab-ab-ab-ab-ab-ab-ab-ab"},
	}
	for _, c := range cases {
		if got := Label(c.in); got != c.want {
			t.Errorf("Label(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := Label(c.in); len(got) > MaxLabelLen {
			t.Errorf("Label(%q) is %d long", c.in, len(got))
		}
	}
}

func TestNewRunIDFormatsUTCTimeLabelAndHexSuffix(t *testing.T) {
	zone := time.FixedZone("plus7", 7*60*60)
	at := time.Date(2026, 10, 2, 14, 3, 9, 0, zone)
	id, err := newRunID(at, "My Script", bytes.NewReader([]byte{0xab, 0xcd}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "20261002-070309-my-script-abcd"; id != want {
		t.Fatalf("id = %q, want %q", id, want)
	}
}

func TestNewRunIDReportsAnExhaustedRandomSource(t *testing.T) {
	if _, err := newRunID(time.Unix(0, 0), "x", bytes.NewReader([]byte{1})); err == nil {
		t.Fatal("expected an error from a short random source")
	}
}
