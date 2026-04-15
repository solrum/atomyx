import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { CompactElement } from "../../adapters/device-controller.port.js";
import { AmbiguityDetector } from "./ambiguity-detector.js";

function make(sel: CompactElement["selector"], role = "view"): CompactElement {
  return {
    selector: sel,
    label: "",
    role,
    clickable: false,
    enabled: true,
    bounds: { left: 0, top: 0, right: 100, bottom: 100 },
    isInIme: false,
  };
}

test("tokenOf prefers resourceId over contentDesc", () => {
  const d = new AmbiguityDetector();
  const e = make({ resourceId: "foo", contentDesc: "bar", text: "baz" });
  assert.equal(d.tokenOf(e), "r:foo");
});

test("tokenOf falls to contentDesc when no resourceId", () => {
  const d = new AmbiguityDetector();
  assert.equal(d.tokenOf(make({ contentDesc: "bar", text: "baz" })), "c:bar");
});

test("tokenOf falls to text when no resourceId or contentDesc", () => {
  const d = new AmbiguityDetector();
  assert.equal(d.tokenOf(make({ text: "baz" })), "t:baz");
});

test("tokenOf uses role when no stable selector", () => {
  const d = new AmbiguityDetector();
  assert.equal(d.tokenOf(make({}, "button")), "role:button");
});

test("computeDuplicateCounts counts duplicates correctly", () => {
  const d = new AmbiguityDetector();
  const counts = d.computeDuplicateCounts([
    make({ contentDesc: "注文" }),
    make({ contentDesc: "注文" }),
    make({ resourceId: "login_btn" }),
  ]);
  assert.equal(counts.get("c:注文"), 2);
  assert.equal(counts.get("r:login_btn"), 1);
});

test("elements with different selector types do not collide", () => {
  const d = new AmbiguityDetector();
  const counts = d.computeDuplicateCounts([
    make({ resourceId: "foo" }),
    make({ contentDesc: "foo" }),
    make({ text: "foo" }),
  ]);
  assert.equal(counts.get("r:foo"), 1);
  assert.equal(counts.get("c:foo"), 1);
  assert.equal(counts.get("t:foo"), 1);
});
