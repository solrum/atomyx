import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import { cp, mkdir, readFile, writeFile } from "node:fs/promises";
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
  const skillsDir = join(targetDir, "skills");
  const agentsDir = join(targetDir, "agents");
  await mkdir(skillsDir, { recursive: true });
  await mkdir(agentsDir, { recursive: true });

  for (const name of SKILL_FILES) {
    await cp(
      join(CONTENT_ROOT, "skills", name),
      join(skillsDir, name),
      { force: overwrite, errorOnExist: !overwrite },
    );
  }
  for (const name of AGENT_FILES) {
    await cp(
      join(CONTENT_ROOT, "agents", name),
      join(agentsDir, name),
      { force: overwrite, errorOnExist: !overwrite },
    );
  }

  await writeFile(
    join(targetDir, VERSION_STAMP),
    JSON.stringify({ version: currentVersion }, null, 2) + "\n",
    "utf8",
  );
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
