import { describe, it } from "node:test";
import assert from "node:assert/strict";
import type { TreeNode } from "../tree/tree-node.js";
import { AttrKeys, Roles } from "../tree/tree-node.js";
import {
  fromTree,
  idMatches,
  textMatches,
  labelMatches,
  roleIs,
  isEnabled,
  isClickable,
  intersect,
  union,
  not,
  first,
  nth,
  below,
  above,
  hasParent,
  hasDescendant,
  hasAncestor,
  sortByTopLeft,
} from "./element-filter.js";

// Test fixture: a tiny login screen.
//
//   root
//   ├── header  (text: "Welcome")          bounds 0,0,400,60
//   ├── form    (container)
//   │   ├── email  (text-field id=email)    bounds 20,100,380,140
//   │   └── pass   (secure-text-field id=pwd)bounds 20,160,380,200
//   └── login   (button text="Sign in")    bounds 20,240,380,280

function n(
  attrs: Record<string, string>,
  opts: {
    enabled?: boolean;
    clickable?: boolean;
    children?: TreeNode[];
  } = {},
): TreeNode {
  return {
    attributes: attrs,
    children: opts.children ?? [],
    enabled: opts.enabled,
    clickable: opts.clickable,
  };
}

function loginTree(): TreeNode {
  const header = n(
    { [AttrKeys.Role]: Roles.Text, [AttrKeys.Text]: "Welcome", [AttrKeys.Bounds]: "0,0,400,60" },
  );
  const email = n(
    {
      [AttrKeys.Role]: Roles.TextField,
      [AttrKeys.Id]: "email",
      [AttrKeys.Label]: "Email",
      [AttrKeys.Bounds]: "20,100,380,140",
    },
    { enabled: true, clickable: true },
  );
  const pass = n(
    {
      [AttrKeys.Role]: Roles.SecureTextField,
      [AttrKeys.Id]: "pwd",
      [AttrKeys.Label]: "Password",
      [AttrKeys.Bounds]: "20,160,380,200",
    },
    { enabled: true, clickable: true },
  );
  const form = n({ [AttrKeys.Role]: Roles.Container }, { children: [email, pass] });
  const login = n(
    {
      [AttrKeys.Role]: Roles.Button,
      [AttrKeys.Text]: "Sign in",
      [AttrKeys.Bounds]: "20,240,380,280",
    },
    { enabled: true, clickable: true },
  );
  return n({ [AttrKeys.Role]: Roles.Container }, { children: [header, form, login] });
}

describe("filter atoms", () => {
  it("idMatches by string", () => {
    const cs = fromTree(loginTree());
    const r = idMatches("email")(cs);
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "email");
  });

  it("textMatches by regex", () => {
    const cs = fromTree(loginTree());
    const r = textMatches(/sign/i)(cs);
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Text], "Sign in");
  });

  it("labelMatches finds the field by label", () => {
    const cs = fromTree(loginTree());
    const r = labelMatches("Email")(cs);
    assert.equal(r.length, 1);
    assert.equal(r[0]!.node.attributes[AttrKeys.Id], "email");
  });

  it("roleIs filters by normalized role", () => {
    const cs = fromTree(loginTree());
    assert.equal(roleIs(Roles.Button)(cs).length, 1);
    assert.equal(roleIs(Roles.TextField)(cs).length, 1);
    assert.equal(roleIs(Roles.Container)(cs).length, 2);
  });
});

describe("state filters", () => {
  it("isEnabled keeps only enabled cursors", () => {
    const cs = fromTree(loginTree());
    assert.equal(isEnabled()(cs).length, 3);
  });

  it("isClickable keeps only clickable cursors", () => {
    const cs = fromTree(loginTree());
    assert.equal(isClickable()(cs).length, 3);
  });
});

describe("composition", () => {
  it("intersect AND-s filters", () => {
    const cs = fromTree(loginTree());
    const r = intersect(roleIs(Roles.Button), textMatches("Sign in"))(cs);
    assert.equal(r.length, 1);
  });

  it("intersect returns empty when one filter matches nothing", () => {
    const cs = fromTree(loginTree());
    const r = intersect(roleIs(Roles.Button), textMatches("Nonexistent"))(cs);
    assert.equal(r.length, 0);
  });

  it("union OR-s filters and dedupes", () => {
    const cs = fromTree(loginTree());
    const r = union(roleIs(Roles.Button), roleIs(Roles.TextField))(cs);
    assert.equal(r.length, 2);
  });

  it("union dedupes cursors that match multiple branches", () => {
    const cs = fromTree(loginTree());
    const r = union(textMatches("Sign in"), roleIs(Roles.Button))(cs);
    // The login button matches both — should appear once.
    assert.equal(r.length, 1);
  });

  it("not inverts filter", () => {
    const cs = fromTree(loginTree());
    const r = not(roleIs(Roles.Container))(cs);
    // 6 nodes total (root, header, form, email, pass, login), 2 are
    // containers (root, form) → 4 non-containers.
    assert.equal(r.length, 4);
  });
});

describe("selection", () => {
  it("first picks head of result", () => {
    const cs = fromTree(loginTree());
    const r = first(roleIs(Roles.Container))(cs);
    assert.equal(r.length, 1);
  });

  it("nth picks by index", () => {
    const cs = fromTree(loginTree());
    const r = nth(roleIs(Roles.Container), 1)(cs);
    assert.equal(r.length, 1);
  });

  it("nth out of range returns empty", () => {
    const cs = fromTree(loginTree());
    assert.equal(nth(roleIs(Roles.Button), 5)(cs).length, 0);
  });

  it("sortByTopLeft orders by geometry", () => {
    const cs = fromTree(loginTree());
    const r = sortByTopLeft()(cs);
    // Header is topmost (0,0), then email (20,100), then pass (20,160), then login (20,240).
    // Containers (root, form) lack bounds and are dropped.
    const ids = r.map((c) => c.node.attributes[AttrKeys.Id] ?? c.node.attributes[AttrKeys.Text]);
    assert.deepEqual(ids, ["Welcome", "email", "pwd", "Sign in"]);
  });
});

describe("spatial filters", () => {
  it("below keeps cursors whose center is below the anchor", () => {
    const cs = fromTree(loginTree());
    const r = below(textMatches("Welcome"))(cs);
    // Everything geometric that is below header (center y=30) — email,pwd,login.
    assert.equal(r.length, 3);
  });

  it("above keeps cursors whose center is above the anchor", () => {
    const cs = fromTree(loginTree());
    const r = above(textMatches("Sign in"))(cs);
    // Above Sign in (center y=260) — header, email, pass.
    assert.equal(r.length, 3);
  });
});

describe("structural filters", () => {
  it("hasParent matches by parent filter", () => {
    const cs = fromTree(loginTree());
    const r = hasParent(roleIs(Roles.Container))(cs);
    // Every non-root node has a parent, and every parent in this
    // fixture is a container (root, form). 6 nodes − 1 root = 5.
    assert.equal(r.length, 5);
  });

  it("hasDescendant matches subtree containing filter", () => {
    const cs = fromTree(loginTree());
    const r = hasDescendant(idMatches("email"))(cs);
    // form AND root both contain email.
    assert.equal(r.length, 2);
  });

  it("hasAncestor matches if any ancestor matches filter", () => {
    const cs = fromTree(loginTree());
    const r = intersect(roleIs(Roles.TextField), hasAncestor(roleIs(Roles.Container)))(cs);
    // email is a text-field whose ancestors include form + root (both containers).
    assert.equal(r.length, 1);
  });
});
