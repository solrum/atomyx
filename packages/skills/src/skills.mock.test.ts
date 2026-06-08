import test from "node:test";
import assert from "node:assert/strict";
import { createMockSkills } from "./skills.mock.js";
import { SKILLS_VERSION } from "./version.js";

test("createMockSkills — copyTo writes paths and returns them in written", async () => {
  const skills = createMockSkills();
  const result = await skills.copyTo("/fake/dir");
  assert.ok(result.written.length > 0, "written must be non-empty");
  assert.deepEqual(result.skipped, []);
});

test("createMockSkills — second copyTo without overwrite throws EEXIST", async () => {
  const skills = createMockSkills();
  await skills.copyTo("/fake/dir");
  await assert.rejects(
    () => skills.copyTo("/fake/dir"),
    (err: unknown) => {
      assert.ok(err instanceof Error);
      assert.equal((err as NodeJS.ErrnoException).code, "EEXIST");
      return true;
    },
  );
});

test("createMockSkills — copyTo with overwrite succeeds on second call", async () => {
  const skills = createMockSkills();
  await skills.copyTo("/fake/dir");
  const result = await skills.copyTo("/fake/dir", { overwrite: true });
  assert.ok(result.written.length > 0);
});

test("createMockSkills — getInstalledVersion returns null before any copyTo", async () => {
  const skills = createMockSkills();
  const result = await skills.getInstalledVersion("/fake/dir");
  assert.equal(result.version, null);
  assert.equal(result.current, SKILLS_VERSION);
  assert.equal(result.upToDate, false);
});

test("createMockSkills — getInstalledVersion returns current version after copyTo", async () => {
  const skills = createMockSkills();
  await skills.copyTo("/fake/dir");
  const result = await skills.getInstalledVersion("/fake/dir");
  assert.equal(result.version, SKILLS_VERSION);
  assert.equal(result.upToDate, true);
});

test("createMockSkills — seed sets initial installedVersion", async () => {
  const skills = createMockSkills({ installedVersion: "0.0.1" });
  const result = await skills.getInstalledVersion("/fake/dir");
  assert.equal(result.version, "0.0.1");
  assert.equal(result.upToDate, false);
});

test("createMockSkills — state() reflects internal write history", async () => {
  const skills = createMockSkills();
  await skills.copyTo("/fake/dir");
  const state = skills.state();
  assert.ok(state.writtenPaths.size > 0);
  assert.equal(state.installedVersion, SKILLS_VERSION);
});
