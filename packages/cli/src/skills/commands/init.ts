import { join } from "node:path";
import { copySkillsTo, currentVersion, SKILL_FILES, AGENT_FILES } from "@atomyx/skills";

/**
 * `atomyx init` — copies bundled skills and agents into the
 * consumer project's `.claude/` directory.
 *
 * Accepted flags (pre-parsed by the skills argv layer):
 *   --target   Override destination directory (default: <cwd>/.claude)
 *   --force    Overwrite existing files without prompting.
 */
export async function runInit(
  flags: Readonly<Record<string, string | boolean>>,
  cwd: string = process.cwd(),
): Promise<number> {
  const targetFlag = flags["--target"];
  const targetDir =
    typeof targetFlag === "string" ? targetFlag : join(cwd, ".claude");
  const force = flags["--force"] === true;

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
