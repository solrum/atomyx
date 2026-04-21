import { describe, it } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { HttpClient, HttpClientError } from "./http-client.js";

/**
 * HttpClient tests run against a local fake HTTP server so every
 * transport path is exercised against real sockets — mirrors the
 * coverage `tcp-client.test.ts` provides for the iOS driver's
 * TCP transport.
 *
 * These existed as a gap for a long time: the `AndroidDriver`
 * tests only hit the happy path through this client, so 5xx /
 * timeout / connection-refused / JSON-vs-text content-type / 204
 * No Content responses had zero coverage. A regression that
 * broke error wrapping would have shipped silently.
 */

type Handler = (req: http.IncomingMessage, res: http.ServerResponse) => void;

async function startFakeServer(
  handler: Handler,
): Promise<{ port: number; close: () => Promise<void> }> {
  const server = http.createServer((req, res) => handler(req, res));
  await new Promise<void>((resolve) =>
    server.listen(0, "127.0.0.1", () => resolve()),
  );
  const port = (server.address() as AddressInfo).port;
  return {
    port,
    close: () => new Promise((r) => server.close(() => r())),
  };
}

function baseUrlFor(port: number): string {
  return `http://127.0.0.1:${port}`;
}

describe("HttpClient happy path", () => {
  it("GET returns parsed JSON when content-type is application/json", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ ok: true, value: 42 }));
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      const result = await client.get<{ ok: boolean; value: number }>("/data");
      assert.equal(result.ok, true);
      assert.equal(result.value, 42);
    } finally {
      await close();
    }
  });

  it("POST serializes the body as JSON and parses the response", async () => {
    const { port, close } = await startFakeServer((req, res) => {
      let body = "";
      req.on("data", (chunk: Buffer) => (body += chunk.toString()));
      req.on("end", () => {
        const parsed = JSON.parse(body);
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ echo: parsed }));
      });
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      const result = await client.post<{ echo: { x: number; y: number } }>(
        "/actions/tap",
        { x: 10, y: 20 },
      );
      assert.deepEqual(result.echo, { x: 10, y: 20 });
    } finally {
      await close();
    }
  });

  it("GET returns raw text when content-type is not JSON", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      res.setHeader("content-type", "text/plain");
      res.end("plain body");
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      const result = await client.get<string>("/plain");
      assert.equal(result, "plain body");
    } finally {
      await close();
    }
  });

  it("204 No Content returns undefined", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      res.statusCode = 204;
      res.end();
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      const result = await client.post<undefined>("/actions/ack", {});
      assert.equal(result, undefined);
    } finally {
      await close();
    }
  });

  it("auto-prepends leading slash when path omits it", async () => {
    const seen: string[] = [];
    const { port, close } = await startFakeServer((req, res) => {
      seen.push(req.url ?? "");
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ ok: true }));
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      await client.get("tree"); // no leading slash
      assert.equal(seen[0], "/tree");
    } finally {
      await close();
    }
  });
});

describe("HttpClient error envelope", () => {
  it("wraps 5xx responses as HttpClientError with status + body", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      res.statusCode = 503;
      res.statusMessage = "Service Unavailable";
      res.setHeader("content-type", "application/json");
      res.end(
        JSON.stringify({
          error:
            "AccessibilityService not connected — enable Atomyx agent in Settings",
        }),
      );
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      let caught: unknown;
      try {
        await client.get("/tree");
      } catch (err) {
        caught = err;
      }
      assert.ok(caught instanceof HttpClientError);
      const e = caught as HttpClientError;
      assert.equal(e.status, 503);
      assert.match(e.message, /GET \/tree/);
      assert.match(e.message, /503/);
      assert.match(e.body ?? "", /AccessibilityService not connected/);
    } finally {
      await close();
    }
  });

  it("wraps 4xx responses similarly (schema validation / missing field)", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      res.statusCode = 400;
      res.statusMessage = "Bad Request";
      res.end("missing bundleId");
    });
    try {
      const client = new HttpClient({ baseUrl: baseUrlFor(port) });
      let caught: unknown;
      try {
        await client.post("/actions/launch", {});
      } catch (err) {
        caught = err;
      }
      assert.ok(caught instanceof HttpClientError);
      const e = caught as HttpClientError;
      assert.equal(e.status, 400);
      assert.equal(e.body, "missing bundleId");
    } finally {
      await close();
    }
  });

  it("wraps connection refused (dead port) as HttpClientError", async () => {
    // Bind + immediately close to guarantee nothing is listening.
    const { port, close } = await startFakeServer((_req, res) => res.end());
    await close();

    const client = new HttpClient({ baseUrl: baseUrlFor(port) });
    let caught: unknown;
    try {
      await client.get("/tree");
    } catch (err) {
      caught = err;
    }
    assert.ok(caught instanceof HttpClientError);
    const e = caught as HttpClientError;
    assert.match(e.message, /GET \/tree failed/);
    // No status because the request never reached a responder.
    assert.equal(e.status, undefined);
  });

  it("wraps slow-response timeouts with a dedicated timeout message", async () => {
    // Server intentionally holds the response open past the
    // client's timeout budget.
    const { port, close } = await startFakeServer((_req, res) => {
      setTimeout(() => res.end(JSON.stringify({ ok: true })), 2_000);
    });
    try {
      const client = new HttpClient({
        baseUrl: baseUrlFor(port),
        defaultTimeoutMs: 100,
      });
      let caught: unknown;
      try {
        await client.get("/slow");
      } catch (err) {
        caught = err;
      }
      assert.ok(caught instanceof HttpClientError);
      const e = caught as HttpClientError;
      assert.match(e.message, /timed out after 100ms/);
      assert.match(e.message, /GET \/slow/);
      assert.equal(e.status, undefined);
    } finally {
      await close();
    }
  });

  it("honors per-call timeout override", async () => {
    const { port, close } = await startFakeServer((_req, res) => {
      setTimeout(() => res.end(JSON.stringify({ ok: true })), 2_000);
    });
    try {
      const client = new HttpClient({
        baseUrl: baseUrlFor(port),
        defaultTimeoutMs: 10_000, // generous default
      });
      let caught: unknown;
      try {
        await client.get("/slow", 50); // override: 50ms
      } catch (err) {
        caught = err;
      }
      assert.ok(caught instanceof HttpClientError);
      assert.match((caught as HttpClientError).message, /timed out after 50ms/);
    } finally {
      await close();
    }
  });
});
