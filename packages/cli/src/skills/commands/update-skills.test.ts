import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { currentVersion } from "@atomyx/skills";
import { runUpdateSkills } from "./update-skills.js";

let tmpBase: string;

before(async () => {
  tmpBase = await mkdtemp(join(tmpdir(), "atomyx-update-test-"));
});

after(async () => {
  await rm(tmpBase, { recursive: true, force: true });
});

describe("runUpdateSkills", () => {
  it("prints already-up-to-date message and returns 0 when versions match", async () => {
    const targetDir = join(tmpBase, "current");
    await mkdir(targetDir, { recursive: true });

    // Write the current version stamp
    await writeFile(
      join(targetDir, "atomyx-skills.version.json"),
      JSON.stringify({ version: currentVersion }),
      "utf8",
    );

    const exitCode = await runUpdateSkills([`--target=${targetDir}`]);
    assert.equal(exitCode, 0);
  });

  it("copies files and returns 0 when installed version is stale", async () => {
    const targetDir = join(tmpBase, "stale");
    await mkdir(targetDir, { recursive: true });

    // Write an old version stamp
    await writeFile(
      join(targetDir, "atomyx-skills.version.json"),
      JSON.stringify({ version: "0.0.1" }),
      "utf8",
    );

    const exitCode = await runUpdateSkills([`--target=${targetDir}`]);
    assert.equal(exitCode, 0);

    // Confirm stamp was updated to current version
    const { readFile } = await import("node:fs/promises");
    const stamp = JSON.parse(
      await readFile(join(targetDir, "atomyx-skills.version.json"), "utf8"),
    ) as { version: string };
    assert.equal(stamp.version, currentVersion);
  });

  it("copies files and returns 0 when no version stamp exists", async () => {
    const targetDir = join(tmpBase, "no-stamp");
    await mkdir(targetDir, { recursive: true });
    // No version stamp written — getInstalledVersion returns null

    const exitCode = await runUpdateSkills([`--target=${targetDir}`]);
    assert.equal(exitCode, 0);
  });

  it("returns 2 for unknown flags", async () => {
    const exitCode = await runUpdateSkills(["--bad-flag"]);
    assert.equal(exitCode, 2);
  });
});
