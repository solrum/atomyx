import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { Dispatcher } from "./dispatcher.js";
import { StdioTransport } from "./stdio-transport.js";

function collectLines(stream: PassThrough): string[] {
  const out: string[] = [];
  let buf = "";
  stream.on("data", (chunk: Buffer) => {
    buf += chunk.toString("utf8");
    let idx: number;
    while ((idx = buf.indexOf("\n")) >= 0) {
      out.push(buf.slice(0, idx));
      buf = buf.slice(idx + 1);
    }
  });
  return out;
}

describe("StdioTransport", () => {
  it("dispatches a single request and writes a response line", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const dispatcher = new Dispatcher();
    dispatcher.register("ping", () => "pong");
    const t = new StdioTransport({ input, output, dispatcher });
    t.start();

    const lines = collectLines(output);
    input.write(`${JSON.stringify({ id: "1", method: "ping" })}\n`);
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.equal(lines.length, 1);
    assert.deepEqual(JSON.parse(lines[0]!), { id: "1", result: "pong" });
  });

  it("writes a ParseError when a line is not valid JSON", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const t = new StdioTransport({ input, output, dispatcher: new Dispatcher() });
    t.start();
    const lines = collectLines(output);

    input.write("not json\n");
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.equal(lines.length, 1);
    const parsed = JSON.parse(lines[0]!);
    assert.equal(parsed.error.code, "ParseError");
  });

  it("emits events on the output stream", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const t = new StdioTransport({ input, output, dispatcher: new Dispatcher() });
    t.start();
    const lines = collectLines(output);

    t.emit({ event: "deviceConnected", payload: { id: "emulator-5554" } });
    await new Promise((r) => setImmediate(r));

    assert.equal(lines.length, 1);
    assert.deepEqual(JSON.parse(lines[0]!), {
      event: "deviceConnected",
      payload: { id: "emulator-5554" },
    });
  });
});
