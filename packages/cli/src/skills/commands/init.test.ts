import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { runInit } from "./init.js";

let tmpBase: string;

before(async () => {
  tmpBase = await mkdtemp(join(tmpdir(), "atomyx-init-test-"));
});

after(async () => {
  await rm(tmpBase, { recursive: true, force: true });
});

describe("runInit", () => {
  it("copies files to --target dir and returns 0", async () => {
    const targetDir = join(tmpBase, "fresh");
    const exitCode = await runInit([`--target=${targetDir}`]);
    assert.equal(exitCode, 0);

    // version stamp should exist
    const { readFile } = await import("node:fs/promises");
    const stamp = JSON.parse(
      await readFile(join(targetDir, "atomyx-skills.version.json"), "utf8"),
    ) as { version: string };
    assert.ok(typeof stamp.version === "string");
    assert.ok(stamp.version.length > 0);
  });

  it("returns 1 and prints advice when files exist and --force is not set", async () => {
    const targetDir = join(tmpBase, "existing");
    // First install succeeds
    const first = await runInit([`--target=${targetDir}`]);
    assert.equal(first, 0);

    // Second install without --force should fail
    const second = await runInit([`--target=${targetDir}`]);
    assert.equal(second, 1);
  });

  it("returns 0 when --force is set and files already exist", async () => {
    const targetDir = join(tmpBase, "force-overwrite");
    // First install
    const first = await runInit([`--target=${targetDir}`]);
    assert.equal(first, 0);

    // Overwrite with --force
    const second = await runInit([`--target=${targetDir}`, "--force"]);
    assert.equal(second, 0);
  });

  it("uses <cwd>/.claude as default target when no --target flag", async () => {
    const tmpDir = join(tmpBase, "default-cwd");
    const exitCode = await runInit([], tmpDir);
    assert.equal(exitCode, 0);

    const { readFile } = await import("node:fs/promises");
    const stamp = JSON.parse(
      await readFile(join(tmpDir, ".claude", "atomyx-skills.version.json"), "utf8"),
    ) as { version: string };
    assert.ok(typeof stamp.version === "string");
    assert.ok(stamp.version.length > 0);
  });

  it("returns 2 for unknown flags", async () => {
    const exitCode = await runInit(["--unknown-flag"]);
    assert.equal(exitCode, 2);
  });
});
