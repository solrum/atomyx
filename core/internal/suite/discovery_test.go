package suite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// buildFixtureTree lays out a small corpus under a fresh temp dir:
//
//	a.yml               tags: [smoke]
//	b.yml               tags: [regression]
//	sub/c.yml           tags: [smoke, critical]
//	node_modules/d.yml  tags: [smoke]   (must never be discovered)
//	.hidden/e.yml       tags: [smoke]   (must never be discovered)
//	.f.yml              tags: [smoke]   (must never be discovered)
//	notes.txt           (not a .yml/.yaml file)
func buildFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeYaml := func(rel string, tags string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		content := "tags: [" + tags + "]\nsteps: []\n"
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeYaml("a.yml", "smoke")
	writeYaml("b.yml", "regression")
	writeYaml("sub/c.yml", "smoke, critical")
	writeYaml("node_modules/d.yml", "smoke")
	writeYaml(".hidden/e.yml", "smoke")
	writeYaml(".f.yml", "smoke")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveRejectsEmptySelector(t *testing.T) {
	root := buildFixtureTree(t)
	_, err := Resolve(context.Background(), Selector{}, DiscoveryOptions{Cwd: root})
	if err == nil {
		t.Fatalf("expected an error for an empty selector")
	}
	wantMsg := "a suite selector requires at least one of tags or globs"
	if err.Error() != wantMsg {
		t.Errorf("got %q, want %q", err.Error(), wantMsg)
	}
}

func TestResolveRejectsTagsAloneWithNoTestRootsOrGlob(t *testing.T) {
	root := buildFixtureTree(t)
	_, err := Resolve(context.Background(), Selector{Tags: []string{"smoke"}}, DiscoveryOptions{Cwd: root})
	if err == nil {
		t.Fatalf("expected an error for a tags-only selector with no testRoots or glob")
	}
	wantMsg := "selecting a suite by tags alone requires testRoots in the " +
		"project config, or at least one glob to narrow the search"
	if err.Error() != wantMsg {
		t.Errorf("got %q, want %q", err.Error(), wantMsg)
	}
}

func TestResolveGlobOnlyStaysWithinSegmentAndSkipsHiddenAndNodeModules(t *testing.T) {
	root := buildFixtureTree(t)
	flows, err := Resolve(context.Background(), Selector{Globs: []string{"*.yml"}}, DiscoveryOptions{Cwd: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPaths(t, root, flows, "a.yml", "b.yml")
}

func TestResolveGlobDoubleStarCrossesSegmentsAndSkipsHiddenAndNodeModules(t *testing.T) {
	root := buildFixtureTree(t)
	flows, err := Resolve(context.Background(), Selector{Globs: []string{"**/*.yml"}}, DiscoveryOptions{Cwd: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPaths(t, root, flows, "a.yml", "b.yml", "sub/c.yml")
}

func TestResolveTagOnlyWithTestRoots(t *testing.T) {
	root := buildFixtureTree(t)
	flows, err := Resolve(context.Background(), Selector{Tags: []string{"critical"}}, DiscoveryOptions{Cwd: root, TestRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPaths(t, root, flows, "sub/c.yml")
}

func TestResolveGlobAndTagCombineWithAnd(t *testing.T) {
	root := buildFixtureTree(t)
	flows, err := Resolve(context.Background(), Selector{
		Globs: []string{"**/*.yml"},
		Tags:  []string{"smoke"},
	}, DiscoveryOptions{Cwd: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// b.yml is tagged "regression", not "smoke", so the glob+tag
	// intersection excludes it even though the glob alone matches it.
	wantPaths(t, root, flows, "a.yml", "sub/c.yml")
}

func TestResolveMissingTestRootYieldsNoCandidatesNotAnError(t *testing.T) {
	root := buildFixtureTree(t)
	missing := filepath.Join(root, "does-not-exist")
	flows, err := Resolve(context.Background(), Selector{Globs: []string{"**/*.yml"}}, DiscoveryOptions{Cwd: root, TestRoots: []string{missing}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("got %d flows from a missing testRoots entry, want 0", len(flows))
	}
}

func TestResolveReturnsCtxErrForAlreadyCanceledContext(t *testing.T) {
	root := buildFixtureTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flows, err := Resolve(ctx, Selector{Globs: []string{"**/*.yml"}}, DiscoveryOptions{Cwd: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() error = %v, want context.Canceled", err)
	}
	if flows != nil {
		t.Fatalf("Resolve() flows = %v, want nil", flows)
	}
}

// buildFlatFixtureTree lays out count sibling .yml files directly
// under a fresh temp dir, with no tags, so a walk of it has enough
// entries at one directory level to observe a cancellation check
// firing partway through rather than only at the very first or very
// last entry.
func buildFlatFixtureTree(t *testing.T, count int) string {
	t.Helper()
	root := t.TempDir()
	for i := range count {
		name := fmt.Sprintf("case-%03d.yml", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("tags: []\nsteps: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// cancelAfterNCalls counts every call to Err and cancels its own
// context once that count reaches n, so a test can force a
// cancellation to land on a specific loop iteration deterministically
// instead of racing a timer against disk I/O.
type cancelAfterNCalls struct {
	context.Context
	n      int
	cancel context.CancelFunc
	calls  int
}

func newCancelAfterNCalls(n int) *cancelAfterNCalls {
	parent, cancel := context.WithCancel(context.Background())
	return &cancelAfterNCalls{Context: parent, n: n, cancel: cancel}
}

func (c *cancelAfterNCalls) Err() error {
	c.calls++
	if c.calls >= c.n {
		c.cancel()
	}
	return c.Context.Err()
}

func TestWalkYamlFilesStopsPromptlyOnMidWalkCancellation(t *testing.T) {
	const fileCount = 200
	const cancelAt = 10
	root := buildFlatFixtureTree(t, fileCount)
	ctx := newCancelAfterNCalls(cancelAt)

	files, err := newDiscoverer(osDiscoveryFS{}).walkYamlFiles(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("walkYamlFiles() error = %v, want context.Canceled", err)
	}
	if files != nil {
		t.Fatalf("walkYamlFiles() files = %v, want nil", files)
	}
	if ctx.calls != cancelAt {
		t.Fatalf("Err() was called %d times, want exactly %d (the walk kept going past cancellation)", ctx.calls, cancelAt)
	}
	if ctx.calls >= fileCount {
		t.Fatalf("Err() was called %d times against a %d-entry directory, want the walk to stop well before the last entry", ctx.calls, fileCount)
	}
}

// TestDiscovererResolveAgainstFakeFilesystem drives resolve through
// an in-memory fstest.MapFS instead of the real filesystem, since
// MapFS's ReadDir and ReadFile methods already satisfy discoveryFS
// structurally. This proves the directory walk depends on the
// discoveryFS contract rather than the os package directly.
func TestDiscovererResolveAgainstFakeFilesystem(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yml":              &fstest.MapFile{Data: []byte("tags: [smoke]\nsteps: []\n")},
		"b.yml":              &fstest.MapFile{Data: []byte("tags: [regression]\nsteps: []\n")},
		"sub/c.yml":          &fstest.MapFile{Data: []byte("tags: [smoke, critical]\nsteps: []\n")},
		"node_modules/d.yml": &fstest.MapFile{Data: []byte("tags: [smoke]\nsteps: []\n")},
		"notes.txt":          &fstest.MapFile{Data: []byte("not yaml")},
	}

	flows, err := newDiscoverer(fsys).resolve(context.Background(), Selector{Globs: []string{"**/*.yml"}}, DiscoveryOptions{Cwd: "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPaths(t, ".", flows, "a.yml", "b.yml", "sub/c.yml")
}

// TestDiscovererResolveTagFilterAgainstFakeFilesystem exercises the
// tag-scan step (readTopLevelTags) against the same fake filesystem,
// confirming tag filtering reads through discoveryFS rather than
// os.ReadFile.
func TestDiscovererResolveTagFilterAgainstFakeFilesystem(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yml": &fstest.MapFile{Data: []byte("tags: [smoke]\nsteps: []\n")},
		"b.yml": &fstest.MapFile{Data: []byte("tags: [regression]\nsteps: []\n")},
	}

	flows, err := newDiscoverer(fsys).resolve(context.Background(), Selector{Tags: []string{"smoke"}}, DiscoveryOptions{Cwd: ".", TestRoots: []string{"."}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPaths(t, ".", flows, "a.yml")
}

// wantPaths asserts flows is exactly the sorted set of root-relative
// paths given in want.
func wantPaths(t *testing.T, root string, flows []ResolvedFlow, want ...string) {
	t.Helper()
	got := make([]string, len(flows))
	for i, f := range flows {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q): %v", root, f.Path, err)
		}
		got[i] = filepath.ToSlash(rel)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
