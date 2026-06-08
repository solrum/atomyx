import { test } from "node:test";
import { strict as assert } from "node:assert";
import type { UiTreeNode } from "../runtime/index.js";
import { SCRIPT_ACTIONS } from "./script-actions-action-catalog.js";
import { bestSelector, selectorsFromNode } from "./script-actions-selectors-from-node.js";

const buttonNode: UiTreeNode = {
  attributes: {
    class: "android.widget.Button",
    id: "com.example:id/login",
    label: "Login button",
    text: "Sign in",
    bounds: "100,200,300,400",
  },
  clickable: true,
  children: [],
};

const emailFieldNode: UiTreeNode = {
  attributes: {
    class: "android.widget.EditText",
    hint: "Email",
    bounds: "10,100,500,180",
  },
  focused: false,
  children: [],
};

const prefilledFieldNode: UiTreeNode = {
  attributes: {
    class: "android.widget.EditText",
    hint: "Email",
    text: "user@example.com",
    bounds: "10,100,500,180",
  },
  children: [],
};

const containerNode: UiTreeNode = {
  attributes: {
    class: "android.view.ViewGroup",
    bounds: "0,0,1080,2340",
  },
  children: [],
};

test("selectorsFromNode", async (t) => {
  await t.test("returns candidates in priority order", () => {
    const out = selectorsFromNode(buttonNode);
    assert.deepEqual(
      out.map((c) => c.kind),
      ["id", "label", "text"],
    );
  });

  await t.test("text kind renders as bare quoted scalar", () => {
    const out = selectorsFromNode({
      attributes: { text: "Sign in" },
      children: [],
    });
    assert.equal(out[0]!.toYamlInline(), '"Sign in"');
  });

  await t.test("non-text kinds render as flow mappings", () => {
    const out = selectorsFromNode(buttonNode);
    const byKind = Object.fromEntries(out.map((c) => [c.kind, c.toYamlInline()]));
    assert.equal(byKind["id"], '{ id: "com.example:id/login" }');
    assert.equal(byKind["label"], '{ label: "Login button" }');
  });

  await t.test("bestSelector picks the first entry", () => {
    assert.equal(bestSelector(selectorsFromNode(buttonNode))?.kind, "id");
    assert.equal(bestSelector([]), null);
  });

  await t.test("empty when only bounds are known", () => {
    assert.deepEqual(selectorsFromNode(containerNode), []);
  });

  await t.test("inline yaml escapes quotes and backslashes", () => {
    const out = selectorsFromNode({
      attributes: { text: 'Hello "world" \\ path' },
      children: [],
    });
    assert.equal(out[0]!.toYamlInline(), '"Hello \\"world\\" \\\\ path"');
  });

  await t.test("role+nth fallback when no stable attribute is present", () => {
    const target: UiTreeNode = {
      attributes: { role: "text-field", class: "android.widget.EditText" },
      children: [],
    };
    const other: UiTreeNode = {
      attributes: { role: "text-field", class: "android.widget.EditText" },
      children: [],
    };
    const root: UiTreeNode = {
      attributes: { role: "container" },
      children: [other, target],
    };
    const out = selectorsFromNode(target, root);
    assert.equal(out.length, 1);
    assert.equal(out[0]!.kind, "role-nth");
    assert.equal(out[0]!.toYamlInline(), '{ role: "text-field", nth: 1 }');
    assert.equal(out[0]!.display, 'role "text-field" #1');
  });

  await t.test("role+nth skipped when attribute selector exists", () => {
    const node: UiTreeNode = {
      attributes: { role: "text-field", id: "email" },
      children: [],
    };
    const root: UiTreeNode = { attributes: {}, children: [node] };
    const out = selectorsFromNode(node, root);
    assert.deepEqual(
      out.map((c) => c.kind),
      ["id"],
    );
  });

  await t.test("role+nth skipped for generic roles", () => {
    const target: UiTreeNode = {
      attributes: { role: "container" },
      children: [],
    };
    const root: UiTreeNode = { attributes: {}, children: [target] };
    assert.deepEqual(selectorsFromNode(target, root), []);
  });

  await t.test("role+nth skipped when tree is not supplied", () => {
    const node: UiTreeNode = {
      attributes: { role: "text-field" },
      children: [],
    };
    assert.deepEqual(selectorsFromNode(node), []);
  });

  await t.test("role+nth index is depth-first across siblings", () => {
    const first: UiTreeNode = {
      attributes: { role: "button" },
      children: [],
    };
    const second: UiTreeNode = {
      attributes: { role: "button" },
      children: [],
    };
    const third: UiTreeNode = {
      attributes: { role: "button" },
      children: [],
    };
    const branch: UiTreeNode = {
      attributes: { role: "container" },
      children: [second],
    };
    const root: UiTreeNode = {
      attributes: { role: "container" },
      children: [first, branch, third],
    };
    assert.equal(selectorsFromNode(first, root)[0]!.toYamlInline(), '{ role: "button", nth: 0 }');
    assert.equal(selectorsFromNode(second, root)[0]!.toYamlInline(), '{ role: "button", nth: 1 }');
    assert.equal(selectorsFromNode(third, root)[0]!.toYamlInline(), '{ role: "button", nth: 2 }');
  });
});

test("SCRIPT_ACTIONS catalog — applicability", async (t) => {
  const ids = (node: UiTreeNode) =>
    SCRIPT_ACTIONS.filter((a) => a.appliesTo(node)).map((a) => a.id);

  await t.test("buttons get tap + asserts but not type", () => {
    const applicable = ids(buttonNode);
    assert.deepEqual(
      applicable,
      ["tap", "waitFor", "assertVisible", "assertNotVisible"],
    );
  });

  await t.test("text inputs unlock type", () => {
    const applicable = ids(emailFieldNode);
    assert.ok(applicable.includes("type"));
  });

  await t.test("role=text-field also unlocks type", () => {
    const flutterField: UiTreeNode = {
      attributes: { role: "text-field", label: "Email" },
      children: [],
    };
    assert.ok(SCRIPT_ACTIONS.find((a) => a.id === "type")!.appliesTo(flutterField));
  });
});

test("SCRIPT_ACTIONS catalog — buildYaml", async (t) => {
  const idSelector = selectorsFromNode(buttonNode)[0]!;
  const textSelector = selectorsFromNode({
    attributes: { text: "Sign in" },
    children: [],
  })[0]!;
  const hintSelector = selectorsFromNode(emailFieldNode)[0]!;

  await t.test("every action's YAML starts with a step marker", () => {
    for (const action of SCRIPT_ACTIONS) {
      const { yaml } = action.buildYaml(buttonNode, idSelector);
      assert.ok(yaml.startsWith("- "), `${action.id} → ${yaml}`);
    }
  });

  await t.test("tap with text selector uses bare shorthand", () => {
    const tap = SCRIPT_ACTIONS.find((a) => a.id === "tap")!;
    assert.equal(
      tap.buildYaml(buttonNode, textSelector).yaml,
      '- tap: "Sign in"',
    );
  });

  await t.test("tap with id selector uses flow mapping shorthand", () => {
    const tap = SCRIPT_ACTIONS.find((a) => a.id === "tap")!;
    assert.equal(
      tap.buildYaml(buttonNode, idSelector).yaml,
      '- tap: { id: "com.example:id/login" }',
    );
  });

  await t.test("type uses TODO placeholder on empty field", () => {
    const type = SCRIPT_ACTIONS.find((a) => a.id === "type")!;
    const built = type.buildYaml(emailFieldNode, hintSelector);
    assert.equal(
      built.yaml,
      '- type: { into: { hint: "Email" }, text: "TODO" }',
    );
    const [ph] = built.placeholders;
    assert.ok(ph);
    assert.equal(built.yaml.slice(ph!.offset, ph!.offset + ph!.length), "TODO");
  });

  await t.test("type pre-fills from existing field text", () => {
    const type = SCRIPT_ACTIONS.find((a) => a.id === "type")!;
    const sel = selectorsFromNode(prefilledFieldNode)[0]!;
    const built = type.buildYaml(prefilledFieldNode, sel);
    assert.match(built.yaml, /text: "user@example.com"/);
    const [ph] = built.placeholders;
    assert.ok(ph);
    assert.equal(
      built.yaml.slice(ph!.offset, ph!.offset + ph!.length),
      "user@example.com",
    );
  });

  await t.test("waitFor emits shorthand without explicit timeout", () => {
    const wait = SCRIPT_ACTIONS.find((a) => a.id === "waitFor")!;
    assert.equal(wait.buildYaml(buttonNode, textSelector).yaml, '- waitFor: "Sign in"');
  });

  await t.test("type with role+nth selector switches to block form", () => {
    const field: UiTreeNode = {
      attributes: { role: "text-field", class: "android.widget.EditText" },
      children: [],
    };
    const root: UiTreeNode = { attributes: {}, children: [field] };
    const sel = selectorsFromNode(field, root)[0]!;
    assert.equal(sel.kind, "role-nth");
    const type = SCRIPT_ACTIONS.find((a) => a.id === "type")!;
    const built = type.buildYaml(field, sel);
    assert.equal(
      built.yaml,
      '- type:\n' +
        '    into:\n' +
        '      role: "text-field"\n' +
        '      nth: 0\n' +
        '    text: "TODO"',
    );
    const [ph] = built.placeholders;
    assert.ok(ph);
    assert.equal(built.yaml.slice(ph!.offset, ph!.offset + ph!.length), "TODO");
  });

  await t.test("assert commands emit shorthand", () => {
    const av = SCRIPT_ACTIONS.find((a) => a.id === "assertVisible")!;
    const anv = SCRIPT_ACTIONS.find((a) => a.id === "assertNotVisible")!;
    assert.equal(
      av.buildYaml(buttonNode, textSelector).yaml,
      '- assertVisible: "Sign in"',
    );
    assert.equal(
      anv.buildYaml(buttonNode, textSelector).yaml,
      '- assertNotVisible: "Sign in"',
    );
  });
});
