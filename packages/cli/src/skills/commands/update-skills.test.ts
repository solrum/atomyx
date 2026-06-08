import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { createMockSkills, SKILLS_VERSION } from "@atomyx/skills";
import { runUpdateSkills } from "./update-skills.js";

describe("runUpdateSkills", () => {
  it("prints already-up-to-date message and returns 0 when versions match", async () => {
    const skills = createMockSkills({ installedVersion: SKILLS_VERSION });
    const exitCode = await runUpdateSkills(skills, { "--target": "/fake/target" });
    assert.equal(exitCode, 0);
  });

  it("copies files and returns 0 when installed version is stale", async () => {
    const skills = createMockSkills({ installedVersion: "0.0.1" });
    const exitCode = await runUpdateSkills(skills, { "--target": "/fake/target" });
    assert.equal(exitCode, 0);
    const result = await skills.getInstalledVersion("/fake/target");
    assert.equal(result.version, SKILLS_VERSION);
  });

  it("copies files and returns 0 when no version stamp exists", async () => {
    const skills = createMockSkills();
    const exitCode = await runUpdateSkills(skills, { "--target": "/fake/target" });
    assert.equal(exitCode, 0);
  });

  it("uses <cwd>/.claude as default target when no --target flag", async () => {
    const skills = createMockSkills();
    const cwd = "/fake/cwd";
    const exitCode = await runUpdateSkills(skills, {}, cwd);
    assert.equal(exitCode, 0);
    const state = skills.state();
    assert.ok(
      [...state.writtenPaths].some((p) => p.startsWith(join(cwd, ".claude"))),
      "written paths must include <cwd>/.claude prefix",
    );
  });

  it("returns 1 and prints a message when copyTo throws", async () => {
    const skills = createMockSkills({ installedVersion: "0.0.0" });

    const originalCopyTo = skills.copyTo.bind(skills);
    skills.copyTo = async (_targetDir, _opts) => {
      const err = Object.assign(new Error("EACCES: permission denied"), { code: "EACCES" });
      throw err;
    };

    const written: string[] = [];
    const origStderr = process.stderr.write.bind(process.stderr);
    process.stderr.write = (s: string) => { written.push(s); return true; };

    let exitCode: number;
    try {
      exitCode = await runUpdateSkills(skills, { "--target": "/fake/readonly" });
    } finally {
      process.stderr.write = origStderr;
      skills.copyTo = originalCopyTo;
    }

    assert.equal(exitCode!, 1, "command must return 1 when copyTo throws");
    assert.ok(
      written.some((s) => s.includes("error")),
      "stderr must contain an error message",
    );
  });
});
