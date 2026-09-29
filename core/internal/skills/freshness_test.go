package skills

import (
	"io/fs"
	"path"
	"testing"
)

// TestEmbeddedContentMatchesManifest proves the embedded content tree
// contains exactly the files declared by SkillFiles and AgentFiles,
// nothing more and nothing less, and that every declared file is
// non-empty. It fails if a file is added to or removed from the
// embed without a matching change to the manifest, and vice versa.
func TestEmbeddedContentMatchesManifest(t *testing.T) {
	embedded, err := readEmbeddedTree()
	if err != nil {
		t.Fatalf("reading embedded content: %v", err)
	}

	want := make(map[string]bool, len(SkillFiles)+len(AgentFiles))
	for _, name := range SkillFiles {
		want[path.Join("skills", name)] = true
	}
	for _, name := range AgentFiles {
		want[path.Join("agents", name)] = true
	}

	for rel := range want {
		data, ok := embedded[rel]
		if !ok {
			t.Errorf("manifest declares %s but the embedded content does not have it", rel)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded file %s is empty", rel)
		}
	}
	for rel := range embedded {
		if !want[rel] {
			t.Errorf("embedded content has %s, not declared by the manifest", rel)
		}
	}
}

func readEmbeddedTree() (map[string][]byte, error) {
	sub, err := fs.Sub(contentFS, contentRoot)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte)
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		out[p] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
