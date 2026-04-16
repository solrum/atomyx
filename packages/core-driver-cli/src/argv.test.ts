import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseArgv, ArgvError } from "./argv.js";

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
    assert.equal(parseArgv(["list-devices"]).command, "list-devices");
  });

  it("unknown command throws", () => {
    assert.throws(() => parseArgv(["nonsense"]), ArgvError);
  });

  it("unknown command error points at atomyx-mcp for MCP use case", () => {
    // Regression: the MCP stdio server moved out of the CLI into
    // @atomyx/core-driver-mcp as a sibling transport. Users who
    // still type `atomyx-driver mcp` should be redirected to the
    // new `atomyx-mcp` binary.
    assert.throws(
      () => parseArgv(["mcp", "--platform", "android"]),
      /atomyx-mcp/,
    );
  });

  it("rejects positional noise after a known command", () => {
    assert.throws(() => parseArgv(["list-devices", "foo"]), ArgvError);
  });
});
