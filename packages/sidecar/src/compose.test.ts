import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { composeSidecar } from "./compose.js";
import { MockDriverFactory, StaticProbe } from "./testing/index.js";

interface LineCollector {
  readonly lines: string[];
  waitFor(count: number): Promise<void>;
}

function collect(output: PassThrough): LineCollector {
  const lines: string[] = [];
  let buf = "";
  const waiters: { n: number; resolve: () => void }[] = [];
  output.on("data", (chunk: Buffer) => {
    buf += chunk.toString("utf8");
    let idx: number;
    while ((idx = buf.indexOf("\n")) >= 0) {
      lines.push(buf.slice(0, idx));
      buf = buf.slice(idx + 1);
    }
    for (const w of [...waiters]) {
      if (lines.length >= w.n) {
        waiters.splice(waiters.indexOf(w), 1);
        w.resolve();
      }
    }
  });
  return {
    lines,
    waitFor(n) {
      if (lines.length >= n) return Promise.resolve();
      return new Promise<void>((resolve) => waiters.push({ n, resolve }));
    },
  };
}

describe("composeSidecar end-to-end", () => {
  it("responds to ping", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const collector = collect(output);
    const handle = composeSidecar({
      input,
      output,
      device: {
        probe: new StaticProbe([[]]),
        factory: new MockDriverFactory() as never,
      },
    });
    handle.start();

    input.write(`${JSON.stringify({ id: "1", method: "ping" })}\n`);
    await collector.waitFor(1);
    assert.deepEqual(JSON.parse(collector.lines[0]!), {
      id: "1",
      result: { ok: true },
    });
    await handle.dispose();
  });

  it("lists devices, selects one, and surfaces deviceConnected event", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const collector = collect(output);
    const handle = composeSidecar({
      input,
      output,
      device: {
        probe: new StaticProbe([
          [
            {
              id: "emulator-5554",
              platform: "android",
              name: "Pixel 7",
              kind: "emulator",
              state: "online",
            },
          ],
        ]),
        factory: new MockDriverFactory() as never,
      },
    });
    handle.start();

    input.write(`${JSON.stringify({ id: "1", method: "listDevices" })}\n`);
    await collector.waitFor(2); // event + response, order may vary

    const asResponses = collector.lines
      .map((l) => JSON.parse(l))
      .sort((a, b) => (a.event ? -1 : 1));
    const event = asResponses.find((m) => m.event === "deviceConnected");
    const response = asResponses.find((m) => m.id === "1");
    assert.ok(event, "deviceConnected event not emitted");
    assert.ok(response?.result, "listDevices response missing");
    assert.equal((response.result as unknown[]).length, 1);

    input.write(
      `${JSON.stringify({
        id: "2",
        method: "selectDevice",
        params: { id: "emulator-5554" },
      })}\n`,
    );
    await collector.waitFor(3);
    const sel = collector.lines
      .map((l) => JSON.parse(l))
      .find((m) => m.id === "2");
    assert.ok(sel?.result, "selectDevice failed");
    assert.equal(handle.session.requireDevice().id, "emulator-5554");

    await handle.dispose();
  });

  it("rejects unknown method with MethodNotFound", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const collector = collect(output);
    const handle = composeSidecar({
      input,
      output,
      device: {
        probe: new StaticProbe([[]]),
        factory: new MockDriverFactory() as never,
      },
    });
    handle.start();

    input.write(`${JSON.stringify({ id: "1", method: "nope" })}\n`);
    await collector.waitFor(1);
    const msg = JSON.parse(collector.lines[0]!);
    assert.equal(msg.error.code, "MethodNotFound");
    await handle.dispose();
  });
});
