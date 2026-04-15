import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { CompactElement } from "../../adapters/device-controller.port.js";
import { FuzzyResourceMatcher } from "./fuzzy-resource-matcher.js";

function el(resourceId: string): CompactElement {
  return {
    selector: { resourceId },
    label: "",
    role: "view",
    clickable: false,
    enabled: true,
    bounds: { left: 0, top: 0, right: 100, bottom: 100 },
    isInIme: false,
  };
}

test("exact match wins", () => {
  const matcher = new FuzzyResourceMatcher();
  const elements = [el("login_btn"), el("com.app:id/login_btn")];
  const result = matcher.match("login_btn", elements);
  assert.equal(result.kind, "single");
  if (result.kind === "single") {
    assert.equal(result.element.selector?.resourceId, "login_btn");
    assert.match(result.reason, /exact/);
  }
});

test("suffix match after /", () => {
  const matcher = new FuzzyResourceMatcher();
  const elements = [el("com.app:id/login_btn"), el("other")];
  const result = matcher.match("login_btn", elements);
  assert.equal(result.kind, "single");
  if (result.kind === "single") {
    assert.match(result.reason, /suffix/);
  }
});

test("substring match as broadest tier", () => {
  const matcher = new FuzzyResourceMatcher();
  const elements = [el("wrapper_login_btn_container")];
  const result = matcher.match("login_btn", elements);
  assert.equal(result.kind, "single");
  if (result.kind === "single") {
    assert.match(result.reason, /substring/);
  }
});

test("multiple exact matches return ambiguous", () => {
  const matcher = new FuzzyResourceMatcher();
  const elements = [el("login_btn"), el("login_btn")];
  const result = matcher.match("login_btn", elements);
  assert.equal(result.kind, "ambiguous");
});

test("exact tier wins over suffix even if suffix would also match", () => {
  const matcher = new FuzzyResourceMatcher();
  const elements = [el("com.app:id/login_btn"), el("login_btn")];
  const result = matcher.match("login_btn", elements);
  assert.equal(result.kind, "single");
  if (result.kind === "single") {
    assert.equal(result.element.selector?.resourceId, "login_btn");
    assert.match(result.reason, /exact/);
  }
});

test("no match returns none", () => {
  const matcher = new FuzzyResourceMatcher();
  const result = matcher.match("nonexistent", [el("foo"), el("bar")]);
  assert.equal(result.kind, "none");
});

test("empty partial returns none", () => {
  const matcher = new FuzzyResourceMatcher();
  const result = matcher.match("", [el("foo")]);
  assert.equal(result.kind, "none");
});
