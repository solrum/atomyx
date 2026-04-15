import { test } from "node:test";
import { strict as assert } from "node:assert";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  CompositeStorage,
  EngineHttpStorage,
  LocalFileStorage,
  resolveTestCaseStorage,
  type TestCaseRecord,
} from "./test-case-storage.ts";

function sampleRecord(overrides: Partial<TestCaseRecord> = {}): TestCaseRecord {
  return {
    title: "Login flow",
    deviceId: "test-device",
    platform: "android",
    actions: [{ type: "tap", args: { selector: { contentDesc: "Login" } }, timestamp: 1 }],
    savedAt: 1700000000000,
    ...overrides,
  };
}

test("LocalFileStorage — writes JSON with slugified id", async () => {
  const dir = mkdtempSync(join(tmpdir(), "adet-storage-test-"));
  try {
    const storage = new LocalFileStorage(dir);
    const result = await storage.save(sampleRecord());

    assert.equal(result.targets.length, 1);
    assert.equal(result.targets[0].name, "local");
    assert.equal(result.targets[0].ok, true);

    const files = readdirSync(dir);
    assert.equal(files.length, 1);
    assert.match(files[0], /^tc_1700000000000_login-flow\.json$/);

    const content = JSON.parse(readFileSync(join(dir, files[0]), "utf8"));
    assert.equal(content.title, "Login flow");
    assert.equal(content.deviceId, "test-device");
  } finally {
    rmSync(dir, { recursive: true });
  }
});

test("EngineHttpStorage — fails fast without projectId/suiteId", async () => {
  const storage = new EngineHttpStorage("http://example.test");
  const result = await storage.save(sampleRecord());
  assert.equal(result.targets[0].ok, false);
  assert.match(result.targets[0].error ?? "", /requires projectId/);
});

test("CompositeStorage — combines results from all storages", async () => {
  const dir = mkdtempSync(join(tmpdir(), "adet-composite-"));
  try {
    const local = new LocalFileStorage(dir);
    const broken = new EngineHttpStorage("http://example.test");
    const composite = new CompositeStorage([local, broken]);
    const result = await composite.save(sampleRecord());
    assert.equal(result.targets.length, 2);
    assert.equal(result.targets[0].ok, true);   // local succeeds
    assert.equal(result.targets[1].ok, false);  // engine fails (no projectId)
  } finally {
    rmSync(dir, { recursive: true });
  }
});

test("resolveTestCaseStorage — defaults to local when no env", () => {
  const storage = resolveTestCaseStorage({});
  assert.equal(storage.name, "local");
});

test("resolveTestCaseStorage — composite when ADET_ENGINE_URL set", () => {
  const storage = resolveTestCaseStorage({ ADET_ENGINE_URL: "http://x" });
  assert.equal(storage.name, "composite");
});

test("resolveTestCaseStorage — engine-only mode", () => {
  const storage = resolveTestCaseStorage({
    ADET_STORAGE_MODE: "engine",
    ADET_ENGINE_URL: "http://x",
  });
  assert.equal(storage.name, "engine");
});
