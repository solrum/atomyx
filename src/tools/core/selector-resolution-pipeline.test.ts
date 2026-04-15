import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { ResolvedElement, Selector } from "../../adapters/device-controller.port.js";
import { SelectorResolutionPipeline } from "./selector-resolution-pipeline.js";

function fakeController(
  hits: Array<{ match: (s: Selector) => boolean; result: ResolvedElement }>,
) {
  const calls: Selector[] = [];
  return {
    calls,
    resolveSelector: async (s: Selector): Promise<ResolvedElement> => {
      calls.push(s);
      for (const hit of hits) {
        if (hit.match(s)) return hit.result;
      }
      return { found: false };
    },
  };
}

test("tries contentDesc before text when agent passes text", async () => {
  const pipeline = new SelectorResolutionPipeline();
  const ctl = fakeController([
    {
      match: (s) => s.contentDesc === "OK",
      result: { found: true, resolvedBy: "contentDesc" },
    },
  ]);
  const { resolved, usedSelector } = await pipeline.resolve({ text: "OK" }, ctl);
  assert.equal(resolved.found, true);
  assert.deepEqual(usedSelector, { contentDesc: "OK" });
  // First attempt should be contentDesc (not text)
  assert.deepEqual(ctl.calls[0], { contentDesc: "OK" });
});

test("tries resourceId first when agent passes it", async () => {
  const pipeline = new SelectorResolutionPipeline();
  const ctl = fakeController([
    { match: (s) => s.resourceId === "login_btn", result: { found: true, resolvedBy: "resourceId" } },
  ]);
  await pipeline.resolve({ resourceId: "login_btn", text: "Login" }, ctl);
  assert.deepEqual(ctl.calls[0], { resourceId: "login_btn" });
});

test("falls through to text if contentDesc does not match", async () => {
  const pipeline = new SelectorResolutionPipeline();
  const ctl = fakeController([
    { match: (s) => s.text === "Hello", result: { found: true, resolvedBy: "text" } },
  ]);
  const { resolved } = await pipeline.resolve({ text: "Hello" }, ctl);
  assert.equal(resolved.found, true);
  // contentDesc tried first, then text
  assert.deepEqual(ctl.calls[0], { contentDesc: "Hello" });
  assert.deepEqual(ctl.calls[1], { text: "Hello" });
});

test("returns found: false when nothing matches", async () => {
  const pipeline = new SelectorResolutionPipeline();
  const ctl = fakeController([]);
  const { resolved } = await pipeline.resolve({ text: "nothing" }, ctl);
  assert.equal(resolved.found, false);
});

test("deduplicates identical attempts", async () => {
  const pipeline = new SelectorResolutionPipeline();
  const ctl = fakeController([]);
  await pipeline.resolve({ contentDesc: "same", text: "same" }, ctl);
  // Only one attempt with contentDesc:"same" should fire, not two
  const contentDescCalls = ctl.calls.filter((s) => s.contentDesc === "same");
  assert.equal(contentDescCalls.length, 1);
});
