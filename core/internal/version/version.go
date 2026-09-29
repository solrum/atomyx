// Package version reports one Atomyx binary's build identity: the
// release version, the VCS commit it was built from, and the build
// date. Every binary (the CLI, the MCP server, the sidecar) uses it
// to answer, for an operator, exactly what is deployed, through its
// own version-reporting subcommand or flag. It is a leaf package
// (imports only the standard library) so any package in the module
// may depend on it.
package version

import (
	"fmt"
	"runtime/debug"
)

// unstamped is Version's value before a release build overrides it,
// and before Read finds a usable module version in the embedded
// build info. It stays a visible marker rather than an empty string
// so an operator running an unreleased build sees an unambiguous
// value instead of a blank field.
const unstamped = "dev"

// develModuleVersion is the placeholder debug.BuildInfo.Main.Version
// carries when the main module was built without a resolved version,
// such as a plain `go build` from a checkout rather than `go install
// module@version`. Read treats it the same as an absent version.
const develModuleVersion = "(devel)"

// Version is the release version string every binary reports. A
// release build overrides it with:
//
//	-ldflags "-X dev.atomyx/internal/version.Version=vX.Y.Z"
//
// Left unstamped, Read falls back to the module version the Go
// toolchain records in the binary when one is available, or else
// reports unstamped.
var Version = unstamped

const (
	unknownCommit = "unknown"
	unknownDate   = "unknown"

	// commitShortLen is how many leading hex characters of the VCS
	// revision Info shows, matching the short-hash length `git
	// rev-parse --short` reports by default.
	commitShortLen = 7
)

// Info is one binary's reported build identity.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// String formats Info as the single operator-readable line every
// binary's version surface prints.
func (i Info) String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", i.Version, i.Commit, i.Date)
}

// Read returns the calling binary's build identity. It never panics:
// a binary carrying no embedded build info, for example one built
// with -buildvcs=false or outside version control, still gets a
// complete Info with clear defaults instead of empty fields.
func Read() Info {
	buildInfo, ok := debug.ReadBuildInfo()
	return infoFrom(Version, buildInfo, ok)
}

// infoFrom derives Info from an explicit build-info snapshot, kept
// separate from Read so the derivation is exercisable against a
// synthetic debug.BuildInfo in tests, not only against the real
// process's embedded one.
func infoFrom(version string, buildInfo *debug.BuildInfo, ok bool) Info {
	info := Info{Version: version, Commit: unknownCommit, Date: unknownDate}
	if !ok || buildInfo == nil {
		return info
	}

	if info.Version == unstamped {
		if moduleVersion := buildInfo.Main.Version; moduleVersion != "" && moduleVersion != develModuleVersion {
			info.Version = moduleVersion
		}
	}

	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				info.Commit = shortCommit(setting.Value)
			}
		case "vcs.time":
			if setting.Value != "" {
				info.Date = setting.Value
			}
		}
	}

	return info
}

// shortCommit truncates a VCS revision to commitShortLen characters,
// the convention operators already expect from `git log --oneline`.
func shortCommit(revision string) string {
	if len(revision) <= commitShortLen {
		return revision
	}
	return revision[:commitShortLen]
}
