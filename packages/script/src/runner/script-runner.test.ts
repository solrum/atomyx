import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "@atomyx/driver/orchestra";
import { SystemClock, NoopLogger } from "@atomyx/core/infra";
import { MockDriver, node } from "@atomyx/driver/testing";
import type { ScriptDefinition } from "@atomyx/shared/script";
import { ScriptRunner } from "./script-runner.js";

function buildRunner() {
  const driver = new MockDriver();
  // Stage a tree with a tappable "Login" button — bounds required
  // for ScrollController to determine viewport position.
  const tree = node({
    id: "root",
    bounds: "0,0,430,932",
    role: "container",
    children: [
      node({ id: "login_btn", text: "Login", role: "button", clickable: true, bounds: "50,100,380,150" }),
      node({ id: "email", text: "", hint: "Email", role: "text-field", clickable: true, bounds: "50,200,380,250" }),
    ],
  });
  driver.stageHierarchyRepeated(tree, 100);
  const clock = new SystemClock();
  const logger = new NoopLogger();
  const orchestra = new Orchestra({ driver, clock, logger });
  const runner = new ScriptRunner({ orchestra, clock, logger });
  return { runner, driver };
}

describe("ScriptRunner", () => {
  it("runs a passing multi-step script", async () => {
    const { runner } = buildRunner();
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Simple flow",
      env: {},
      steps: [
        { command: "launchApp" },
        { command: "tap", selector: { text: "Login" } },
      ],
    };
    const result = await runner.run(script);
    assert.equal(result.ok, true);
    assert.equal(result.passedSteps, 2);
    assert.equal(result.totalSteps, 2);
    assert.equal(result.failedAtStep, undefined);
  });

  it("fails fast on first failing step", async () => {
    const { runner } = buildRunner();
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Fail test",
      env: {},
      steps: [
        { command: "launchApp" },
        { command: "tap", selector: { text: "NonExistent" } },
        { command: "screenshot" }, // should not run
      ],
    };
    const result = await runner.run(script);
    assert.equal(result.ok, false);
    assert.equal(result.failedAtStep, 1);
    assert.equal(result.steps.length, 2); // only 2 steps executed
    assert.equal(result.passedSteps, 1);
  });

  it("reports unknown command", async () => {
    const { runner } = buildRunner();
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Unknown",
      env: {},
      steps: [{ command: "nonexistent" } as never],
    };
    const result = await runner.run(script);
    assert.equal(result.ok, false);
    assert.equal(result.failedAtStep, 0);
    assert.match(result.steps[0]!.detail!, /Unknown command/);
  });

  it("throws when the abort signal fires between steps", async () => {
    const { driver } = buildRunner();
    const clock = new SystemClock();
    const logger = new NoopLogger();
    const orchestra = new Orchestra({ driver, clock, logger });
    const controller = new AbortController();
    const runner = new ScriptRunner({
      orchestra,
      clock,
      logger,
      signal: controller.signal,
      onStep: (e) => {
        if (e.type === "stepCompleted" && e.stepIndex === 0) {
          controller.abort();
        }
      },
    });
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Abortable",
      env: {},
      steps: [
        { command: "launchApp" },
        { command: "tap", selector: { text: "Login" } },
      ],
    };
    await assert.rejects(() => runner.run(script), /aborted/i);
  });

  it("throws immediately when the signal is already aborted", async () => {
    const { driver } = buildRunner();
    const clock = new SystemClock();
    const logger = new NoopLogger();
    const orchestra = new Orchestra({ driver, clock, logger });
    const runner = new ScriptRunner({
      orchestra,
      clock,
      logger,
      signal: AbortSignal.abort(),
    });
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Pre-aborted",
      env: {},
      steps: [{ command: "launchApp" }],
    };
    await assert.rejects(() => runner.run(script), /aborted/i);
  });

  it("collects screenshots in artifacts", async () => {
    const { runner } = buildRunner();
    const script: ScriptDefinition = {
      appId: "com.test",
      name: "Screenshot",
      env: {},
      steps: [
        { command: "launchApp" },
        { command: "screenshot", label: "evidence" },
      ],
    };
    const result = await runner.run(script);
    assert.equal(result.ok, true);
    const screenshots = result.artifacts.getScreenshots();
    assert.equal(screenshots.length, 1);
    assert.equal(screenshots[0]!.label, "evidence");
  });
});
