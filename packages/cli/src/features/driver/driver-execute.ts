import { ArgvError, parseArgv, printHelp } from "./driver-argv.js";
import { runListDevices } from "./driver-list-devices.js";
import { runScript } from "./driver-run-script.js";
import type { DriverFactory } from "./driver.contract.js";

const VERSION = "0.1.0";

export async function executeDriver(
  factory: DriverFactory,
  args: readonly string[],
): Promise<void> {
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
      await runScript(factory, argv.flags);
      return;
    case "version":
      process.stdout.write(`atomyx ${VERSION}\n`);
      return;
    case "help":
      printHelp(process.stdout.write.bind(process.stdout));
      return;
  }
}
