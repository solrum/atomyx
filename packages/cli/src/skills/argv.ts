/**
 * Argv parser for the skills-module subcommands.
 *
 * Grammar:
 *
 *   atomyx <command> [--flag=value | --bool | --help | -h]
 *
 * Commands handled here (skills shortcuts):
 *
 *   init            Copy bundled skills into <target>/.claude/.
 *   update-skills   Update an existing skills install.
 */

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

/**
 * Flag definitions per command.
 * - `true`  = boolean flag (no value)
 * - `false` = value flag (requires next arg or =value form)
 */
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

    // --help / -h shortcut
    if (arg === "--help" || arg === "-h") {
      return { command: commandName, flags, help: true };
    }

    // --flag=value form
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

    // --flag or -h form
    if (!(arg in defs)) {
      throw new ArgvError(
        `Command "${commandName}" does not accept "${arg}". ` +
          `Valid flags: ${Object.keys(defs).join(", ") || "(none)"}.`,
      );
    }

    if (defs[arg]) {
      // Boolean flag
      if (arg in flags) {
        throw new ArgvError(`Flag "${arg}" was specified more than once.`);
      }
      flags[arg] = true;
    } else {
      // Value flag — consume next arg
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
