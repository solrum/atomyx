package runfolder

import "errors"

// ErrNoSummary reports that a run carries no MCP block, so it has no
// summary to set.
var ErrNoSummary = errors.New("run has no summary field")

// ErrRunLive reports that a writer still holds the run, so another
// process must not rewrite its meta.
var ErrRunLive = errors.New("run is still being written")

// SetSummary replaces the summary in the meta of the run folder with
// the given id under root, and reports whether that folder exists. The
// rest of the meta is kept as read. A run with no MCP block returns
// ErrNoSummary, and a run whose lock is held returns ErrRunLive,
// because its writer would overwrite the change when it finalizes. An
// unfinished run with no live writer keeps its missing completedAt.
func SetSummary(root, id, summary string) (bool, error) {
	meta, ok, err := NewReader(root).Get(id)
	if err != nil || !ok {
		return false, err
	}
	if NewLockLiveness(root).IsLive(id) {
		return true, ErrRunLive
	}
	if meta.MCP == nil {
		return true, ErrNoSummary
	}
	info := *meta.MCP
	info.Summary = &summary
	meta.MCP = &info
	return true, writeMetaFile(RunDir(root, id), meta)
}
