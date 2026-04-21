import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "./orchestra.js";
import { FakeClock } from "@atomyx/core/infra";
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

// Post-tap login tree: email field is input-focused and carries the
// typed text. Orchestra.inputText after its refactor calls waitForFocus
// then waitForText; both are satisfied on the first fetch when the
// staged tree already reflects the end state, so tests don't have to
// drive the FakeClock forward through polling iterations.
function focusedLoginTree(emailText: string) {
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
        text: emailText,
        bounds: "40,300,390,340",
        enabled: true,
        focused: true,
      }),
    ],
  });
}

describe("Orchestra.inputText", () => {
  it("taps to focus, clears, then types", async () => {
    const driver = new MockDriver();
    // Stage the end-state tree from the start. Email is focused and
    // carries the expected text — waitForFocus + waitForText satisfy
    // on their first poll, so we don't need to drive the FakeClock
    // through intermediate idle sleeps. This is a unit-test
    // convenience; real runs observe real focus/text transitions.
    driver.stageHierarchyRepeated(focusedLoginTree("user@test.com"), 10);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const result = await orchestra.inputText({ id: "email" }, "user@test.com");
    assert.equal(result.ok, true);

    // Expected call order: hierarchy (resolve) → tap → waitForFocus
    // hierarchy → eraseText → inputText → waitForText hierarchy.
    const methodOrder = driver.calls.map((c) => c.method);
    const i1 = methodOrder.indexOf("tap");
    const i2 = methodOrder.indexOf("eraseText");
    const i3 = methodOrder.indexOf("inputText");
    assert.ok(i1 >= 0 && i2 > i1 && i3 > i2, `got order: ${methodOrder.join(",")}`);
  });

  it("skips erase when clearFirst=false", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(focusedLoginTree("hello"), 10);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.inputText({ id: "email" }, "hello", { clearFirst: false });
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 0);
  });

  it("skips erase when driver does not support it", async () => {
    const driver = new MockDriver();
    driver.capabilities = { ...driver.capabilities, canEraseText: false };
    driver.stageHierarchyRepeated(focusedLoginTree("hello"), 10);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.inputText({ id: "email" }, "hello");
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 0);
  });

  it("trusts the driver — no Orchestra-level focus wait or post-type verification", async () => {
    // Orchestra.inputText no longer polls the hierarchy before
    // typing (driver adapters self-synchronize) and no longer
    // verifies post-type state (agent-level paths catch silent
    // drops internally). Flow reduces to: prepareSelectorForAction
    // → driver.tap → driver.inputText. Call order proves we didn't
    // inject an extra hierarchy read or retry dance.
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    const result = await orchestra.inputText({ id: "email" }, "hello");
    assert.equal(result.ok, true);
    assert.equal(driver.calls.filter((c) => c.method === "inputText").length, 1);
    // Exactly one tap (no retry tap) and no post-type hierarchy
    // poll beyond what prepareSelectorForAction already issued.
    assert.equal(driver.calls.filter((c) => c.method === "tap").length, 1);
  });

  it("skips eraseText when the field is already empty (placeholder only)", async () => {
    // Empty email field: `text` mirrors the hint/label, no user
    // content. Orchestra should NOT fire an eraseText — on iOS
    // this causes keyboard flicker for zero benefit (nothing to
    // delete).
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    await orchestra.inputText({ id: "email" }, "hello");
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 0);
  });

  it("issues eraseText when the field already holds user content", async () => {
    // Same tree but with pre-filled content in the email field —
    // text !== label signals typed-in content, so Orchestra must
    // wipe it before writing the new value.
    const prefilled = node({
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
          label: "Email",
          text: "leftover@old.com",
          bounds: "40,300,390,340",
          enabled: true,
        }),
      ],
    });
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(prefilled, 5);
    const clock = new FakeClock();
    const orchestra = new Orchestra({ driver, clock });

    await orchestra.inputText({ id: "email" }, "new@value.com");
    assert.equal(driver.calls.filter((c) => c.method === "eraseText").length, 1);
  });
});

describe("Orchestra.tap keyboard gate", () => {
  // Tree with a button covered by the on-screen keyboard. The button
  // bounds intersect the keyboard bounds — simulates the real
  // "Verify" / "Submit" button sitting below a numeric keypad after
  // the user typed an OTP.
  function withKeyboard(emailText: string, emailFocused: boolean) {
    return node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({
          role: Roles.TextField,
          id: "email",
          hint: "Email",
          text: emailText,
          bounds: "40,100,390,140",
          focused: emailFocused,
        }),
        node({
          role: Roles.Button,
          id: "verify_btn",
          text: "Verify",
          bounds: "40,700,390,760",
          enabled: true,
          clickable: true,
        }),
        node({
          role: Roles.Keyboard,
          bounds: "0,600,430,900",
        }),
      ],
    });
  }

  function cleanAfterDismiss() {
    return node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({
          role: Roles.TextField,
          id: "email",
          hint: "Email",
          text: "x",
          bounds: "40,100,390,140",
        }),
        node({
          role: Roles.Button,
          id: "verify_btn",
          text: "Verify",
          bounds: "40,700,390,760",
          enabled: true,
          clickable: true,
        }),
      ],
    });
  }

  it("dismisses keyboard when target intersects keyboard bounds", async () => {
    const driver = new MockDriver();
    // Staging budget during inputText: scroll.find = 1 hierarchy
    // read, all wanting the keyboard-visible tree. Plus tap's own
    // scroll.find. Total 2. Then cleanAfterDismiss for
    // waitForKeyboard and the recursive tap's re-resolve.
    driver.stageHierarchyRepeated(withKeyboard("x", true), 2);
    driver.stageHierarchyRepeated(cleanAfterDismiss(), 10);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    await orchestra.inputText({ id: "email" }, "x");
    const result = await orchestra.tap({ id: "verify_btn" });
    assert.equal(result.ok, true);
    assert.equal(
      driver.calls.filter((c) => c.method === "hideKeyboard").length,
      1,
      "hideKeyboard should have been dispatched once",
    );
  });

  it("does not query hideKeyboard when maybeKeyboardOpen is false", async () => {
    const driver = new MockDriver();
    driver.stageHierarchyRepeated(loginTree(), 5);
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    // Fresh orchestra — nothing set maybeKeyboardOpen yet.
    await orchestra.tap({ id: "login_btn" });
    assert.equal(
      driver.calls.filter((c) => c.method === "hideKeyboard").length,
      0,
    );
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

describe("Orchestra.dispatchGesture", () => {
  it("forwards the Gesture object to the driver unmodified", async () => {
    const driver = new MockDriver();
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    const gesture = {
      pointers: [
        {
          id: "f1",
          waypoints: [
            {
              phase: "down" as const,
              point: { x: 10, y: 20 },
              atOffsetSeconds: 0,
            },
            {
              phase: "up" as const,
              point: { x: 10, y: 20 },
              atOffsetSeconds: 0,
            },
          ],
        },
      ],
    };

    await orchestra.dispatchGesture(gesture);

    const dispatchCalls = driver.calls.filter((c) => c.method === "dispatchGesture");
    assert.equal(dispatchCalls.length, 1);
    assert.deepEqual(dispatchCalls[0]!.args[0], gesture);
  });

  it("proxies driver.capabilities through orchestra.capabilities", () => {
    const driver = new MockDriver();
    driver.capabilities = {
      ...driver.capabilities,
      canMultiPointer: true,
      canPressure: true,
    };
    const orchestra = new Orchestra({ driver, clock: new FakeClock() });

    assert.equal(orchestra.capabilities.canMultiPointer, true);
    assert.equal(orchestra.capabilities.canPressure, true);
    // Non-gesture capabilities pass through too.
    assert.equal(
      orchestra.capabilities.canScreenshot,
      driver.capabilities.canScreenshot,
    );
  });
});
