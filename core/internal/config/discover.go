package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var configFileNames = []string{"atomyx.config.yaml", "atomyx.config.yml"}

// ValidationError reports that a config document failed schema
// validation. Its Error() text is informational only. A caller that
// needs to inspect individual failures should read Issues instead of
// matching on wording.
type ValidationError struct {
	Path   string
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, fmt.Sprintf("%s: %s", pathString(issue.Path), issue.Message))
	}
	return fmt.Sprintf("invalid Atomyx config at %s: %s", e.Path, strings.Join(parts, "; "))
}

func pathString(path []any) string {
	if len(path) == 0 {
		return "(root)"
	}
	segments := make([]string, len(path))
	for i, segment := range path {
		switch v := segment.(type) {
		case string:
			segments[i] = v
		case int:
			segments[i] = strconv.Itoa(v)
		default:
			segments[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(segments, ".")
}

// Load discovers (or resolves an explicit path to), reads, parses,
// validates, and env-ref-resolves an Atomyx project config. It
// returns (nil, nil) when discovery finds no config file rather than
// treating that as an error, so a project without a config keeps
// working from flags alone.
func Load(opts LoadOptions) (*LoadedConfig, error) {
	var path string
	if opts.ConfigPath != "" {
		path = resolvePath(opts.Cwd, opts.ConfigPath)
	} else {
		discovered, err := discoverConfigPath(resolveAbs(opts.Cwd))
		if err != nil {
			return nil, err
		}
		if discovered == "" {
			return nil, nil
		}
		path = discovered
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	cfg, issues := ParseAndValidate(raw)
	if len(issues) > 0 {
		return nil, &ValidationError{Path: path, Issues: issues}
	}

	resolved, err := ResolveEnvRefs(*cfg, path)
	if err != nil {
		return nil, err
	}

	return &LoadedConfig{Path: path, Config: resolved}, nil
}

func discoverConfigPath(startDir string) (string, error) {
	dir := startDir
	for {
		for _, name := range configFileNames {
			candidate := filepath.Join(dir, name)
			if fileExists(candidate) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func resolvePath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(resolveAbs(cwd), path)
}

func resolveAbs(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
