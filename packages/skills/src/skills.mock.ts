import { SKILLS_VERSION } from "./version.js";
import type { CopyOptions, CopyResult, InstalledVersionResult, SkillsApi } from "./skills.contract.js";

export interface MockSkillsState {
  readonly installedVersion: string | null;
  readonly writtenPaths: ReadonlySet<string>;
}

export function createMockSkills(seed?: { installedVersion?: string }): SkillsApi & {
  state(): MockSkillsState;
} {
  const writtenPaths = new Set<string>();
  let installedVersion: string | null = seed?.installedVersion ?? null;

  return {
    state(): MockSkillsState {
      return { installedVersion, writtenPaths };
    },

    async copyTo(targetDir: string, opts: CopyOptions = {}): Promise<CopyResult> {
      const overwrite = opts.overwrite ?? false;
      const written: string[] = [];
      const skipped: string[] = [];

      const candidatePaths = [
        `${targetDir}/skills/atomyx-test-loop.md`,
        `${targetDir}/skills/atomyx-debug-failure.md`,
        `${targetDir}/skills/atomyx-script-authoring.md`,
        `${targetDir}/agents/atomyx-explorer.md`,
        `${targetDir}/agents/atomyx-replayer.md`,
      ];

      const conflicts = candidatePaths.filter((p) => writtenPaths.has(p));

      if (!overwrite && conflicts.length > 0) {
        const err = Object.assign(
          new Error(`skills files already exist in ${targetDir}: ${conflicts.join(", ")}`),
          { code: "EEXIST" },
        );
        throw err;
      }

      for (const p of candidatePaths) {
        if (!overwrite && writtenPaths.has(p)) {
          skipped.push(p);
        } else {
          writtenPaths.add(p);
          written.push(p);
        }
      }

      installedVersion = SKILLS_VERSION;
      return { written, skipped };
    },

    async getInstalledVersion(_targetDir: string): Promise<InstalledVersionResult> {
      return {
        version: installedVersion,
        current: SKILLS_VERSION,
        upToDate: installedVersion === SKILLS_VERSION,
      };
    },
  };
}
