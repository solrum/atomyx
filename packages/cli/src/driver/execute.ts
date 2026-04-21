/**
 * Driver-module subcommand dispatcher. The unified CLI in
 * `main.ts` strips the `driver` prefix and calls `execute(rest)`
 * from here; direct invocation is also supported for testing.
 */

import { ArgvError, parseArgv, printHelp } from "./argv.js";
import { runListDevices } from "./commands/list-devices.js";
import { runScript } from "./commands/run-script.js";

const VERSION = "0.1.0";

export async function execute(args: readonly string[]): Promise<void> {
  let argv;
  try {
    argv = parseArgv(args);
  } catch (err) {
    if (err instanceof ArgvError) {
      process.stderr.write(`error: ${err.message}\n\n`);
      printHelp();
      process.exit(2);
    }
    throw err;
  }

  switch (argv.command) {
    case "list-devices":
      await runListDevices({ json: !!argv.flags["--json"] });
      return;
    case "run":
      await runScript(argv.flags);
      return;
    case "version":
      process.stdout.write(`atomyx ${VERSION}\n`);
      return;
    case "help":
      printHelp(process.stdout.write.bind(process.stdout));
      return;
  }
}
