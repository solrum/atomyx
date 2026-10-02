package mcp

import (
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

var testRunEpoch = time.Date(2026, time.October, 2, 7, 3, 9, 0, time.UTC)

// countingReader yields an increasing byte sequence, so run id suffixes
// are deterministic and distinct.
type countingReader struct{ n byte }

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

// testRunStarter starts runs under root with a fixed clock.
func testRunStarter(root string) *runfolder.Starter {
	return runfolder.NewStarter(root, func() time.Time { return testRunEpoch }, &countingReader{})
}

// testRunCatalog reads runs under root, counting a run the store holds
// open or any process locks as running.
func testRunCatalog(store storage.Storage, root string, runStore *storage.RunStore) *storage.RunCatalog {
	return storage.NewRunCatalog(store, root, storage.NewRunLiveness(runStore, root))
}

// withRunDeps fills the run dependencies NewServer requires with ones
// rooted in a temporary directory, leaving any the test set.
func withRunDeps(t testing.TB, deps ServerDeps) ServerDeps {
	t.Helper()
	root := t.TempDir()
	if deps.RunStore == nil {
		deps.RunStore = storage.NewRunStore()
	}
	if deps.Runs == nil {
		deps.Runs = testRunCatalog(deps.Storage, root, deps.RunStore)
	}
	if deps.RunStarter == nil {
		deps.RunStarter = testRunStarter(root)
	}
	return deps
}

// mustNewServer builds a server from deps completed by withRunDeps.
func mustNewServer(t testing.TB, deps ServerDeps) *Server {
	t.Helper()
	server, err := NewServer(Info{Name: "atomyx-test", Version: "0.0.0-test"}, withRunDeps(t, deps))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server
}
