import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, existsSync, readFileSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir, platform } from "node:os";
import { join } from "node:path";
import {
  copySkillsTo,
  currentVersion,
  getInstalledVersion,
  SKILL_FILES,
  AGENT_FILES,
} from "./index.js";

function makeTempDir(): string {
  return mkdtempSync(join(tmpdir(), "atomyx-skills-test-"));
}

test("currentVersion is a semver-shaped string", () => {
  assert.match(currentVersion, /^\d+\.\d+\.\d+/);
});

test("copySkillsTo writes every bundled skill and agent", async () => {
  const dir = makeTempDir();
  try {
    await copySkillsTo(dir);
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
    assert.equal(stamp.version, currentVersion);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copySkillsTo defaults to non-destructive copy", async () => {
  const dir = makeTempDir();
  try {
    await copySkillsTo(dir);
    await assert.rejects(copySkillsTo(dir));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("copySkillsTo with overwrite replaces existing files", async () => {
  const dir = makeTempDir();
  try {
    await copySkillsTo(dir);
    await copySkillsTo(dir, { overwrite: true });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("getInstalledVersion reads back the stamped version", async () => {
  const dir = makeTempDir();
  try {
    await copySkillsTo(dir);
    assert.equal(await getInstalledVersion(dir), currentVersion);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("getInstalledVersion returns null when no stamp exists", async () => {
  const dir = makeTempDir();
  try {
    assert.equal(await getInstalledVersion(dir), null);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// copySkillsTo atomicity: a write failure must leave the target in a
// consistent state — either all files present with the version stamp,
// or no new files written (no partial state). Skip on Windows where
// chmod-based read-only tricks differ.
test("copySkillsTo leaves target clean when a write fails mid-copy", { skip: platform() === "win32" }, async () => {
  const dir = makeTempDir();
  try {
    // Pre-create the skills subdir with a read-only file for the FIRST skill
    // so the non-overwrite path detects a conflict and throws before writing.
    const firstSkill = SKILL_FILES[0];
    mkdirSync(join(dir, "skills"), { recursive: true });
    writeFileSync(join(dir, "skills", firstSkill), "existing");
    chmodSync(join(dir, "skills", firstSkill), 0o444);

    // copySkillsTo(overwrite=false) must throw due to the pre-existing file.
    await assert.rejects(
      () => copySkillsTo(dir, { overwrite: false }),
      (err: unknown) => {
        assert.ok(err instanceof Error);
        assert.equal((err as NodeJS.ErrnoException).code, "EEXIST");
        return true;
      },
    );

    // The version stamp must NOT have been written (no partial state).
    assert.equal(
      await getInstalledVersion(dir),
      null,
      "version stamp must be absent after a failed copy",
    );

    // No agent files should have been written (nothing beyond the pre-existing skill).
    for (const name of AGENT_FILES) {
      assert.ok(
        !existsSync(join(dir, "agents", name)),
        `agent ${name} must not be written after a failed copy`,
      );
    }
  } finally {
    // Restore writability so rmSync can clean up.
    try { chmodSync(join(dir, "skills", SKILL_FILES[0]), 0o644); } catch { /* best effort */ }
    rmSync(dir, { recursive: true, force: true });
  }
});
