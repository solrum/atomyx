import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { FileStorage, InMemoryStorage } from "./file-storage.js";

function tempRoot(): string {
  return mkdtempSync(join(tmpdir(), "atomyx-storage-test-"));
}

describe("FileStorage", () => {
  it("saves + loads a JSON record", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    await storage.save("bugs/123", { title: "x", steps: ["a", "b"] });
    const loaded = await storage.load<{ title: string; steps: string[] }>("bugs/123");
    assert.deepEqual(loaded, { title: "x", steps: ["a", "b"] });
  });

  it("saves + loads a string as markdown", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    await storage.save("notes/plan", "# Plan\n\n- Step 1");
    const loaded = await storage.load<string>("notes/plan");
    assert.equal(loaded, "# Plan\n\n- Step 1");
  });

  it("load returns null for missing key", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    assert.equal(await storage.load("nope"), null);
  });

  it("list returns keys under a prefix", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    await storage.save("bugs/1", { x: 1 });
    await storage.save("bugs/2", { x: 2 });
    await storage.save("runs/a", { x: 3 });
    const bugs = await storage.list("bugs");
    assert.deepEqual(bugs.sort(), ["bugs/1", "bugs/2"]);
  });

  it("list empty for missing prefix", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    const r = await storage.list("nothing");
    assert.deepEqual(r, []);
  });

  it("delete removes a record", async () => {
    const storage = new FileStorage({ root: tempRoot() });
    await storage.save("bugs/1", { x: 1 });
    await storage.delete("bugs/1");
    assert.equal(await storage.load("bugs/1"), null);
  });
});

describe("InMemoryStorage", () => {
  it("round-trips records", async () => {
    const storage = new InMemoryStorage();
    await storage.save("k", { v: 1 });
    const r = await storage.load<{ v: number }>("k");
    assert.deepEqual(r, { v: 1 });
  });

  it("list + delete", async () => {
    const storage = new InMemoryStorage();
    await storage.save("a/1", {});
    await storage.save("a/2", {});
    await storage.save("b/1", {});
    assert.deepEqual(await storage.list("a"), ["a/1", "a/2"]);
    await storage.delete("a/1");
    assert.deepEqual(await storage.list("a"), ["a/2"]);
  });
});
