package runfolder

import (
	"path/filepath"
)

// MCPTestKeyPrefix starts the test key of a run driven through the MCP
// server, which has no script file to anchor a path.
const MCPTestKeyPrefix = "mcp:"

// DeriveTestKey returns the stable identity of the script or scenario file
// at path, so the same test keeps one key whatever directory a run starts
// from. The key is the path relative to the nearest ancestor directory
// that holds an Atomyx config file, else the nearest ancestor that is a
// git root, else the file's own directory, written with forward slashes.
// The search starts at the file's own directory.
//
// holdsConfig reports whether a directory directly contains an Atomyx
// config file, and holdsGitRoot whether it directly contains a .git entry.
// Both are injected so the derivation does no I/O of its own.
//
// path must be absolute. A relative path has no ancestors to walk, so its
// cleaned form with forward slashes is returned.
func DeriveTestKey(holdsConfig, holdsGitRoot func(dir string) bool, path string) string {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return filepath.ToSlash(clean)
	}
	start := filepath.Dir(clean)
	root := nearestAncestor(start, holdsConfig)
	if root == "" {
		root = nearestAncestor(start, holdsGitRoot)
	}
	if root == "" {
		root = start
	}
	rel, err := filepath.Rel(root, clean)
	if err != nil {
		return filepath.ToSlash(clean)
	}
	return filepath.ToSlash(rel)
}

// MCPTestKey returns the test key of an MCP run named name.
func MCPTestKey(name string) string {
	return MCPTestKeyPrefix + name
}

// nearestAncestor returns the first directory from start upward, start
// included, for which holds is true, or an empty string when none is.
func nearestAncestor(start string, holds func(dir string) bool) string {
	dir := start
	for {
		if holds(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
