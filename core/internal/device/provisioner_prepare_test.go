package device

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/process"
)

type prepareFixture struct {
	runner     *process.Fake
	background *process.FakeBackground
	provision  *DriverProvisioner
	projectDir string
}

func newPrepareFixture(t *testing.T) *prepareFixture {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "AtomyxDriver.xcodeproj"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := process.NewFake()
	background := process.NewFakeBackground()
	return &prepareFixture{
		runner: runner, background: background, projectDir: projectDir,
		provision: NewDriverProvisioner(DriverProvisionerDeps{Runner: runner, Background: background, IosProjectDir: projectDir}),
	}
}

func (f *prepareFixture) assertNothingTouched(t *testing.T) {
	t.Helper()
	if got := f.runner.Invocations(); len(got) != 0 {
		t.Fatalf("process invocations = %+v, want none", got)
	}
	if got := f.background.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want none", got)
	}
	if _, err := os.Stat(filepath.Join(f.projectDir, "build")); !os.IsNotExist(err) {
		t.Fatalf("build directory stat error = %v, want it not created", err)
	}
}

func TestPrepareBuildsAnIosRunnerWithoutStartingIt(t *testing.T) {
	f := newPrepareFixture(t)

	err := f.provision.Prepare(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator})

	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	builds := 0
	for _, call := range f.runner.Invocations() {
		if call.Name == "xcodebuild" && len(call.Args) > 0 && call.Args[0] == "build-for-testing" {
			builds++
		}
	}
	if builds != 1 {
		t.Fatalf("process invocations = %+v, want exactly one xcodebuild build", f.runner.Invocations())
	}
	if got := f.background.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want no runner started by Prepare", got)
	}
}

func TestPrepareIsANoOpForAndroid(t *testing.T) {
	f := newPrepareFixture(t)

	err := f.provision.Prepare(context.Background(), Descriptor{ID: "emulator-5554", Platform: PlatformAndroid, Kind: KindEmulator})

	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	f.assertNothingTouched(t)
}

func TestPrepareIsANoOpForAServedRunner(t *testing.T) {
	f := newPrepareFixture(t)

	err := f.provision.Prepare(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator, Port: iosControlPort})

	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	f.assertNothingTouched(t)
}
