package cli

import "io"

// PrintSkillsCommandHelp writes the full flag reference for one
// skills-module command to w. This text is user-facing help output,
// so its wording and layout are a stable contract that must not
// change without deliberate intent.
func PrintSkillsCommandHelp(command SkillsCommand, w io.Writer) {
	switch command {
	case SkillsCommandInit:
		_, _ = io.WriteString(w, `atomyx init [--target=<path>] [--force]
  Copy bundled Atomyx skill and agent files into <target>/.claude/.
  Defaults to the current working directory.

  --target=<path>   Target directory (defaults to cwd)
  --force           Overwrite existing files
  --help, -h        Show this message
`)
	case SkillsCommandUpdateSkills:
		_, _ = io.WriteString(w, `atomyx update-skills [--target=<path>]
  Update an existing Atomyx skills install to the bundled version.

  --target=<path>   Target directory (defaults to cwd)
  --help, -h        Show this message
`)
	}
}

// PrintSkillsModuleHelp writes the skills module's own top-level
// usage to w. This text is user-facing help output, so its wording
// and layout are a stable contract that must not change without
// deliberate intent.
func PrintSkillsModuleHelp(w io.Writer) {
	_, _ = io.WriteString(w, `atomyx skills: install and update Claude skills

USAGE
  atomyx skills <command> [flags]

COMMANDS
  init             Copy bundled skills into <cwd>/.claude
  update-skills    Overwrite existing skills when a newer version is available
  help             Print this usage and exit

FLAGS (init)
  --target=<path>  Destination directory (default: <cwd>/.claude)
  --force          Overwrite existing files without prompting
  --help, -h       Show command help

FLAGS (update-skills)
  --target=<path>  Destination directory (default: <cwd>/.claude)
  --help, -h       Show command help

SHORTCUTS
  atomyx init                  is shorthand for atomyx skills init
  atomyx update-skills         is shorthand for atomyx skills update-skills

SEE ALSO
  https://atomyx.dev
`)
}
