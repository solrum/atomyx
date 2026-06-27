import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, existsSync, readFileSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir, platform } from "node:os";
import { join } from "node:path";
import { createFsSkills } from "./skills.fs.js";
import { SKILL_FILES, AGENT_FILES } from "./skills.files.js";
import { SKILLS_VERSION } from "./version.js";

function makeTempDir(): string {
  return mkdtempSync(join(tmpdir(), "atomyx-skills-test-"));
}

const skills = createFsSkills();

test("copyTo writes every bundled skill and agent", async () => {
  const dir = makeTempDir();
  try {
    await skills.copyTo(dir);
    for (const name of SKILL_FILES) {
      assert.ok(
        existsSync(join(dir, "skills", name)),
        `expected skill ${name} to be copied`,
      );
    }
    for (const name of AGENT_FILES) {
      assert.ok(
        existsSync(join(dir, "agents", name)),
        `expected agent ${name} to be copied`,
      );
    }
    const stampPath = join(dir, "atomyx-skills.version.json");
    assert.ok(existsSync(stampPath), "version stamp written");
    const stamp = JSON.parse(readFileSync(stampPath, "utf8")) as {
      version: string;
    };
    assert.equal(stamp.version, SKILLS_VERSION);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copyTo returns written paths and empty skipped on success", async () => {
  const dir = makeTempDir();
  try {
    const result = await skills.copyTo(dir);
    assert.ok(result.written.length > 0, "written must be non-empty");
    assert.deepEqual(result.skipped, []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copyTo defaults to non-destructive copy", async () => {
  const dir = makeTempDir();
  try {
    await skills.copyTo(dir);
    await assert.rejects(() => skills.copyTo(dir));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copyTo with overwrite replaces existing files", async () => {
  const dir = makeTempDir();
  try {
    await skills.copyTo(dir);
    await skills.copyTo(dir, { overwrite: true });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("getInstalledVersion reads back the stamped version", async () => {
  const dir = makeTempDir();
  try {
    await skills.copyTo(dir);
    const result = await skills.getInstalledVersion(dir);
    assert.equal(result.version, SKILLS_VERSION);
    assert.equal(result.current, SKILLS_VERSION);
    assert.equal(result.upToDate, true);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("getInstalledVersion returns null version when no stamp exists", async () => {
  const dir = makeTempDir();
  try {
    const result = await skills.getInstalledVersion(dir);
    assert.equal(result.version, null);
    assert.equal(result.upToDate, false);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copyTo leaves target clean when a write fails mid-copy", { skip: platform() === "win32" }, async () => {
  const dir = makeTempDir();
  try {
    const firstSkill = SKILL_FILES[0];
    mkdirSync(join(dir, "skills"), { recursive: true });
    writeFileSync(join(dir, "skills", firstSkill), "existing");
    chmodSync(join(dir, "skills", firstSkill), 0o444);

    await assert.rejects(
      () => skills.copyTo(dir, { overwrite: false }),
      (err: unknown) => {
        assert.ok(err instanceof Error);
        assert.equal((err as NodeJS.ErrnoException).code, "EEXIST");
        return true;
      },
    );

    const versionResult = await skills.getInstalledVersion(dir);
    assert.equal(
      versionResult.version,
      null,
      "version stamp must be absent after a failed copy",
    );

    for (const name of AGENT_FILES) {
      assert.ok(
        !existsSync(join(dir, "agents", name)),
        `agent ${name} must not be written after a failed copy`,
      );
    }
  } finally {
    try { chmodSync(join(dir, "skills", SKILL_FILES[0]), 0o644); } catch { /* best effort */ }
    rmSync(dir, { recursive: true, force: true });
  }
});
