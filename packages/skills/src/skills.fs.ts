import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import { cp, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { SKILLS_VERSION } from "./version.js";
import { SKILL_FILES, AGENT_FILES } from "./skills.files.js";
import type { CopyOptions, CopyResult, InstalledVersionResult, SkillsApi } from "./skills.contract.js";

const CONTENT_ROOT = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "content",
);

const VERSION_STAMP = "atomyx-skills.version.json";

export function createFsSkills(): SkillsApi {
  return {
    async copyTo(targetDir: string, opts: CopyOptions = {}): Promise<CopyResult> {
      const overwrite = opts.overwrite ?? false;

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
      const written: string[] = [];
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
          JSON.stringify({ version: SKILLS_VERSION }, null, 2) + "\n",
          "utf8",
        );

        const skillsDir = join(targetDir, "skills");
        const agentsDir = join(targetDir, "agents");
        await mkdir(skillsDir, { recursive: true });
        await mkdir(agentsDir, { recursive: true });

        for (const name of SKILL_FILES) {
          await rename(join(tmpSkillsDir, name), join(skillsDir, name));
          written.push(join("skills", name));
        }
        for (const name of AGENT_FILES) {
          await rename(join(tmpAgentsDir, name), join(agentsDir, name));
          written.push(join("agents", name));
        }
        // Write the version stamp last so getInstalledVersion returns null if
        // any of the renames above failed.
        await rename(join(tmpDir, VERSION_STAMP), join(targetDir, VERSION_STAMP));
      } finally {
        await rm(tmpDir, { recursive: true, force: true });
      }

      return { written, skipped: [] };
    },

    async getInstalledVersion(targetDir: string): Promise<InstalledVersionResult> {
      let version: string | null = null;
      try {
        const raw = await readFile(join(targetDir, VERSION_STAMP), "utf8");
        const parsed = JSON.parse(raw) as Partial<{ version: string }>;
        version = typeof parsed.version === "string" ? parsed.version : null;
      } catch {
        version = null;
      }
      return { version, current: SKILLS_VERSION, upToDate: version === SKILLS_VERSION };
    },
  };
}
