package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dev.atomyx/internal/storageroot"
)

const (
	fileModeDir  = 0o755
	fileModeFile = 0o644
)

// FileStorageOptions configures FileStorage. Root overrides the
// storage root directory; when empty, FileStorage falls back to the
// ATOMYX_STORAGE_DIR environment variable, then to "~/.atomyx".
type FileStorageOptions struct {
	Root string
}

// FileStorage is the default filesystem Storage. Each record is
// written as a file under a root directory:
//
//	"bugs/123"       -> <root>/bugs/123.json
//	"runs/abc"       -> <root>/runs/abc.json
//	"notes/plan"     -> <root>/notes/plan.md   (string values -> .md)
//
// A record saved as a plain string is written as ".md" so it reads
// directly as text; anything else is pretty-printed JSON. Load tries
// both extensions, since the caller doesn't otherwise know which one
// a given key used.
type FileStorage struct {
	root string
}

// NewFileStorage constructs a FileStorage rooted at opts.Root, or the
// ATOMYX_STORAGE_DIR environment variable, or "~/.atomyx" in that
// order of precedence.
func NewFileStorage(opts FileStorageOptions) (*FileStorage, error) {
	root, err := storageroot.Resolve(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	return &FileStorage{root: root}, nil
}

func (f *FileStorage) Save(key string, data any) error {
	if s, ok := data.(string); ok {
		path := f.pathFor(key, "md")
		if err := os.MkdirAll(filepath.Dir(path), fileModeDir); err != nil {
			return err
		}
		return writeFileAtomic(path, []byte(s))
	}

	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	path := f.pathFor(key, "json")
	if err := os.MkdirAll(filepath.Dir(path), fileModeDir); err != nil {
		return err
	}
	return writeFileAtomic(path, body)
}

// writeFileAtomic replaces path's contents by writing to a new file in
// the same directory and renaming it into place, so a concurrent Load
// or a crash mid-write can never observe a truncated or partially
// written record: the rename either hasn't happened yet, in which case
// the previous complete file is still there, or it has, in which case
// the new complete file is there. Same-directory placement is what
// makes the rename atomic, since rename across filesystems is not.
//
// The final file reuses path's existing permission bits when path
// already exists, so overwriting a record does not silently change who
// can read it; a path that doesn't exist yet falls back to
// fileModeFile. Any failure before the rename removes the temporary
// file so a failed Save leaves no orphan behind.
func writeFileAtomic(path string, body []byte) error {
	perm := fs.FileMode(fileModeFile)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (f *FileStorage) Load(key string, out any) (bool, error) {
	for _, ext := range [...]string{"json", "md"} {
		path := f.pathFor(key, ext)
		body, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false, err
		}
		if ext == "md" {
			s, ok := out.(*string)
			if !ok {
				return false, fmt.Errorf("storage: load %q: record is text, out must be *string", key)
			}
			*s = string(body)
			return true, nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (f *FileStorage) List(prefix string) ([]string, error) {
	base := f.root
	if prefix != "" {
		base = filepath.Join(f.root, prefix)
	}
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}

	var matches []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext == ".json" || ext == ".md" {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(f.root, m)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))
		keys = append(keys, rel)
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *FileStorage) ListChildren(prefix string) ([]string, error) {
	dir := strings.TrimSuffix(prefix, "/")
	entries, err := os.ReadDir(filepath.Join(f.root, dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".json" && ext != ".md") {
			continue
		}
		keys = append(keys, dir+"/"+strings.TrimSuffix(entry.Name(), ext))
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *FileStorage) Delete(key string) error {
	for _, ext := range [...]string{"json", "md"} {
		if err := os.Remove(f.pathFor(key, ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// pathFor maps a storage key to its on-disk path. Occurrences of ".."
// are neutralized so a key cannot escape the storage root.
func (f *FileStorage) pathFor(key, ext string) string {
	safe := strings.ReplaceAll(key, "..", "_")
	return filepath.Join(f.root, safe+"."+ext)
}

// Root returns the directory every record is stored under.
func (f *FileStorage) Root() string { return f.root }
