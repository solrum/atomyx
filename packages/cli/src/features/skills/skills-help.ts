export function printCommandHelp(
  command: "init" | "update-skills",
  write: (s: string) => void = (s) => process.stderr.write(s),
): void {
  switch (command) {
    case "init":
      write(`atomyx init [--target=<path>] [--force]
  Copy bundled Atomyx skill and agent files into <target>/.claude/.
  Defaults to the current working directory.

  --target=<path>   Target directory (defaults to cwd)
  --force           Overwrite existing files
  --help, -h        Show this message
`);
      return;
    case "update-skills":
      write(`atomyx update-skills [--target=<path>]
  Update an existing Atomyx skills install to the bundled version.

  --target=<path>   Target directory (defaults to cwd)
  --help, -h        Show this message
`);
      return;
  }
}

export function printModuleHelp(
  write: (s: string) => void = (s) => process.stdout.write(s),
): void {
  write(`atomyx skills — install and update Claude skills

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
  atomyx init                  → atomyx skills init
  atomyx update-skills         → atomyx skills update-skills

SEE ALSO
  https://atomyx.dev
`);
}
