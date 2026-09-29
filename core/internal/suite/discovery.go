package suite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlExtensions are the file extensions Resolve's filesystem walk
// collects as candidate test files.
var yamlExtensions = map[string]bool{".yml": true, ".yaml": true}

// skippedNames are directory or file names Resolve's filesystem walk
// never descends into or collects. A dot-prefixed name is skipped
// separately, not listed here.
var skippedNames = map[string]bool{"node_modules": true}

// Selector selects a subset of the test corpus. Tags and Globs
// combine with AND when both are given: a candidate must match at
// least one glob (when Globs is non-empty) and carry at least one
// matching tag (when Tags is non-empty). Giving only one of the two
// selects purely by that dimension.
type Selector struct {
	Tags  []string
	Globs []string
}

// ResolvedFlow is one flow file Resolve found, ready to be loaded and
// run.
type ResolvedFlow struct {
	// Path is the absolute filesystem path to the script or scenario
	// file.
	Path string
}

// DiscoveryOptions configures one Resolve call.
type DiscoveryOptions struct {
	// Cwd is the base directory glob patterns resolve against when
	// TestRoots is empty.
	Cwd string
	// TestRoots are the directories to search for candidate files.
	// Takes precedence over Cwd whenever it is non-empty.
	TestRoots []string
}

// discoveryFS is the narrow filesystem contract a discoverer's
// directory walk depends on: reading a directory's entries and
// reading one file's bytes. Depending on this contract instead of the
// os package directly lets a test drive the walk against an in-memory
// filesystem without touching disk.
type discoveryFS interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// osDiscoveryFS implements discoveryFS against the real OS
// filesystem. Resolve wires this in by default; only a test
// constructs a discoverer with a different discoveryFS.
type osDiscoveryFS struct{}

func (osDiscoveryFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (osDiscoveryFS) ReadFile(name string) ([]byte, error)       { return os.ReadFile(name) }

// discoverer resolves a Selector against a test corpus read through
// fs, so the directory-walk and tag-scan logic is testable against a
// filesystem double while every production call site keeps talking to
// the real filesystem via Resolve.
type discoverer struct {
	fs discoveryFS
}

// newDiscoverer constructs a discoverer that reads the corpus through
// fs.
func newDiscoverer(fs discoveryFS) *discoverer {
	return &discoverer{fs: fs}
}

// Resolve resolves selector against the on-disk test corpus rooted at
// options.TestRoots (or options.Cwd when TestRoots is empty),
// returning the deduplicated, path-sorted list of matching files.
//
// It rejects an empty selector, and rejects a tags-only selector that
// has neither TestRoots nor a glob to narrow the filesystem walk.
// A TestRoots entry that does not exist on disk contributes no
// candidates rather than failing the call.
//
// A large test corpus can take a while to walk and tag-scan; Resolve
// checks ctx between search roots and between files so a caller that
// cancels ctx gets ctx.Err() back promptly instead of waiting out the
// full walk.
func Resolve(ctx context.Context, selector Selector, options DiscoveryOptions) ([]ResolvedFlow, error) {
	return newDiscoverer(osDiscoveryFS{}).resolve(ctx, selector, options)
}

func (d *discoverer) resolve(ctx context.Context, selector Selector, options DiscoveryOptions) ([]ResolvedFlow, error) {
	tags := selector.Tags
	globs := selector.Globs
	testRoots := options.TestRoots

	if len(tags) == 0 && len(globs) == 0 {
		return nil, errors.New("a suite selector requires at least one of tags or globs")
	}
	if len(tags) > 0 && len(globs) == 0 && len(testRoots) == 0 {
		return nil, errors.New(
			"selecting a suite by tags alone requires testRoots in the " +
				"project config, or at least one glob to narrow the search",
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var searchDirs []string
	if len(testRoots) > 0 {
		for _, root := range testRoots {
			searchDirs = append(searchDirs, resolveDir(options.Cwd, root))
		}
	} else {
		searchDirs = []string{resolveDir(options.Cwd, ".")}
	}

	var globMatcher *GlobMatcher
	if len(globs) > 0 {
		globMatcher = NewGlobMatcher(globs)
	}

	matches := make(map[string]bool)
	for _, dir := range searchDirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, err := d.walkYamlFiles(ctx, dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if globMatcher != nil {
				relativePath := toPosixRelative(dir, file)
				if !globMatcher.MatchAny(relativePath) {
					continue
				}
			}
			matches[file] = true
		}
	}

	candidates := make([]string, 0, len(matches))
	for file := range matches {
		candidates = append(candidates, file)
	}

	if len(tags) > 0 {
		var withMatchingTag []string
		for _, file := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if hasAnyTag(d.readTopLevelTags(file), tags) {
				withMatchingTag = append(withMatchingTag, file)
			}
		}
		candidates = withMatchingTag
	}

	sort.Strings(candidates)

	flows := make([]ResolvedFlow, len(candidates))
	for i, path := range candidates {
		flows[i] = ResolvedFlow{Path: path}
	}
	return flows, nil
}

// resolveDir resolves a testRoots entry (or cwd itself) against cwd:
// an absolute path is used as-is, a relative one is joined onto cwd.
func resolveDir(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

// walkYamlFiles collects every .yml/.yaml file under dir, recursing
// into subdirectories, skipping any entry (file or directory) whose
// name is dot-prefixed or listed in skippedNames. Any error reading
// dir (a testRoots entry that does not exist, or one Resolve's caller
// otherwise cannot see) yields no candidates from that directory
// rather than failing the whole walk. It checks ctx before descending
// into each entry and returns ctx.Err() as soon as ctx is canceled,
// so a walk over a large or slow tree does not run to completion
// after the caller has stopped waiting on it.
func (d *discoverer) walkYamlFiles(ctx context.Context, dir string) ([]string, error) {
	entries, err := d.fs.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	var files []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || skippedNames[name] {
			continue
		}
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			nested, err := d.walkYamlFiles(ctx, full)
			if err != nil {
				return nil, err
			}
			files = append(files, nested...)
		} else if entry.Type().IsRegular() && hasYamlExtension(name) {
			files = append(files, full)
		}
	}
	return files, nil
}

func hasYamlExtension(name string) bool {
	dotIdx := strings.LastIndexByte(name, '.')
	return dotIdx >= 0 && yamlExtensions[name[dotIdx:]]
}

func toPosixRelative(dir, file string) string {
	rel, err := filepath.Rel(dir, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

func hasAnyTag(fileTags, wanted []string) bool {
	for _, tag := range fileTags {
		for _, w := range wanted {
			if tag == w {
				return true
			}
		}
	}
	return false
}

// readTopLevelTags reads the top-level "tags" field out of the YAML
// document at path, returning nil for any file that cannot be read,
// fails to parse, or whose top level is not a mapping (including a
// script file, whose top level is a step sequence). A file that only
// needs a tag-membership check may still fail full schema validation
// for reasons unrelated to tags, and that failure belongs to the run
// step, not discovery.
func (d *discoverer) readTopLevelTags(path string) []string {
	raw, err := d.fs.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	rawTags, ok := doc["tags"]
	if !ok {
		return nil
	}
	list, ok := rawTags.([]any)
	if !ok {
		return nil
	}
	tags := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags
}
