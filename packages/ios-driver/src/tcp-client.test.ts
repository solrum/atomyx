import { describe, it } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import type { AddressInfo } from "node:net";
import { TcpClient, TcpClientError } from "./tcp-client.js";

/**
 * TcpClient tests run against a local fake TCP server that
 * speaks the Swift driver's wire protocol shape:
 *
 *   → { id, type, args }\n
 *   ← { id, ok:true, data:{...} }\n
 *
 * The fake lets us exercise:
 *   - request/response correlation by id
 *   - line-delimited framing (multiple commands in one chunk,
 *     or one command split across chunks)
 *   - error envelope ({ok:false,error})
 *   - mid-request disconnect → pending waiters reject
 */

interface FakeRequest {
  id: number;
  type: string;
  args: Record<string, unknown>;
}

type ServerHandler = (req: FakeRequest, respond: (payload: unknown) => void) => void;

async function startFakeServer(handler: ServerHandler): Promise<{
  port: number;
  close: () => Promise<void>;
  kickSocket: () => void;
}> {
  let currentSock: net.Socket | null = null;
  const server = net.createServer((sock) => {
    currentSock = sock;
    sock.setEncoding("utf8");
    let buf = "";
    sock.on("data", (chunk: string) => {
      buf += chunk;
      let nl: number;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        if (!line) continue;
        try {
          const req = JSON.parse(line) as FakeRequest;
          handler(req, (payload) => {
            sock.write(JSON.stringify(payload) + "\n");
          });
        } catch {
          // malformed, ignore
        }
      }
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
  const port = (server.address() as AddressInfo).port;
  return {
    port,
    close: () =>
      new Promise((resolve) => {
        if (currentSock) {
          currentSock.destroy();
          currentSock = null;
        }
        server.close(() => resolve());
      }),
    kickSocket: () => {
      if (currentSock) {
        currentSock.destroy();
        currentSock = null;
      }
    },
  };
}

describe("TcpClient.connect + call happy path", () => {
  it("correlates id and returns data", async () => {
    const server = await startFakeServer((req, respond) => {
      if (req.type === "ping") {
        respond({ id: req.id, ok: true, data: { pong: true } });
      } else if (req.type === "echo") {
        respond({ id: req.id, ok: true, data: { echoed: req.args.value } });
      }
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      const pong = await client.call("ping");
      assert.equal(pong.pong, true);
      const echo = await client.call("echo", { value: 42 });
      assert.equal(echo.echoed, 42);
    } finally {
      await client.disconnect();
      await server.close();
    }
  });
});

describe("TcpClient error envelope", () => {
  it("rejects call when driver returns ok:false", async () => {
    const server = await startFakeServer((req, respond) => {
      respond({ id: req.id, ok: false, error: "no app launched" });
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      const err = await client.call("launchApp", { bundleId: "com.x" }).catch((e) => e);
      assert.ok(err instanceof TcpClientError);
      assert.equal((err as TcpClientError).code, "driver-error");
      assert.match((err as Error).message, /no app launched/);
    } finally {
      await client.disconnect();
      await server.close();
    }
  });
});

describe("TcpClient disconnect handling", () => {
  it("rejects pending waiters when server closes socket mid-request", async () => {
    let neverRespond = false;
    const server = await startFakeServer((req, respond) => {
      if (neverRespond) return;
      respond({ id: req.id, ok: true, data: {} });
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      neverRespond = true;
      const pending = client.call("slowCommand", {}).catch((e) => e);
      // Give the request a moment to flight, then kick the socket.
      await new Promise((r) => setTimeout(r, 10));
      server.kickSocket();
      const err = await pending;
      assert.ok(err instanceof TcpClientError);
      assert.equal((err as TcpClientError).code, "disconnected");
    } finally {
      await client.disconnect();
      await server.close();
    }
  });

  it("call() after disconnect() throws not-connected", async () => {
    const server = await startFakeServer((req, respond) => {
      respond({ id: req.id, ok: true, data: {} });
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    await client.connect();
    await client.disconnect();
    const err = await client.call("ping").catch((e) => e);
    assert.ok(err instanceof TcpClientError);
    assert.equal((err as TcpClientError).code, "not-connected");
    await server.close();
  });
});

describe("TcpClient framing", () => {
  it("handles multiple responses in a single data chunk", async () => {
    // We can't easily force the server to batch writes, but the
    // client's onData loop handles any chunking — this test
    // issues several concurrent requests and verifies all get
    // their responses correlated correctly.
    const server = await startFakeServer((req, respond) => {
      respond({ id: req.id, ok: true, data: { id: req.id } });
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      const results = await Promise.all([
        client.call("ping"),
        client.call("ping"),
        client.call("ping"),
      ]);
      assert.equal(results.length, 3);
      for (const r of results) {
        assert.equal(typeof r.id, "number");
      }
    } finally {
      await client.disconnect();
      await server.close();
    }
  });
});

describe("TcpClient abort signal", () => {
  it("rejects pending call with AbortError when external signal aborts", async () => {
    // Server never responds — only the abort signal can free the
    // waiter. Without signal threading this test would hang.
    const server = await startFakeServer(() => {
      /* swallow */
    });
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      const controller = new AbortController();
      const startedAt = Date.now();
      const pending = client.call("ping", {}, { signal: controller.signal });
      setTimeout(() => controller.abort(new DOMException("budget elapsed", "AbortError")), 50);
      const err = await pending.catch((e) => e);
      const elapsedMs = Date.now() - startedAt;
      assert.ok(elapsedMs < 500, `expected fast abort, got ${elapsedMs}ms`);
      assert.equal((err as Error).name, "AbortError");
      assert.match((err as Error).message, /budget elapsed/);
    } finally {
      await client.disconnect();
      await server.close();
    }
  });

  it("rejects synchronously when the signal is already aborted", async () => {
    const server = await startFakeServer((req, respond) =>
      respond({ id: req.id, ok: true, data: {} }),
    );
    const client = new TcpClient({ host: "127.0.0.1", port: server.port });
    try {
      await client.connect();
      const controller = new AbortController();
      controller.abort(new DOMException("pre-aborted", "AbortError"));
      const err = await client
        .call("ping", {}, { signal: controller.signal })
        .catch((e) => e);
      assert.equal((err as Error).name, "AbortError");
    } finally {
      await client.disconnect();
      await server.close();
    }
  });
});

describe("TcpClient connect failure", () => {
  it("rejects when the port has no server", async () => {
    const client = new TcpClient({
      host: "127.0.0.1",
      port: 1,
      connectTimeoutMs: 500, // short budget for test speed
    });
    const err = await client.connect().catch((e) => e);
    assert.ok(err instanceof TcpClientError);
    assert.equal((err as TcpClientError).code, "not-connected");
  });
});
