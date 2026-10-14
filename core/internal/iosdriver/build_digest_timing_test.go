package iosdriver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

// realRunnerProjectDir is the shipped iOS runner project, read only.
const realRunnerProjectDir = "../../../platforms/ios-agent"

// TestBuildDigest_MeasuresTheShippedRunnerTree reports how long one digest
// takes over the real runner project. The cost decides whether a
// time-keyed cache is needed, and stays under the budget below.
func TestBuildDigest_MeasuresTheShippedRunnerTree(t *testing.T) {
	if _, err := os.Stat(filepath.Join(realRunnerProjectDir, xctestProjectSpecFileName)); err != nil {
		t.Skipf("runner project not available: %v", err)
	}
	const runs = 20
	const budget = 50 * time.Millisecond
	l := newDigestLauncher(t, realRunnerProjectDir, LaunchConfig{Kind: KindSimulator}, "/Xcode", "v")
	files, err := l.digestInputFiles()
	if err != nil {
		t.Fatal(err)
	}
	var worst, total time.Duration
	for range runs {
		start := time.Now()
		digestOf(t, l)
		elapsed := time.Since(start)
		total += elapsed
		worst = max(worst, elapsed)
	}
	t.Logf("digest over %d files: mean %v, worst %v across %d runs", len(files), total/runs, worst, runs)
	if mean := total / runs; mean > budget {
		t.Fatalf("mean digest time %v exceeds %v, so a time-keyed cache is needed", mean, budget)
	}
}

func BenchmarkBuildDigest_ShippedRunnerTree(b *testing.B) {
	if _, err := os.Stat(filepath.Join(realRunnerProjectDir, xctestProjectSpecFileName)); err != nil {
		b.Skipf("runner project not available: %v", err)
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
