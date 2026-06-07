/**
 * Skills-module subcommand dispatcher. The unified CLI in
 * `router.ts` strips the `skills` prefix and calls `execute(rest)`
 * from here; shortcuts `init` and `update-skills` also route here.
 */

import { runInit } from "./commands/init.js";
import { runUpdateSkills } from "./commands/update-skills.js";

export async function execute(args: readonly string[]): Promise<void> {
  const command = args[0];

  switch (command) {
    case "init": {
      const code = await runInit(args.slice(1));
      if (code !== 0) process.exit(code);
      return;
    }
    case "update-skills": {
      const code = await runUpdateSkills(args.slice(1));
      if (code !== 0) process.exit(code);
      return;
    }
    case "help":
    case undefined:
      printHelp(process.stdout.write.bind(process.stdout));
      return;
    default:
      process.stderr.write(`error: unknown skills command "${command}"\n\n`);
      printHelp(process.stderr.write.bind(process.stderr));
      process.exit(2);
  }
}

function printHelp(write: (s: string) => void): void {
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

FLAGS (update-skills)
  --target=<path>  Destination directory (default: <cwd>/.claude)

SHORTCUTS
  atomyx init                  → atomyx skills init
  atomyx update-skills         → atomyx skills update-skills

SEE ALSO
  https://atomyx.dev
`);
}
