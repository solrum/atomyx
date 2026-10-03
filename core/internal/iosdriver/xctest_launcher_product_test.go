package iosdriver_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev.atomyx/internal/iosdriver"
)

func newProductLauncher(t *testing.T, appModTime time.Time) (*iosdriver.XctestLauncher, string, string) {
	t.Helper()
	projectDir, pbxproj := newTestProjectDir(t)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(pbxproj, old, old); err != nil {
		t.Fatal(err)
	}
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, appModTime)
	runner := &fakeXcodebuildRunner{}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir, Kind: iosdriver.KindSimulator, UDID: "SIM-1",
	})
	return l, projectDir, pbxproj
}

func TestXctestLauncher_CurrentBuildProduct_FreshProductReportsItsPathAndStamp(t *testing.T) {
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	l, projectDir, _ := newProductLauncher(t, stamp)

	product, ok := l.CurrentBuildProduct()

	if !ok {
		t.Fatal("CurrentBuildProduct() ok = false, want a fresh product reported")
	}
	wantPath := filepath.Join(projectDir, "build", "Build", "Products", "Debug-iphonesimulator", "AtomyxDriverAgent-Runner.app")
	if product.Path != wantPath {
		t.Errorf("Path = %q, want %q", product.Path, wantPath)
	}
	if !product.ModTime.Equal(stamp) || product.ModTime.Location() != time.UTC {
		t.Errorf("ModTime = %v, want %v in UTC", product.ModTime, stamp)
	}
}

func TestXctestLauncher_CurrentBuildProduct_StaleProductReportsNothing(t *testing.T) {
	l, _, pbxproj := newProductLauncher(t, time.Now().Add(-time.Hour))
	if err := os.Chtimes(pbxproj, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	if product, ok := l.CurrentBuildProduct(); ok {
		t.Fatalf("CurrentBuildProduct() = %+v, want nothing for a product older than the project file", product)
	}
}

func TestXctestLauncher_CurrentBuildProduct_MissingProductReportsNothing(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	runner := &fakeXcodebuildRunner{}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir, Kind: iosdriver.KindSimulator, UDID: "SIM-1",
	})

	if product, ok := l.CurrentBuildProduct(); ok {
		t.Fatalf("CurrentBuildProduct() = %+v, want nothing when no product was built", product)
	}
}

func TestBuildProduct_Matches(t *testing.T) {
	stamp := time.Date(2026, time.October, 3, 9, 0, 0, 123, time.UTC)
	product := iosdriver.BuildProduct{Path: "/build/app", ModTime: stamp}
	tests := []struct {
		name  string
		left  iosdriver.BuildProduct
		right iosdriver.BuildProduct
		want  bool
	}{
		{"same path and stamp", product, iosdriver.BuildProduct{Path: "/build/app", ModTime: stamp.In(time.FixedZone("x", 3600))}, true},
		{"rebuilt product", product, iosdriver.BuildProduct{Path: "/build/app", ModTime: stamp.Add(time.Nanosecond)}, false},
		{"another path", product, iosdriver.BuildProduct{Path: "/other/app", ModTime: stamp}, false},
		{"zero matches nothing", iosdriver.BuildProduct{}, iosdriver.BuildProduct{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.left.Matches(tc.right); got != tc.want {
				t.Fatalf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}
