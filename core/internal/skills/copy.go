package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// versionStampFile is the file CopyTo writes at the target
// directory's root recording the installed version, matching the
// TypeScript skills package's own VERSION_STAMP name.
const versionStampFile = "atomyx-skills.version.json"

// CopyResult reports what CopyTo wrote. Skipped is always empty:
// CopyTo either writes every bundled file or returns an error before
// writing any of them. The field exists so a caller can tell the two
// outcomes apart from the same result shape a future partial-copy
// mode would use.
type CopyResult struct {
	Written []string
	Skipped []string
}

// InstalledVersionResult reports a target directory's installed
// skills version. Version is empty when the target has no version
// stamp, or one that failed to parse.
type InstalledVersionResult struct {
	Version  string
	Current  string
	UpToDate bool
}

// ErrAlreadyExists is returned by CopyTo when overwrite is false and
// one or more destination files already exist under targetDir.
type ErrAlreadyExists struct {
	TargetDir string
	Conflicts []string
}

func (e *ErrAlreadyExists) Error() string {
	return fmt.Sprintf("skills files already exist in %s: %s", e.TargetDir, strings.Join(e.Conflicts, ", "))
}

// CopyTo writes every bundled skill and agent file under targetDir,
// as targetDir/skills/<name> and targetDir/agents/<name>, plus a
// version stamp file at targetDir/atomyx-skills.version.json.
//
// When overwrite is false, CopyTo first checks every destination
// path for an existing file; if any exist, it writes nothing and
// returns an *ErrAlreadyExists naming every conflicting path. When
// overwrite is true, an existing file at any destination path is
// replaced.
//
// Writes are staged into a temporary directory alongside targetDir
// and moved into place last, so a failure partway through never
// leaves targetDir holding a mix of old and new content.
func CopyTo(targetDir string, overwrite bool) (CopyResult, error) {
	if !overwrite {
		if conflicts := existingConflicts(targetDir); len(conflicts) > 0 {
			return CopyResult{}, &ErrAlreadyExists{TargetDir: targetDir, Conflicts: conflicts}
		}
	}

	tmpDir := filepath.Join(targetDir, fmt.Sprintf(".claude-skills.tmp-%d", os.Getpid()))
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpSkillsDir := filepath.Join(tmpDir, "skills")
	tmpAgentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(tmpSkillsDir, 0o755); err != nil {
		return CopyResult{}, err
	}
	if err := os.MkdirAll(tmpAgentsDir, 0o755); err != nil {
		return CopyResult{}, err
	}

	for _, name := range SkillFiles {
		if err := copyEmbeddedFile(filepath.Join(contentRoot, "skills", name), filepath.Join(tmpSkillsDir, name)); err != nil {
			return CopyResult{}, err
		}
	}
	for _, name := range AgentFiles {
		if err := copyEmbeddedFile(filepath.Join(contentRoot, "agents", name), filepath.Join(tmpAgentsDir, name)); err != nil {
			return CopyResult{}, err
		}
	}

	stamp, err := json.MarshalIndent(struct {
		Version string `json:"version"`
	}{Version: Version}, "", "  ")
	if err != nil {
		return CopyResult{}, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, versionStampFile), append(stamp, '\n'), 0o644); err != nil {
		return CopyResult{}, err
	}

	skillsDir := filepath.Join(targetDir, "skills")
	agentsDir := filepath.Join(targetDir, "agents")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return CopyResult{}, err
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return CopyResult{}, err
	}

	written := make([]string, 0, len(SkillFiles)+len(AgentFiles))
	for _, name := range SkillFiles {
		if err := os.Rename(filepath.Join(tmpSkillsDir, name), filepath.Join(skillsDir, name)); err != nil {
			return CopyResult{Written: written}, err
		}
		written = append(written, filepath.Join("skills", name))
	}
	for _, name := range AgentFiles {
		if err := os.Rename(filepath.Join(tmpAgentsDir, name), filepath.Join(agentsDir, name)); err != nil {
			return CopyResult{Written: written}, err
		}
		written = append(written, filepath.Join("agents", name))
	}

	// The version stamp moves into place last, so a reader calling
	// GetInstalledVersion mid-failure sees no stamp rather than a
	// stamp claiming files that did not all land.
	if err := os.Rename(filepath.Join(tmpDir, versionStampFile), filepath.Join(targetDir, versionStampFile)); err != nil {
		return CopyResult{Written: written}, err
	}

	return CopyResult{Written: written, Skipped: []string{}}, nil
}

// GetInstalledVersion reads targetDir's version stamp and compares it
// against Version. A missing or unparsable stamp reports an empty
// Version and UpToDate false, never an error.
func GetInstalledVersion(targetDir string) InstalledVersionResult {
	data, err := os.ReadFile(filepath.Join(targetDir, versionStampFile))
	if err != nil {
		return InstalledVersionResult{Current: Version}
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return InstalledVersionResult{Current: Version}
	}
	return InstalledVersionResult{
		Version:  parsed.Version,
		Current:  Version,
		UpToDate: parsed.Version == Version,
	}
}

func existingConflicts(targetDir string) []string {
	var conflicts []string
	for _, name := range SkillFiles {
		if fileExists(filepath.Join(targetDir, "skills", name)) {
			conflicts = append(conflicts, filepath.Join("skills", name))
		}
	}
	for _, name := range AgentFiles {
		if fileExists(filepath.Join(targetDir, "agents", name)) {
			conflicts = append(conflicts, filepath.Join("agents", name))
		}
	}
	return conflicts
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyEmbeddedFile(src, dst string) error {
	data, err := contentFS.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
