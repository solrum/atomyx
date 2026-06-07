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
import { execute as skillsExecute } from "./skills/execute.js";

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
  skills: { execute: skillsExecute },
  // Future:
  // mcp:    { execute: mcpExecute },
  // test:   { execute: testMgmtExecute },
  // studio: { execute: studioExecute },
};

/**
 * Shortcuts — common commands that skip the module prefix.
 *
 * `atomyx run --file x.yml`  → resolve to module "driver", args ["run", "--file", "x.yml"]
 * `atomyx devices`           → resolve to module "driver", args ["list-devices"]
 * `atomyx init`              → resolve to module "skills", args ["init"]
 * `atomyx update-skills`     → resolve to module "skills", args ["update-skills"]
 */
export const shortcuts: Record<
  string,
  (args: readonly string[]) => { module: string; args: readonly string[] }
> = {
  run: (args) => ({ module: "driver", args: ["run", ...args] }),
  devices: (args) => ({ module: "driver", args: ["list-devices", ...args] }),
  init: (args) => ({ module: "skills", args: ["init", ...args] }),
  "update-skills": (args) => ({ module: "skills", args: ["update-skills", ...args] }),
};
