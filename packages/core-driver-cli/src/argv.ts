/**
 * Tiny hand-written argv parser for the `atomyx-driver` utility
 * CLI. Covers non-MCP subcommands only — the MCP stdio transport
 * ships as a separate `atomyx-mcp` binary from
 * `@atomyx/core-driver-mcp` per the "CLI and MCP are parallel
 * transports" contract in `.claude/docs/architecture.md` §3.
 *
 * Grammar:
 *
 *   atomyx-driver <command> [--flag=value | --flag value | --bool]
 *
 * Commands:
 *
 *   list-devices  Enumerate connected devices.
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
  readonly command: "list-devices" | "version" | "help";
}

const VALID_COMMANDS = new Set(["list-devices", "version", "help"]);

export function parseArgv(args: readonly string[]): ParsedArgv {
  if (args.length === 0) {
    return { command: "help" };
  }

  const first = args[0]!;
  if (first === "--help" || first === "-h") {
    return { command: "help" };
  }
  if (first === "--version" || first === "-v") {
    return { command: "version" };
  }
  if (!VALID_COMMANDS.has(first)) {
    throw new ArgvError(
      `Unknown command "${first}". Valid commands: list-devices, version, help. ` +
        `For the MCP server, run \`atomyx-mcp\` — see ` +
        `@atomyx/core-driver-mcp package.`,
    );
  }

  const command = first as ParsedArgv["command"];
  // No subcommand of the utility CLI takes flags today; reject
  // unknown positional/flag noise to keep the surface tight.
  if (args.length > 1) {
    throw new ArgvError(
      `Command "${command}" does not accept additional arguments.`,
    );
  }
  return { command };
}

export function printHelp(write: (s: string) => void = (s) => process.stderr.write(s)): void {
  write(`atomyx-driver — Atomyx core-driver utility CLI

USAGE
  atomyx-driver <command>

COMMANDS
  list-devices    Enumerate connected iOS + Android devices.
  version         Print version and exit.
  help            Print this usage and exit.

MCP STDIO SERVER
  The MCP server is a separate binary shipped from
  @atomyx/core-driver-mcp — run \`atomyx-mcp\` directly. Example:

      atomyx-mcp --platform android --device emulator-5554

  CLI and MCP are parallel transports over the same core-driver
  Orchestra; see .claude/docs/architecture.md §3 for the contract.

SEE ALSO
  https://atomyx.dev — documentation
`);
}
