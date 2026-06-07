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
 * Flags:
 *   --target=<path>   Override destination directory (default: <cwd>/.claude)
 */
export async function runUpdateSkills(args: readonly string[], cwd: string = process.cwd()): Promise<number> {
  let targetDir = join(cwd, ".claude");

  for (const arg of args) {
    if (arg.startsWith("--target=")) {
      targetDir = arg.slice("--target=".length);
    } else {
      process.stderr.write(`error: unknown flag "${arg}"\n`);
      return 2;
    }
  }

  const installedVersion = await getInstalledVersion(targetDir);

  if (installedVersion === currentVersion) {
    process.stdout.write(`Atomyx skills are already up to date (v${currentVersion})\n`);
    return 0;
  }

  await copySkillsTo(targetDir, { overwrite: true });

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
