import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import { cp, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { SKILLS_VERSION } from "./version.js";

export { SKILLS_VERSION } from "./version.js";

export const currentVersion = SKILLS_VERSION;

export const SKILL_FILES = [
  "atomyx-test-loop.md",
  "atomyx-debug-failure.md",
  "atomyx-script-authoring.md",
] as const;

export const AGENT_FILES = [
  "atomyx-explorer.md",
  "atomyx-replayer.md",
] as const;

const CONTENT_ROOT = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "content",
);

const VERSION_STAMP = "atomyx-skills.version.json";

export interface CopySkillsOptions {
  readonly overwrite?: boolean;
}

export interface InstalledVersionStamp {
  readonly version: string;
}

export async function copySkillsTo(
  targetDir: string,
  options: CopySkillsOptions = {},
): Promise<void> {
  const overwrite = options.overwrite ?? false;

  // Validate that no destination file already exists before touching anything,
  // so a non-overwrite call either fully succeeds or leaves the target untouched.
  if (!overwrite) {
    const conflicts: string[] = [];
    for (const name of SKILL_FILES) {
      try {
        await readFile(join(targetDir, "skills", name));
        conflicts.push(join("skills", name));
      } catch {
        // file absent — no conflict
      }
    }
    for (const name of AGENT_FILES) {
      try {
        await readFile(join(targetDir, "agents", name));
        conflicts.push(join("agents", name));
      } catch {
        // file absent — no conflict
      }
    }
    if (conflicts.length > 0) {
      const err = Object.assign(
        new Error(`skills files already exist in ${targetDir}: ${conflicts.join(", ")}`),
        { code: "EEXIST" },
      );
      throw err;
    }
  }

  // Stage all writes into a sibling temp directory so that a failure mid-copy
  // never leaves the target in a partially-written state. The temp dir sits on
  // the same filesystem as targetDir so the final renames are atomic on most
  // systems (no cross-device copy).
  const tmpDir = `${targetDir}/.claude-skills.tmp-${process.pid}`;
  try {
    const tmpSkillsDir = join(tmpDir, "skills");
    const tmpAgentsDir = join(tmpDir, "agents");
    await mkdir(tmpSkillsDir, { recursive: true });
    await mkdir(tmpAgentsDir, { recursive: true });

    for (const name of SKILL_FILES) {
      await cp(join(CONTENT_ROOT, "skills", name), join(tmpSkillsDir, name), { force: true });
    }
    for (const name of AGENT_FILES) {
      await cp(join(CONTENT_ROOT, "agents", name), join(tmpAgentsDir, name), { force: true });
    }

    await writeFile(
      join(tmpDir, VERSION_STAMP),
      JSON.stringify({ version: currentVersion }, null, 2) + "\n",
      "utf8",
    );

    // All files in temp are ready. Move them into place.
    const skillsDir = join(targetDir, "skills");
    const agentsDir = join(targetDir, "agents");
    await mkdir(skillsDir, { recursive: true });
    await mkdir(agentsDir, { recursive: true });

    for (const name of SKILL_FILES) {
      await rename(join(tmpSkillsDir, name), join(skillsDir, name));
    }
    for (const name of AGENT_FILES) {
      await rename(join(tmpAgentsDir, name), join(agentsDir, name));
    }
    // Write the version stamp last so getInstalledVersion returns null if
    // any of the renames above failed.
    await rename(join(tmpDir, VERSION_STAMP), join(targetDir, VERSION_STAMP));
  } finally {
    // Clean up the temp dir regardless of success or failure.
    await rm(tmpDir, { recursive: true, force: true });
  }
}

export async function getInstalledVersion(
  targetDir: string,
): Promise<string | null> {
  try {
    const raw = await readFile(join(targetDir, VERSION_STAMP), "utf8");
    const parsed = JSON.parse(raw) as Partial<InstalledVersionStamp>;
    return typeof parsed.version === "string" ? parsed.version : null;
  } catch {
    return null;
  }
}

export function getContentRoot(): string {
  return CONTENT_ROOT;
}
