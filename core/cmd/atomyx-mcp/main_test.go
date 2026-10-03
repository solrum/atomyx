package main

import (
	"strings"
	"testing"

	"dev.atomyx/internal/device"
)

func TestVersionLinePrintsANonEmptyIdentityNamingTheBinary(t *testing.T) {
	line := versionLine()
	if !strings.HasPrefix(line, binaryName+" ") {
		t.Errorf("versionLine: got %q, want prefix %q", line, binaryName+" ")
	}
	if !strings.Contains(line, "(commit ") {
		t.Errorf("versionLine: got %q, want it to include the commit", line)
	}
}

func TestParseArgsAcceptsNoArguments(t *testing.T) {
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs(nil): %v", err)
	}
	if opts.version || opts.pinned != nil {
		t.Fatalf("parseArgs(nil): got %+v, want zero options", opts)
	}
}

func TestParseArgsReadsVersionFlags(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		opts, err := parseArgs([]string{flag})
		if err != nil || !opts.version {
			t.Errorf("parseArgs(%q): got %+v, %v, want version", flag, opts, err)
		}
	}
}

func TestParseArgsPinsTheDeviceWithItsPlatform(t *testing.T) {
	opts, err := parseArgs([]string{"--platform", "ios", "--device", "ABC-123"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.pinned == nil || opts.pinned.ID != "ABC-123" || opts.pinned.Platform != device.PlatformIOS {
		t.Fatalf("parseArgs: got %+v, want ios/ABC-123 pinned", opts.pinned)
	}
}

func TestParseArgsAcceptsEqualsForm(t *testing.T) {
	opts, err := parseArgs([]string{"--platform=android", "--device=emulator-5554"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.pinned == nil || opts.pinned.ID != "emulator-5554" || opts.pinned.Platform != device.PlatformAndroid {
		t.Fatalf("parseArgs: got %+v, want android/emulator-5554 pinned", opts.pinned)
	}
}

func TestParseArgsRejectsMisuse(t *testing.T) {
	cases := [][]string{
		{"--device", "emulator-5554"},
		{"--platform", "android"},
		{"--device"},
		{"--platform", "windows", "--device", "x"},
		{"--device", "", "--platform", "android"},
		{"--bogus"},
	}
	for _, args := range cases {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%v): got nil error, want a usage error", args)
		}
	}
}
