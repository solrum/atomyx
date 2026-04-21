import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { RunStore } from "./run-store.js";

describe("RunStore", () => {
  it("start creates an active run with unique id", () => {
    const store = new RunStore();
    const { run, erroredPredecessor } = store.start({
      name: "login flow",
      source: "exploratory",
    });
    assert.equal(run.status, "running");
    assert.equal(run.name, "login flow");
    assert.equal(run.source, "exploratory");
    assert.equal(run.actionCount, 0);
    assert.equal(run.findings.length, 0);
    assert.equal(store.current(), run);
    assert.equal(erroredPredecessor, undefined);
  });

  it("incrementActions bumps the counter on active run", () => {
    const store = new RunStore();
    store.start({ name: "x" });
    store.incrementActions();
    store.incrementActions();
    store.incrementActions();
    assert.equal(store.current()!.actionCount, 3);
  });

  it("incrementActions is a no-op when no run is active", () => {
    const store = new RunStore();
    store.incrementActions(); // should not throw
    assert.equal(store.current(), null);
  });

  it("finish marks the run with the verdict and clears current", () => {
    const store = new RunStore();
    store.start({ name: "x" });
    const finished = store.finish("passed");
    assert.equal(finished!.status, "passed");
    assert.ok(finished!.finishedAt);
    assert.equal(store.current(), null);
  });

  it("finish returns null when no run is active", () => {
    const store = new RunStore();
    assert.equal(store.finish("passed"), null);
  });

  it("finish called twice is a no-op on the second call", () => {
    const store = new RunStore();
    store.start({ name: "x" });
    store.finish("passed");
    assert.equal(store.finish("passed"), null);
  });

  it("starting a new run while one is active returns the errored predecessor", () => {
    const store = new RunStore();
    const first = store.start({ name: "first" }).run;
    const second = store.start({ name: "second" });
    assert.equal(first.status, "error");
    assert.ok(first.finishedAt);
    assert.equal(second.run.status, "running");
    assert.equal(store.current(), second.run);
    // The force-errored run is surfaced so callers can persist it.
    assert.equal(second.erroredPredecessor, first);
  });

  it("recordBug adds a finding to the active run", () => {
    const store = new RunStore();
    store.start({ name: "x" });
    const bug = store.recordBug({
      title: "crash on login",
      description: "tapping login crashes",
    });
    assert.ok(bug.id);
    assert.equal(store.current()!.findings.length, 1);
    assert.equal(store.current()!.findings[0]!.title, "crash on login");
  });

  it("recordBug throws when no run is active", () => {
    const store = new RunStore();
    assert.throws(() => store.recordBug({ title: "x", description: "y" }), /no active run/);
  });
});
