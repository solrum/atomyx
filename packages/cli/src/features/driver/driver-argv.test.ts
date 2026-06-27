import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseArgv, ArgvError } from "./driver-argv.js";

describe("parseArgv — commands", () => {
  it("empty args → help", () => {
    assert.equal(parseArgv([]).command, "help");
  });

  it("--help → help", () => {
    assert.equal(parseArgv(["--help"]).command, "help");
    assert.equal(parseArgv(["-h"]).command, "help");
  });

  it("--version → version", () => {
    assert.equal(parseArgv(["--version"]).command, "version");
    assert.equal(parseArgv(["-v"]).command, "version");
  });

  it("list-devices → list-devices", () => {
    const result = parseArgv(["list-devices"]);
    assert.equal(result.command, "list-devices");
    assert.equal(result.flags["--json"], undefined);
  });

  it("run → run", () => {
    assert.equal(
      parseArgv(["run", "--file", "test.yml"]).command,
      "run",
    );
  });

  it("unknown command throws", () => {
    assert.throws(() => parseArgv(["nonsense"]), ArgvError);
  });

  it("unknown command error points at atomyx mcp for MCP use case", () => {
    assert.throws(
      () => parseArgv(["mcp", "--platform", "android"]),
      /atomyx mcp/,
    );
  });

  it("rejects unknown positional/flag noise", () => {
    assert.throws(() => parseArgv(["list-devices", "foo"]), ArgvError);
  });
});

describe("parseArgv — boolean flags", () => {
  it("list-devices --json sets json flag", () => {
    const result = parseArgv(["list-devices", "--json"]);
    assert.equal(result.flags["--json"], true);
  });

  it("rejects --json for commands that don't accept it", () => {
    assert.throws(() => parseArgv(["version", "--json"]), ArgvError);
  });

  it("rejects unknown flags for list-devices", () => {
    assert.throws(() => parseArgv(["list-devices", "--bogus"]), ArgvError);
  });
});

describe("parseArgv — value flags", () => {
  it("run --file parses value", () => {
    const result = parseArgv(["run", "--file", "test.yml"]);
    assert.equal(result.flags["--file"], "test.yml");
  });

  it("run --file=value parses equals form", () => {
    const result = parseArgv(["run", "--file=test.yml"]);
    assert.equal(result.flags["--file"], "test.yml");
  });

  it("run with all flags", () => {
    const result = parseArgv([
      "run",
      "--file", "flow.yml",
      "--platform", "ios",
      "--device", "ABC-123",
      "--proxy", "file:/tmp/cap.jsonl",
      "--json",
    ]);
    assert.equal(result.flags["--file"], "flow.yml");
    assert.equal(result.flags["--platform"], "ios");
    assert.equal(result.flags["--device"], "ABC-123");
    assert.equal(result.flags["--proxy"], "file:/tmp/cap.jsonl");
    assert.equal(result.flags["--json"], true);
  });

  it("value flag without value throws", () => {
    assert.throws(() => parseArgv(["run", "--file"]), /requires a value/);
  });

  it("value flag with next flag as value throws", () => {
    assert.throws(
      () => parseArgv(["run", "--file", "--json"]),
      /requires a value/,
    );
  });
});
