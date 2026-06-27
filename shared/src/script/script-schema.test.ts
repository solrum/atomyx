import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  ScriptDefinitionSchema,
  ScriptSelectorSchema,
  ScriptStepSchema,
  SCRIPT_DEFAULTS,
} from "./script-schema.js";

/**
 * These tests guard the *refinements* added on top of the basic
 * shape — the new regexes, numeric ranges, and min-length
 * constraints introduced when we adopted zod as the single source
 * of truth. The basic happy-path is already covered indirectly by
 * `packages/script` parser tests.
 */
describe("ScriptDefinitionSchema — refinement constraints", () => {
  const baseEntry = {
    appId: "com.example.app",
    name: "Smoke",
    env: {},
    steps: [{ command: "launchApp" as const }],
  };

  it("accepts a minimal entry script", () => {
    const result = ScriptDefinitionSchema.safeParse(baseEntry);
    assert.equal(result.success, true);
  });

  it("accepts the configured format values", () => {
    for (const format of [SCRIPT_DEFAULTS.format, "atomyx/v2", "atomyx/v42"]) {
      const result = ScriptDefinitionSchema.safeParse({ ...baseEntry, format });
      assert.equal(result.success, true, `format "${format}" should be accepted`);
    }
  });

  it("rejects a format string that does not match atomyx/v<n>", () => {
    for (const format of ["v1", "atomyx-v1", "atomyx/vFOO", "other/v1"]) {
      const result = ScriptDefinitionSchema.safeParse({ ...baseEntry, format });
      assert.equal(result.success, false, `format "${format}" should be rejected`);
    }
  });

  it("rejects non-string env values (was: coerced in the old parser)", () => {
    const result = ScriptDefinitionSchema.safeParse({
      ...baseEntry,
      env: { count: 5 as unknown as string },
    });
    assert.equal(result.success, false);
  });

  it("rejects unknown top-level keys (strict object)", () => {
    const result = ScriptDefinitionSchema.safeParse({
      ...baseEntry,
      notAField: true,
    });
    assert.equal(result.success, false);
  });

  it("rejects empty-string tags", () => {
    const result = ScriptDefinitionSchema.safeParse({
      ...baseEntry,
      tags: ["smoke", ""],
    });
    assert.equal(result.success, false);
  });
});

describe("ScriptStepSchema — command-level constraints", () => {
  it("accepts capture with a valid var name", () => {
    const result = ScriptStepSchema.safeParse({
      command: "capture",
      pattern: "POST /api/login",
      as: "loginResp",
    });
    assert.equal(result.success, true);
  });

  it("rejects capture var names that start with a digit", () => {
    const result = ScriptStepSchema.safeParse({
      command: "capture",
      pattern: "POST /api/login",
      as: "1bad",
    });
    assert.equal(result.success, false);
  });

  it("rejects capture var names with spaces or dashes", () => {
    for (const as of ["bad name", "bad-name"]) {
      const result = ScriptStepSchema.safeParse({
        command: "capture",
        pattern: "POST /api/login",
        as,
      });
      assert.equal(result.success, false, `"${as}" should be rejected`);
    }
  });

  it("accepts assertApi with a status in 100–599", () => {
    for (const status of [100, 200, 404, 500, 599]) {
      const result = ScriptStepSchema.safeParse({
        command: "assertApi",
        from: "x",
        status,
      });
      assert.equal(result.success, true, `status ${status} should be accepted`);
    }
  });

  it("rejects assertApi with a status out of range", () => {
    for (const status of [0, 99, 600, 1000]) {
      const result = ScriptStepSchema.safeParse({
        command: "assertApi",
        from: "x",
        status,
      });
      assert.equal(result.success, false, `status ${status} should be rejected`);
    }
  });

  it("rejects handle with an empty branches array", () => {
    const result = ScriptStepSchema.safeParse({
      command: "handle",
      branches: [],
    });
    assert.equal(result.success, false);
  });

  it("rejects branch with an empty `on` array", () => {
    const result = ScriptStepSchema.safeParse({
      command: "branch",
      from: "resp",
      on: [],
    });
    assert.equal(result.success, false);
  });

  it("accepts handle with a valid branch + nested tap step", () => {
    const result = ScriptStepSchema.safeParse({
      command: "handle",
      branches: [
        {
          when: { visible: "OTP" },
          do: [
            {
              command: "tap",
              selector: { text: "Verify" },
            },
          ],
        },
      ],
    });
    assert.equal(result.success, true);
  });

  it("rejects pointer with both actions and pointers set", () => {
    const result = ScriptStepSchema.safeParse({
      command: "pointer",
      actions: [{ type: "up" }],
      pointers: [],
    });
    assert.equal(result.success, false);
  });

  it("rejects pointer with neither actions nor pointers", () => {
    const result = ScriptStepSchema.safeParse({
      command: "pointer",
      moveDurationMs: 100,
    });
    assert.equal(result.success, false);
  });

  it("rejects pressKey with an empty key", () => {
    const result = ScriptStepSchema.safeParse({
      command: "pressKey",
      key: "",
    });
    assert.equal(result.success, false);
  });

  it("accepts pointer pressure in [0, 1]", () => {
    for (const pressure of [0, 0.25, 1]) {
      const result = ScriptStepSchema.safeParse({
        command: "pointer",
        actions: [
          { type: "down", target: { selector: { text: "x" } }, pressure },
          { type: "up" },
        ],
      });
      assert.equal(result.success, true, `pressure ${pressure} should be accepted`);
    }
  });

  it("rejects pointer pressure out of [0, 1]", () => {
    for (const pressure of [-0.1, 1.1, 2]) {
      const result = ScriptStepSchema.safeParse({
        command: "pointer",
        actions: [
          { type: "down", target: { selector: { text: "x" } }, pressure },
          { type: "up" },
        ],
      });
      assert.equal(result.success, false, `pressure ${pressure} should be rejected`);
    }
  });
});

describe("ScriptSelectorSchema", () => {
  it("accepts an empty selector (priority broadening takes over)", () => {
    const result = ScriptSelectorSchema.safeParse({});
    assert.equal(result.success, true);
  });

  it("rejects unknown selector fields", () => {
    const result = ScriptSelectorSchema.safeParse({
      text: "Foo",
      notAField: "oops",
    });
    assert.equal(result.success, false);
  });

  it("rejects a negative nth", () => {
    const result = ScriptSelectorSchema.safeParse({ nth: -1 });
    assert.equal(result.success, false);
  });
});
