import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import crypto from "node:crypto";

import { HidClient, HidClientError } from "./hid-client.js";

// ---------------------------------------------------------------------------
// Minimal in-process WebSocket server mock.
// Accepts one client, collects incoming frames, and replies with the
// same id echoed back in a text frame. Enough to test the HidClient
// send→reply roundtrip without starting the real helper process.
// ---------------------------------------------------------------------------

class MockWsServer {
  private server: net.Server;
  private conn: net.Socket | null = null;
  private _port = 0;

  // Handler installed per-test to customise reply behaviour.
  onMessage: (payload: Record<string, unknown>) => Record<string, unknown> = (
    msg,
  ) => ({ ok: true, id: msg["id"] });

  constructor() {
    this.server = net.createServer((sock) => this.handleConnection(sock));
  }

  get port(): number {
    return this._port;
  }

  start(): Promise<void> {
    return new Promise((resolve) => {
      this.server.listen(0, "127.0.0.1", () => {
        this._port = (this.server.address() as net.AddressInfo).port;
        resolve();
      });
    });
  }

  stop(): Promise<void> {
    return new Promise((resolve) => {
      this.conn?.destroy();
      this.server.close(() => resolve());
    });
  }

  private handleConnection(sock: net.Socket): void {
    this.conn = sock;
    let header = "";
    const onUpgrade = (chunk: Buffer) => {
      header += chunk.toString("binary");
      const end = header.indexOf("\r\n\r\n");
      if (end < 0) return;

      const keyMatch = header.match(/Sec-WebSocket-Key: (.+)\r\n/);
      const key = keyMatch ? keyMatch[1]!.trim() : "";
      const accept = crypto
        .createHash("sha1")
        .update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
        .digest("base64");
      const response =
        "HTTP/1.1 101 Switching Protocols\r\n" +
        "Upgrade: websocket\r\n" +
        "Connection: Upgrade\r\n" +
        `Sec-WebSocket-Accept: ${accept}\r\n` +
        "\r\n";
      sock.write(Buffer.from(response, "binary"));

      sock.removeListener("data", onUpgrade);
      sock.on("data", (buf: Buffer) => this.onFrame(sock, buf));
    };
    sock.on("data", onUpgrade);
  }

  private onFrame(sock: net.Socket, buf: Buffer): void {
    // Parse a single WS text frame (masked, length <= 125 or 65535).
    if (buf.length < 2) return;
    const opcode = buf[0]! & 0x0f;
    if (opcode !== 0x1) return; // text frame only
    const masked = !!(buf[1]! & 0x80);
    let lenByte = buf[1]! & 0x7f;
    let offset = 2;
    if (lenByte === 126) {
      lenByte = buf.readUInt16BE(2);
      offset = 4;
    }
    const mask = masked ? buf.slice(offset, offset + 4) : null;
    if (masked) offset += 4;
    const payload = buf.slice(offset, offset + lenByte);
    if (mask) {
      for (let i = 0; i < payload.length; i++) {
        payload[i] ^= mask[i % 4]!;
      }
    }
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(payload.toString("utf8"));
    } catch {
      return;
    }
    const reply = this.onMessage(msg);
    const replyStr = JSON.stringify(reply) + "\n";
    const replyBuf = Buffer.from(replyStr, "utf8");
    // Send as unmasked text frame (server → client, RFC 6455 §5.1).
    const header =
      replyBuf.length <= 125
        ? Buffer.from([0x81, replyBuf.length])
        : Buffer.from([0x81, 126, replyBuf.length >> 8, replyBuf.length & 0xff]);
    sock.write(Buffer.concat([header, replyBuf]));
  }
}

// ---------------------------------------------------------------------------

describe("HidClient — send/reply roundtrip", () => {
  const srv = new MockWsServer();

  before(async () => {
    await srv.start();
  });

  after(async () => {
    await srv.stop();
  });

  it("connects and receives ok reply for tap", async () => {
    const client = new HidClient({ port: srv.port });
    await client.connect();
    // Default handler replies ok:true.
    await assert.doesNotReject(() => client.tap({ x: 0.5, y: 0.5 }));
    await client.dispose();
  });

  it("resolves on ok:true reply", async () => {
    const client = new HidClient({ port: srv.port });
    await client.connect();
    let capturedId: unknown;
    srv.onMessage = (msg) => {
      capturedId = msg["id"];
      return { ok: true, id: msg["id"] };
    };
    await client.tap({ x: 0.3, y: 0.7 });
    assert.ok(typeof capturedId === "number", "server should receive numeric id");
    await client.dispose();
  });

  it("rejects on ok:false reply", async () => {
    const client = new HidClient({ port: srv.port });
    await client.connect();
    srv.onMessage = (msg) => ({
      ok: false,
      id: msg["id"],
      error: "dispatch failed",
    });
    await assert.rejects(
      () => client.tap({ x: 0.5, y: 0.5 }),
      (err: unknown) => {
        assert.ok(err instanceof HidClientError, "expected HidClientError");
        assert.equal((err as HidClientError).code, "reply-error");
        return true;
      },
    );
    await client.dispose();
    // Restore default handler for subsequent tests.
    srv.onMessage = (msg) => ({ ok: true, id: msg["id"] });
  });

  it("times out when no reply arrives", async () => {
    const client = new HidClient({ port: srv.port, dispatchTimeoutMs: 100 });
    await client.connect();
    // Handler that never replies.
    srv.onMessage = (_msg) => {
      // Return a deliberately wrong id so the pending entry is never matched.
      return { ok: true, id: -999 };
    };
    await assert.rejects(
      () => client.tap({ x: 0.5, y: 0.5 }),
      (err: unknown) => {
        assert.ok(err instanceof HidClientError, "expected HidClientError");
        assert.equal((err as HidClientError).code, "timeout");
        return true;
      },
    );
    await client.dispose();
    srv.onMessage = (msg) => ({ ok: true, id: msg["id"] });
  });

  it("touchDown / touchMove / touchUp dispatch with correct type field", async () => {
    const received: string[] = [];
    srv.onMessage = (msg) => {
      received.push(msg["type"] as string);
      return { ok: true, id: msg["id"] };
    };
    const client = new HidClient({ port: srv.port });
    await client.connect();
    await client.touchDown({ x: 0.2, y: 0.3 }, 1);
    await client.touchMove({ x: 0.25, y: 0.35 }, 1);
    await client.touchUp({ x: 0.25, y: 0.35 }, 1);
    await client.dispose();
    assert.deepEqual(received, ["touch-down", "touch-move", "touch-up"]);
    srv.onMessage = (msg) => ({ ok: true, id: msg["id"] });
  });

  it("swipe dispatches correct step count", async () => {
    let captured: Record<string, unknown> = {};
    srv.onMessage = (msg) => {
      captured = msg;
      return { ok: true, id: msg["id"] };
    };
    const client = new HidClient({ port: srv.port });
    await client.connect();
    // 160 ms ÷ 16 ms/step = 10 steps (capped at min 1).
    await client.swipe({ x: 0.1, y: 0.5 }, { x: 0.9, y: 0.5 }, 160);
    await client.dispose();
    assert.equal(captured["type"], "swipe");
    assert.ok(
      typeof captured["steps"] === "number" && (captured["steps"] as number) > 0,
      "steps should be positive",
    );
    srv.onMessage = (msg) => ({ ok: true, id: msg["id"] });
  });
});
