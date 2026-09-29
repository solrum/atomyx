package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"dev.atomyx/internal/skills"
)

// RunInit installs the bundled Atomyx skill and agent files into the
// target directory named by flags["--target"], defaulting to
// <cwd>/.claude. It refuses to overwrite an existing install unless
// flags["--force"] is set.
func RunInit(deps Deps, flags map[string]FlagValue) int {
	stderr := deps.stderr()

	targetDir, err := resolveSkillsTargetDir(flags)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}
	force := flagBool(flags, "--force")

	if _, err := skills.CopyTo(targetDir, force); err != nil {
		var exist *skills.ErrAlreadyExists
		if errors.As(err, &exist) {
			_, _ = fmt.Fprintf(stderr,
				"error: files already exist in %s\n"+
					"  Use --force to overwrite, or run `atomyx update-skills` to upgrade.\n",
				targetDir)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 1
	}

	printSkillsInstallSummary(deps.stdout(), targetDir)
	return 0
}

func printSkillsInstallSummary(w io.Writer, targetDir string) {
	_, _ = fmt.Fprintf(w, "Atomyx skills installed (v%s)\n", skills.Version)
	_, _ = fmt.Fprintf(w, "  Destination: %s\n", targetDir)
	for _, f := range skills.AllFiles() {
		_, _ = fmt.Fprintf(w, "  + %s\n", f)
	}
}

// resolveSkillsTargetDir resolves flags["--target"] to the skills
// destination directory, defaulting to <cwd>/.claude when the flag is
// absent. Both the init and update-skills commands share this
// resolution so a --target given to either one behaves the same way.
func resolveSkillsTargetDir(flags map[string]FlagValue) (string, error) {
	if v, ok := flagString(flags, "--target"); ok {
		return v, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".claude"), nil
}
