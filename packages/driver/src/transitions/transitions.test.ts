import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  detectLoading,
  diffAppeared,
  detectMotion,
  classifyFailure,
  type CompactElement,
} from "./index.js";
import { treeNodeToCompactElements } from "./compact-element.js";
import { node } from "../testing/fixtures.js";
import { Roles } from "../tree/tree-node.js";

function el(opts: Partial<CompactElement> & { elementId?: string }): CompactElement {
  return {
    elementId: opts.elementId ?? "e",
    role: opts.role ?? "other",
    label: opts.label ?? "",
    text: opts.text ?? "",
    resourceId: opts.resourceId,
    bounds: opts.bounds ?? { left: 0, top: 0, right: 100, bottom: 100 },
    clickable: opts.clickable ?? false,
    enabled: opts.enabled ?? true,
    selector: opts.selector,
  };
}

describe("detectLoading", () => {
  it("finds structural loading roles", () => {
    const els = [
      el({ role: "ProgressBar", label: "" }),
      el({ role: "button", label: "Login" }),
    ];
    const r = detectLoading(els);
    assert.equal(r.detected, true);
    assert.equal(r.structural, true);
  });

  it("ignores non-loading roles without extra keywords", () => {
    const els = [el({ role: "button", label: "Loading..." })];
    const r = detectLoading(els);
    assert.equal(r.detected, false);
  });

  it("matches extra keyword when provided", () => {
    const els = [el({ role: "text", label: "Loading..." })];
    const r = detectLoading(els, ["loading"]);
    assert.equal(r.detected, true);
  });
});

describe("detectMotion", () => {
  it("reports movement when bounds shift between samples", () => {
    const before = [
      el({
        elementId: "a",
        selector: { resourceId: "x" },
        bounds: { left: 0, top: 0, right: 100, bottom: 100 },
      }),
    ];
    const after = [
      el({
        elementId: "a",
        selector: { resourceId: "x" },
        bounds: { left: 20, top: 20, right: 120, bottom: 120 },
      }),
    ];
    const r = detectMotion(before, after);
    assert.equal(r.detected, true);
    assert.equal(r.movedElementCount, 1);
  });

  it("ignores sub-threshold motion", () => {
    const before = [el({ selector: { resourceId: "x" } })];
    const after = [
      el({
        selector: { resourceId: "x" },
        bounds: { left: 1, top: 1, right: 101, bottom: 101 },
      }),
    ];
    const r = detectMotion(before, after, 10);
    assert.equal(r.detected, false);
  });
});

describe("diffAppeared", () => {
  it("finds new elements not present before", () => {
    const before = [el({ selector: { resourceId: "a" }, label: "A" })];
    const after = [
      el({ selector: { resourceId: "a" }, label: "A" }),
      el({ selector: { resourceId: "b" }, label: "Error occurred", role: "dialog" }),
    ];
    const r = diffAppeared(before, after);
    assert.equal(r.length, 1);
    assert.equal(r[0]!.label, "Error occurred");
    assert.equal(r[0]!.looksLikeDialog, true);
  });
});

describe("classifyFailure", () => {
  it("classifies loading scrim appearing", () => {
    const before = [el({ selector: { resourceId: "btn" }, label: "Login" })];
    const after = [
      el({ selector: { resourceId: "btn" }, label: "Login" }),
      el({ role: "ProgressBar", label: "" }),
    ];
    const r = classifyFailure(before, after, false, false);
    assert.equal(r.classification, "still_loading");
    assert.match(r.hint, /loading indicator/i);
  });

  it("classifies as no_change when nothing appeared", () => {
    const before = [el({ selector: { resourceId: "btn" }, label: "Login" })];
    const after = [el({ selector: { resourceId: "btn" }, label: "Login" })];
    const r = classifyFailure(before, after, false, false);
    assert.equal(r.classification, "no_change_detected");
  });

  it("classifies partial_transition when anchor absent but new not appeared", () => {
    const before = [el({ selector: { resourceId: "old" }, label: "Old" })];
    const after = [el({ selector: { resourceId: "other" }, label: "Other" })];
    const r = classifyFailure(before, after, true, false);
    // before had "old", now has "other" → "Other" is appeared; unless dialog-like,
    // we fall to partial_transition when absentOk && !appearOk
    assert.equal(r.classification, "partial_transition");
  });
});

describe("treeNodeToCompactElements", () => {
  it("flattens a tree into compact elements", () => {
    const tree = node({
      role: Roles.Container,
      bounds: "0,0,430,932",
      children: [
        node({
          role: Roles.Button,
          id: "login",
          text: "Sign in",
          label: "Sign in",
          bounds: "100,400,330,460",
          enabled: true,
          clickable: true,
        }),
      ],
    });
    const compact = treeNodeToCompactElements(tree);
    // root + button = 2
    assert.equal(compact.length, 2);
    const btn = compact.find((e) => e.resourceId === "login");
    assert.ok(btn);
    assert.equal(btn!.clickable, true);
    assert.equal(btn!.role, Roles.Button);
  });

  it("drops nodes with no bounds", () => {
    const tree = node({
      role: Roles.Container,
      children: [node({ role: Roles.Text, text: "no bounds" })],
    });
    const compact = treeNodeToCompactElements(tree);
    assert.equal(compact.length, 0);
  });
});
