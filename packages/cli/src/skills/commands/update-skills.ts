import { join } from "node:path";
import {
  copySkillsTo,
  currentVersion,
  getInstalledVersion,
  SKILL_FILES,
  AGENT_FILES,
} from "@atomyx/skills";

/**
 * `atomyx update-skills` — overwrites installed skills/agents with
 * the bundled version when a newer version is available.
 *
 * Accepted flags (pre-parsed by the skills argv layer):
 *   --target   Override destination directory (default: <cwd>/.claude)
 */
export async function runUpdateSkills(
  flags: Readonly<Record<string, string | boolean>>,
  cwd: string = process.cwd(),
): Promise<number> {
  const targetFlag = flags["--target"];
  const targetDir =
    typeof targetFlag === "string" ? targetFlag : join(cwd, ".claude");

  const installedVersion = await getInstalledVersion(targetDir);

  if (installedVersion === currentVersion) {
    process.stdout.write(
      `Atomyx skills are already up to date (v${currentVersion})\n`,
    );
    return 0;
  }

  try {
    await copySkillsTo(targetDir, { overwrite: true });
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
  const fromLabel = installedVersion ?? "(none)";
  out(`Atomyx skills updated: v${fromLabel} → v${currentVersion}\n`);
  out(`  Destination: ${targetDir}\n`);
  for (const f of allFiles) {
    out(`  ↺ ${f}\n`);
  }
  return 0;
}
