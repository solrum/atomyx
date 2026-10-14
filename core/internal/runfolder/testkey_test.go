package runfolder

import (
	"path/filepath"
	"testing"
)

type fakeProbe struct {
	configs map[string]bool
	gits    map[string]bool
}

func TestDeriveTestKey(t *testing.T) {
	base := t.TempDir()
	at := func(parts ...string) string { return filepath.Join(append([]string{base}, parts...)...) }

	tests := []struct {
		name  string
		probe fakeProbe
		path  string
		want  string
	}{
		{
			name:  "config in an ancestor wins",
			probe: fakeProbe{configs: map[string]bool{at("proj"): true}, gits: map[string]bool{at(): true}},
			path:  at("proj", "flows", "login.yml"),
			want:  "flows/login.yml",
		},
		{
			name:  "nearest config wins over a farther one",
			probe: fakeProbe{configs: map[string]bool{at("proj"): true, at("proj", "app"): true}},
			path:  at("proj", "app", "flows", "a.yml"),
			want:  "flows/a.yml",
		},
		{
			name:  "config beside the file",
			probe: fakeProbe{configs: map[string]bool{at("proj", "flows"): true}},
			path:  at("proj", "flows", "a.yml"),
			want:  "a.yml",
		},
		{
			name:  "git root when no config",
			probe: fakeProbe{gits: map[string]bool{at("repo"): true}},
			path:  at("repo", "tests", "deep", "a.yml"),
			want:  "tests/deep/a.yml",
		},
		{
			name:  "config above the git root still wins",
			probe: fakeProbe{configs: map[string]bool{at("mono"): true}, gits: map[string]bool{at("mono", "pkg"): true}},
			path:  at("mono", "pkg", "a.yml"),
			want:  "pkg/a.yml",
		},
		{
			name:  "file directory when neither exists",
			probe: fakeProbe{},
			path:  at("loose", "a.yml"),
			want:  "a.yml",
		},
		{
			name:  "unclean path is cleaned",
			probe: fakeProbe{configs: map[string]bool{at("proj"): true}},
			path:  at("proj", "flows", "..", "flows", "a.yml"),
			want:  "flows/a.yml",
		},
		{
			name:  "relative path is returned slashed",
			probe: fakeProbe{},
			path:  filepath.Join("flows", "a.yml"),
			want:  "flows/a.yml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTestKey(func(d string) bool { return tc.probe.configs[d] }, func(d string) bool { return tc.probe.gits[d] }, tc.path); got != tc.want {
				t.Fatalf("DeriveTestKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMCPTestKey(t *testing.T) {
	if got := MCPTestKey("smoke"); got != "mcp:smoke" {
		t.Fatalf("MCPTestKey = %q", got)
	}
}
