import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { ScrollController, ScrollUnreachableError } from "./scroll-controller.js";
import { FakeClock } from "../infra/clock.port.js";
import { MockDriver } from "../testing/mock-driver.js";
import { node } from "../testing/fixtures.js";
import { AttrKeys, Roles } from "../tree/tree-node.js";
import { compileSelector } from "../selectors/priority-broadening.js";

/**
 * These tests exercise the cross-platform scroll-into-view
 * controller entirely in-memory. The `MockDriver`'s `onSwipe`
 * hook is how we model "state changes after a swipe" — each
 * test scripts a sequence of hierarchies the driver returns
 * across successive calls, with swipes advancing the sequence.
 */

function targetAt(y: number, id = "target") {
  return node({
    role: Roles.Cell,
    id,
    text: "Target",
    bounds: `0,${y - 20},430,${y + 20}`,
  });
}

/** Container wrapping a single target at the given y midpoint. */
function listWithTargetAt(y: number) {
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [targetAt(y)],
  });
}

/** Container with NO target — used for the "not found" case. */
function emptyList() {
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [],
  });
}

async function flush(times = 5): Promise<void> {
  for (let i = 0; i < times; i++) await Promise.resolve();
}

describe("ScrollController.ensureVisible", () => {
  it("returns immediately when element is already in the inset viewport", async () => {
    const driver = new MockDriver().stageHierarchy(listWithTargetAt(500));
    const clock = new FakeClock();
    const controller = new ScrollController({ driver, clock });
    const r = await controller.ensureVisible(compileSelector({ id: "target" }));
    assert.equal(r.node.attributes[AttrKeys.Id], "target");
    // No swipes should have been dispatched — element was visible.
    assert.equal(driver.calls.filter((c) => c.method === "swipe").length, 0);
  });

  it("scrolls once when element is below the viewport", async () => {
    const driver = new MockDriver();
    // Sequence:
    //   call 1: element at y=900 (inside raw bounds but below 50pt
    //           bottom inset → isInsideInsetViewport=false on a
    //           932pt screen: 900 > 932-50=882).
    //   swipe → element moves up to y=500 (comfortably centered).
    //   call 2: element at y=500.
    driver.stageHierarchy(listWithTargetAt(900));
    driver.stageHierarchy(listWithTargetAt(500));
    driver.onSwipe = () => {
      // no-op — state transition is modeled by the staged queue
    };
    const clock = new FakeClock();
    const controller = new ScrollController({ driver, clock });
    const promise = controller.ensureVisible(compileSelector({ id: "target" }));

    // Driver call 1 (hierarchy), swipe, sleep(500), driver call 2
    // — advance clock to drain the sleep.
    await flush();
    clock.advance(500);
    await flush();

    const r = await promise;
    assert.equal(r.node.attributes[AttrKeys.Id], "target");
    assert.equal(driver.calls.filter((c) => c.method === "swipe").length, 1);
  });

  it("phase 0 scroll-search finds a target that starts absent", async () => {
    const driver = new MockDriver();
    // Initial hierarchy: no target.
    driver.stageHierarchy(emptyList());
    // First UP swipe reveals target (at centered y=500).
    driver.stageHierarchy(listWithTargetAt(500));
    const clock = new FakeClock();
    const controller = new ScrollController({ driver, clock });
    const promise = controller.ensureVisible(compileSelector({ id: "target" }));

    // Drain first call + first swipe's settleWaitMs.
    await flush();
    clock.advance(500);
    await flush();

    const r = await promise;
    assert.equal(r.node.attributes[AttrKeys.Id], "target");
    // One UP swipe from phase 0 — no positional loop since element
    // landed centered.
    const swipes = driver.calls.filter((c) => c.method === "swipe");
    assert.equal(swipes.length, 1);
  });

  it("throws ScrollUnreachableError when budget is exhausted", async () => {
    const driver = new MockDriver();
    // Stage many empty trees — enough for all UP + DOWN budget swipes.
    driver.stageHierarchyRepeated(emptyList(), 100);
    const clock = new FakeClock();
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 2 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    // Drain all settles (4 swipes × 500ms).
    for (let i = 0; i < 6; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);
    // 2 UP + 2 DOWN = 4 swipes dispatched.
    const swipes = driver.calls.filter((c) => c.method === "swipe");
    assert.equal(swipes.length, 4);
  });

  it("progress check: throws when bounds don't change after a swipe", async () => {
    const driver = new MockDriver();
    // Element below viewport at y=900 — not inside inset rect.
    // Two identical hierarchies in a row = bounds didn't move.
    driver.stageHierarchy(listWithTargetAt(900));
    driver.stageHierarchy(listWithTargetAt(900));
    const clock = new FakeClock();
    const controller = new ScrollController({ driver, clock });
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    await flush();
    clock.advance(500);
    await flush();

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);
  });
});
