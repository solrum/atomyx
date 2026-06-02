import { test } from "node:test";
import { strict as assert } from "node:assert";
import type { UiTreeNode } from "../../../domain/features/runtime/index.js";
import { attributeRows } from "./attribute-rows.js";

test("attributeRows", async (t) => {
  await t.test("returns empty array for null", () => {
    assert.deepEqual(attributeRows(null), []);
  });

  await t.test("returns empty array for a node with no data", () => {
    const node: UiTreeNode = { attributes: {}, children: [] };
    assert.deepEqual(attributeRows(node), []);
  });

  await t.test("includes every raw attribute", () => {
    const node: UiTreeNode = {
      attributes: { class: "button", label: "Sign in", id: "btn" },
      children: [],
    };
    const rows = attributeRows(node);
    assert.deepEqual(
      rows.map((r) => r.key),
      ["class", "id", "label"],
    );
  });

  await t.test("appends defined state flags as stringified values", () => {
    const node: UiTreeNode = {
      attributes: { class: "button" },
      children: [],
      clickable: true,
      enabled: false,
    };
    const rows = attributeRows(node);
    assert.deepEqual(rows, [
      { key: "class", value: "button" },
      { key: "clickable", value: "true" },
      { key: "enabled", value: "false" },
    ]);
  });

  await t.test("skips undefined state flags", () => {
    const node: UiTreeNode = {
      attributes: { class: "button" },
      children: [],
      clickable: true,
    };
    const rows = attributeRows(node);
    assert.equal(rows.length, 2);
    assert.ok(!rows.some((r) => r.key === "enabled"));
  });

  await t.test("sorts alphabetically by key", () => {
    const node: UiTreeNode = {
      attributes: { z: "1", a: "2", m: "3" },
      children: [],
      selected: true,
    };
    const keys = attributeRows(node).map((r) => r.key);
    assert.deepEqual(keys, ["a", "m", "selected", "z"]);
  });

  await t.test("includes visible state flag", () => {
    const node: UiTreeNode = {
      attributes: { class: "button" },
      children: [],
      visible: false,
    };
    const rows = attributeRows(node);
    assert.ok(rows.some((r) => r.key === "visible" && r.value === "false"));
  });
});
