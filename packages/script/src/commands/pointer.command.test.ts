import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "@atomyx/driver/orchestra";
import { SystemClock, NoopLogger } from "@atomyx/core/infra";
import { MockDriver, node } from "@atomyx/driver/testing";
import type { ScriptDefinition } from "@atomyx/shared/script";
import { ScriptRunner } from "../runner/script-runner.js";
import { DEFAULT_COMMANDS } from "./index.js";

function buildRunner() {
  const driver = new MockDriver();
  const tree = node({
    id: "root",
    bounds: "0,0,430,932",
    role: "container",
    children: [
      node({
        id: "item_A",
        text: "Item A",
        label: "Item A",
        role: "button",
        clickable: true,
        bounds: "20,100,200,200",
      }),
      node({
        id: "drop_zone",
        text: "Drop zone",
        label: "Drop zone",
        role: "container",
        bounds: "220,400,420,600",
      }),
    ],
  });
  driver.stageHierarchyRepeated(tree, 100);
  const clock = new SystemClock();
  const logger = new NoopLogger();
  const orchestra = new Orchestra({ driver, clock, logger });
  const runner = new ScriptRunner({
    orchestra,
    clock,
    logger,
    commands: DEFAULT_COMMANDS,
  });
  return { runner, driver };
}

function script(steps: readonly unknown[]): ScriptDefinition {
  return {
    appId: "com.test",
    name: "test",
    env: {},
    steps: steps as ScriptDefinition["steps"],
  };
}

function gestureCalls(driver: MockDriver) {
  return driver.calls.filter((c) =>
    ["tap", "longPress", "swipe"].includes(c.method),
  );
}

describe("pointer command — pattern classification (happy paths)", () => {
  it("[down, up] at coord → tapAt", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 100, y: 200 } } },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "tap");
    assert.deepEqual(calls[0]!.args, [{ x: 100, y: 200 }]);
  });

  it("[down, wait, up] → longPressAt with hold duration", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 50, y: 60 } } },
            { type: "wait", ms: 750 },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "longPress");
    assert.deepEqual(calls[0]!.args, [{ x: 50, y: 60 }, 750]);
  });

  it("[down, move, up] → swipe with minimum press", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 10, y: 20 } } },
            { type: "move", target: { point: { x: 30, y: 40 } } },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "swipe");
    // from, to, pressMs=50 (DRAG_MIN_PRESS_MS)
    assert.deepEqual(calls[0]!.args, [
      { x: 10, y: 20 },
      { x: 30, y: 40 },
      50,
    ]);
  });

  it("[down, wait(N), move, up] → swipe with press=N (longpress+drag)", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 10, y: 20 } } },
            { type: "wait", ms: 800 },
            { type: "move", target: { point: { x: 300, y: 600 } } },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "swipe");
    assert.deepEqual(calls[0]!.args, [
      { x: 10, y: 20 },
      { x: 300, y: 600 },
      800,
    ]);
  });

  it("resolves a selector target to element center", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            {
              type: "down",
              target: { selector: { text: "Item A", label: "Item A" } },
            },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "tap");
    // Item A bounds=20,100,200,200 → center (110, 150)
    assert.deepEqual(calls[0]!.args, [{ x: 110, y: 150 }]);
  });

  it("longpress+drag with selector targets — end-to-end", async () => {
    const { runner, driver } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            {
              type: "down",
              target: { selector: { text: "Item A", label: "Item A" } },
            },
            { type: "wait", ms: 800 },
            {
              type: "move",
              target: { selector: { text: "Drop zone", label: "Drop zone" } },
            },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const calls = gestureCalls(driver);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.method, "swipe");
    // Item A center (110,150) → Drop zone bounds 220,400,420,600 center (320,500)
    assert.deepEqual(calls[0]!.args, [
      { x: 110, y: 150 },
      { x: 320, y: 500 },
      800,
    ]);
  });
});

describe("pointer command — multi-pointer form", () => {
  function buildMultiRunner() {
    // Same fixture but driver reports canMultiPointer=true so
    // the multi-pointer path can execute.
    const driver = new MockDriver();
    driver.capabilities = { ...driver.capabilities, canMultiPointer: true };
    driver.stageHierarchyRepeated(
      node({
        id: "root",
        bounds: "0,0,430,932",
        role: "container",
        children: [],
      }),
      100,
    );
    const clock = new SystemClock();
    const logger = new NoopLogger();
    const orchestra = new Orchestra({ driver, clock, logger });
    const runner = new ScriptRunner({
      orchestra,
      clock,
      logger,
      commands: DEFAULT_COMMANDS,
    });
    return { runner, driver };
  }

  it("dispatches a 2-pointer pinch gesture via dispatchGesture", async () => {
    const { runner, driver } = buildMultiRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 100, y: 300 } } },
                { type: "move", target: { point: { x: 50, y: 300 } } },
                { type: "up" },
              ],
            },
            {
              id: "f2",
              actions: [
                { type: "down", target: { point: { x: 100, y: 500 } } },
                { type: "move", target: { point: { x: 150, y: 500 } } },
                { type: "up" },
              ],
            },
          ],
          moveDurationMs: 300,
        },
      ]),
    );
    assert.equal(result.ok, true);
    const gestureCalls2 = driver.calls.filter((c) => c.method === "dispatchGesture");
    assert.equal(gestureCalls2.length, 1);
    const gesture = gestureCalls2[0]!.args[0] as {
      pointers: Array<{ id: string; waypoints: unknown[] }>;
    };
    assert.equal(gesture.pointers.length, 2);
    assert.equal(gesture.pointers[0]!.id, "f1");
    assert.equal(gesture.pointers[1]!.id, "f2");
    assert.equal(gesture.pointers[0]!.waypoints.length, 3);
  });

  it("rejects single-pointer gesture in the multi-pointer form", async () => {
    const { runner } = buildMultiRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "only",
              actions: [
                { type: "down", target: { point: { x: 0, y: 0 } } },
                { type: "up" },
              ],
            },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const detail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(detail, /POINTER_FORM_CONFLICT/);
    assert.match(detail, /at least 2 pointers/);
  });

  it("rejects duplicate pointer ids", async () => {
    const { runner } = buildMultiRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 0, y: 0 } } },
                { type: "up" },
              ],
            },
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 10, y: 10 } } },
                { type: "up" },
              ],
            },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const detail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(detail, /duplicate pointer id "f1"/);
  });

  it("rejects multi-pointer when driver capability is false", async () => {
    // Use the default single-pointer fixture where canMultiPointer=false.
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 0, y: 0 } } },
                { type: "up" },
              ],
            },
            {
              id: "f2",
              actions: [
                { type: "down", target: { point: { x: 10, y: 10 } } },
                { type: "up" },
              ],
            },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const detail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(detail, /POINTER_MULTI_NOT_SUPPORTED/);
  });

  it("surfaces per-pointer shape errors with the pointer id", async () => {
    const { runner } = buildMultiRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "good",
              actions: [
                { type: "down", target: { point: { x: 0, y: 0 } } },
                { type: "up" },
              ],
            },
            {
              id: "bad",
              actions: [
                // no opening down — violates rule 1 scoped to this pointer
                { type: "up" },
              ],
            },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const detail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(detail, /POINTER_NO_OPENING_DOWN/);
    assert.match(detail, /pointer "bad"/);
  });
});

describe("pointer command — pressure (3D Touch)", () => {
  function buildPressureRunner() {
    const driver = new MockDriver();
    driver.capabilities = {
      ...driver.capabilities,
      canMultiPointer: true,
      canPressure: true,
    };
    driver.stageHierarchyRepeated(
      node({
        id: "root",
        bounds: "0,0,430,932",
        role: "container",
        children: [],
      }),
      100,
    );
    const clock = new SystemClock();
    const logger = new NoopLogger();
    const orchestra = new Orchestra({ driver, clock, logger });
    const runner = new ScriptRunner({
      orchestra,
      clock,
      logger,
      commands: DEFAULT_COMMANDS,
    });
    return { runner, driver };
  }

  it("routes single-pointer + pressure through dispatchGesture with pressure preserved", async () => {
    const { runner, driver } = buildPressureRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 100, y: 200 } }, pressure: 0.9 },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, true);
    const gestureCalls = driver.calls.filter((c) => c.method === "dispatchGesture");
    assert.equal(gestureCalls.length, 1);
    const gesture = gestureCalls[0]!.args[0] as {
      pointers: Array<{ waypoints: Array<{ phase: string; pressure?: number }> }>;
    };
    assert.equal(gesture.pointers[0]!.waypoints[0]!.pressure, 0.9);
    // `up` carries no pressure
    assert.equal(gesture.pointers[0]!.waypoints[1]!.pressure, undefined);
  });

  it("rejects pressure when canPressure=false", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } }, pressure: 0.5 },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const detail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(detail, /POINTER_PRESSURE_NOT_SUPPORTED/);
  });

  it("multi-pointer gesture carries pressure on each waypoint", async () => {
    const { runner, driver } = buildPressureRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 50, y: 50 } }, pressure: 0.3 },
                { type: "move", target: { point: { x: 150, y: 50 } }, pressure: 0.7 },
                { type: "up" },
              ],
            },
            {
              id: "f2",
              actions: [
                { type: "down", target: { point: { x: 50, y: 200 } }, pressure: 0.3 },
                { type: "up" },
              ],
            },
          ],
          moveDurationMs: 300,
        },
      ]),
    );
    assert.equal(result.ok, true);
    const gestureCalls = driver.calls.filter((c) => c.method === "dispatchGesture");
    const gesture = gestureCalls[0]!.args[0] as {
      pointers: Array<{ id: string; waypoints: Array<{ phase: string; pressure?: number }> }>;
    };
    assert.equal(gesture.pointers[0]!.waypoints[0]!.pressure, 0.3);
    assert.equal(gesture.pointers[0]!.waypoints[1]!.pressure, 0.7);
  });
});

describe("pointer command — validator errors", () => {
  it("POINTER_EMPTY_SEQUENCE when actions is empty", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([{ command: "pointer", actions: [] }]),
    );
    assert.equal(result.ok, false);
    const lastDetail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(lastDetail,/POINTER_EMPTY_SEQUENCE/);
  });

  it("POINTER_NO_OPENING_DOWN when sequence starts with up", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        { command: "pointer", actions: [{ type: "up" }] },
      ]),
    );
    assert.equal(result.ok, false);
    const lastDetail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(lastDetail,/POINTER_NO_OPENING_DOWN/);
  });

  it("POINTER_NO_CLOSING_UP when sequence does not end with up", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } } },
            { type: "wait", ms: 50 },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const lastDetail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(lastDetail,/POINTER_NO_CLOSING_UP/);
  });

  it("POINTER_NESTED_DOWN when a second down opens before matching up", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } } },
            { type: "down", target: { point: { x: 10, y: 10 } } },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const lastDetail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(lastDetail,/POINTER_NESTED_DOWN/);
  });

  it("POINTER_INVALID_WAIT for out-of-range wait", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } } },
            { type: "wait", ms: 60_000 },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    const lastDetail = result.steps[result.steps.length - 1]?.detail ?? "";
    assert.match(lastDetail,/POINTER_INVALID_WAIT/);
  });

  it("POINTER_INVALID_MOVE_DURATION for out-of-range moveDurationMs", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } } },
            { type: "up" },
          ],
          moveDurationMs: 20_000,
        },
      ]),
    );
    assert.equal(result.ok, false);
    assert.match(
      result.steps[result.steps.length - 1]?.detail ?? "",
      /POINTER_INVALID_MOVE_DURATION/,
    );
  });

  it("POINTER_MULTI_NOT_SUPPORTED when pointers: is used", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          pointers: [
            {
              id: "f1",
              actions: [
                { type: "down", target: { point: { x: 0, y: 0 } } },
                { type: "up" },
              ],
            },
            {
              id: "f2",
              actions: [
                { type: "down", target: { point: { x: 10, y: 10 } } },
                { type: "up" },
              ],
            },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    assert.match(
      result.steps[result.steps.length - 1]?.detail ?? "",
      /POINTER_MULTI_NOT_SUPPORTED/,
    );
  });

  it("POINTER_PATTERN_NOT_EXPRESSIBLE for repeated moves in one pointer", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            { type: "down", target: { point: { x: 0, y: 0 } } },
            { type: "move", target: { point: { x: 10, y: 10 } } },
            { type: "move", target: { point: { x: 20, y: 20 } } },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    assert.match(
      result.steps[result.steps.length - 1]?.detail ?? "",
      /POINTER_PATTERN_NOT_EXPRESSIBLE/,
    );
  });

  it("POINTER_SELECTOR_RESOLUTION_FAILED when selector matches nothing", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(
      script([
        {
          command: "pointer",
          actions: [
            {
              type: "down",
              target: { selector: { text: "No such element" } },
            },
            { type: "up" },
          ],
        },
      ]),
    );
    assert.equal(result.ok, false);
    assert.match(
      result.steps[result.steps.length - 1]?.detail ?? "",
      /POINTER_SELECTOR_RESOLUTION_FAILED/,
    );
  });
});
