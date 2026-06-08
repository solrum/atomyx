import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { normalizeAndroidTree, classNameToRole, type AndroidRawElement } from "./tree-normalizer.js";

describe("classNameToRole", () => {
  it("maps Android Button to button role", () => {
    assert.equal(classNameToRole("android.widget.Button"), "button");
    assert.equal(classNameToRole("android.widget.ImageButton"), "button");
  });

  it("maps EditText to text-field", () => {
    assert.equal(classNameToRole("android.widget.EditText"), "text-field");
  });

  it("maps SearchView to search-field", () => {
    assert.equal(classNameToRole("android.widget.SearchView"), "search-field");
  });

  it("maps TextView to text", () => {
    assert.equal(classNameToRole("android.widget.TextView"), "text");
  });

  it("maps layout classes to container", () => {
    assert.equal(classNameToRole("android.widget.FrameLayout"), "container");
    assert.equal(classNameToRole("android.widget.LinearLayout"), "container");
    assert.equal(classNameToRole("androidx.constraintlayout.widget.ConstraintLayout"), "container");
    assert.equal(classNameToRole("androidx.compose.ui.platform.ComposeView"), "other");
  });

  it("maps form controls", () => {
    assert.equal(classNameToRole("android.widget.CheckBox"), "checkbox");
    assert.equal(classNameToRole("android.widget.RadioButton"), "radio-button");
    assert.equal(classNameToRole("android.widget.Switch"), "switch");
    assert.equal(classNameToRole("android.widget.SeekBar"), "slider");
  });

  it("falls through to 'other' for unknown classes", () => {
    assert.equal(classNameToRole("com.example.CustomView"), "other");
    assert.equal(classNameToRole(undefined), "other");
    assert.equal(classNameToRole(""), "other");
  });
});

describe("normalizeAndroidTree", () => {
  it("maps canonical attribute keys from wire fields", () => {
    const raw: AndroidRawElement = {
      elementId: "el-1",
      className: "android.widget.Button",
      resourceId: "com.app:id/login_btn",
      contentDesc: "Login button",
      text: "Sign in",
      bounds: { left: 100, top: 200, right: 300, bottom: 260 },
      clickable: true,
      enabled: true,
    };
    const wire = normalizeAndroidTree(raw);
    assert.equal(wire.attributes["id"], "com.app:id/login_btn");
    assert.equal(wire.attributes["label"], "Login button");
    assert.equal(wire.attributes["text"], "Sign in");
    assert.equal(wire.attributes["class"], "android.widget.Button");
    assert.equal(wire.attributes["role"], "button");
    assert.equal(wire.attributes["bounds"], "100,200,300,260");
    assert.equal(wire.clickable, true);
    assert.equal(wire.enabled, true);
  });

  it("drops empty strings from attributes", () => {
    const wire = normalizeAndroidTree({
      elementId: "el-1",
      className: "android.widget.TextView",
      resourceId: "",
      contentDesc: "",
      text: "",
      clickable: false,
      enabled: true,
    });
    // id, label, text all empty → not present.
    assert.equal(wire.attributes["id"], undefined);
    assert.equal(wire.attributes["label"], undefined);
    assert.equal(wire.attributes["text"], undefined);
    // class + role always present.
    assert.equal(wire.attributes["class"], "android.widget.TextView");
    assert.equal(wire.attributes["role"], "text");
  });

  it("recursively normalizes children", () => {
    const raw: AndroidRawElement = {
      elementId: "root",
      className: "android.widget.FrameLayout",
      bounds: { left: 0, top: 0, right: 1080, bottom: 2400 },
      children: [
        {
          elementId: "c1",
          className: "android.widget.Button",
          resourceId: "btn1",
          bounds: { left: 10, top: 20, right: 100, bottom: 80 },
        },
        {
          elementId: "c2",
          className: "android.widget.EditText",
          resourceId: "input1",
          bounds: { left: 10, top: 100, right: 200, bottom: 160 },
        },
      ],
    };
    const wire = normalizeAndroidTree(raw);
    assert.equal(wire.children.length, 2);
    assert.equal(wire.children[0]!.attributes["role"], "button");
    assert.equal(wire.children[1]!.attributes["role"], "text-field");
    assert.equal(wire.attributes["role"], "container");
  });

  it("handles missing bounds gracefully", () => {
    const wire = normalizeAndroidTree({
      elementId: "el-1",
      className: "android.widget.TextView",
    });
    assert.equal(wire.attributes["bounds"], undefined);
  });

  it("derives button role when bare View is clickable + has label (Flutter Semantics)", () => {
    // Gangan tab: Flutter Semantics emits the category tile as
    // `android.view.View` with clickable=true and a content
    // description. Class-name table cannot identify it; signal-
    // based fallback recovers `button`.
    const wire = normalizeAndroidTree({
      elementId: "tab-1",
      className: "android.view.View",
      contentDesc: "Xe máy",
      clickable: true,
    });
    assert.equal(wire.attributes["role"], "button");
  });

  it("derives button role when bare View is clickable + has text", () => {
    const wire = normalizeAndroidTree({
      elementId: "btn-1",
      className: "android.view.View",
      text: "Tin nhắn",
      clickable: true,
    });
    assert.equal(wire.attributes["role"], "button");
  });

  it("derives text role for non-clickable View with text", () => {
    const wire = normalizeAndroidTree({
      elementId: "lbl-1",
      className: "android.view.View",
      text: "TIN MỚI ĐĂNG",
      clickable: false,
    });
    assert.equal(wire.attributes["role"], "text");
  });

  it("keeps role 'other' for bare View with no actionable signal", () => {
    const wire = normalizeAndroidTree({
      elementId: "decor-1",
      className: "android.view.View",
      clickable: false,
    });
    assert.equal(wire.attributes["role"], "other");
  });

  it("keeps role 'other' for bare View with only contentDesc and not clickable", () => {
    // Decorative description (a11y label on a non-interactive
    // region) shouldn't be promoted — too ambiguous.
    const wire = normalizeAndroidTree({
      elementId: "icon-1",
      className: "android.view.View",
      contentDesc: "Logo",
      clickable: false,
    });
    assert.equal(wire.attributes["role"], "other");
  });

  it("plumbs focused state when set", () => {
    const wire = normalizeAndroidTree({
      elementId: "el-1",
      className: "android.widget.EditText",
      resourceId: "email",
      focused: true,
    });
    assert.equal(wire.focused, true);
  });

  it("leaves focused undefined when raw field is missing", () => {
    const wire = normalizeAndroidTree({
      elementId: "el-1",
      className: "android.widget.TextView",
    });
    assert.equal(wire.focused, undefined);
  });

  it("plumbs selected and visible state when set", () => {
    const wire = normalizeAndroidTree({
      elementId: "tab-1",
      className: "android.widget.TextView",
      selected: true,
      visible: false,
    });
    assert.equal(wire.selected, true);
    assert.equal(wire.visible, false);
  });

  it("emits checked only when checkable is true", () => {
    const checkbox = normalizeAndroidTree({
      elementId: "cb-1",
      className: "android.widget.CheckBox",
      checkable: true,
      checked: true,
    });
    assert.equal(checkbox.checked, true);
    // Plain views report checked=false by default in
    // AccessibilityNodeInfo even when checked is meaningless.
    // Adapter must NOT emit it on non-checkable nodes — consumers
    // would otherwise see a misleading "checked=false" for buttons,
    // text fields, etc.
    const button = normalizeAndroidTree({
      elementId: "btn-1",
      className: "android.widget.Button",
      checkable: false,
      checked: false,
    });
    assert.equal(button.checked, undefined);
  });

  it("preserves document order of children (matters for z-order)", () => {
    const raw: AndroidRawElement = {
      elementId: "root",
      className: "android.widget.FrameLayout",
      children: [
        { elementId: "a", className: "android.widget.TextView", resourceId: "a" },
        { elementId: "b", className: "android.widget.TextView", resourceId: "b" },
        { elementId: "c", className: "android.widget.TextView", resourceId: "c" },
      ],
    };
    const wire = normalizeAndroidTree(raw);
    assert.deepEqual(
      wire.children.map((c) => c.attributes["id"]),
      ["a", "b", "c"],
    );
  });
});
