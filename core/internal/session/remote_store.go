package session

import (
	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

// captureStore holds what a remote handle has pulled and received from the
// holder: the newest captures and the recent step lines. It does no I/O,
// and its owner guards it with one lock, so every method requires that
// lock to be held.
type captureStore struct {
	generation uint64
	snap       Snapshot
	steps      stepRing
}

// storeTree keeps r when it is newer than the held tree and reports
// whether it did. A result of the held revision still refreshes the
// holder's pairing, which can change with no new capture.
func (c *captureStore) storeTree(r attach.TreeResult) bool {
	if r.Revision < c.snap.TreeRevision {
		return false
	}
	c.applyState(r.CaptureState)
	c.snap.TreeStable = r.Stable
	if r.Revision == c.snap.TreeRevision {
		return false
	}
	c.snap.HasTree = r.Tree != nil
	c.snap.Tree = wire.TreeNodeWire{}
	if r.Tree != nil {
		c.snap.Tree = *r.Tree
	}
	c.snap.TreeEpoch, c.snap.TreeRevision = r.Epoch, r.Revision
	c.snap.Generation = c.generation
	return true
}

func (c *captureStore) storeScreenshot(r attach.ScreenshotResult) bool {
	if r.Revision < c.snap.ScreenshotRevision {
		return false
	}
	c.applyState(r.CaptureState)
	c.snap.ScreenshotStable = r.Stable
	if r.Revision == c.snap.ScreenshotRevision {
		return false
	}
	c.snap.HasScreenshot = len(r.Image) > 0
	c.snap.Screenshot = r.Image
	c.snap.ScreenshotFormat = ImageFormat(r.Format)
	c.snap.ScreenshotEpoch, c.snap.ScreenshotRevision = r.Epoch, r.Revision
	c.snap.Generation = c.generation
	return true
}

func (c *captureStore) applyState(s attach.CaptureState) {
	c.snap.Paired, c.snap.Highlightable, c.snap.Scale = s.Paired, s.Highlightable, s.Scale
}

// storeStep keeps the line of n and returns it.
func (c *captureStore) storeStep(n attach.StepNotice) runfolder.StepLine {
	line := runfolder.StepLine{
		SchemaVersion: n.Line.SchemaVersion,
		RunID:         n.Line.RunID,
		Seq:           n.Line.Seq,
		StepIndex:     n.Line.StepIndex,
		Command:       n.Line.Command,
		Phase:         runfolder.StepPhase(n.Line.Phase),
		Ok:            n.Line.Ok,
		DurationMs:    n.Line.DurationMs,
		Detail:        n.Line.Detail,
		Artifacts:     n.Line.Artifacts,
	}
	c.steps.push(line)
	return line
}

// forgetSteps drops the held lines, because the lines that follow a gap
// are not contiguous with them.
func (c *captureStore) forgetSteps() { c.steps = stepRing{} }
