package lease

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func testEnv(t *testing.T) (Environment, string) {
	t.Helper()
	root := t.TempDir()
	return Environment{
		GOOS:          "darwin",
		Getenv:        func(string) string { return "" },
		UserCacheDir:  func() (string, error) { return filepath.Join(root, "cache"), nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  filepath.Join(root, "tmp"),
		MaxSocketPath: 4096,
	}, root
}

func mkFallbackRoot(t *testing.T, env Environment) {
	t.Helper()
	if err := os.MkdirAll(env.FallbackRoot, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRuntimeDir_DarwinUsesRunUnderCacheDir(t *testing.T) {
	env, root := testEnv(t)
	got, err := ResolveRuntimeDir(env)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "cache", "atomyx", "run")
	if got != want {
		t.Fatalf("dir = %q, want %q", got, want)
	}
	info, _ := os.Stat(got)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestResolveRuntimeDir_NonLocalHomeFallsBackToTmp(t *testing.T) {
	env, _ := testEnv(t)
	mkFallbackRoot(t, env)
	var probed []string
	env.IsLocalFS = func(p string) (bool, error) { probed = append(probed, p); return false, nil }

	got, err := ResolveRuntimeDir(env)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(env.FallbackRoot, "atomyx-"+strconv.Itoa(os.Getuid()))
	if got != want {
		t.Fatalf("dir = %q, want fallback %q", got, want)
	}
	if len(probed) == 0 {
		t.Fatal("the local-filesystem check was never consulted")
	}
}

func TestResolveRuntimeDir_StatfsErrorFallsBack(t *testing.T) {
	env, _ := testEnv(t)
	mkFallbackRoot(t, env)
	env.IsLocalFS = func(string) (bool, error) { return false, errors.New("statfs failed") }

	got, err := ResolveRuntimeDir(env)
	if err != nil || !strings.HasPrefix(got, env.FallbackRoot) {
		t.Fatalf("dir = %q, err = %v", got, err)
	}
}

func TestResolveRuntimeDir_CacheDirErrorFallsBack(t *testing.T) {
	env, _ := testEnv(t)
	mkFallbackRoot(t, env)
	env.UserCacheDir = func() (string, error) { return "", errors.New("no HOME") }

	got, err := ResolveRuntimeDir(env)
	if err != nil || !strings.HasPrefix(got, env.FallbackRoot) {
		t.Fatalf("dir = %q, err = %v", got, err)
	}
}

func TestResolveRuntimeDir_LongSocketPathFallsBack(t *testing.T) {
	env, root := testEnv(t)
	mkFallbackRoot(t, env)
	long := filepath.Join(root, "cache")
	env.MaxSocketPath = len(long) + 1 + socketNameLen + len("/atomyx/run") - 1
	env.UserCacheDir = func() (string, error) { return long, nil }

	got, err := ResolveRuntimeDir(env)
	if err != nil || !strings.HasPrefix(got, env.FallbackRoot) {
		t.Fatalf("dir = %q, err = %v", got, err)
	}
}

func TestResolveRuntimeDir_LinuxUsesXDGRuntimeDir(t *testing.T) {
	env, root := testEnv(t)
	env.GOOS = "linux"
	xdg := filepath.Join(root, "x")
	if err := os.MkdirAll(xdg, 0o700); err != nil {
		t.Fatal(err)
	}
	env.Getenv = func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return xdg
		}
		return ""
	}
	got, err := ResolveRuntimeDir(env)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(xdg, "atomyx") && !strings.HasPrefix(got, env.FallbackRoot) {
		t.Fatalf("dir = %q", got)
	}
}

func TestResolveRuntimeDir_LinuxWithoutXDGFallsBack(t *testing.T) {
	env, _ := testEnv(t)
	mkFallbackRoot(t, env)
	env.GOOS = "linux"

	got, err := ResolveRuntimeDir(env)
	if err != nil || !strings.HasPrefix(got, env.FallbackRoot) {
		t.Fatalf("dir = %q, err = %v", got, err)
	}
}

func TestResolveRuntimeDir_SymlinkIsRejected(t *testing.T) {
	env, root := testEnv(t)
	mkFallbackRoot(t, env)
	env.IsLocalFS = func(string) (bool, error) { return false, nil }
	target := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.FallbackRoot, "atomyx-"+strconv.Itoa(os.Getuid()))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveRuntimeDir(env)
	var unsafe *UnsafeDirError
	if !errors.As(err, &unsafe) {
		t.Fatalf("err = %v, want UnsafeDirError", err)
	}
}

func TestResolveRuntimeDir_ForeignOwnerIsRejected(t *testing.T) {
	env, _ := testEnv(t)
	mkFallbackRoot(t, env)
	env.IsLocalFS = func(string) (bool, error) { return false, nil }
	env.Getuid = func() int { return os.Getuid() + 1 }

	_, err := ResolveRuntimeDir(env)
	var unsafe *UnsafeDirError
	if !errors.As(err, &unsafe) {
		t.Fatalf("err = %v, want UnsafeDirError", err)
	}
}
