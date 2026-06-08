import { test } from "node:test";
import { strict as assert } from "node:assert";
import type { UiTreeNode } from "../../../domain/features/runtime/index.js";
import {
  collectBranchPaths,
  collectInterestingPaths,
  isInformative,
} from "./ui-inspector-tree-filter.js";

function node(
  attrs: Readonly<Record<string, string>>,
  children: readonly UiTreeNode[] = [],
): UiTreeNode {
  return { attributes: attrs, children };
}

test("isInformative", async (t) => {
  await t.test("treats id / text / label as informative", () => {
    assert.equal(isInformative(node({ class: "other", label: "Login" })), true);
    assert.equal(isInformative(node({ class: "other", text: "Hi" })), true);
    assert.equal(isInformative(node({ class: "other", id: "btn" })), true);
  });

  await t.test("treats real classes as informative", () => {
    assert.equal(isInformative(node({ class: "button" })), true);
    assert.equal(isInformative(node({ class: "staticText" })), true);
  });

  await t.test("treats wrapper classes without labels as noise", () => {
    assert.equal(isInformative(node({ class: "other" })), false);
    assert.equal(isInformative(node({ class: "node" })), false);
    assert.equal(isInformative(node({})), false);
  });
});

test("collectInterestingPaths", async (t) => {
  await t.test("returns an empty set for a null tree", () => {
    const set = collectInterestingPaths(null);
    assert.equal(set.size, 0);
  });

  await t.test("marks ancestors of a labelled descendant", () => {
    // root → other → other → button "Login"
    const tree = node({ class: "other" }, [
      node({ class: "other" }, [
        node({ class: "button", label: "Login" }),
      ]),
    ]);
    const set = collectInterestingPaths(tree);
    // root must surface so the labelled descendant is reachable
    assert.ok(set.has(""));
    assert.ok(set.has("0"));
    assert.ok(set.has("0.0"));
  });

  await t.test("drops dead `window` whose subtree is all spacers", () => {
    // root → [labelled branch, dead window with only spacers]
    const tree = node({ class: "root", label: "App" }, [
      node({ class: "window" }, [
        node({ class: "button", label: "Open" }),
      ]),
      node({ class: "window" }, [
        node({ class: "other" }, [node({ class: "other" })]),
      ]),
    ]);
    const set = collectInterestingPaths(tree);
    assert.ok(set.has(""), "root has labelled ancestor");
    assert.ok(set.has("0"), "first window has labelled descendant");
    assert.ok(set.has("0.0"), "labelled button");
    assert.ok(!set.has("1"), "dead window must be filtered out");
    assert.ok(!set.has("1.0"));
    assert.ok(!set.has("1.0.0"));
  });

  await t.test("drops spacer leaves with no semantics", () => {
    const tree = node({ class: "other" }, [
      node({ class: "other" }), // pure spacer
      node({ class: "button", label: "Tap" }),
    ]);
    const set = collectInterestingPaths(tree);
    assert.ok(set.has(""));
    assert.ok(!set.has("0"), "spacer leaf is dropped");
    assert.ok(set.has("1"));
  });

  await t.test("keeps a class-only leaf when an ancestor has labels", () => {
    // The set is transitive — siblings of labelled descendants do
    // not gain interest, so this confirms the rule does not leak.
    const tree = node({ class: "other", label: "Form" }, [
      node({ class: "scrollView" }), // class-only leaf, no descendants
    ]);
    const set = collectInterestingPaths(tree);
    assert.ok(set.has(""));
    assert.ok(!set.has("0"), "class-only leaf is not interesting on its own");
  });
});

test("collectBranchPaths", async (t) => {
  await t.test("returns empty set for a null tree", () => {
    assert.equal(collectBranchPaths(null).size, 0);
  });

  await t.test("returns empty set for a leaf-only root", () => {
    assert.equal(collectBranchPaths(node({})).size, 0);
  });

  await t.test("omits the root path so Collapse All keeps it open", () => {
    const tree = node({}, [node({}, [node({})])]);
    const set = collectBranchPaths(tree);
    assert.ok(!set.has(""), "root must stay expanded");
    assert.ok(set.has("0"), "intermediate branch is collapsible");
    assert.ok(!set.has("0.0"), "leaf is skipped");
  });

  await t.test("enumerates every non-leaf path", () => {
    const tree = node({}, [
      node({}, [node({}), node({})]),
      node({}),
      node({}, [node({})]),
    ]);
    const set = collectBranchPaths(tree);
    assert.deepEqual([...set].sort(), ["0", "2"]);
  });
});
