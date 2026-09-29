package device

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveIosProjectDir_Configured_ReturnsVerbatim guards the first
// tier: an explicit setting always wins, even one that names a path
// that does not exist, because a caller that configured a directory
// gets to own the resulting error rather than have it silently
// papered over by a fallback.
func TestResolveIosProjectDir_Configured_ReturnsVerbatim(t *testing.T) {
	got := resolveIosProjectDir("/does/not/exist/ios-agent", "/also/missing")
	if want := "/does/not/exist/ios-agent"; got != want {
		t.Fatalf("resolveIosProjectDir() = %q, want %q", got, want)
	}
}

// TestResolveIosProjectDir_EnvExists_ReturnsIt guards the second tier:
// an environment override is trusted once it is confirmed to point at
// a real path on disk.
func TestResolveIosProjectDir_EnvExists_ReturnsIt(t *testing.T) {
	dir := t.TempDir()

	got := resolveIosProjectDir("", dir)
	if got != dir {
		t.Fatalf("resolveIosProjectDir() = %q, want %q", got, dir)
	}
}

// TestResolveIosProjectDir_EnvMissing_FallsThrough guards that a
// dangling environment override is ignored rather than returned
// as-is: since nothing else resolves in this scenario (a temp dir with
// no platforms/ios-agent anywhere up the tree), the result must be
// empty rather than the nonexistent env path.
func TestResolveIosProjectDir_EnvMissing_FallsThrough(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", cwd, err)
	}
	t.Chdir(cwd)

	got := resolveIosProjectDir("", filepath.Join(root, "does-not-exist"))
	if got != "" {
		t.Fatalf("resolveIosProjectDir() = %q, want empty", got)
	}
}

// TestResolveIosProjectDir_WalksUpFromCwd guards the walk-up fallback
// that matters most in production: a runtime binary spawned with its
// working directory several levels below the repository root, and
// neither a configured directory nor the environment variable set,
// still finds platforms/ios-agent by climbing from the current working
// directory.
func TestResolveIosProjectDir_WalksUpFromCwd(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, "platforms", "ios-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", agentDir, err)
	}
	cwd := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", cwd, err)
	}
	t.Chdir(cwd)

	got := resolveIosProjectDir("", "")
	if got != agentDir {
		t.Fatalf("resolveIosProjectDir() = %q, want %q", got, agentDir)
	}
}

// TestResolveIosProjectDir_NoMatchAnywhere_ReturnsEmpty guards the
// terminal case: when configured and env are both empty and no anchor
// finds a platforms/ios-agent directory within the walk-up bound, the
// function returns empty so the launcher can report its own
// no-project-found error instead of proceeding with a bogus path.
func TestResolveIosProjectDir_NoMatchAnywhere_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", cwd, err)
	}
	t.Chdir(cwd)

	got := resolveIosProjectDir("", "")
	if got != "" {
		t.Fatalf("resolveIosProjectDir() = %q, want empty", got)
	}
}
