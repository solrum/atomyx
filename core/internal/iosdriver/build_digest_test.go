package iosdriver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

func writeTreeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newDigestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTreeFile(t, dir, "project.yml", "name: AtomyxDriver\n")
	writeTreeFile(t, dir, "AtomyxDriver.xcodeproj/project.pbxproj", "{}")
	writeTreeFile(t, dir, "Tests/Runner.swift", "let a = 1\n")
	writeTreeFile(t, dir, "Tests/Bridge/Bridge.h", "int a;\n")
	writeTreeFile(t, dir, "Tests/Info.plist", "<plist/>")
	writeTreeFile(t, dir, "App/Generated-Info.plist", "<plist/>")
	return dir
}

func newDigestLauncher(t *testing.T, dir string, cfg LaunchConfig, developerDir, version string) *XctestLauncher {
	t.Helper()
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: developerDir + "\n"}, nil)
	fake.Stub(process.ExactMatch("xcodebuild", "-version"), process.Result{Stdout: version + "\n"}, nil)
	cfg.ProjectDir = dir
	return NewXctestLauncher(fake, process.NewFakeBackground(), nil, cfg)
}

func digestOf(t *testing.T, l *XctestLauncher) string {
	t.Helper()
	digest, err := l.buildDigest(context.Background())
	if err != nil {
		t.Fatalf("buildDigest() error = %v", err)
	}
	return digest
}

func simulatorLauncher(t *testing.T, dir string) *XctestLauncher {
	return newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "Xcode 27.0")
}

func TestBuildDigest_IsStableForAnUnchangedTree(t *testing.T) {
	dir := newDigestTree(t)
	first := digestOf(t, simulatorLauncher(t, dir))
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "Tests/Runner.swift"), later, later); err != nil {
		t.Fatal(err)
	}
	if got := digestOf(t, simulatorLauncher(t, dir)); got != first {
		t.Fatalf("digest changed with only a modification time: %s then %s", first, got)
	}
}

func TestBuildDigest_ChangesWithEveryCompiledInput(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{"swift source content", func(t *testing.T, dir string) { writeTreeFile(t, dir, "Tests/Runner.swift", "let a = 2\n") }},
		{"bridging header", func(t *testing.T, dir string) { writeTreeFile(t, dir, "Tests/Bridge/Bridge.h", "int b;\n") }},
		{"test bundle plist", func(t *testing.T, dir string) { writeTreeFile(t, dir, "Tests/Info.plist", "<plist>1</plist>") }},
		{"generated app plist", func(t *testing.T, dir string) { writeTreeFile(t, dir, "App/Generated-Info.plist", "<plist>1</plist>") }},
		{"added source file", func(t *testing.T, dir string) { writeTreeFile(t, dir, "Sources/New.swift", "") }},
		{"removed source file", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "Tests/Info.plist")); err != nil {
				t.Fatal(err)
			}
		}},
		{"project spec", func(t *testing.T, dir string) { writeTreeFile(t, dir, "project.yml", "name: Other\n") }},
		{"project file", func(t *testing.T, dir string) {
			writeTreeFile(t, dir, "AtomyxDriver.xcodeproj/project.pbxproj", "{ changed }")
		}},
		{"project spec removed", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "project.yml")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newDigestTree(t)
			before := digestOf(t, simulatorLauncher(t, dir))
			tc.change(t, dir)
			if after := digestOf(t, simulatorLauncher(t, dir)); after == before {
				t.Fatalf("digest did not change after a change to the %s", tc.name)
			}
		})
	}
}

func TestBuildDigest_IgnoresFinderMetadata(t *testing.T) {
	dir := newDigestTree(t)
	before := digestOf(t, simulatorLauncher(t, dir))
	writeTreeFile(t, dir, "Tests/.DS_Store", "junk")
	writeTreeFile(t, dir, "Tests/Bridge/.DS_Store", "junk")
	if after := digestOf(t, simulatorLauncher(t, dir)); after != before {
		t.Fatal("digest changed after a .DS_Store file appeared")
	}
}

func TestBuildDigest_SeparatesAContentMoveBetweenFiles(t *testing.T) {
	dir := newDigestTree(t)
	writeTreeFile(t, dir, "Sources/A.swift", "ab")
	writeTreeFile(t, dir, "Sources/B.swift", "c")
	before := digestOf(t, simulatorLauncher(t, dir))
	writeTreeFile(t, dir, "Sources/A.swift", "a")
	writeTreeFile(t, dir, "Sources/B.swift", "bc")
	if after := digestOf(t, simulatorLauncher(t, dir)); after == before {
		t.Fatal("digest did not change when content moved from one file to another")
	}
}

func TestBuildDigest_ChangesWithTheSelectedXcode(t *testing.T) {
	dir := newDigestTree(t)
	base := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "Xcode 27.0"))
	if got := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "Xcode 27.1")); got == base {
		t.Fatal("digest did not change with the Xcode version")
	}
	if got := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Beta/Xcode", "Xcode 27.0")); got == base {
		t.Fatal("digest did not change with the Xcode developer directory")
	}
}

func TestBuildDigest_ReadsTheXcodeVersionOncePerDeveloperDirectory(t *testing.T) {
	dir := newDigestTree(t)
	l := simulatorLauncher(t, dir)
	digestOf(t, l)
	digestOf(t, l)
	counts := map[string]int{}
	for _, call := range l.proc.(*process.Fake).Invocations() {
		counts[call.Name]++
	}
	if counts["xcode-select"] != 2 || counts["xcodebuild"] != 1 {
		t.Fatalf("host commands = %v, want the developer directory read twice and the version read once", counts)
	}
}

func TestBuildDigest_SeparatesDestinationKinds(t *testing.T) {
	dir := newDigestTree(t)
	sim := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "v"))
	dev := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindDevice, DevTeam: "T"}, "/Xcode", "v"))
	if sim == dev {
		t.Fatal("simulator and device share a digest")
	}
}

func TestBuildDigest_DeviceSigningInputs(t *testing.T) {
	dir := newDigestTree(t)
	base := LaunchConfig{Kind: KindDevice, DevTeam: "TEAM1", CodeSignStyle: "Manual", ProvisioningProfileSpecifier: "profile-a"}
	want := digestOf(t, newDigestLauncher(t, dir, base, "/Xcode", "v"))
	variants := map[string]LaunchConfig{
		"team":    {Kind: KindDevice, DevTeam: "TEAM2", CodeSignStyle: "Manual", ProvisioningProfileSpecifier: "profile-a"},
		"style":   {Kind: KindDevice, DevTeam: "TEAM1", CodeSignStyle: "Automatic", ProvisioningProfileSpecifier: "profile-a"},
		"profile": {Kind: KindDevice, DevTeam: "TEAM1", CodeSignStyle: "Manual", ProvisioningProfileSpecifier: "profile-b"},
	}
	for name, cfg := range variants {
		if got := digestOf(t, newDigestLauncher(t, dir, cfg, "/Xcode", "v")); got == want {
			t.Errorf("digest did not change with the signing %s", name)
		}
	}
}

func TestBuildDigest_SimulatorIgnoresSigningInputs(t *testing.T) {
	dir := newDigestTree(t)
	plain := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "v"))
	signed := digestOf(t, newDigestLauncher(t, dir, LaunchConfig{Kind: KindSimulator, DevTeam: "TEAM1"}, "/Xcode", "v"))
	if plain != signed {
		t.Fatal("a simulator digest changed with a signing team the simulator build ignores")
	}
}

func TestBuildDigest_ToleratesMissingSourceDirectories(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "AtomyxDriver.xcodeproj/project.pbxproj", "{}")
	if got := digestOf(t, simulatorLauncher(t, dir)); got == "" {
		t.Fatal("digest is empty for a project with no source directories")
	}
}

func TestBuildDigest_ReportsAnUnreadableSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not stop root")
	}
	dir := newDigestTree(t)
	path := filepath.Join(dir, "Tests/Runner.swift")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, err := simulatorLauncher(t, dir).buildDigest(context.Background()); err == nil {
		t.Fatal("buildDigest() error = nil, want the unreadable source reported")
	}
}
