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
    // Non-text elementType keeps the a11y label in `label`; `text`
    // stays empty so consumers can tell a button apart from a text
    // leaf at attribute level.
    assert.equal(wire.attributes["label"], "Sign in");
    assert.equal(wire.attributes["text"], undefined);
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
    // Button keeps a11y label in `label`, no `text` mirror.
    assert.equal(wire.children[0]!.attributes["label"], "Save");
    assert.equal(wire.children[0]!.attributes["text"], undefined);
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

  it("plumbs focused state when set", () => {
    const wire = normalizeIosTree({
      elementType: "textField",
      identifier: "email",
      focused: true,
    });
    assert.equal(wire.focused, true);
  });

  it("leaves focused undefined when raw field is missing", () => {
    const wire = normalizeIosTree({ elementType: "staticText" });
    assert.equal(wire.focused, undefined);
  });

  it("plumbs selected through to node-level boolean", () => {
    const wire = normalizeIosTree({
      elementType: "button",
      label: "Active tab",
      selected: true,
    });
    assert.equal(wire.selected, true);
  });

  it("surfaces title and a11y traits via ext:* attribute keys", () => {
    const wire = normalizeIosTree({
      elementType: "button",
      label: "Save",
      title: "Save document",
      traits: ["button", "selected"],
    });
    assert.equal(wire.attributes["ext:ios-title"], "Save document");
    assert.equal(wire.attributes["ext:ios-traits"], "button,selected");
  });

  it("plumbs visible through to node-level boolean", () => {
    const onScreen = normalizeIosTree({
      elementType: "button",
      label: "Save",
      visible: true,
    });
    assert.equal(onScreen.visible, true);
    const offScreen = normalizeIosTree({
      elementType: "cell",
      label: "Row 50",
      visible: false,
    });
    assert.equal(offScreen.visible, false);
  });

  it("leaves visible undefined when raw field is missing", () => {
    const wire = normalizeIosTree({ elementType: "staticText" });
    assert.equal(wire.visible, undefined);
  });

  it("surfaces ext:ios-accessible flag verbatim", () => {
    const leaf = normalizeIosTree({
      elementType: "staticText",
      label: "Hello",
      accessible: true,
    });
    assert.equal(leaf.attributes["ext:ios-accessible"], "true");
    const container = normalizeIosTree({
      elementType: "other",
      accessible: false,
    });
    assert.equal(container.attributes["ext:ios-accessible"], "false");
  });

  it("emits ext:ios-a11y-bounds only when distinct from layout bounds", () => {
    const same = normalizeIosTree({
      elementType: "button",
      bounds: { left: 0, top: 0, right: 100, bottom: 50 },
      accessibilityFrame: { left: 0, top: 0, right: 100, bottom: 50 },
    });
    assert.equal(same.attributes["ext:ios-a11y-bounds"], "0,0,100,50");
    const distinct = normalizeIosTree({
      elementType: "button",
      bounds: { left: 0, top: 0, right: 100, bottom: 50 },
      accessibilityFrame: { left: 5, top: 5, right: 105, bottom: 55 },
    });
    assert.equal(distinct.attributes["ext:ios-a11y-bounds"], "5,5,105,55");
  });
});

describe("normalizeIosTree — trait-driven role", () => {
  it("button trait wins over staticText elementType (Flutter button merge)", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Tất cả",
      traits: ["button", "staticText"],
    });
    assert.equal(wire.attributes["role"], "button");
    assert.equal(wire.clickable, true);
    // Label routed as a11y description, not visible text — the
    // node is a button, not a text leaf.
    assert.equal(wire.attributes["label"], "Tất cả");
    assert.equal(wire.attributes["text"], undefined);
  });

  it("image+staticText traits collapse to container (Flutter merged card)", () => {
    const wire = normalizeIosTree({
      elementType: "image",
      label: "MacBook Pro M3 14 inch\n35.900.000đ\nĐang đăng",
      traits: ["image", "staticText"],
    });
    assert.equal(wire.attributes["role"], "container");
    assert.equal(wire.attributes["label"], "MacBook Pro M3 14 inch\n35.900.000đ\nĐang đăng");
    assert.equal(wire.attributes["text"], undefined);
  });

  it("button+image traits collapse to container (icon button card)", () => {
    const wire = normalizeIosTree({
      elementType: "button",
      label: "Open menu",
      traits: ["button", "image"],
    });
    assert.equal(wire.attributes["role"], "container");
  });

  it("staticText trait alone yields role=text with label routed to text", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Welcome",
      traits: ["staticText"],
    });
    assert.equal(wire.attributes["role"], "text");
    assert.equal(wire.attributes["text"], "Welcome");
    assert.equal(wire.attributes["label"], undefined);
  });

  it("image trait alone yields role=image", () => {
    const wire = normalizeIosTree({
      elementType: "image",
      label: "Avatar",
      traits: ["image"],
    });
    assert.equal(wire.attributes["role"], "image");
  });

  it("header trait yields role=heading with text routing", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Section title",
      traits: ["header", "staticText"],
    });
    assert.equal(wire.attributes["role"], "heading");
    assert.equal(wire.attributes["text"], "Section title");
  });

  it("link trait yields role=link and clickable", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Read more",
      traits: ["link", "staticText"],
    });
    assert.equal(wire.attributes["role"], "link");
    assert.equal(wire.clickable, true);
  });

  it("searchField trait wins over elementType image", () => {
    const wire = normalizeIosTree({
      elementType: "image",
      traits: ["searchField"],
    });
    assert.equal(wire.attributes["role"], "search-field");
  });

  it("keyboardKey trait yields role=key", () => {
    const wire = normalizeIosTree({
      elementType: "other",
      label: "Q",
      traits: ["keyboardKey"],
    });
    assert.equal(wire.attributes["role"], "key");
  });

  it("falls back to elementType mapping when traits are absent", () => {
    const wire = normalizeIosTree({ elementType: "button", label: "Sign in" });
    assert.equal(wire.attributes["role"], "button");
  });

  it("falls back to label-shape heuristic when traits are absent and elementType is image", () => {
    const wire = normalizeIosTree({
      elementType: "image",
      label: "MacBook Pro M3 14 inch\n35.900.000đ\nĐang đăng",
    });
    // No traits → label-shape detector promotes to container.
    assert.equal(wire.attributes["role"], "container");
  });

  it("staticText trait + tall bounds demotes role to container (Flutter merged card)", () => {
    // Gangan marketplace card: trait says staticText, but bounds
    // height (108 px) far exceeds a real one-line text leaf
    // (~25 px). Discriminator is geometric — the merged card
    // contains an icon above and a label below.
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Xe máy",
      traits: ["staticText"],
      bounds: { left: 18, top: 229, right: 110, bottom: 337 },
    });
    assert.equal(wire.attributes["role"], "container");
  });

  it("staticText trait with normal bounds stays text", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Hồ Chí Minh, VN",
      traits: ["staticText"],
      bounds: { left: 23, top: 80, right: 335, bottom: 105 },
    });
    assert.equal(wire.attributes["role"], "text");
  });

  it("image trait + tall bounds demotes role to container (icon-only card)", () => {
    // Pure-icon card variant: Flutter merge collapses an icon
    // tile (no visible label) into a single image leaf with
    // traits=["image"]. Bounds height (~120 px) outranges any
    // real icon glyph (~24-44 px), proving it is a merged region.
    const wire = normalizeIosTree({
      elementType: "image",
      traits: ["image"],
      bounds: { left: 18, top: 229, right: 110, bottom: 350 },
    });
    assert.equal(wire.attributes["role"], "container");
  });

  it("image trait with normal bounds stays image", () => {
    const wire = normalizeIosTree({
      elementType: "image",
      traits: ["image"],
      bounds: { left: 26, top: 161, right: 46, bottom: 180 },
    });
    assert.equal(wire.attributes["role"], "image");
  });

  it("staticText trait without bounds stays text (no signal to override)", () => {
    const wire = normalizeIosTree({
      elementType: "staticText",
      label: "Some label",
      traits: ["staticText"],
    });
    assert.equal(wire.attributes["role"], "text");
  });

  it("keyboard elementType always wins (Orchestra hardcode)", () => {
    const wire = normalizeIosTree({
      elementType: "keyboard",
      traits: ["button"], // would otherwise force role=button
    });
    assert.equal(wire.attributes["role"], "keyboard");
  });
});
