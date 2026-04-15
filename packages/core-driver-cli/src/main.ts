#!/usr/bin/env node
/**
 * Atomyx core-driver CLI entry point.
 *
 * `atomyx-driver` binary. Parses argv, dispatches to subcommand
 * handlers, prints structured errors on failure.
 *
 * The shebang line above lets npm install this as a global
 * binary (npm i -g @atomyx/core-driver-cli) and have it
 * resolvable on PATH as `atomyx-driver`. The package.json `bin`
 * field points at this compiled file.
 */

import { ArgvError, parseArgv, printHelp } from "./argv.js";
import { runMcp } from "./commands/mcp.js";
import { runListDevices } from "./commands/list-devices.js";

const VERSION = "0.1.0";

async function main(): Promise<void> {
  let argv;
  try {
    argv = parseArgv(process.argv.slice(2));
  } catch (err) {
    if (err instanceof ArgvError) {
      process.stderr.write(`error: ${err.message}\n\n`);
      printHelp();
      process.exit(2);
    }
    throw err;
  }

  switch (argv.command) {
    case "mcp":
      await runMcp(argv);
      return;
    case "list-devices":
      await runListDevices();
      return;
    case "version":
      process.stdout.write(`atomyx-driver ${VERSION}\n`);
      return;
    case "help":
      printHelp(process.stdout.write.bind(process.stdout));
      return;
  }
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(`error: ${message}\n`);
  if (process.env.ATOMYX_DEBUG === "1" && err instanceof Error && err.stack) {
    process.stderr.write(err.stack + "\n");
  }
  process.exit(1);
});
