import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MockProjectRegistry } from "./projects.registry.mock.js";
import { sortRecentProjects } from "./projects.types.js";

describe("MockProjectRegistry", () => {
  it("registers a new project on first touch and bumps lastOpenedAt on revisit", async () => {
    let t = 1_000;
    const reg = new MockProjectRegistry({ now: () => t });

    const first = await reg.touch("/Users/me/projects/demo");
    assert.equal(first.pinned, false);
    assert.equal(first.displayName, "demo");
    assert.equal(first.lastOpenedAt, 1_000);

    t = 5_000;
    const second = await reg.touch("/Users/me/projects/demo");
    assert.equal(second.id, first.id, "same path → same id");
    assert.equal(second.lastOpenedAt, 5_000);
    assert.equal(second.addedAt, 1_000, "addedAt is frozen");
  });

  it("toggles pin without losing order metadata", async () => {
    const reg = new MockProjectRegistry();
    const { id } = await reg.touch("/a/b");
    await reg.setPinned(id, true);
    const list = await reg.list();
    assert.equal(list[0]!.pinned, true);
  });

  it("remove drops the entry", async () => {
    const reg = new MockProjectRegistry();
    const { id } = await reg.touch("/a/b");
    await reg.remove(id);
    assert.equal((await reg.list()).length, 0);
  });
});

describe("sortRecentProjects", () => {
  it("puts pinned first alphabetically, rest by recency desc", () => {
    const sorted = sortRecentProjects([
      { id: "1", path: "/z", displayName: "zeta", pinned: false, lastOpenedAt: 100, addedAt: 0 },
      { id: "2", path: "/b", displayName: "beta", pinned: true, lastOpenedAt: 10, addedAt: 0 },
      { id: "3", path: "/a", displayName: "alpha", pinned: true, lastOpenedAt: 20, addedAt: 0 },
      { id: "4", path: "/g", displayName: "gamma", pinned: false, lastOpenedAt: 500, addedAt: 0 },
    ]);
    assert.deepEqual(
      sorted.map((p) => p.displayName),
      ["alpha", "beta", "gamma", "zeta"],
    );
  });
});
