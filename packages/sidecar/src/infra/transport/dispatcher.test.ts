import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Dispatcher, DispatcherError } from "./dispatcher.js";

describe("Dispatcher", () => {
  it("routes a method to the registered handler", async () => {
    const d = new Dispatcher();
    d.register("sum", (params) => {
      const p = params as { a: number; b: number };
      return p.a + p.b;
    });
    const res = await d.dispatch({ id: "1", method: "sum", params: { a: 2, b: 3 } });
    assert.deepEqual(res, { id: "1", result: 5 });
  });

  it("returns MethodNotFound for unknown methods", async () => {
    const d = new Dispatcher();
    const res = await d.dispatch({ id: "2", method: "nope" });
    assert.equal(
      (res as { error?: { code: string } }).error?.code,
      "MethodNotFound",
    );
  });

  it("serializes DispatcherError with explicit code", async () => {
    const d = new Dispatcher();
    d.register("bad", () => {
      throw new DispatcherError("NoDeviceSelected", "pick a device first");
    });
    const res = await d.dispatch({ id: "3", method: "bad" });
    assert.deepEqual(res, {
      id: "3",
      error: {
        code: "NoDeviceSelected",
        message: "pick a device first",
        data: undefined,
      },
    });
  });

  it("wraps thrown non-DispatcherError as InternalError", async () => {
    const d = new Dispatcher();
    d.register("boom", () => {
      throw new Error("kaboom");
    });
    const res = await d.dispatch({ id: "4", method: "boom" });
    assert.equal(
      (res as { error?: { code: string } }).error?.code,
      "InternalError",
    );
  });

  it("forbids duplicate registration", () => {
    const d = new Dispatcher();
    d.register("a", () => null);
    assert.throws(() => d.register("a", () => null));
  });

  it("lists registered methods sorted", () => {
    const d = new Dispatcher();
    d.register("b", () => null);
    d.register("a", () => null);
    assert.deepEqual(d.methods(), ["a", "b"]);
  });
});
