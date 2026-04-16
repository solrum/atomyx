import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "./orchestra.js";
import { FakeClock } from "../infra/clock.port.js";
import { MockDriver } from "../testing/mock-driver.js";
import { node, modalObscuredTree } from "../testing/fixtures.js";
import { AttrKeys, Roles } from "../tree/tree-node.js";

/**
 * End-to-end Orchestra tests. The MockDriver lets us assert:
 *   - The right coordinate was tapped (from scripted hierarchies).
 *   - ActionResult carries the expected ok/fail + reason shape.
 *   - Selector pipeline wires ScrollController + obscurement +
 *     priority broadening into a single tap flow.
 *
 * Async fast-forward helper: Orchestra internally runs polling
 * loops (via Finder + ScrollController) that await clock.sleep().
 * Tests that need multiple iterations drive the loop with
 * `tick(clock, ms)` which flushes microtasks between advances.
 */

async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

async function tick(clock: FakeClock, ms: number): Promise<void> {
  await flushMicrotasks();
  clock.advance(ms);
  await flushMicrotasks();
}

function loginTree() {
  return node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [
      node({
        role: Roles.Button,
        id: "login_btn",
        text: "Sign in",
        label: "Login button",
        bounds: "100,400,330,460",
        enabled: true,
        clickable: true,
      }),
      node({
        role: Roles.TextField,
        id: "email",
        hint: "Email address",
        bounds: "40,300,390,340",
        enabled: true,
      }),
    ],
  });
}

describe("Orchestra.tap with selector", () => {
  it("taps the element center when visible and unobscured", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    const result = await orchestra.tap({ id: "login_btn" });

    assert.equal(result.ok, true);
    if (result.ok) assert.equal(result.resolvedBy, "id");

    const taps = driver.calls.filter((c) => c.method === "tap");
    assert.equal(taps.length, 1);
    // Element bounds 100,400,330,460 → center (215, 430)
    const point = taps[0]!.args[0] as { x: number; y: number };
    assert.equal(point.x, 215);
    assert.equal(point.y, 430);
  });

  it("reports the priority slot that actually matched", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const r = await orchestra.tap({ label: "Login button" });
    assert.equal(r.ok, true);
    if (r.ok) assert.equal(r.resolvedBy, "label");
  });

  it("obscurement ancestor check works across fresh hierarchy instances", async () => {
    // Regression: Orchestra.prepareSelectorForAction used to call
    // driver.hierarchy() twice — once via scroll.ensureVisible and
    // once explicitly for the obscurement check — then compared
    // TreeNode references between the two results. On real
    // drivers that rebuild the TreeNode graph from JSON per call,
    // the two trees are referentially distinct even when
    // structurally identical, so detectObscurement's `topmost ===
    // target` + `containsNode(reference walk)` both silently
    // returned false. The algorithm then fell through to generic-
    // container suppression, which does NOT suppress nodes with
    // distinctive role or non-empty id — so every tap on an
    // ordinary element that happened to be its own pre-order
    // topmost (i.e. any interior leaf) reported "obscured by
    // [itself]".
    //
    // The bug stayed hidden because MockDriver.stageHierarchyRepeated
    // pushes the SAME TreeNode instance N times, which
    // accidentally satisfies the reference equality. Stage two
    // DEEP COPIES here so each hierarchy() call returns a fresh
    // object graph — that's what real IosDriver / AndroidDriver
    // produce.
    const tree1 = loginTree();
    const tree2 = JSON.parse(JSON.stringify(tree1)) as ReturnType<typeof loginTree>;
    const driver = new MockDriver();
    driver.stageHierarchy(tree1);
    driver.stageHierarchy(tree2);
    driver.stageHierarchyRepeated(tree2, 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const result = await orchestra.tap({ id: "login_btn" });
    assert.equal(
      result.ok,
      true,
      result.ok
        ? ""
        : `tap should succeed on unobscured element but got: ${result.reason}`,
    );
  });

  it("returns ok:false with obscurer info when element is covered by a modal", async () => {
    const { root, target } = modalObscuredTree();
    const treeWithEnabled = {
      ...root,
      children: root.children.map((c) =>
        c === target ? { ...c, enabled: true, clickable: true } : c,
      ),
    };
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(treeWithEnabled, 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const result = await orchestra.tap({ id: "target" });

    assert.equal(result.ok, false);
    if (!result.ok) {
      assert.ok(result.reason.includes("visually obscured"));
      assert.equal(result.obscurer?.id, "confirm-sheet");
    }
    // No tap should have been dispatched — safety check blocked it.
    assert.equal(driver.calls.filter((c) => c.method === "tap").length, 0);
  });

  it("returns ok:false with actionable message when selector never resolves", async () => {
    const driver = new MockDriver();
    // Stage enough empty trees for the scroll-search budget
    // (6 up + 6 down swipes = 13 hierarchy polls).
    driver.stageHierarchyRepeated(
      node({ role: Roles.Container, bounds: "0,0,430,932" }),
      30,
    );
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    const promise = orchestra.tap({ id: "nope" });
    // Drive the scroll-search loop forward. Each swipe has a
    // 500ms settle wait.
    for (let i = 0; i < 14; i++) {
      await tick(clock, 500);
    }
    const result = await promise;

    assert.equal(result.ok, false);
    if (!result.ok) assert.ok(result.reason.includes("scroll"));
  });
});

describe("Orchestra.inputText", () => {
  it("taps to focus, clears, then types", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const result = await orchestra.inputText({ id: "email" }, "user@test.com");
    assert.equal(result.ok, true);

    // Expected call order: hierarchy (resolve) → hierarchy
    // (obscurement check) → tap → eraseText → inputText.
    const methodOrder = driver.calls.map((c) => c.method);
    const i1 = methodOrder.indexOf("tap");
    const i2 = methodOrder.indexOf("eraseText");
    const i3 = methodOrder.indexOf("inputText");
    assert.ok(i1 >= 0 && i2 > i1 && i3 > i2, `got order: ${methodOrder.join(",")}`);
  });

  it("skips erase when clearFirst=false", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.inputText({ id: "email" }, "hello", { clearFirst: false });
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 0);
  });

  it("skips erase when driver does not support it", async () => {
    const driver = new MockDriver();
    driver.capabilities = { ...driver.capabilities, canEraseText: false };
    driver.stageHierarchyRepeated(loginTree(), 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.inputText({ id: "email" }, "hello");
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 0);
  });
});

describe("Orchestra.swipeDirection", () => {
  it("computes from/to points for each direction", async () => {
    const driver = new MockDriver();
    driver.screen = { width: 400, height: 800 };
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.swipeDirection("up");
    await orchestra.swipeDirection("down");
    await orchestra.swipeDirection("left");
    await orchestra.swipeDirection("right");

    const swipes = driver.calls.filter((c) => c.method === "swipe");
    assert.equal(swipes.length, 4);

    // up: finger moves from lower-than-center to higher-than-center
    const up = swipes[0]!.args as [{ x: number; y: number }, { x: number; y: number }, number];
    assert.equal(up[0].x, 200);
    assert.equal(up[1].x, 200);
    assert.ok(up[0].y > up[1].y, "up swipe finger goes up");
  });
});

describe("Orchestra.waitForIdle", () => {
  it("delegates to driver when native waitForIdle supported", async () => {
    const driver = new MockDriver();
    driver.capabilities = { ...driver.capabilities, canWaitForIdle: true };
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });
    const result = await orchestra.waitForIdle(1000);
    assert.equal(result, true);
    assert.equal(driver.calls.filter((c) => c.method === "waitForIdle").length, 1);
  });

  it("falls back to host-side tree-diff polling", async () => {
    const driver = new MockDriver();
    // canWaitForIdle is false by default in MockDriver.
    // Stage two identical trees → host detects idle on first poll.
    driver.stageHierarchyRepeated(
      node({ role: Roles.Container, bounds: "0,0,430,932" }),
      5,
    );
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    const promise = orchestra.waitForIdle(1000);
    await tick(clock, 200);
    const result = await promise;
    assert.equal(result, true);
  });
});

describe("Orchestra.pressKey", () => {
  it("wraps driver KeyResult into ActionResult", async () => {
    const driver = new MockDriver();
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });
    const r = await orchestra.pressKey("back");
    assert.equal(r.ok, true);
    assert.equal(driver.calls.filter((c) => c.method === "pressKey").length, 1);
  });
});

describe("Orchestra coordinate primitives", () => {
  it("tapAt bypasses selector pipeline", async () => {
    const driver = new MockDriver();
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });
    await orchestra.tapAt({ x: 100, y: 200 });

    // Should only have a single tap call — no hierarchy fetch, no
    // scroll, no obscurement.
    const methods = driver.calls.map((c) => c.method);
    assert.deepEqual(methods, ["tap"]);
    assert.deepEqual((driver.calls[0]!.args[0] as object), { x: 100, y: 200 });
  });
});

describe("Orchestra.find / findOne", () => {
  it("find returns all matches", async () => {
    const driver = new MockDriver().stageHierarchy(loginTree());
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });
    const r = await orchestra.find({ role: Roles.Button });
    assert.equal(r.length, 1);
  });

  it("findOne returns first match or null", async () => {
    const driver = new MockDriver()
      .stageHierarchy(loginTree())
      .stageHierarchy(loginTree());
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const hit = await orchestra.findOne({ id: "email" });
    assert.ok(hit);
    assert.equal(hit!.node.attributes[AttrKeys.Id], "email");

    const miss = await orchestra.findOne({ id: "nope" });
    assert.equal(miss, null);
  });
});
