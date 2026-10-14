package cli

import (
	"dev.atomyx/internal/history"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

// catalogSource reads runs for history from a run catalog. It remembers
// how many run records the catalog could not read, so a command can
// report them after history has consumed the source.
type catalogSource struct {
	catalog *storage.RunCatalog
	root    string
	skipped int
}

// source returns the history source backed by the catalog.
func (s *catalogSource) source() history.Source {
	return history.FuncSource{
		ListMetas: func() ([]runfolder.Meta, error) {
			listing, err := s.catalog.List()
			if err != nil {
				return nil, err
			}
			s.skipped = len(listing.Skipped)
			return listing.Runs, nil
		},
		ReadRunIndex: func(runID string) (runfolder.Index, error) {
			return runfolder.ReadIndex(runfolder.RunDir(s.root, runID))
		},
	}
}
