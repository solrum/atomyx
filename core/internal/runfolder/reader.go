package runfolder

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reader reads the run folders of this contract version under one
// root. It never writes. A Reader is safe for concurrent use because
// it holds no state beyond the root.
type Reader struct {
	root string
}

// NewReader returns a Reader for the run folders under root.
func NewReader(root string) *Reader {
	return &Reader{root: root}
}

// Get reads the meta of one run. It reports false with a nil error when
// no run folder with that id holds a meta.json, and an error when the
// meta cannot be read or violates the contract.
//
// The returned Meta is normalized. A meta without CompletedAt carries
// no Result and no Outcome, whatever the file says, so the caller
// settles it through Meta.Settle. A completed meta that names only an
// Outcome gains its Result. A meta whose runId differs from its folder
// name is damaged.
func (r *Reader) Get(id string) (Meta, bool, error) {
	if !ValidRunID(id) {
		return Meta{}, false, nil
	}
	body, err := os.ReadFile(filepath.Join(RunDir(r.root, id), MetaFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, false, nil
		}
		return Meta{}, false, err
	}
	meta, err := decodeMeta(id, body)
	if err != nil {
		return Meta{}, false, err
	}
	return meta, true, nil
}

// Has reports whether a run folder with the given id holds a meta.json,
// readable or not.
func (r *Reader) Has(id string) bool {
	if !ValidRunID(id) {
		return false
	}
	_, err := os.Stat(filepath.Join(RunDir(r.root, id), MetaFileName))
	return err == nil
}

// SkippedRun names a run record that could not be read and the reason.
type SkippedRun struct {
	ID  string
	Err error
}

// List reads the meta of every run folder, sorted by run id. A folder
// that holds no meta.json is not a run and is ignored, and so is an
// import scratch folder or a folder that vanishes while the listing
// runs. A folder whose meta cannot be read is left out of the result and
// reported in skipped, so a single damaged run never hides the others.
// An error is returned only when the run directory itself cannot be
// listed.
func (r *Reader) List() (metas []Meta, skipped []SkippedRun, err error) {
	entries, err := os.ReadDir(VersionedRunsDir(r.root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), importTempPrefix) {
			continue
		}
		meta, ok, err := r.Get(entry.Name())
		if err != nil {
			skipped = append(skipped, SkippedRun{ID: entry.Name(), Err: err})
			continue
		}
		if ok {
			metas = append(metas, meta)
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].RunID < metas[j].RunID })
	return metas, skipped, nil
}

// ValidRunID reports whether id is a bare run id, one that names a
// single path element and so cannot reach outside the run directory.
func ValidRunID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`) && !strings.ContainsRune(id, 0)
}

func decodeMeta(folderID string, body []byte) (Meta, error) {
	var meta Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		return Meta{}, fmt.Errorf("decoding %s: %w", MetaFileName, err)
	}
	if meta.SchemaVersion < 1 || meta.SchemaVersion > SchemaVersion {
		return Meta{}, fmt.Errorf("%s schemaVersion %d is outside 1 to %d", MetaFileName, meta.SchemaVersion, SchemaVersion)
	}
	if meta.RunID == "" {
		meta.RunID = folderID
	}
	if meta.RunID != folderID {
		return Meta{}, fmt.Errorf("%s runId %q does not match its folder %q", MetaFileName, meta.RunID, folderID)
	}
	if meta.CompletedAt == nil {
		meta.Result, meta.Outcome = "", ""
		return meta, nil
	}
	if meta.Outcome != "" {
		result, ok := ResultOf(meta.Outcome)
		if !ok {
			return Meta{}, fmt.Errorf("%s outcome %q is not a known outcome", MetaFileName, meta.Outcome)
		}
		if meta.Result == "" {
			meta.Result = result
		}
	}
	if meta.Result == "" {
		return Meta{}, fmt.Errorf("%s has completedAt but neither result nor outcome", MetaFileName)
	}
	return meta, nil
}
