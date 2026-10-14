package history

import "dev.atomyx/internal/runfolder"

// FuncSource is a Source built from two functions, for a caller that
// reads runs through its own storage and has no type of its own to
// declare for the port.
type FuncSource struct {
	// ListMetas returns the meta of every readable run.
	ListMetas func() ([]runfolder.Meta, error)
	// ReadRunIndex returns the index.json of one run, with the same
	// not-found contract as Source.ReadIndex.
	ReadRunIndex func(runID string) (runfolder.Index, error)
}

var _ Source = FuncSource{}

// Metas calls ListMetas.
func (f FuncSource) Metas() ([]runfolder.Meta, error) { return f.ListMetas() }

// ReadIndex calls ReadRunIndex.
func (f FuncSource) ReadIndex(runID string) (runfolder.Index, error) { return f.ReadRunIndex(runID) }
