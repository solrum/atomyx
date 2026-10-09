package cli

import (
	"dev.atomyx/internal/runfolder"
)

// suiteJSONResult is the object `atomyx suite --json` prints to standard
// output. Entries have the shape of the entries in index.json, so a
// consumer reads one format for both.
type suiteJSONResult struct {
	Ok        bool                   `json:"ok"`
	RunFolder string                 `json:"runFolder"`
	Entries   []runfolder.IndexEntry `json:"entries"`
}
