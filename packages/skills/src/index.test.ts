import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, existsSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
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
