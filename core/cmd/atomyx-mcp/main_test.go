package main

import (
	"strings"
	"testing"
)

func TestVersionRequestedRecognizesBothFlagForms(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--version"}, true},
		{[]string{"-v"}, true},
		{[]string{"--help"}, false},
	}
	for _, c := range cases {
		if got := versionRequested(c.args); got != c.want {
			t.Errorf("versionRequested(%v): got %v, want %v", c.args, got, c.want)
		}
	}
}

func TestVersionLinePrintsANonEmptyIdentityNamingTheBinary(t *testing.T) {
	line := versionLine()
	if !strings.HasPrefix(line, binaryName+" ") {
		t.Errorf("versionLine: got %q, want prefix %q", line, binaryName+" ")
	}
	if !strings.Contains(line, "(commit ") {
		t.Errorf("versionLine: got %q, want it to include the commit", line)
	}
}
