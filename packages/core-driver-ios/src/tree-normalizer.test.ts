import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { iosElementTypeToRole, normalizeIosTree, type IosRawElement } from "./tree-normalizer.js";

describe("iosElementTypeToRole", () => {
  it("direct maps for interactive types", () => {
    assert.equal(iosElementTypeToRole("button"), "button");
    assert.equal(iosElementTypeToRole("textField"), "text-field");
    assert.equal(iosElementTypeToRole("secureTextField"), "secure-text-field");
    assert.equal(iosElementTypeToRole("searchField"), "search-field");
    assert.equal(iosElementTypeToRole("switch"), "switch");
    assert.equal(iosElementTypeToRole("slider"), "slider");
    assert.equal(iosElementTypeToRole("link"), "link");
    assert.equal(iosElementTypeToRole("cell"), "cell");
  });

  it("maps display text types", () => {
    assert.equal(iosElementTypeToRole("staticText"), "text");
    assert.equal(iosElementTypeToRole("heading"), "heading");
  });

  it("consolidates container types", () => {
    assert.equal(iosElementTypeToRole("table"), "container");
    assert.equal(iosElementTypeToRole("collectionView"), "container");
    assert.equal(iosElementTypeToRole("scrollView"), "container");
    assert.equal(iosElementTypeToRole("group"), "container");
    assert.equal(iosElementTypeToRole("window"), "container");
    assert.equal(iosElementTypeToRole("stackView"), "container");
  });

  it("maps navigation chrome to menu", () => {
    assert.equal(iosElementTypeToRole("navigationBar"), "menu");
    assert.equal(iosElementTypeToRole("tabBar"), "menu");
    assert.equal(iosElementTypeToRole("toolbar"), "menu");
  });

  it("maps dialog + alert distinctly", () => {
    assert.equal(iosElementTypeToRole("alert"), "alert");
    assert.equal(iosElementTypeToRole("dialog"), "dialog");
    assert.equal(iosElementTypeToRole("sheet"), "dialog");
  });

  it("keyboard + key", () => {
    assert.equal(iosElementTypeToRole("keyboard"), "keyboard");
    assert.equal(iosElementTypeToRole("key"), "key");
  });

  it("unknown types fall through to 'other'", () => {
    assert.equal(iosElementTypeToRole("webView"), "other");
    assert.equal(iosElementTypeToRole("unexpectedType"), "other");
    assert.equal(iosElementTypeToRole("other"), "other");
  });
});

describe("normalizeIosTree", () => {
  it("maps canonical attribute keys from legacy fields", () => {
    const raw: IosRawElement = {
      elementType: "button",
      identifier: "login_btn",
      label: "Sign in",
      bounds: { left: 100, top: 200, right: 300, bottom: 260 },
      enabled: true,
    };
    const wire = normalizeIosTree(raw);
    assert.equal(wire.attributes["id"], "login_btn");
    assert.equal(wire.attributes["label"], "Sign in");
    assert.equal(wire.attributes["text"], "Sign in"); // mirrored from label
    assert.equal(wire.attributes["class"], "button");
    assert.equal(wire.attributes["role"], "button");
    assert.equal(wire.attributes["bounds"], "100,200,300,260");
    assert.equal(wire.enabled, true);
    assert.equal(wire.clickable, true); // button ∈ INTERACTIVE_ROLES
  });

  it("prefers value over label for the text attribute on text fields", () => {
    const raw: IosRawElement = {
      elementType: "textField",
      identifier: "email",
      label: "Email address",
      value: "user@test.com",
      bounds: { left: 0, top: 0, right: 400, bottom: 60 },
      enabled: true,
    };
    const wire = normalizeIosTree(raw);
    // label attribute unchanged
    assert.equal(wire.attributes["label"], "Email address");
    // value attribute present
    assert.equal(wire.attributes["value"], "user@test.com");
    // text reflects the current value (what an agent would see as content)
    assert.equal(wire.attributes["text"], "user@test.com");
  });

  it("drops empty identifier/label/value", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      identifier: "",
      label: "",
      value: "",
    });
    assert.equal(wire.attributes["id"], undefined);
    assert.equal(wire.attributes["label"], undefined);
    assert.equal(wire.attributes["value"], undefined);
    assert.equal(wire.attributes["text"], undefined);
    // class + role still set.
    assert.equal(wire.attributes["class"], "staticText");
    assert.equal(wire.attributes["role"], "text");
  });

  it("derives clickable from interactive role whitelist", () => {
    const button = normalizeIosTree({ elementType: "button" });
    const staticText = normalizeIosTree({ elementType: "staticText" });
    const container = normalizeIosTree({ elementType: "group" });
    assert.equal(button.clickable, true);
    assert.equal(staticText.clickable, false);
    assert.equal(container.clickable, false);
  });

  it("recursively normalizes children", () => {
    const raw: IosRawElement = {
      elementType: "window",
      bounds: { left: 0, top: 0, right: 430, bottom: 932 },
      children: [
        {
          elementType: "button",
          identifier: "btn1",
          label: "Save",
          bounds: { left: 10, top: 10, right: 100, bottom: 50 },
        },
        {
          elementType: "textField",
          identifier: "input1",
          label: "Email",
          value: "a@b.com",
          bounds: { left: 10, top: 60, right: 400, bottom: 100 },
        },
      ],
    };
    const wire = normalizeIosTree(raw);
    assert.equal(wire.children.length, 2);
    assert.equal(wire.children[0]!.attributes["role"], "button");
    assert.equal(wire.children[0]!.attributes["text"], "Save");
    assert.equal(wire.children[1]!.attributes["role"], "text-field");
    assert.equal(wire.children[1]!.attributes["text"], "a@b.com");
    assert.equal(wire.attributes["role"], "container");
  });

  it("preserves document order of children (matters for z-order)", () => {
    const raw: IosRawElement = {
      elementType: "window",
      children: [
        { elementType: "button", identifier: "a" },
        { elementType: "button", identifier: "b" },
        { elementType: "button", identifier: "c" },
      ],
    };
    const wire = normalizeIosTree(raw);
    assert.deepEqual(
      wire.children.map((c) => c.attributes["id"]),
      ["a", "b", "c"],
    );
  });

  it("handles missing bounds without crashing", () => {
    const wire = normalizeIosTree({ elementType: "button" });
    assert.equal(wire.attributes["bounds"], undefined);
  });
});
