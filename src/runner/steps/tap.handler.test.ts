import { test } from "node:test";
import { strict as assert } from "node:assert";
import { MockController } from "../../testing/mock-controller.ts";
import { TapStepHandler } from "./tap.handler.ts";

test("TapStepHandler — passes when controller returns ok", async () => {
  const ctl = new MockController().queueTapResponse({ ok: true, reason: "ok" });
  const handler = new TapStepHandler();
  const step = { tap: { contentDesc: "Login" } };

  const result = await handler.execute(step as any, { controller: ctl, index: 0 });

  assert.equal(result.status, "passed");
  assert.equal(result.kind, "tap");
  assert.equal(ctl.calls[0].method, "tap");
  assert.deepEqual((ctl.calls[0].args as any).contentDesc, "Login");
});

test("TapStepHandler — fails when controller returns ok=false", async () => {
  const ctl = new MockController().queueTapResponse({ ok: false, reason: "not found" });
  const handler = new TapStepHandler();

  const result = await handler.execute(
    { tap: { resourceId: "btn_x" } } as any,
    { controller: ctl, index: 1 },
  );

  assert.equal(result.status, "failed");
  assert.match(result.error ?? "", /not found/);
});

test("TapStepHandler — matches() identifies tap steps", () => {
  const handler = new TapStepHandler();
  assert.equal(handler.matches({ tap: { text: "x" } } as any), true);
  assert.equal(handler.matches({ launch: "com.x" } as any), false);
});
