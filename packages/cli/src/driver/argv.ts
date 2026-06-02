/**
 * Argv parser for the driver-module subcommands. The MCP stdio
 * transport is shipped as a separate binary (`atomyx-mcp`); this
 * parser only handles the direct-CLI grammar.
 *
 * Grammar:
 *
 *   atomyx driver <command> [--flag=value | --flag value | --bool]
 *
 * Commands:
 *
 *   list-devices  Enumerate connected devices.
 *   run           Run a YML test script.
 *   version       Print version + exit.
 *   help          Print usage + exit (default when no command).
 */

export class ArgvError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ArgvError";
  }
}

export interface ParsedArgv {
  readonly command: "list-devices" | "run" | "version" | "help";
  readonly flags: Readonly<Record<string, string | boolean>>;
}

const VALID_COMMANDS = new Set(["list-devices", "run", "version", "help"]);

/**
 * Flag definitions per command.
 * - `true` = boolean flag (no value)
 * - `false` = value flag (requires next arg)
 */
const COMMAND_FLAGS: Record<string, Record<string, boolean>> = {
  "list-devices": { "--json": true },
  run: {
    "--file": false,
    "--platform": false,
    "--device": false,
    "--proxy": false,
    "--json": true,
  },
};

export function parseArgv(args: readonly string[]): ParsedArgv {
  if (args.length === 0) {
    return { command: "help", flags: {} };
  }

  const first = args[0]!;
  if (first === "--help" || first === "-h") {
    return { command: "help", flags: {} };
  }
  if (first === "--version" || first === "-v") {
    return { command: "version", flags: {} };
  }
  if (!VALID_COMMANDS.has(first)) {
    throw new ArgvError(
      `Unknown command "${first}". Valid commands: ${[...VALID_COMMANDS].join(", ")}. ` +
        `For the MCP server, run \`atomyx mcp\` — see ` +
        `@atomyx/mcp package.`,
    );
  }

  const command = first as ParsedArgv["command"];
  const defs = COMMAND_FLAGS[command] ?? {};
  const flags: Record<string, string | boolean> = {};

  for (let i = 1; i < args.length; i++) {
    const arg = args[i]!;

    // Handle --flag=value form
    const eqIdx = arg.indexOf("=");
    if (eqIdx > 0) {
      const key = arg.slice(0, eqIdx);
      if (!(key in defs)) {
        throw new ArgvError(
          `Command "${command}" does not accept "${key}". ` +
            `Valid flags: ${Object.keys(defs).join(", ") || "(none)"}.`,
        );
      }
      flags[key] = arg.slice(eqIdx + 1);
      continue;
    }

    if (!(arg in defs)) {
      throw new ArgvError(
        `Command "${command}" does not accept "${arg}". ` +
          `Valid flags: ${Object.keys(defs).join(", ") || "(none)"}.`,
      );
    }

    if (defs[arg]) {
      // Boolean flag
      flags[arg] = true;
    } else {
      // Value flag — consume next arg
      const nextArg = args[i + 1];
      if (nextArg === undefined || nextArg.startsWith("--")) {
        throw new ArgvError(`Flag "${arg}" requires a value.`);
      }
      flags[arg] = nextArg;
      i++; // skip value
    }
  }

  return { command, flags };
}

export function printHelp(
  write: (s: string) => void = (s) => process.stderr.write(s),
): void {
  write(`atomyx driver — device-interaction subcommands

USAGE
  atomyx driver <command> [flags]

COMMANDS
  list-devices [--json]    Enumerate connected devices.
  run [flags]              Run a YML test script.
  version                  Print version and exit.
  help                     Print this usage and exit.

RUN FLAGS
  --file <path>            Path to the YML test script (required).
  --platform <ios|android> Target platform.
  --device <id>            Device identifier as reported by list-devices.
  --proxy <type:path>      Network capture (e.g. "file:/tmp/capture.jsonl").
  --json                   Output results as JSON.

MCP STDIO SERVER
  The MCP server ships as a separate binary — run \`atomyx mcp\`.

SEE ALSO
  https://atomyx.dev — documentation
`);
}
