import test from "node:test";
import assert from "node:assert/strict";
import { SKILLS_VERSION, SKILL_FILES, AGENT_FILES } from "./index.js";

test("SKILLS_VERSION is a semver-shaped string", () => {
  assert.match(SKILLS_VERSION, /^\d+\.\d+\.\d+/);
});

test("SKILL_FILES is non-empty and every entry has atomyx- prefix", () => {
  assert.ok(SKILL_FILES.length > 0);
  for (const name of SKILL_FILES) {
    assert.ok(name.startsWith("atomyx-"), `expected atomyx- prefix: ${name}`);
  }
});

test("AGENT_FILES is non-empty and every entry has atomyx- prefix", () => {
  assert.ok(AGENT_FILES.length > 0);
  for (const name of AGENT_FILES) {
    assert.ok(name.startsWith("atomyx-"), `expected atomyx- prefix: ${name}`);
  }
});
