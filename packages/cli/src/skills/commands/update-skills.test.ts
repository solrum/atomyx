import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile, mkdir, chmod } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir, platform } from "node:os";
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

    await writeFile(
      join(targetDir, "atomyx-skills.version.json"),
      JSON.stringify({ version: currentVersion }),
      "utf8",
    );

    const exitCode = await runUpdateSkills({ "--target": targetDir });
    assert.equal(exitCode, 0);
  });

  it("copies files and returns 0 when installed version is stale", async () => {
    const targetDir = join(tmpBase, "stale");
    await mkdir(targetDir, { recursive: true });

    await writeFile(
      join(targetDir, "atomyx-skills.version.json"),
      JSON.stringify({ version: "0.0.1" }),
      "utf8",
    );

    const exitCode = await runUpdateSkills({ "--target": targetDir });
    assert.equal(exitCode, 0);

    const { readFile } = await import("node:fs/promises");
    const stamp = JSON.parse(
      await readFile(join(targetDir, "atomyx-skills.version.json"), "utf8"),
    ) as { version: string };
    assert.equal(stamp.version, currentVersion);
  });

  it("copies files and returns 0 when no version stamp exists", async () => {
    const targetDir = join(tmpBase, "no-stamp");
    await mkdir(targetDir, { recursive: true });

    const exitCode = await runUpdateSkills({ "--target": targetDir });
    assert.equal(exitCode, 0);
  });

  it("uses <cwd>/.claude as default target when no --target flag", async () => {
    const tmpDir = join(tmpBase, "default-cwd-update");
    // pre-install so update has something to update
    const { copySkillsTo } = await import("@atomyx/skills");
    await copySkillsTo(join(tmpDir, ".claude"), { overwrite: false });

    const exitCode = await runUpdateSkills({}, tmpDir);
    assert.equal(exitCode, 0);
  });

  // Bug B: copySkillsTo throwing an EACCES error must be caught; the command
  // must print a user-friendly message and return 1 (not propagate the raw
  // exception).
  it("returns 1 and prints a message when the target directory is read-only", { skip: platform() === "win32" }, async () => {
    const targetDir = join(tmpBase, "eacces-test");
    await mkdir(targetDir, { recursive: true });
    await writeFile(
      join(targetDir, "atomyx-skills.version.json"),
      JSON.stringify({ version: "0.0.0" }),
      "utf8",
    );
    await chmod(targetDir, 0o555);

    const written: string[] = [];
    const origStderr = process.stderr.write.bind(process.stderr);
    process.stderr.write = (s: string) => { written.push(s); return true; };

    let exitCode: number;
    try {
      exitCode = await runUpdateSkills({ "--target": targetDir });
    } finally {
      await chmod(targetDir, 0o755);
      process.stderr.write = origStderr;
    }

    assert.equal(exitCode!, 1, "command must return 1 when target is read-only");
    assert.ok(
      written.some((s) => s.includes("error")),
      "stderr must contain an error message",
    );
  });
});
