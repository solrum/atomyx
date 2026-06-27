import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { ScrollController, ScrollUnreachableError } from "./scroll-controller.js";
import { FakeClock } from "@atomyx/core/infra";
import { MockDriver } from "../testing/mock-driver.js";
import { node } from "../testing/fixtures.js";
import { AttrKeys, Roles } from "../tree/tree-node.js";
import { compileSelector } from "../selectors/priority-broadening.js";
import { Orchestra } from "../orchestra/orchestra.js";

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

/**
 * A multi-item list with enough leaves that the thin-tree
 * pre-check does NOT reduce the swipe budget.
 */
function fatList(count = 8) {
  const children = Array.from({ length: count }, (_, i) =>
    node({
      role: Roles.Cell,
      id: `item-${i}`,
      text: `Item ${i}`,
      bounds: `0,${i * 100},430,${(i + 1) * 100}`,
    }),
  );
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children,
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
    // Initial hierarchy: no target (consumed by ensureVisible's initial find).
    driver.stageHierarchy(emptyList());
    // scrollSearch pre-loop hierarchy call returns this; first UP
    // swipe returns sticky version of same tree → target found.
    driver.stageHierarchy(listWithTargetAt(500));
    const clock = new FakeClock();
    const controller = new ScrollController({ driver, clock });
    const promise = controller.ensureVisible(compileSelector({ id: "target" }));

    // Drain initial find + pre-loop hierarchy + first swipe's settleWaitMs.
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
    // Stage many empty trees — enough for all UP + DOWN budget swipes
    // plus the pre-loop hierarchy call in scrollSearch.
    driver.stageHierarchyRepeated(emptyList(), 100);
    const clock = new FakeClock();
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 2 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    // Drain all settles. 6 iterations covers at least 2 swipes × 500ms.
    for (let i = 0; i < 6; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);
    // Saturation detection fires on the first swipe in each direction
    // (identical empty tree → same hash). Total: 1 UP + 1 DOWN = 2 swipes.
    const swipes = driver.calls.filter((c) => c.method === "swipe");
    assert.equal(swipes.length, 2);
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

describe("ScrollController.scrollSearch — saturation detection", () => {
  it("saturation breaks UP direction early when tree is unchanged", async () => {
    const driver = new MockDriver();
    // emptyList staged many times; UP swipes return same tree → saturation on i=0.
    // DOWN swipes run their full budget (no saturation scenario staged below).
    // Here we verify UP breaks early: only 1 UP swipe, then full budget DOWN.
    driver.stageHierarchyRepeated(emptyList(), 2); // initial + pre-loop
    // DOWN loop: stage distinct alternating trees so DOWN doesn't saturate.
    // Since we just need to exhaust budget without finding element, stage
    // a changing tree (different each call) for the DOWN phase.
    const treeA = node({ role: Roles.Container, bounds: "0,0,430,932", children: [
      node({ role: Roles.Cell, id: "a", text: "A", bounds: "0,100,430,140" }),
    ]});
    const treeB = node({ role: Roles.Container, bounds: "0,0,430,932", children: [
      node({ role: Roles.Cell, id: "b", text: "B", bounds: "0,200,430,240" }),
    ]});
    // Stage alternating trees for each DOWN swipe (budget=2 → 2 swipes DOWN).
    driver.stageHierarchy(treeA); // DOWN swipe 1
    driver.stageHierarchy(treeB); // DOWN swipe 2

    const clock = new FakeClock();
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 2 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    for (let i = 0; i < 8; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);

    const swipes = driver.calls.filter((c) => c.method === "swipe");
    // 1 UP (saturated) + 2 DOWN (full budget, trees differ) = 3 total.
    assert.equal(swipes.length, 3);
  });

  it("both directions saturate — total swipes = 2", async () => {
    const driver = new MockDriver();
    // Every hierarchy call returns the same empty tree → saturation on
    // the very first swipe in each direction.
    driver.stageHierarchyRepeated(emptyList(), 50);
    const clock = new FakeClock();
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 6 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    for (let i = 0; i < 4; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);

    const swipes = driver.calls.filter((c) => c.method === "swipe");
    assert.equal(swipes.length, 2);
  });

  it("full scroll when tree changes every swipe and element never found", async () => {
    // Stage enough distinct trees for initial find + pre-loop + 12 loop iterations
    // (budget=6 per direction, 2 directions, each needs a changing tree).
    // Trees differ on every call → no saturation → full budget consumed.
    const driver = new MockDriver();
    // initial find + pre-loop = 2 hierarchy calls before any swipe
    driver.stageHierarchy(emptyList());
    driver.stageHierarchy(emptyList());
    // 6 UP + 6 DOWN = 12 swipe iterations; each needs a distinct tree.
    for (let i = 0; i < 12; i++) {
      driver.stageHierarchy(node({
        role: Roles.Container,
        bounds: "0,0,430,932",
        children: [
          // Different text per iteration → different hash.
          node({ role: Roles.Cell, id: `x${i}`, text: `x${i}`, bounds: `0,${i * 20},430,${i * 20 + 20}` }),
        ],
      }));
    }

    const clock = new FakeClock();
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 6 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    for (let i = 0; i < 15; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);

    const swipes = driver.calls.filter((c) => c.method === "swipe");
    // No saturation → full 6 UP + 6 DOWN = 12 swipes.
    assert.equal(swipes.length, 12);
  });

  it("viewport pre-check caps budget for thin trees", async () => {
    // Thin tree: 1 leaf sitting at y=200 on a 932pt screen.
    // leafCount=1 < 5 AND maxBottom=240 < 932*0.9=838.8 → effectiveBudget=2.
    const thinTree = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({ role: Roles.Cell, id: "item", text: "Item", bounds: "0,200,430,240" }),
      ],
    });

    const driver = new MockDriver();
    // initial find + pre-loop = 2 hierarchy calls, then 2 swipes per direction.
    driver.stageHierarchyRepeated(thinTree, 50);

    const clock = new FakeClock();
    // Configured budget = 6, but thin tree pre-check caps at 2.
    const controller = new ScrollController(
      { driver, clock },
      { scrollSearchBudget: 6 },
    );
    const promise = controller
      .ensureVisible(compileSelector({ id: "target" }))
      .catch((e) => e);

    for (let i = 0; i < 8; i++) {
      await flush();
      clock.advance(500);
    }

    const result = await promise;
    assert.ok(result instanceof ScrollUnreachableError);

    const swipes = driver.calls.filter((c) => c.method === "swipe");
    // thinTree doesn't change → saturation on first swipe of each direction.
    // effectiveBudget=2 but saturation fires at i=0 → 1 UP + 1 DOWN = 2 total.
    assert.equal(swipes.length, 2);
  });
});

describe("Orchestra.tap – scrollToFind option", () => {
  it("scrollToFind: false → swipe spy never called", async () => {
    const driver = new MockDriver();
    // Single tree with target visible in the center.
    driver.stageHierarchyRepeated(
      node({
        role: Roles.Container,
        bounds: "0,0,430,932",
        children: [
          node({
            role: Roles.Button,
            id: "btn",
            text: "Click me",
            bounds: "100,450,330,510",
            enabled: true,
            clickable: true,
          }),
        ],
      }),
      10,
    );
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    const result = await orchestra.tap({ id: "btn" }, { scrollToFind: false });

    assert.equal(result.ok, true);
    // scrollToFind=false → ScrollController.ensureVisible was skipped →
    // no swipes dispatched.
    assert.equal(driver.calls.filter((c) => c.method === "swipe").length, 0);
  });
});
