package lease

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	// maxSocketPath is the longest unix socket path the supported hosts
	// accept, in bytes.
	maxSocketPath = 104

	// socketNameLen is the length of "<16 hex key>.sock".
	socketNameLen = keyHexLen + len(".sock")
)

// Environment is every host fact ResolveRuntimeDir reads, so a test can
// substitute each one.
type Environment struct {
	// GOOS is the operating system name, as runtime.GOOS reports it.
	GOOS string
	// Getenv reads an environment variable.
	Getenv func(string) string
	// UserCacheDir reports the per-user cache directory.
	UserCacheDir func() (string, error)
	// Getuid reports the current user id.
	Getuid func() int
	// IsLocalFS reports whether the filesystem holding path is local to
	// this host. A network filesystem cannot be trusted to honor flock
	// across hosts, so a home-based directory on one is not used.
	IsLocalFS func(path string) (bool, error)
	// MaxSocketPath is the longest socket path a directory may produce.
	// Zero means the platform limit of 104 bytes.
	MaxSocketPath int
	// FallbackRoot is the directory that holds the per-user fallback
	// runtime directory. It is /tmp outside tests.
	FallbackRoot string
}

// DefaultEnvironment returns the Environment of the running process.
func DefaultEnvironment() Environment {
	return Environment{
		GOOS:         runtime.GOOS,
		Getenv:       os.Getenv,
		UserCacheDir: os.UserCacheDir,
		Getuid:       os.Getuid,
		IsLocalFS:    isLocalFS,
		FallbackRoot: "/tmp",
	}
}

// UnsafeDirError reports a runtime directory that is not a real
// directory owned by the current user.
type UnsafeDirError struct {
	Path   string
	Reason string
}

func (e *UnsafeDirError) Error() string {
	return fmt.Sprintf("runtime directory %s is not safe to use: %s", e.Path, e.Reason)
}

// ResolveRuntimeDir picks, creates and validates the directory that
// holds lock and socket files.
//
// Linux uses $XDG_RUNTIME_DIR/atomyx. Other hosts use a run
// subdirectory of the user cache directory. The per-user fallback
// <FallbackRoot>/atomyx-<uid> replaces that choice when it is unset,
// when the cache directory is unavailable, when a socket path under it
// would exceed 104 bytes, when it cannot be created, or when it sits on
// a filesystem that is not local.
//
// The returned directory has mode 0700 at creation, is a real
// directory, is not a symlink, and is owned by the current user.
// Otherwise the error is an *UnsafeDirError.
func ResolveRuntimeDir(env Environment) (string, error) {
	fallback := filepath.Join(env.FallbackRoot, "atomyx-"+strconv.Itoa(env.Getuid()))
	if candidate, ok := preferredDir(env); ok && usable(env, candidate) {
		return validateDir(env, candidate)
	}
	if err := os.MkdirAll(fallback, dirMode); err != nil {
		return "", fmt.Errorf("create runtime directory %s: %w", fallback, err)
	}
	return validateDir(env, fallback)
}

func preferredDir(env Environment) (string, bool) {
	if env.GOOS == "linux" {
		xdg := env.Getenv("XDG_RUNTIME_DIR")
		if xdg == "" {
			return "", false
		}
		return filepath.Join(xdg, "atomyx"), true
	}
	cache, err := env.UserCacheDir()
	if err != nil || cache == "" {
		return "", false
	}
	return filepath.Join(cache, "atomyx", "run"), true
}

func usable(env Environment, dir string) bool {
	limit := env.MaxSocketPath
	if limit == 0 {
		limit = maxSocketPath
	}
	if len(dir)+1+socketNameLen > limit {
		return false
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return false
	}
	local, err := env.IsLocalFS(dir)
	return err == nil && local
}

func validateDir(env Environment, dir string) (string, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("inspect runtime directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", &UnsafeDirError{Path: dir, Reason: "it is a symlink"}
	}
	if !info.IsDir() {
		return "", &UnsafeDirError{Path: dir, Reason: "it is not a directory"}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != env.Getuid() {
		return "", &UnsafeDirError{Path: dir, Reason: "it is not owned by the current user"}
	}
	return dir, nil
}
