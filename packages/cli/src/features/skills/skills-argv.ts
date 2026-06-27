export class ArgvError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ArgvError";
  }
}

export interface ParsedArgv {
  readonly command: "init" | "update-skills";
  readonly flags: Readonly<Record<string, string | boolean>>;
  readonly help: boolean;
}

export const COMMAND_FLAGS: Record<string, Record<string, boolean>> = {
  init: {
    "--target": false,
    "--force": true,
    "--help": true,
    "-h": true,
  },
  "update-skills": {
    "--target": false,
    "--help": true,
    "-h": true,
  },
};

export function parseArgv(
  args: readonly string[],
  commandName: "init" | "update-skills",
): ParsedArgv {
  const defs = COMMAND_FLAGS[commandName] ?? {};
  const flags: Record<string, string | boolean> = {};

  for (let i = 0; i < args.length; i++) {
    const arg = args[i]!;

    if (arg === "--help" || arg === "-h") {
      return { command: commandName, flags, help: true };
    }

    const eqIdx = arg.indexOf("=");
    if (eqIdx > 0) {
      const key = arg.slice(0, eqIdx);
      if (!(key in defs)) {
        throw new ArgvError(
          `Command "${commandName}" does not accept "${key}". ` +
            `Valid flags: ${Object.keys(defs).join(", ") || "(none)"}.`,
        );
      }
      const value = arg.slice(eqIdx + 1);
      if (value === "") {
        throw new ArgvError(
          `Flag "${key}" requires a non-empty value.`,
        );
      }
      if (key in flags) {
        throw new ArgvError(`Flag "${key}" was specified more than once.`);
      }
      flags[key] = value;
      continue;
    }

    if (!(arg in defs)) {
      throw new ArgvError(
        `Command "${commandName}" does not accept "${arg}". ` +
          `Valid flags: ${Object.keys(defs).join(", ") || "(none)"}.`,
      );
    }

    if (defs[arg]) {
      if (arg in flags) {
        throw new ArgvError(`Flag "${arg}" was specified more than once.`);
      }
      flags[arg] = true;
    } else {
      const nextArg = args[i + 1];
      if (nextArg === undefined || nextArg.startsWith("-")) {
        throw new ArgvError(`Flag "${arg}" requires a value.`);
      }
      if (arg in flags) {
        throw new ArgvError(`Flag "${arg}" was specified more than once.`);
      }
      flags[arg] = nextArg;
      i++;
    }
  }

  return { command: commandName, flags, help: false };
}
