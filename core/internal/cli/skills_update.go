package cli

import (
	"fmt"

	"dev.atomyx/internal/skills"
)

// RunUpdateSkills upgrades an existing Atomyx skills install at the
// target directory named by flags["--target"] (defaulting to
// <cwd>/.claude) to the bundled version, overwriting every file
// unconditionally. It is a no-op when the install is already
// current.
func RunUpdateSkills(deps Deps, flags map[string]FlagValue) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

	targetDir, err := resolveSkillsTargetDir(flags)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	versionResult := skills.GetInstalledVersion(targetDir)
	if versionResult.UpToDate {
		_, _ = fmt.Fprintf(stdout, "Atomyx skills are already up to date (v%s)\n", skills.Version)
		return 0
	}

	if _, err := skills.CopyTo(targetDir, true); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: failed to update skills in %s\n  %s\n", targetDir, err)
		return 1
	}

	fromLabel := versionResult.Version
	if fromLabel == "" {
		fromLabel = "(none)"
	}
	_, _ = fmt.Fprintf(stdout, "Atomyx skills updated: v%s to v%s\n", fromLabel, skills.Version)
	_, _ = fmt.Fprintf(stdout, "  Destination: %s\n", targetDir)
	for _, f := range skills.AllFiles() {
		_, _ = fmt.Fprintf(stdout, "  ↺ %s\n", f)
	}
	return 0
}
