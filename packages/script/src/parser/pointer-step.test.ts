import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseScript } from "./yaml-parser.js";
import { ScriptParseError } from "./selector-compiler.js";
import type { PointerStep } from "@atomyx/shared/script";

function firstStep(yaml: string): PointerStep {
  const script = parseScript(yaml);
  assert.equal(script.steps.length, 1);
  assert.equal(script.steps[0]!.command, "pointer");
  return script.steps[0] as PointerStep;
}

describe("pointer step — single-pointer form", () => {
  it("parses the longpress+drag canonical example", () => {
    const step = firstStep(`
appId: com.example
---
- pointer:
    actions:
      - down: "Item A"
      - wait: 800
      - move: { x: 300, y: 600 }
      - up
    moveDurationMs: 300
`);
    assert.deepEqual(step.actions, [
      { type: "down", target: { selector: { text: "Item A", label: "Item A" } } },
      { type: "wait", ms: 800 },
      { type: "move", target: { point: { x: 300, y: 600 } } },
      { type: "up" },
    ]);
    assert.equal(step.moveDurationMs, 300);
    assert.equal(step.pointers, undefined);
  });

  it("parses a coordinate-only drag", () => {
    const step = firstStep(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 100, y: 200 }
      - move: { x: 300, y: 400 }
      - up
`);
    assert.deepEqual(step.actions, [
      { type: "down", target: { point: { x: 100, y: 200 } } },
      { type: "move", target: { point: { x: 300, y: 400 } } },
      { type: "up" },
    ]);
    assert.equal(step.moveDurationMs, undefined);
  });

  it("parses an object-form selector target", () => {
    const step = firstStep(`
appId: com.example
---
- pointer:
    actions:
      - down: { id: "btn_item", role: "button" }
      - up
`);
    assert.deepEqual(step.actions, [
      {
        type: "down",
        target: { selector: { id: "btn_item", role: "button" } },
      },
      { type: "up" },
    ]);
  });
});

describe("pointer step — multi-pointer form (reserved for later milestone)", () => {
  it("parses two parallel pointer groups", () => {
    const step = firstStep(`
appId: com.example
---
- pointer:
    pointers:
      - id: finger1
        actions:
          - down: { x: 100, y: 300 }
          - move: { x: 300, y: 300 }
          - up
      - id: finger2
        actions:
          - down: { x: 100, y: 500 }
          - move: { x: 300, y: 500 }
          - up
`);
    assert.equal(step.actions, undefined);
    assert.equal(step.pointers?.length, 2);
    assert.equal(step.pointers![0]!.id, "finger1");
    assert.equal(step.pointers![1]!.id, "finger2");
  });
});

describe("pointer step — parse errors", () => {
  it("rejects missing actions AND pointers", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    moveDurationMs: 200
`),
      (err) =>
        err instanceof ScriptParseError &&
        /must specify either `actions` or `pointers`/.test(err.message),
    );
  });

  it("rejects both actions AND pointers together", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions: [up]
    pointers:
      - id: f1
        actions: [up]
`),
      (err) =>
        err instanceof ScriptParseError &&
        /mutually exclusive/.test(err.message),
    );
  });

  it("rejects unknown action key", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - jump: 1
`),
      (err) =>
        err instanceof ScriptParseError &&
        /unknown action "jump"/.test(err.message),
    );
  });

  it("rejects bare string other than \"up\"", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down
      - up
`),
      (err) =>
        err instanceof ScriptParseError &&
        /bare string must be "up"/.test(err.message),
    );
  });

  it("rejects wait without numeric ms", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 1, y: 2 }
      - wait: fast
      - up
`),
      (err) =>
        err instanceof ScriptParseError &&
        /wait expects a number/.test(err.message),
    );
  });

  it("rejects non-object pointer value", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer: "drag"
`),
      (err) =>
        err instanceof ScriptParseError &&
        /expected an object with `actions` or `pointers`/.test(err.message),
    );
  });

  it("rejects multi-key action object", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 1, y: 2 }
        wait: 100
`),
      (err) =>
        err instanceof ScriptParseError &&
        /single-key object/.test(err.message),
    );
  });

  it("rejects non-numeric moveDurationMs", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 1, y: 2 }
      - up
    moveDurationMs: fast
`),
      (err) =>
        err instanceof ScriptParseError &&
        /moveDurationMs: expected number/.test(err.message),
    );
  });

  it("accepts pressure on down/move actions", () => {
    const step = firstStep(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 100, y: 200, pressure: 0.75 }
      - move: { x: 300, y: 400, pressure: 0.9 }
      - up
`);
    assert.equal(
      (step.actions![0] as { type: "down"; pressure?: number }).pressure,
      0.75,
    );
    assert.equal(
      (step.actions![1] as { type: "move"; pressure?: number }).pressure,
      0.9,
    );
  });

  it("rejects pressure out of [0.0, 1.0] range", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 0, y: 0, pressure: 1.5 }
      - up
`),
      (err) =>
        err instanceof ScriptParseError &&
        /pressure must be a number in \[0\.0, 1\.0\]/.test(err.message),
    );
  });

  it("rejects non-numeric pressure", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    actions:
      - down: { x: 0, y: 0, pressure: hard }
      - up
`),
      (err) =>
        err instanceof ScriptParseError &&
        /pressure must be a number/.test(err.message),
    );
  });

  it("rejects pointer group without id", () => {
    assert.throws(
      () =>
        parseScript(`
appId: com.example
---
- pointer:
    pointers:
      - actions: [up]
`),
      (err) =>
        err instanceof ScriptParseError &&
        /missing string `id`/.test(err.message),
    );
  });
});
