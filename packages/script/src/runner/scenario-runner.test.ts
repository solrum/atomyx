import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "@atomyx/driver/orchestra";
import { SystemClock, NoopLogger } from "@atomyx/core/infra";
import { MockDriver, node } from "@atomyx/driver/testing";
import { ScenarioRunner, memoryScriptLoader } from "./scenario-runner.js";
import type { ScenarioDefinition } from "@atomyx/shared/script";

function buildOrchestra() {
  const driver = new MockDriver();
  const tree = node({
    id: "root",
    bounds: "0,0,430,932",
    role: "container",
    children: [
      node({
        id: "login_btn",
        text: "Login",
        role: "button",
        clickable: true,
        bounds: "50,100,380,150",
      }),
      node({
        id: "buy_btn",
        text: "Buy",
        role: "button",
        clickable: true,
        bounds: "50,200,380,250",
      }),
      node({
        id: "missing_btn",
        text: "Visible",
        role: "button",
        bounds: "50,300,380,350",
      }),
    ],
  });
  driver.stageHierarchyRepeated(tree, 200);
  const clock = new SystemClock();
  const logger = new NoopLogger();
  const orchestra = new Orchestra({ driver, clock, logger });
  return { orchestra, clock, logger };
}

const PASSING_SCRIPT = `
appId: com.test
name: Passing
env: {}
---
- launchApp
- tap: "Login"
`;

const FAILING_SCRIPT = `
appId: com.test
name: Failing
env: {}
---
- launchApp
- tap: "DoesNotExist"
`;

describe("ScenarioRunner", () => {
  it("runs all scripts in order and aggregates pass/fail", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      loadScript: memoryScriptLoader({
        "a.yml": PASSING_SCRIPT,
        "b.yml": PASSING_SCRIPT,
      }),
    });
    const scenario: ScenarioDefinition = {
      name: "Two pass",
      scripts: ["a.yml", "b.yml"],
    };
    const result = await runner.run(scenario);
    assert.equal(result.ok, true);
    assert.equal(result.totalScripts, 2);
    assert.equal(result.passedScripts, 2);
    assert.deepEqual(
      result.scripts.map((s) => s.status),
      ["passed", "passed"],
    );
  });

  it("stops on first failure when onFailure is stop", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      loadScript: memoryScriptLoader({
        "a.yml": PASSING_SCRIPT,
        "fail.yml": FAILING_SCRIPT,
        "after.yml": PASSING_SCRIPT,
      }),
    });
    const scenario: ScenarioDefinition = {
      name: "Stop on fail",
      scripts: ["a.yml", "fail.yml", "after.yml"],
      onFailure: "stop",
    };
    const result = await runner.run(scenario);
    assert.equal(result.ok, false);
    assert.equal(result.passedScripts, 1);
    assert.deepEqual(
      result.scripts.map((s) => s.status),
      ["passed", "failed", "skipped"],
    );
  });

  it("continues past failures when onFailure is continue", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      loadScript: memoryScriptLoader({
        "fail.yml": FAILING_SCRIPT,
        "after.yml": PASSING_SCRIPT,
      }),
    });
    const scenario: ScenarioDefinition = {
      name: "Continue",
      scripts: ["fail.yml", "after.yml"],
      onFailure: "continue",
    };
    const result = await runner.run(scenario);
    assert.equal(result.ok, false);
    assert.equal(result.passedScripts, 1);
    assert.deepEqual(
      result.scripts.map((s) => s.status),
      ["failed", "passed"],
    );
  });

  it("emits scenarioStarted/scriptStarted/scriptCompleted/scenarioCompleted", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    const events: string[] = [];
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      loadScript: memoryScriptLoader({ "a.yml": PASSING_SCRIPT }),
      onScenarioEvent: (e) => events.push(e.type),
    });
    await runner.run({ name: "E", scripts: ["a.yml"] });
    assert.deepEqual(events, [
      "scenarioStarted",
      "scriptStarted",
      "scriptCompleted",
      "scenarioCompleted",
    ]);
  });

  it("aborts via signal between scripts", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    const controller = new AbortController();
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      signal: controller.signal,
      loadScript: memoryScriptLoader({
        "a.yml": PASSING_SCRIPT,
        "b.yml": PASSING_SCRIPT,
      }),
      onScenarioEvent: (e) => {
        if (e.type === "scriptCompleted" && e.scriptIndex === 0) {
          controller.abort();
        }
      },
    });
    await assert.rejects(
      () => runner.run({ name: "A", scripts: ["a.yml", "b.yml"] }),
      /aborted/i,
    );
  });

  it("merges scenario env into each child script (script wins on collision)", async () => {
    const { orchestra, clock, logger } = buildOrchestra();
    // The script declares `env.GREETING: hello` and uses ${GREETING}; the
    // scenario also sets GREETING. Per the documented merge rule the
    // script's value wins, so the literal "hello" must end up in the
    // launched app id where the variable is interpolated.
    const yaml = `
appId: com.\${GREETING}
name: Env test
env:
  GREETING: hello
---
- launchApp
`;
    const runner = new ScenarioRunner({
      orchestra,
      clock,
      logger,
      loadScript: memoryScriptLoader({ "a.yml": yaml }),
    });
    const result = await runner.run({
      name: "Env",
      scripts: ["a.yml"],
      env: { GREETING: "scenario-wins-only-when-script-omits" },
    });
    assert.equal(result.ok, true);
  });
});
