package drivertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInstallSource(t *testing.T) {
	root := t.TempDir()
	apk := filepath.Join(root, "build", "app.apk")
	if err := os.MkdirAll(filepath.Dir(apk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apk, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "build", "Runner.app")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	notBundle := filepath.Join(root, "build", "plain")
	if err := os.Mkdir(notBundle, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		value   string
		kind    InstallSourceKind
		root    string
		want    string
		wantErr string
	}{
		{name: "absolute file", value: apk, kind: InstallSourceFile, want: apk},
		{name: "relative file resolves against root", value: "build/app.apk", kind: InstallSourceFile, root: root, want: apk},
		{name: "relative without root is rejected", value: "build/app.apk", kind: InstallSourceFile, wantErr: "absolute path"},
		{name: "missing file", value: filepath.Join(root, "nope.apk"), kind: InstallSourceFile, wantErr: "cannot be read"},
		{name: "directory where file expected", value: bundle, kind: InstallSourceFile, wantErr: "regular file"},
		{name: "empty value", value: "", kind: InstallSourceFile, wantErr: "is empty"},
		{name: "app bundle", value: bundle, kind: InstallSourceAppBundle, want: bundle},
		{name: "relative app bundle", value: "build/Runner.app", kind: InstallSourceAppBundle, root: root, want: bundle},
		{name: "file where bundle expected", value: apk, kind: InstallSourceAppBundle, wantErr: ".app directory"},
		{name: "directory without app suffix", value: notBundle, kind: InstallSourceAppBundle, wantErr: ".app directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveInstallSource("ATOMYX_TEST_VAR", tc.value, tc.kind, tc.root)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("got %q, want error containing %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "ATOMYX_TEST_VAR") {
					t.Fatalf("error %q should name the variable and contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRepoRootFrom(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "internal", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "go.mod"), []byte("module m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := repoRootFrom(filepath.Join(root, "core", "internal", "x")); got != root {
		t.Fatalf("got %q, want %q", got, root)
	}
}
