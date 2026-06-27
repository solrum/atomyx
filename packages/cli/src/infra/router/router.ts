import type { DriverFactory } from "../../features/driver/index.js";
import type { SkillsApi } from "@atomyx/skills";
import { executeDriver } from "../../features/driver/index.js";
import { executeSkills } from "../../features/skills/index.js";

export interface RouterContext {
  readonly driverFactory: DriverFactory;
  readonly skills: SkillsApi;
}

interface ModuleExecutor {
  execute(args: readonly string[]): Promise<void>;
}

export interface Shortcut {
  (args: readonly string[]): { module: string; args: readonly string[] };
}

export const modules = (ctx: RouterContext): Record<string, ModuleExecutor> => ({
  driver: { execute: (args) => executeDriver(ctx.driverFactory, args) },
  skills: { execute: (args) => executeSkills(ctx.skills, args) },
});

export const shortcuts: Record<string, Shortcut> = {
  run: (args) => ({ module: "driver", args: ["run", ...args] }),
  devices: (args) => ({ module: "driver", args: ["list-devices", ...args] }),
  init: (args) => ({ module: "skills", args: ["init", ...args] }),
  "update-skills": (args) => ({ module: "skills", args: ["update-skills", ...args] }),
};
