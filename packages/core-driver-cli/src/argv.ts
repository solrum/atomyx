/**
 * Tiny hand-written argv parser. Atomyx CLI surface is small
 * enough that pulling in commander/yargs/clipanion is wasted
 * weight — a 60-line parser covers every subcommand the CLI
 * ships with, with zero runtime dependencies.
 *
 * Grammar:
 *
 *   atomyx-driver <command> [--flag=value | --flag value | --bool]
 *
 * Commands:
 *
 *   mcp           Start the MCP stdio server. Requires --platform.
 *   list-devices  Enumerate connected devices (planned).
 *   version       Print version + exit.
 *   help          Print usage + exit (default when no command).
 *
 * Flags consumed by `mcp`:
 *
 *   --platform <ios|android>   (required)
 *   --device <udid|serial>     (required for ios real device + Android
 *                               emulator; optional for ios simulator
 *                               where the driver discovers booted)
 *   --kind <simulator|device>  (ios only; defaults to simulator)
 *   --port <number>            (override TCP port for ios driver)
 *   --log-level <debug|info|warn|error>  (defaults to info)
 *
 * Unknown commands or required flags missing → throws
 * `ArgvError` with a usage hint. main.ts catches and prints to
 * stderr with exit code 1.
 */

export class ArgvError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ArgvError";
  }
}

export interface ParsedArgv {
  readonly command: "mcp" | "list-devices" | "version" | "help";
  readonly platform?: "ios" | "android";
  readonly device?: string;
  readonly kind?: "simulator" | "device";
  readonly port?: number;
  readonly logLevel?: "debug" | "info" | "warn" | "error";
}

const VALID_COMMANDS = new Set(["mcp", "list-devices", "version", "help"]);

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
      `Unknown command "${first}". Valid commands: mcp, list-devices, version, help.`,
    );
  }

  const command = first as ParsedArgv["command"];
  const flags: Record<string, string | boolean> = {};
  for (let i = 1; i < args.length; i++) {
    const a = args[i]!;
    if (!a.startsWith("--")) {
      throw new ArgvError(`Unexpected positional argument "${a}".`);
    }
    const eq = a.indexOf("=");
    if (eq >= 0) {
      flags[a.slice(2, eq)] = a.slice(eq + 1);
      continue;
    }
    const key = a.slice(2);
    const next = args[i + 1];
    if (next !== undefined && !next.startsWith("--")) {
      flags[key] = next;
      i++;
    } else {
      flags[key] = true;
    }
  }

  if (command === "mcp") {
    return validateMcp(flags);
  }
  return { command };
}

function validateMcp(flags: Record<string, string | boolean>): ParsedArgv {
  const platform = flags["platform"];
  if (platform !== "ios" && platform !== "android") {
    throw new ArgvError(
      `mcp: --platform <ios|android> is required.\n` +
        `Example: atomyx-driver mcp --platform ios --kind simulator --device 00008101-...`,
    );
  }

  const kindRaw = flags["kind"];
  let kind: "simulator" | "device" | undefined;
  if (kindRaw !== undefined) {
    if (kindRaw !== "simulator" && kindRaw !== "device") {
      throw new ArgvError(`mcp: --kind must be "simulator" or "device".`);
    }
    kind = kindRaw;
  }

  if (platform === "ios" && kind === "device" && typeof flags["device"] !== "string") {
    throw new ArgvError(
      `mcp: --device <udid> is required when --platform=ios --kind=device.`,
    );
  }
  if (platform === "android" && typeof flags["device"] !== "string") {
    throw new ArgvError(
      `mcp: --device <serial> is required for --platform=android. ` +
        `Run \`adb devices\` to find the serial.`,
    );
  }

  const portRaw = flags["port"];
  let port: number | undefined;
  if (typeof portRaw === "string") {
    const parsed = Number(portRaw);
    if (!Number.isInteger(parsed) || parsed <= 0) {
      throw new ArgvError(`mcp: --port must be a positive integer.`);
    }
    port = parsed;
  }

  const levelRaw = flags["log-level"];
  let logLevel: ParsedArgv["logLevel"];
  if (typeof levelRaw === "string") {
    if (!["debug", "info", "warn", "error"].includes(levelRaw)) {
      throw new ArgvError(`mcp: --log-level must be debug | info | warn | error.`);
    }
    logLevel = levelRaw as ParsedArgv["logLevel"];
  }

  return {
    command: "mcp",
    platform,
    device: typeof flags["device"] === "string" ? flags["device"] : undefined,
    kind,
    port,
    logLevel,
  };
}

export function printHelp(write: (s: string) => void = (s) => process.stderr.write(s)): void {
  write(`atomyx-driver — Atomyx core-driver CLI

USAGE
  atomyx-driver <command> [flags]

COMMANDS
  mcp             Start the MCP stdio server (the primary use case).
  list-devices    Enumerate connected devices (planned).
  version         Print version and exit.
  help            Print this usage and exit.

MCP COMMAND FLAGS
  --platform ios|android        Required. Which device platform to drive.
  --device <udid|serial>        Required for android and ios-device.
                                  Optional for ios-simulator (auto-detect).
  --kind simulator|device       iOS only. Default: simulator.
  --port <number>               iOS only. Override TCP port (default 22087).
  --log-level <level>           debug | info | warn | error (default info).

EXAMPLES
  atomyx-driver mcp --platform ios --kind simulator
  atomyx-driver mcp --platform ios --kind device --device 00008101-001529640E52001E
  atomyx-driver mcp --platform android --device emulator-5554

SEE ALSO
  https://atomyx.dev — documentation
`);
}
