import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseArgv, ArgvError } from "./skills-argv.js";

// ── init ──────────────────────────────────────────────────────────────────────

describe("parseArgv — init: good inputs", () => {
  it("no flags → help: false, empty flags", () => {
    const result = parseArgv([], "init");
    assert.equal(result.command, "init");
    assert.equal(result.help, false);
    assert.deepEqual(result.flags, {});
  });

  it("--help → help: true", () => {
    const result = parseArgv(["--help"], "init");
    assert.equal(result.help, true);
    assert.equal(result.command, "init");
  });

  it("-h → help: true", () => {
    const result = parseArgv(["-h"], "init");
    assert.equal(result.help, true);
  });

  it("--target=<path> sets target flag", () => {
    const result = parseArgv(["--target=/some/path"], "init");
    assert.equal(result.flags["--target"], "/some/path");
    assert.equal(result.help, false);
  });

  it("--force sets force flag", () => {
    const result = parseArgv(["--force"], "init");
    assert.equal(result.flags["--force"], true);
  });

  it("--target=<path> --force together", () => {
    const result = parseArgv(["--target=/out", "--force"], "init");
    assert.equal(result.flags["--target"], "/out");
    assert.equal(result.flags["--force"], true);
  });

  it("--force --target=<path> order independent", () => {
    const result = parseArgv(["--force", "--target=/out"], "init");
    assert.equal(result.flags["--target"], "/out");
    assert.equal(result.flags["--force"], true);
  });
});

describe("parseArgv — init: error cases", () => {
  it("unknown flag throws ArgvError", () => {
    assert.throws(() => parseArgv(["--bogus"], "init"), ArgvError);
  });

  it("unknown flag error names the command", () => {
    assert.throws(
      () => parseArgv(["--bogus"], "init"),
      /Command "init" does not accept "--bogus"/,
    );
  });

  it("--target= with empty value throws ArgvError", () => {
    assert.throws(() => parseArgv(["--target="], "init"), ArgvError);
  });

  it("--target= empty value error mentions --target", () => {
    assert.throws(
      () => parseArgv(["--target="], "init"),
      /--target/,
    );
  });

  it("--target without =value and no next arg throws ArgvError", () => {
    assert.throws(() => parseArgv(["--target"], "init"), /requires a value/);
  });

  it("--target followed by another flag throws ArgvError", () => {
    assert.throws(
      () => parseArgv(["--target", "--force"], "init"),
      /requires a value/,
    );
  });

  it("duplicate boolean flag throws ArgvError", () => {
    assert.throws(
      () => parseArgv(["--force", "--force"], "init"),
      /more than once/,
    );
  });

  it("duplicate value flag throws ArgvError", () => {
    assert.throws(
      () => parseArgv(["--target=/a", "--target=/b"], "init"),
      /more than once/,
    );
  });
});

// ── update-skills ─────────────────────────────────────────────────────────────

describe("parseArgv — update-skills: good inputs", () => {
  it("no flags → help: false, empty flags", () => {
    const result = parseArgv([], "update-skills");
    assert.equal(result.command, "update-skills");
    assert.equal(result.help, false);
    assert.deepEqual(result.flags, {});
  });

  it("--help → help: true", () => {
    const result = parseArgv(["--help"], "update-skills");
    assert.equal(result.help, true);
    assert.equal(result.command, "update-skills");
  });

  it("-h → help: true", () => {
    const result = parseArgv(["-h"], "update-skills");
    assert.equal(result.help, true);
  });

  it("--target=<path> sets target flag", () => {
    const result = parseArgv(["--target=/some/path"], "update-skills");
    assert.equal(result.flags["--target"], "/some/path");
    assert.equal(result.help, false);
  });
});

describe("parseArgv — update-skills: error cases", () => {
  it("--force is not accepted (update-skills has no --force)", () => {
    assert.throws(
      () => parseArgv(["--force"], "update-skills"),
      ArgvError,
    );
  });

  it("unknown flag throws ArgvError", () => {
    assert.throws(() => parseArgv(["--json"], "update-skills"), ArgvError);
  });

  it("unknown flag error names the command", () => {
    assert.throws(
      () => parseArgv(["--json"], "update-skills"),
      /Command "update-skills" does not accept "--json"/,
    );
  });

  it("--target= with empty value throws ArgvError", () => {
    assert.throws(() => parseArgv(["--target="], "update-skills"), ArgvError);
  });

  it("--target= empty value error mentions --target", () => {
    assert.throws(
      () => parseArgv(["--target="], "update-skills"),
      /--target/,
    );
  });

  it("--target without value throws ArgvError", () => {
    assert.throws(
      () => parseArgv(["--target"], "update-skills"),
      /requires a value/,
    );
  });

  it("duplicate --target throws ArgvError", () => {
    assert.throws(
      () => parseArgv(["--target=/a", "--target=/b"], "update-skills"),
      /more than once/,
    );
  });
});

// ── ArgvError shape ───────────────────────────────────────────────────────────

describe("ArgvError shape", () => {
  it("name is ArgvError", () => {
    try {
      parseArgv(["--bogus"], "init");
      assert.fail("should have thrown");
    } catch (err) {
      assert.ok(err instanceof ArgvError);
      assert.equal((err as ArgvError).name, "ArgvError");
    }
  });

  it("message is non-empty", () => {
    try {
      parseArgv(["--bogus"], "init");
      assert.fail("should have thrown");
    } catch (err) {
      assert.ok(err instanceof ArgvError);
      assert.ok((err as ArgvError).message.length > 0);
    }
  });
});
