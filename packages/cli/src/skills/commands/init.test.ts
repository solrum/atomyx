import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { createMockSkills } from "@atomyx/skills";
import { runInit } from "./init.js";

describe("runInit", () => {
  it("copies files to --target dir and returns 0", async () => {
    const skills = createMockSkills();
    const exitCode = await runInit(skills, { "--target": "/fake/target" });
    assert.equal(exitCode, 0);
    const state = skills.state();
    assert.ok(state.writtenPaths.size > 0);
  });

  it("returns 1 and prints advice when files exist and --force is not set", async () => {
    const skills = createMockSkills();
    const first = await runInit(skills, { "--target": "/fake/target" });
    assert.equal(first, 0);

    const second = await runInit(skills, { "--target": "/fake/target" });
    assert.equal(second, 1);
  });

  it("returns 0 when --force is set and files already exist", async () => {
    const skills = createMockSkills();
    const first = await runInit(skills, { "--target": "/fake/target" });
    assert.equal(first, 0);

    const second = await runInit(skills, { "--target": "/fake/target", "--force": true });
    assert.equal(second, 0);
  });

  it("uses <cwd>/.claude as default target when no --target flag", async () => {
    const skills = createMockSkills();
    const cwd = "/fake/cwd";
    const exitCode = await runInit(skills, {}, cwd);
    assert.equal(exitCode, 0);
    const state = skills.state();
    assert.ok(
      [...state.writtenPaths].some((p) => p.startsWith(join(cwd, ".claude"))),
      "written paths must include <cwd>/.claude prefix",
    );
  });
});
