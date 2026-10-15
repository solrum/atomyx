package iosdriver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
)

// realRunnerProjectDir is the shipped iOS runner project, read only.
const realRunnerProjectDir = "../../../platforms/ios-agent"

func BenchmarkBuildDigest_ShippedRunnerTree(b *testing.B) {
	if _, err := os.Stat(filepath.Join(realRunnerProjectDir, xctestProjectSpecFileName)); err != nil {
		drivertest.SkipBecause(b, drivertest.SkipHostPrecondition, fmt.Sprintf("runner project not available: %v", err))
	}
	fake := process.NewFake()
	l := NewXctestLauncher(fake, process.NewFakeBackground(), nil, LaunchConfig{ProjectDir: realRunnerProjectDir, Kind: KindSimulator})
	b.ResetTimer()
	for range b.N {
		if _, err := l.buildDigest(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
