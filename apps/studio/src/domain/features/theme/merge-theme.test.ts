import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MAX_INHERITANCE_DEPTH, mergeTheme } from "./merge-theme.js";
import type { Theme } from "./types.js";
import { THEME_SCHEMA_VERSION } from "./types.js";

function mkTheme(
  id: string,
  attributes: Theme["attributes"],
  extendsId?: string,
): Theme {
  return {
    schemaVersion: THEME_SCHEMA_VERSION,
    id,
    label: id,
    ...(extendsId !== undefined ? { extends: extendsId } : {}),
    monacoBase: "vs-dark",
    attributes,
  };
}

function library(...themes: Theme[]): Map<string, Theme> {
  const m = new Map<string, Theme>();
  for (const t of themes) m.set(t.id, t);
  return m;
}

describe("mergeTheme", () => {
  it("returns the single theme's attributes when no extends", () => {
    const base = mkTheme("base", {
      ATOMYX_KEYWORD: { foreground: "#111111" },
    });
    const result = mergeTheme("base", library(base));
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.equal(result.attributes.ATOMYX_KEYWORD.foreground, "#111111");
    }
  });

  it("merges child on top of parent field-wise", () => {
    const parent = mkTheme("parent", {
      ATOMYX_KEYWORD: { foreground: "#111111", fontStyle: "bold" },
    });
    const child = mkTheme(
      "child",
      {
        ATOMYX_KEYWORD: { foreground: "#222222" },
      },
      "parent",
    );
    const result = mergeTheme("child", library(parent, child));
    assert.equal(result.ok, true);
    if (result.ok) {
      const bundle = result.attributes.ATOMYX_KEYWORD;
      assert.equal(bundle.foreground, "#222222");
      assert.equal(bundle.fontStyle, "bold");
    }
  });

  it("applies overrides on top of the child", () => {
    const base = mkTheme("base", {
      ATOMYX_KEYWORD: { foreground: "#111111" },
    });
    const result = mergeTheme("base", library(base), {
      ATOMYX_KEYWORD: { foreground: "#ff00ff" },
    });
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.equal(result.attributes.ATOMYX_KEYWORD.foreground, "#ff00ff");
    }
  });

  it("fills missing attributes from DEFAULT_ATTRIBUTES", () => {
    const base = mkTheme("base", {});
    const result = mergeTheme("base", library(base));
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.ok(result.attributes.EDITOR_BACKGROUND.foreground);
      assert.ok(result.attributes.RUN_STEP_RUNNING.foreground);
    }
  });

  it("detects cycles in the extends chain", () => {
    const a = mkTheme("a", {}, "b");
    const b = mkTheme("b", {}, "a");
    const result = mergeTheme("a", library(a, b));
    assert.equal(result.ok, false);
    if (!result.ok) {
      assert.equal(result.issues[0]!.code, "cycle");
    }
  });

  it("reports when a parent is missing", () => {
    const a = mkTheme("a", {}, "ghost");
    const result = mergeTheme("a", library(a));
    assert.equal(result.ok, false);
    if (!result.ok) {
      assert.equal(result.issues[0]!.code, "missing-parent");
    }
  });

  it("enforces the inheritance depth cap", () => {
    const chain: Theme[] = [];
    for (let i = 0; i <= MAX_INHERITANCE_DEPTH + 2; i++) {
      chain.push(
        mkTheme(
          `t-${i}`,
          {},
          i === MAX_INHERITANCE_DEPTH + 2 ? undefined : `t-${i + 1}`,
        ),
      );
    }
    const lib = library(...chain);
    const result = mergeTheme("t-0", lib);
    assert.equal(result.ok, false);
    if (!result.ok) {
      assert.equal(result.issues[0]!.code, "depth-exceeded");
    }
  });
});
