import { describe, it } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import type { AddressInfo } from "node:net";
import { canConnect, probeDriverPing } from "./xctest-launcher.js";

/**
 * Tests for the TCP probe utilities exported by xctest-launcher.
 * XctestLauncher itself requires xcodebuild and is tested via
 * integration smoke scripts — only the hermetic probe functions
 * are unit-tested here.
 */

describe("canConnect", () => {
  it("returns true when a listener is present", async () => {
    const server = net.createServer();
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    const port = (server.address() as AddressInfo).port;
    try {
      assert.equal(await canConnect(port, 1000), true);
    } finally {
      await new Promise<void>((r) => server.close(() => r()));
    }
  });

  it("returns false when no listener", async () => {
    // Port 1 is almost certainly not listening.
    assert.equal(await canConnect(1, 300), false);
  });
});

describe("probeDriverPing", () => {
  it("returns true when server responds with pong", async () => {
    const server = net.createServer((sock) => {
      sock.setEncoding("utf8");
      sock.on("data", (chunk: string) => {
        const req = JSON.parse(chunk.trim());
        if (req.type === "ping") {
          sock.write(
            JSON.stringify({ id: req.id, ok: true, data: { pong: true } }) + "\n",
          );
        }
      });
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    const port = (server.address() as AddressInfo).port;
    try {
      assert.equal(await probeDriverPing(port), true);
    } finally {
      await new Promise<void>((r) => server.close(() => r()));
    }
  });

  it("returns false when server responds with non-pong", async () => {
    const server = net.createServer((sock) => {
      sock.setEncoding("utf8");
      sock.on("data", () => {
        sock.write(JSON.stringify({ ok: false }) + "\n");
      });
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    const port = (server.address() as AddressInfo).port;
    try {
      assert.equal(await probeDriverPing(port), false);
    } finally {
      await new Promise<void>((r) => server.close(() => r()));
    }
  });

  it("returns false when no listener", async () => {
    assert.equal(await probeDriverPing(1), false);
  });
});
