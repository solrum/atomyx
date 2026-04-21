import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { writeFileSync, unlinkSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { NullCapture } from "./null-capture.js";
import { FileCapture } from "./file-capture.js";
import { createCapture, registerCaptureAdapter } from "./capture-factory.js";
import type { NetworkCapture, CapturedRequest } from "@atomyx/shared/script";

const TMP = join(tmpdir(), "atomyx-test-capture");

function writeCaptureFile(
  filename: string,
  entries: CapturedRequest[],
): string {
  mkdirSync(TMP, { recursive: true });
  const path = join(TMP, filename);
  const content = entries.map((e) => JSON.stringify(e)).join("\n") + "\n";
  writeFileSync(path, content, "utf-8");
  return path;
}

describe("NullCapture", () => {
  it("waitForRequest throws with guidance", async () => {
    const nc = new NullCapture();
    await assert.rejects(
      () => nc.waitForRequest("POST /api/test"),
      /capture adapter/,
    );
  });

  it("getAll returns empty", () => {
    const nc = new NullCapture();
    assert.deepStrictEqual(nc.getAll(), []);
  });
});

describe("FileCapture", () => {
  it("reads existing entries from file", async () => {
    const path = writeCaptureFile("existing.jsonl", [
      {
        method: "POST",
        url: "https://api.test.com/transfer",
        status: 200,
        headers: {},
        body: { ok: true },
        timestamp: 1000,
      },
    ]);
    const fc = new FileCapture(path);
    await fc.start();
    const all = fc.getAll();
    assert.equal(all.length, 1);
    assert.equal(all[0]!.method, "POST");
    assert.equal(all[0]!.status, 200);
    await fc.stop();
    unlinkSync(path);
  });

  it("waitForRequest matches method + path", async () => {
    const path = writeCaptureFile("match.jsonl", [
      {
        method: "GET",
        url: "https://api.test.com/users",
        status: 200,
        headers: {},
        body: [],
        timestamp: 1000,
      },
      {
        method: "POST",
        url: "https://api.test.com/transfer",
        status: 201,
        headers: {},
        body: { id: "tx-123" },
        timestamp: 2000,
      },
    ]);
    const fc = new FileCapture(path);
    await fc.start();
    const result = await fc.waitForRequest("POST /transfer", 1000);
    assert.equal(result.status, 201);
    assert.deepStrictEqual(result.body, { id: "tx-123" });
    await fc.stop();
    unlinkSync(path);
  });

  it("waitForRequest times out when no match", async () => {
    const path = writeCaptureFile("nomatch.jsonl", [
      {
        method: "GET",
        url: "https://api.test.com/health",
        status: 200,
        headers: {},
        body: null,
        timestamp: 1000,
      },
    ]);
    const fc = new FileCapture(path);
    await fc.start();
    await assert.rejects(
      () => fc.waitForRequest("POST /transfer", 300),
      /Timed out/,
    );
    await fc.stop();
    unlinkSync(path);
  });
});

describe("createCapture factory", () => {
  it('type "none" returns NullCapture', () => {
    const nc = createCapture({ type: "none" });
    assert.ok(nc instanceof NullCapture);
  });

  it("undefined config returns NullCapture", () => {
    const nc = createCapture();
    assert.ok(nc instanceof NullCapture);
  });

  it('type "file" requires path', () => {
    assert.throws(() => createCapture({ type: "file" }), /path/);
  });

  it('type "file" creates FileCapture', () => {
    const fc = createCapture({ type: "file", path: "/tmp/test.jsonl" });
    assert.ok(fc instanceof FileCapture);
  });

  it("unknown type throws with available list", () => {
    assert.throws(
      () => createCapture({ type: "unknown-proxy" }),
      /Available/,
    );
  });

  it("registerCaptureAdapter adds custom adapter", () => {
    const custom: NetworkCapture = {
      start: async () => {},
      stop: async () => {},
      waitForRequest: async () => ({
        method: "GET",
        url: "",
        status: 200,
        headers: {},
        body: null,
        timestamp: 0,
      }),
      getAll: () => [],
    };
    registerCaptureAdapter("custom-test", () => custom);
    const result = createCapture({ type: "custom-test" });
    assert.strictEqual(result, custom);
  });
});
