package history

import (
	"errors"
	"io/fs"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func TestFuncSourceDelegatesToItsFunctions(t *testing.T) {
	want := []runfolder.Meta{{RunID: "r1"}}
	source := FuncSource{
		ListMetas: func() ([]runfolder.Meta, error) { return want, nil },
		ReadRunIndex: func(runID string) (runfolder.Index, error) {
			if runID != "r1" {
				return runfolder.Index{}, errors.New("unexpected run")
			}
			return runfolder.Index{}, fs.ErrNotExist
		},
	}

	metas, err := source.Metas()
	if err != nil || len(metas) != 1 || metas[0].RunID != "r1" {
		t.Fatalf("Metas = %+v, %v", metas, err)
	}
	if _, err := source.ReadIndex("r1"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadIndex error = %v, want fs.ErrNotExist", err)
	}
}
