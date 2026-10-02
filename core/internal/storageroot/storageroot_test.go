package storageroot

import (
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Setenv(EnvVar, "")
	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, HomeDirName); got != want {
		t.Errorf("default root = %q, want %q", got, want)
	}

	envRoot := t.TempDir()
	t.Setenv(EnvVar, envRoot)
	if got, _ := Resolve(""); got != envRoot {
		t.Errorf("env root = %q, want %q", got, envRoot)
	}

	explicit := t.TempDir()
	if got, _ := Resolve(explicit); got != explicit {
		t.Errorf("explicit root = %q, want %q", got, explicit)
	}
}
