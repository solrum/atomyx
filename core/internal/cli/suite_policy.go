package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/suite"
)

// suitePolicy is how a suite treats a failing entry: how often it retries
// and which entries are quarantined.
type suitePolicy struct {
	Retries    int
	Quarantine []string
}

// resolveSuitePolicy reads the policy of the named config suite, then lets
// --retries override the retry count. An ad-hoc suite has no config entry,
// so it retries only when --retries says so and quarantines nothing.
func resolveSuitePolicy(positional *string, flags map[string]FlagValue, loaded *config.LoadedConfig) (suitePolicy, error) {
	var policy suitePolicy
	if positional != nil && loaded != nil && loaded.Config.Suites != nil {
		entry := (*loaded.Config.Suites)[*positional]
		if entry.Retries != nil {
			policy.Retries = int(*entry.Retries)
		}
		if entry.Quarantine != nil {
			policy.Quarantine = *entry.Quarantine
		}
	}
	raw, given := flagString(flags, "--retries")
	if !given {
		return policy, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > config.MaxSuiteRetries {
		return suitePolicy{}, fmt.Errorf("--retries must be a whole number from 0 to %d, got %q", config.MaxSuiteRetries, raw)
	}
	policy.Retries = n
	return policy, nil
}

// quarantineMatcher reports whether an entry path matches any quarantine
// glob. Globs match the path relative to a search directory exactly as a
// suite's own globs do, and the search directories are the test roots
// resolved against cwd, or cwd alone when there are none.
func quarantineMatcher(globs []string, cwd string, testRoots []string) func(string) bool {
	if len(globs) == 0 {
		return nil
	}
	matcher := suite.NewGlobMatcher(globs)
	roots := []string{filepath.Clean(cwd)}
	if len(testRoots) > 0 {
		roots = roots[:0]
		for _, root := range testRoots {
			if !filepath.IsAbs(root) {
				root = filepath.Join(cwd, root)
			}
			roots = append(roots, filepath.Clean(root))
		}
	}
	return func(entryPath string) bool {
		for _, root := range roots {
			rel, err := filepath.Rel(root, entryPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if matcher.MatchAny(filepath.ToSlash(rel)) {
				return true
			}
		}
		return false
	}
}
