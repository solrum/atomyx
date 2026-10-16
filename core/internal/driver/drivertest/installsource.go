package drivertest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// InstallSourceKind says what shape of filesystem entry an install
// fixture variable must name.
type InstallSourceKind int

const (
	// InstallSourceFile names a regular file, such as an Android APK.
	InstallSourceFile InstallSourceKind = iota

	// InstallSourceAppBundle names a directory ending in .app, such as
	// a built iOS Runner.app.
	InstallSourceAppBundle
)

const appBundleSuffix = ".app"

// RequireInstallSource resolves the install fixture path held in the
// environment variable name and returns an absolute path, or fails the
// calling test. A relative value is resolved against the repository
// root, because go test runs with the package directory as its working
// directory and a path written relative to the repository would
// otherwise miss. It must run before any step that changes the device,
// so a misconfigured fixture can never leave the device without its
// app. A misconfiguration fails instead of skipping, because the
// operator set the variable on purpose and a skip would hide the
// mistake.
func RequireInstallSource(t *testing.T, name string, kind InstallSourceKind) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("%s: read the working directory: %v", name, err)
	}
	path, err := ResolveInstallSource(name, os.Getenv(name), kind, repoRootFrom(cwd))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// ResolveInstallSource validates one install fixture value. An empty
// repoRoot means the repository root could not be found, in which case
// a relative value is rejected with a message asking for an absolute
// path. The returned error names the variable.
func ResolveInstallSource(name, value string, kind InstallSourceKind, repoRoot string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s is empty: set it to the path of the install fixture", name)
	}
	path := value
	if !filepath.IsAbs(path) {
		if repoRoot == "" {
			return "", fmt.Errorf("%s=%q is relative and the repository root was not found: use an absolute path", name, value)
		}
		path = filepath.Join(repoRoot, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s points at %q, which cannot be read: %w", name, path, err)
	}
	switch kind {
	case InstallSourceAppBundle:
		if !info.IsDir() || filepath.Ext(path) != appBundleSuffix {
			return "", fmt.Errorf("%s points at %q, which is not an %s directory", name, path, appBundleSuffix)
		}
	default:
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s points at %q, which is not a regular file", name, path)
		}
	}
	return path, nil
}

// repoRootFrom walks up from dir to the directory that contains
// core/go.mod and returns it, or "" when none is found.
func repoRootFrom(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "core", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
