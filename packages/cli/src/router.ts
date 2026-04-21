/**
 * Module router — maps subcommands to module CLI executors.
 *
 * Adding a new module:
 * 1. Create `@atomyx/<module>-cli` with an `execute(args)` export
 * 2. Add dependency to `@atomyx/cli` package.json
 * 3. Register in `modules` below
 * 4. Optionally add shortcuts
 */

import { execute as driverExecute } from "./driver/execute.js";

export interface ModuleExecutor {
  execute(args: readonly string[]): Promise<void>;
}

/**
 * Registered modules. Key = subcommand name.
 *
 * `atomyx driver run ...` → modules["driver"].execute(["run", ...])
 */
export const modules: Record<string, ModuleExecutor> = {
  driver: { execute: driverExecute },
  // Future:
  // mcp:    { execute: mcpExecute },
  // test:   { execute: testMgmtExecute },
  // studio: { execute: studioExecute },
};

/**
 * Shortcuts — common commands that skip the module prefix.
 *
 * `atomyx run --file x.yml` → resolve to module "driver", args ["run", "--file", "x.yml"]
 * `atomyx devices`          → resolve to module "driver", args ["list-devices"]
 */
export const shortcuts: Record<
  string,
  (args: readonly string[]) => { module: string; args: readonly string[] }
> = {
  run: (args) => ({ module: "driver", args: ["run", ...args] }),
  devices: (args) => ({ module: "driver", args: ["list-devices", ...args] }),
};
