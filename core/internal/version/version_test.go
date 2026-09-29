package version

import (
	"runtime/debug"
	"testing"
)

func TestInfoFromDefaultsWhenNoBuildInfo(t *testing.T) {
	info := infoFrom(unstamped, nil, false)
	if info.Version != unstamped {
		t.Errorf("Version: got %q, want %q", info.Version, unstamped)
	}
	if info.Commit != unknownCommit {
		t.Errorf("Commit: got %q, want %q", info.Commit, unknownCommit)
	}
	if info.Date != unknownDate {
		t.Errorf("Date: got %q, want %q", info.Date, unknownDate)
	}
}

func TestInfoFromExtractsVCSSettings(t *testing.T) {
	buildInfo := &debug.BuildInfo{
		Main: debug.Module{Version: develModuleVersion},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "1234567890abcdef1234567890abcdef12345678"},
			{Key: "vcs.time", Value: "2026-09-16T00:00:00Z"},
		},
	}

	info := infoFrom(unstamped, buildInfo, true)

	if info.Commit != "1234567" {
		t.Errorf("Commit: got %q, want %q", info.Commit, "1234567")
	}
	if info.Date != "2026-09-16T00:00:00Z" {
		t.Errorf("Date: got %q, want %q", info.Date, "2026-09-16T00:00:00Z")
	}
	if info.Version != unstamped {
		t.Errorf("Version: got %q, want unstamped %q since Main.Version is the devel placeholder", info.Version, unstamped)
	}
}

func TestInfoFromFallsBackToModuleVersionWhenUnstamped(t *testing.T) {
	buildInfo := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}

	info := infoFrom(unstamped, buildInfo, true)

	if info.Version != "v0.9.0" {
		t.Errorf("Version: got %q, want %q", info.Version, "v0.9.0")
	}
}

func TestInfoFromKeepsStampedVersionOverModuleFallback(t *testing.T) {
	buildInfo := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}

	info := infoFrom("v9.9.9-test", buildInfo, true)

	if info.Version != "v9.9.9-test" {
		t.Errorf("Version: got %q, want the stamped value %q", info.Version, "v9.9.9-test")
	}
}

func TestInfoFromIgnoresEmptyVCSValues(t *testing.T) {
	buildInfo := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: ""},
			{Key: "vcs.time", Value: ""},
		},
	}

	info := infoFrom(unstamped, buildInfo, true)

	if info.Commit != unknownCommit {
		t.Errorf("Commit: got %q, want %q", info.Commit, unknownCommit)
	}
	if info.Date != unknownDate {
		t.Errorf("Date: got %q, want %q", info.Date, unknownDate)
	}
}

func TestShortCommitTruncatesToConventionalLength(t *testing.T) {
	got := shortCommit("1234567890abcdef")
	if want := "1234567"; got != want {
		t.Errorf("shortCommit: got %q, want %q", got, want)
	}
}

func TestShortCommitLeavesShortRevisionUnchanged(t *testing.T) {
	got := shortCommit("abc")
	if want := "abc"; got != want {
		t.Errorf("shortCommit: got %q, want %q", got, want)
	}
}

func TestInfoStringFormatsAllThreeFields(t *testing.T) {
	info := Info{Version: "v1.2.3", Commit: "abc1234", Date: "2024-01-01T00:00:00Z"}
	got := info.String()
	want := "v1.2.3 (commit abc1234, built 2024-01-01T00:00:00Z)"
	if got != want {
		t.Errorf("String: got %q, want %q", got, want)
	}
}

func TestReadReturnsANonEmptyIdentity(t *testing.T) {
	info := Read()
	if info.Version == "" || info.Commit == "" || info.Date == "" {
		t.Errorf("Read: got %+v, want every field populated with either a real value or a default", info)
	}
}
