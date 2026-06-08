import { join } from "node:path";
import { SKILL_FILES, AGENT_FILES, SKILLS_VERSION } from "@atomyx/skills";
import type { SkillsApi } from "@atomyx/skills";

export async function runUpdateSkills(
  skills: SkillsApi,
  flags: Readonly<Record<string, string | boolean>>,
  cwd: string = process.cwd(),
): Promise<number> {
  const targetFlag = flags["--target"];
  const targetDir =
    typeof targetFlag === "string" ? targetFlag : join(cwd, ".claude");

  const versionResult = await skills.getInstalledVersion(targetDir);

  if (versionResult.upToDate) {
    process.stdout.write(
      `Atomyx skills are already up to date (v${SKILLS_VERSION})\n`,
    );
    return 0;
  }

  try {
    await skills.copyTo(targetDir, { overwrite: true });
  } catch (err) {
    const code =
      err instanceof Error
        ? (err as NodeJS.ErrnoException).code
        : undefined;
    process.stderr.write(
      `error: failed to update skills in ${targetDir}` +
        (code != null ? ` (${code})` : "") +
        "\n" +
        (err instanceof Error ? `  ${err.message}\n` : ""),
    );
    return 1;
  }

  const allFiles = [...SKILL_FILES, ...AGENT_FILES];
  const out = process.stdout.write.bind(process.stdout);
  const fromLabel = versionResult.version ?? "(none)";
  out(`Atomyx skills updated: v${fromLabel} → v${SKILLS_VERSION}\n`);
  out(`  Destination: ${targetDir}\n`);
  for (const f of allFiles) {
    out(`  ↺ ${f}\n`);
  }
  return 0;
}
