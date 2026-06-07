import { join } from "node:path";
import { copySkillsTo, currentVersion, SKILL_FILES, AGENT_FILES } from "@atomyx/skills";

/**
 * `atomyx init` — copies bundled skills and agents into the
 * consumer project's `.claude/` directory.
 *
 * Flags:
 *   --target=<path>   Override destination directory (default: <cwd>/.claude)
 *   --force           Overwrite existing files without prompting.
 */
export async function runInit(args: readonly string[], cwd: string = process.cwd()): Promise<number> {
  let targetDir = join(cwd, ".claude");
  let force = false;

  for (const arg of args) {
    if (arg.startsWith("--target=")) {
      targetDir = arg.slice("--target=".length);
    } else if (arg === "--force") {
      force = true;
    } else {
      process.stderr.write(`error: unknown flag "${arg}"\n`);
      return 2;
    }
  }

  try {
    await copySkillsTo(targetDir, { overwrite: force });
  } catch (err) {
    const isExist =
      err instanceof Error &&
      ((err as NodeJS.ErrnoException).code === "EEXIST" ||
        err.message.includes("already exists"));
    if (isExist) {
      process.stderr.write(
        `error: files already exist in ${targetDir}\n` +
          `  Use --force to overwrite, or run \`atomyx update-skills\` to upgrade.\n`,
      );
      return 1;
    }
    throw err;
  }

  const allFiles = [...SKILL_FILES, ...AGENT_FILES];
  const out = process.stdout.write.bind(process.stdout);
  out(`Atomyx skills installed (v${currentVersion})\n`);
  out(`  Destination: ${targetDir}\n`);
  for (const f of allFiles) {
    out(`  + ${f}\n`);
  }
  return 0;
}
