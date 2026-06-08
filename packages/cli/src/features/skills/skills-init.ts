import { join } from "node:path";
import { SKILL_FILES, AGENT_FILES, SKILLS_VERSION } from "@atomyx/skills";
import type { SkillsApi } from "@atomyx/skills";

export async function runInit(
  skills: SkillsApi,
  flags: Readonly<Record<string, string | boolean>>,
  cwd: string = process.cwd(),
): Promise<number> {
  const targetFlag = flags["--target"];
  const targetDir =
    typeof targetFlag === "string" ? targetFlag : join(cwd, ".claude");
  const force = flags["--force"] === true;

  try {
    await skills.copyTo(targetDir, { overwrite: force });
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
  out(`Atomyx skills installed (v${SKILLS_VERSION})\n`);
  out(`  Destination: ${targetDir}\n`);
  for (const f of allFiles) {
    out(`  + ${f}\n`);
  }
  return 0;
}
