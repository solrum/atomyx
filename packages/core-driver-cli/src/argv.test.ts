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

  it("unknown command throws", () => {
    assert.throws(() => parseArgv(["nonsense"]), ArgvError);
  });
});

describe("parseArgv — mcp command", () => {
  it("ios simulator without device flag", () => {
    const r = parseArgv(["mcp", "--platform", "ios", "--kind", "simulator"]);
    assert.equal(r.command, "mcp");
    assert.equal(r.platform, "ios");
    assert.equal(r.kind, "simulator");
    assert.equal(r.device, undefined);
  });

  it("ios device requires --device", () => {
    assert.throws(
      () => parseArgv(["mcp", "--platform", "ios", "--kind", "device"]),
      /--device .* required/,
    );
  });

  it("ios device with udid", () => {
    const r = parseArgv([
      "mcp",
      "--platform",
      "ios",
      "--kind",
      "device",
      "--device",
      "00008101-001529640E52001E",
    ]);
    assert.equal(r.kind, "device");
    assert.equal(r.device, "00008101-001529640E52001E");
  });

  it("android requires --device serial", () => {
    assert.throws(
      () => parseArgv(["mcp", "--platform", "android"]),
      /--device .* required/,
    );
  });

  it("android with serial", () => {
    const r = parseArgv(["mcp", "--platform", "android", "--device", "emulator-5554"]);
    assert.equal(r.platform, "android");
    assert.equal(r.device, "emulator-5554");
  });

  it("--platform required", () => {
    assert.throws(() => parseArgv(["mcp"]), /--platform .* required/);
  });

  it("--platform must be ios or android", () => {
    assert.throws(() => parseArgv(["mcp", "--platform", "windows"]), /required/);
  });

  it("--kind must be simulator or device", () => {
    assert.throws(
      () => parseArgv(["mcp", "--platform", "ios", "--kind", "robot"]),
      /must be "simulator" or "device"/,
    );
  });

  it("--port parses positive integer", () => {
    const r = parseArgv([
      "mcp",
      "--platform",
      "ios",
      "--kind",
      "simulator",
      "--port",
      "12345",
    ]);
    assert.equal(r.port, 12345);
  });

  it("--port rejects non-integer", () => {
    assert.throws(
      () =>
        parseArgv([
          "mcp",
          "--platform",
          "ios",
          "--kind",
          "simulator",
          "--port",
          "abc",
        ]),
      /positive integer/,
    );
  });

  it("--log-level parses valid levels", () => {
    const r = parseArgv([
      "mcp",
      "--platform",
      "android",
      "--device",
      "x",
      "--log-level",
      "debug",
    ]);
    assert.equal(r.logLevel, "debug");
  });

  it("--log-level rejects invalid level", () => {
    assert.throws(
      () =>
        parseArgv([
          "mcp",
          "--platform",
          "android",
          "--device",
          "x",
          "--log-level",
          "trace",
        ]),
      /debug \| info \| warn \| error/,
    );
  });

  it("supports --flag=value form", () => {
    const r = parseArgv([
      "mcp",
      "--platform=ios",
      "--kind=simulator",
      "--port=22087",
    ]);
    assert.equal(r.platform, "ios");
    assert.equal(r.port, 22087);
  });

  it("rejects positional argument after command", () => {
    assert.throws(
      () => parseArgv(["mcp", "extraArg", "--platform", "ios", "--kind", "simulator"]),
      /Unexpected positional argument/,
    );
  });
});

describe("parseArgv — list-devices", () => {
  it("parses with no flags", () => {
    const r = parseArgv(["list-devices"]);
    assert.equal(r.command, "list-devices");
  });
});
