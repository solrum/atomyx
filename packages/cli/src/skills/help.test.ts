import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { printCommandHelp, printModuleHelp } from "./help.js";

describe("printCommandHelp", () => {
  it("prints init usage when command is 'init'", () => {
    const lines: string[] = [];
    printCommandHelp("init", (s) => lines.push(s));
    assert.ok(lines.join("").includes("atomyx init"), "should include command name");
    assert.ok(lines.join("").includes("--force"), "should include --force flag");
  });

  it("prints update-skills usage when command is 'update-skills'", () => {
    const lines: string[] = [];
    printCommandHelp("update-skills", (s) => lines.push(s));
    assert.ok(
      lines.join("").includes("atomyx update-skills"),
      "should include command name",
    );
    assert.ok(lines.join("").includes("--target"), "should include --target flag");
  });
});

describe("printModuleHelp", () => {
  it("prints combined skills-module usage", () => {
    const lines: string[] = [];
    printModuleHelp((s) => lines.push(s));
    const out = lines.join("");
    assert.ok(out.includes("atomyx skills"), "should include module name");
    assert.ok(out.includes("init"), "should list init subcommand");
    assert.ok(out.includes("update-skills"), "should list update-skills subcommand");
  });
});
