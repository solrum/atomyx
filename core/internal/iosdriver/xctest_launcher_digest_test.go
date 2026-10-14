package iosdriver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/iosdriver"
)

const digestTestSource = "Tests/Runner.swift"

type digestEnv struct {
	t      *testing.T
	dir    string
	runner *fakeXcodebuildRunner
}

func newDigestEnv(t *testing.T) *digestEnv {
	t.Helper()
	dir, _ := newTestProjectDir(t)
	env := &digestEnv{t: t, dir: dir, runner: &fakeXcodebuildRunner{xcodeVersion: "27.0"}}
	env.write("project.yml", "name: AtomyxDriver\n")
	env.write(digestTestSource, "let a = 1\n")
	return env
}

func (e *digestEnv) write(rel, content string) {
	e.t.Helper()
	path := filepath.Join(e.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *digestEnv) launcher(cfg iosdriver.LaunchConfig) *iosdriver.XctestLauncher {
	cfg.ProjectDir = e.dir
	if cfg.UDID == "" {
		cfg.UDID = "UDID-1"
	}
	return iosdriver.NewXctestLauncher(e.runner, e.runner, &fakeClock{}, cfg)
}

func (e *digestEnv) simulator() *iosdriver.XctestLauncher {
	return e.launcher(iosdriver.LaunchConfig{Kind: iosdriver.KindSimulator})
}

func (e *digestEnv) device(team string) *iosdriver.XctestLauncher {
	return e.launcher(iosdriver.LaunchConfig{Kind: iosdriver.KindDevice, DevTeam: team})
}

func (e *digestEnv) ensure(l *iosdriver.XctestLauncher) {
	e.t.Helper()
	if err := l.EnsureBuilt(context.Background()); err != nil {
		e.t.Fatalf("EnsureBuilt() error = %v", err)
	}
}

func (e *digestEnv) builds() int {
	count := 0
	for _, call := range e.runner.Invocations() {
		if len(call.Args) > 0 && call.Args[0] == "build-for-testing" {
			count++
		}
	}
	return count
}

func (e *digestEnv) productPath(config string) string {
	return filepath.Join(e.dir, "build", "Build", "Products", config, "AtomyxDriverAgent-Runner.app")
}

func (e *digestEnv) digestFile(config string) string {
	data, err := os.ReadFile(e.productPath(config) + ".digest")
	if err != nil {
		return ""
	}
	return string(data)
}

func TestEnsureBuilt_UnchangedTreeBuildsOnce(t *testing.T) {
	env := newDigestEnv(t)
	l := env.simulator()
	env.ensure(l)
	env.ensure(env.simulator())
	if got := env.builds(); got != 1 {
		t.Fatalf("builds = %d, want 1 for an unchanged tree", got)
	}
	if env.digestFile("Debug-iphonesimulator") == "" {
		t.Fatal("no digest file was written after the build")
	}
}

func TestEnsureBuilt_SourceChangeWithRestoredTimeRebuilds(t *testing.T) {
	env := newDigestEnv(t)
	source := filepath.Join(env.dir, digestTestSource)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	env.ensure(env.simulator())

	env.write(digestTestSource, "let a = 2\n")
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	env.ensure(env.simulator())

	if got := env.builds(); got != 2 {
		t.Fatalf("builds = %d, want 2 after a content change that kept its old modification time", got)
	}
}

func TestEnsureBuilt_TouchWithoutContentChangeDoesNotRebuild(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(env.dir, digestTestSource), future, future); err != nil {
		t.Fatal(err)
	}
	env.ensure(env.simulator())

	if got := env.builds(); got != 1 {
		t.Fatalf("builds = %d, want 1 when only a modification time changed", got)
	}
}

func TestEnsureBuilt_NonSwiftInputChangeRebuilds(t *testing.T) {
	env := newDigestEnv(t)
	env.write("Tests/Info.plist", "<plist/>")
	env.ensure(env.simulator())
	env.write("Tests/Info.plist", "<plist>2</plist>")
	env.ensure(env.simulator())
	if got := env.builds(); got != 2 {
		t.Fatalf("builds = %d, want 2 after a plist change", got)
	}
}

func TestEnsureBuilt_SelectedXcodeChangeRebuilds(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())
	env.runner.xcodeVersion = "27.1"
	env.ensure(env.simulator())
	if got := env.builds(); got != 2 {
		t.Fatalf("builds = %d, want 2 after the selected Xcode changed", got)
	}
}

func TestEnsureBuilt_SigningInputChangeRebuilds(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.device("TEAM1"))
	env.ensure(env.device("TEAM1"))
	if got := env.builds(); got != 1 {
		t.Fatalf("builds = %d, want 1 for the same signing team", got)
	}
	env.ensure(env.device("TEAM2"))
	if got := env.builds(); got != 2 {
		t.Fatalf("builds = %d, want 2 after the signing team changed", got)
	}
}

func TestEnsureBuilt_FailedBuildLeavesTheProductStale(t *testing.T) {
	env := newDigestEnv(t)
	source := filepath.Join(env.dir, digestTestSource)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	env.ensure(env.simulator())
	built := env.digestFile("Debug-iphonesimulator")
	if built == "" {
		t.Fatal("setup: no digest after the first build")
	}

	env.write(digestTestSource, "let a = 2\n")
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	env.runner.buildErr = errors.New("exit status 65")
	if err := env.simulator().EnsureBuilt(context.Background()); err == nil {
		t.Fatal("EnsureBuilt() error = nil, want the build failure")
	}
	env.runner.buildErr = nil

	if got := env.digestFile("Debug-iphonesimulator"); got == "" || got == built {
		t.Fatalf("digest file = %q after a failed build, want a marker that is not the previous digest %q", got, built)
	}
	if product, ok := env.simulator().CurrentBuildProduct(context.Background()); ok {
		t.Fatalf("CurrentBuildProduct() = %+v, want nothing for a product whose build failed", product)
	}
	env.ensure(env.simulator())
	if got := env.builds(); got != 3 {
		t.Fatalf("builds = %d, want 3 because the failed build left the product stale", got)
	}
}

func TestEnsureBuilt_EditDuringTheBuildForcesAnotherBuild(t *testing.T) {
	env := newDigestEnv(t)
	env.runner.buildGate = func() { env.write(digestTestSource, "let a = 3\n") }
	env.ensure(env.simulator())
	env.runner.buildGate = nil

	env.ensure(env.simulator())

	if got := env.builds(); got != 2 {
		t.Fatalf("builds = %d, want 2 because the source changed while the first build ran", got)
	}
}

func TestEnsureBuilt_SimulatorAndDeviceKeepSeparateDigests(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())
	env.ensure(env.device("TEAM1"))

	sim, dev := env.digestFile("Debug-iphonesimulator"), env.digestFile("Debug-iphoneos")
	if sim == "" || dev == "" || sim == dev {
		t.Fatalf("digests = %q and %q, want two different recorded digests", sim, dev)
	}

	env.ensure(env.device("TEAM2"))
	if got := env.digestFile("Debug-iphonesimulator"); got != sim {
		t.Fatalf("simulator digest = %q after a device rebuild, want it unchanged (%q)", got, sim)
	}
	if got := env.digestFile("Debug-iphoneos"); got == dev {
		t.Fatal("device digest did not change after the signing team changed")
	}
}

func TestCurrentBuildProduct_ReportsTheDigestOfABuiltProduct(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())

	product, ok := env.simulator().CurrentBuildProduct(context.Background())
	if !ok || product.Digest == "" {
		t.Fatalf("CurrentBuildProduct() = %+v, %v, want the product with its digest", product, ok)
	}

	env.write(digestTestSource, "let a = 2\n")
	if product, ok := env.simulator().CurrentBuildProduct(context.Background()); ok {
		t.Fatalf("CurrentBuildProduct() = %+v, want nothing after a source change", product)
	}
}

func TestCurrentBuildProduct_DigestLessFreshProductIsAdopted(t *testing.T) {
	env := newDigestEnv(t)
	old := time.Now().Add(-time.Hour)
	createFreshRunnerApp(t, env.dir, iosdriver.KindSimulator, old)
	for _, rel := range []string{"project.yml", digestTestSource, "AtomyxDriver.xcodeproj/project.pbxproj"} {
		before := old.Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(env.dir, rel), before, before); err != nil {
			t.Fatal(err)
		}
	}

	product, ok := env.simulator().CurrentBuildProduct(context.Background())

	if !ok || product.Digest != "" {
		t.Fatalf("CurrentBuildProduct() = %+v, %v, want a digest-less product reported as current", product, ok)
	}
	recorded := iosdriver.BuildProduct{Path: product.Path, ModTime: product.ModTime}
	if !recorded.Matches(product) {
		t.Fatal("a record without a digest does not match the digest-less product it was taken from")
	}
	env.ensure(env.simulator())
	if got := env.builds(); got != 0 {
		t.Fatalf("builds = %d, want none for a fresh digest-less product", got)
	}
}

func TestInspectBuildProduct_SharedXcodeIdentityReadsTheVersionOnce(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())
	reads := env.runner.VersionReads()
	shared := iosdriver.NewXcodeIdentity()
	for i := 0; i < 3; i++ {
		launcher := env.launcher(iosdriver.LaunchConfig{Kind: iosdriver.KindSimulator, Xcode: shared})
		if _, reason := launcher.InspectBuildProduct(context.Background()); reason != "" {
			t.Fatalf("InspectBuildProduct() reason = %q, want a current product", reason)
		}
	}
	if got := env.runner.VersionReads() - reads; got != 1 {
		t.Fatalf("version reads = %d across three checks sharing one identity, want 1", got)
	}
}

func TestInspectBuildProduct_SwitchedXcodeIsReadAgainThroughASharedIdentity(t *testing.T) {
	env := newDigestEnv(t)
	shared := iosdriver.NewXcodeIdentity()
	cfg := iosdriver.LaunchConfig{Kind: iosdriver.KindSimulator, Xcode: shared}
	env.ensure(env.launcher(cfg))

	env.runner.developerDir = "/Applications/Xcode-beta.app/Contents/Developer"
	env.runner.xcodeVersion = "28.0"
	_, reason := env.launcher(cfg).InspectBuildProduct(context.Background())

	if reason != "the runner product was built from other inputs" {
		t.Fatalf("reason = %q, want the switched Xcode to read as other inputs", reason)
	}
}

func TestInspectBuildProduct_UnreadableXcodeIsReportedAsTheReason(t *testing.T) {
	env := newDigestEnv(t)
	env.ensure(env.simulator())
	env.runner.xcodeErr = errors.New("xcode-select: no developer directory")

	product, reason := env.simulator().InspectBuildProduct(context.Background())

	if !strings.Contains(reason, "cannot be read") || !strings.Contains(reason, "selected Xcode") {
		t.Fatalf("reason = %q, want the unreadable Xcode named", reason)
	}
	if product != (iosdriver.BuildProduct{}) {
		t.Fatalf("product = %+v, want none for a product that cannot be checked", product)
	}
}

func TestInspectBuildProduct_ReasonsForAMissingAndAnInterruptedProduct(t *testing.T) {
	env := newDigestEnv(t)
	if _, reason := env.simulator().InspectBuildProduct(context.Background()); reason != "the runner product is missing" {
		t.Fatalf("missing product reason = %q", reason)
	}
	env.ensure(env.simulator())
	env.write(digestTestSource, "let a = 2\n")
	env.runner.buildErr = errors.New("exit status 65")
	_ = env.simulator().EnsureBuilt(context.Background())
	_, reason := env.simulator().InspectBuildProduct(context.Background())
	if reason != "the last build of the runner product did not finish" {
		t.Fatalf("interrupted build reason = %q", reason)
	}
}
