import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { EventBus } from "./event-bus.js";
import type { JsonRpcEvent } from "../transport/types.js";

describe("EventBus", () => {
  it("delivers emitted events to every subscriber", () => {
    const bus = new EventBus();
    const received: JsonRpcEvent[] = [];
    bus.subscribe((e) => received.push(e));
    bus.subscribe((e) => received.push(e));
    bus.emit({ event: "x", payload: 1 });
    assert.equal(received.length, 2);
  });

  it("unsubscribe stops further delivery", () => {
    const bus = new EventBus();
    const received: JsonRpcEvent[] = [];
    const un = bus.subscribe((e) => received.push(e));
    un();
    bus.emit({ event: "y", payload: 2 });
    assert.equal(received.length, 0);
  });

  it("isolates a throwing listener from the others", () => {
    const bus = new EventBus();
    let reached = false;
    bus.subscribe(() => {
      throw new Error("boom");
    });
    bus.subscribe(() => {
      reached = true;
    });
    bus.emit({ event: "z", payload: null });
    assert.equal(reached, true);
  });
});
