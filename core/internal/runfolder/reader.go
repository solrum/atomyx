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
// result cancelled and outcome interrupted, whatever the file says,
// and a completed meta that names only an Outcome gains its Result.
func (r *Reader) Get(id string) (Meta, bool, error) {
	if !validRunID(id) {
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

// List reads the meta of every run folder, sorted by run id. A folder
// that holds no meta.json is not a run and is skipped. A folder whose
// meta cannot be read is left out of the result and reported in the
// returned error, which joins one error per such folder, so a single
// damaged run never hides the others.
func (r *Reader) List() ([]Meta, error) {
	entries, err := os.ReadDir(VersionedRunsDir(r.root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var metas []Meta
	var skipped []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, ok, err := r.Get(entry.Name())
		if err != nil {
			skipped = append(skipped, fmt.Errorf("run %s: %w", entry.Name(), err))
			continue
		}
		if ok {
			metas = append(metas, meta)
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].RunID < metas[j].RunID })
	return metas, errors.Join(skipped...)
}

// validRunID rejects ids that could name a path outside the run
// directory.
func validRunID(id string) bool {
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
	if meta.CompletedAt == nil {
		meta.Result = ResultCancelled
		meta.Outcome = OutcomeInterrupted
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
