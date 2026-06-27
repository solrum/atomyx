import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseScenario, isScenarioYaml } from "./scenario-parser.js";
import { ScriptParseError } from "./selector-compiler.js";

describe("parseScenario", () => {
  it("parses a minimal scenario", () => {
    const yaml = `
name: Smoke
scripts:
  - flows/login.yml
  - flows/checkout.yml
`;
    const s = parseScenario(yaml);
    assert.equal(s.name, "Smoke");
    assert.deepEqual(s.scripts, ["flows/login.yml", "flows/checkout.yml"]);
    assert.equal(s.onFailure, undefined);
  });

  it("accepts onFailure, env, tags, description", () => {
    const yaml = `
name: Regression
description: Full sweep across the checkout funnel
scripts:
  - a.yml
onFailure: continue
env:
  BASE_URL: https://staging.example.com
tags:
  - regression
  - release-candidate
`;
    const s = parseScenario(yaml);
    assert.equal(s.onFailure, "continue");
    assert.equal(s.env?.BASE_URL, "https://staging.example.com");
    assert.deepEqual(s.tags, ["regression", "release-candidate"]);
    assert.match(s.description ?? "", /checkout funnel/);
  });

  it("rejects scenarios with no scripts", () => {
    const yaml = `
name: Empty
scripts: []
`;
    assert.throws(() => parseScenario(yaml), ScriptParseError);
  });

  it("rejects scenarios missing the name field", () => {
    const yaml = `scripts: [a.yml]`;
    assert.throws(() => parseScenario(yaml), ScriptParseError);
  });

  it("rejects unknown top-level keys", () => {
    const yaml = `
name: X
scripts: [a.yml]
unknownField: nope
`;
    assert.throws(() => parseScenario(yaml), ScriptParseError);
  });

  it("rejects malformed YAML", () => {
    assert.throws(() => parseScenario("name: X\n  bad: indent"), ScriptParseError);
  });
});

describe("isScenarioYaml", () => {
  it("detects scenario shape via scripts: key", () => {
    assert.equal(isScenarioYaml("name: x\nscripts: [a.yml]"), true);
  });

  it("returns false for a regular script", () => {
    const yaml = `
appId: com.test
name: x
env: {}
---
- launchApp
`;
    assert.equal(isScenarioYaml(yaml), false);
  });

  it("returns false for a flow fragment", () => {
    assert.equal(isScenarioYaml("- launchApp\n- tap: Login"), false);
  });

  it("returns false for invalid YAML", () => {
    assert.equal(isScenarioYaml("name: X\n  bad: indent"), false);
  });
});
