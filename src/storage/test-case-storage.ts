/**
 * TestCaseStorage — Strategy for persisting recorded test cases.
 *
 * Implementations:
 *   - LocalFileStorage: writes JSON to ~/.atomyx/test-cases/<id>.json (default)
 *   - EngineHttpStorage: POSTs to a synapse engine endpoint
 *   - CompositeStorage: runs multiple storages in sequence (best-effort)
 *
 * Resolution:
 *   resolveTestCaseStorage() inspects env vars and returns the appropriate
 *   storage instance. This keeps Atomyx self-contained for open source while
 *   allowing synapse-internal users to push to their engine.
 *
 *   ATOMYX_ENGINE_URL  → enables engine push (composite with local)
 *   ATOMYX_STORAGE_DIR → override local persistence directory
 *   ATOMYX_STORAGE_MODE → "local" | "engine" | "composite" (overrides auto-detection)
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export interface TestCaseRecord {
  title: string;
  description?: string;
  projectId?: string;
  suiteId?: string;
  deviceId: string;
  platform: string;
  actions: Array<{ type: string; args: Record<string, unknown>; timestamp: number }>;
  savedAt: number;
}

export interface TestCaseStorageResult {
  targets: Array<{ name: string; ok: boolean; ref: string; error?: string }>;
}

export interface TestCaseStorage {
  readonly name: string;
  save(record: TestCaseRecord): Promise<TestCaseStorageResult>;
}

// ────────────────────────────────────────────────────────────────────
// LocalFileStorage
// ────────────────────────────────────────────────────────────────────

export class LocalFileStorage implements TestCaseStorage {
  readonly name = "local";

  constructor(private readonly baseDir: string = defaultLocalDir()) {}

  async save(record: TestCaseRecord): Promise<TestCaseStorageResult> {
    try {
      mkdirSync(this.baseDir, { recursive: true });
      const id = `tc_${record.savedAt}_${slugify(record.title)}`;
      const path = join(this.baseDir, `${id}.json`);
      writeFileSync(path, JSON.stringify({ id, ...record }, null, 2));
      return { targets: [{ name: this.name, ok: true, ref: path }] };
    } catch (err) {
      return {
        targets: [
          { name: this.name, ok: false, ref: "", error: err instanceof Error ? err.message : String(err) },
        ],
      };
    }
  }
}

// ────────────────────────────────────────────────────────────────────
// EngineHttpStorage
// ────────────────────────────────────────────────────────────────────

export class EngineHttpStorage implements TestCaseStorage {
  readonly name = "engine";

  constructor(private readonly engineUrl: string) {}

  async save(record: TestCaseRecord): Promise<TestCaseStorageResult> {
    if (!record.projectId || !record.suiteId) {
      return {
        targets: [
          {
            name: this.name,
            ok: false,
            ref: "",
            error: "engine storage requires projectId + suiteId",
          },
        ],
      };
    }
    try {
      const url = `${this.engineUrl}/api/test-management/projects/${record.projectId}/cases/from-atomyx-recording`;
      const res = await fetch(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          title: record.title,
          description: record.description,
          suiteId: record.suiteId,
          deviceId: record.deviceId,
          platform: record.platform,
          actions: record.actions,
        }),
      });
      if (!res.ok) {
        const body = await res.text();
        return {
          targets: [{ name: this.name, ok: false, ref: url, error: `${res.status} ${body}` }],
        };
      }
      const created = (await res.json()) as { id: string };
      return { targets: [{ name: this.name, ok: true, ref: created.id }] };
    } catch (err) {
      return {
        targets: [
          {
            name: this.name,
            ok: false,
            ref: this.engineUrl,
            error: err instanceof Error ? err.message : String(err),
          },
        ],
      };
    }
  }
}

// ────────────────────────────────────────────────────────────────────
// CompositeStorage — best-effort across multiple
// ────────────────────────────────────────────────────────────────────

export class CompositeStorage implements TestCaseStorage {
  readonly name = "composite";

  constructor(private readonly storages: TestCaseStorage[]) {}

  async save(record: TestCaseRecord): Promise<TestCaseStorageResult> {
    const targets: TestCaseStorageResult["targets"] = [];
    for (const s of this.storages) {
      const r = await s.save(record);
      targets.push(...r.targets);
    }
    return { targets };
  }
}

// ────────────────────────────────────────────────────────────────────
// Resolution
// ────────────────────────────────────────────────────────────────────

export function resolveTestCaseStorage(env: NodeJS.ProcessEnv = process.env): TestCaseStorage {
  const mode = env.ATOMYX_STORAGE_MODE;
  const engineUrl = env.ATOMYX_ENGINE_URL;
  const baseDir = env.ATOMYX_STORAGE_DIR;
  const local = new LocalFileStorage(baseDir);

  if (mode === "local" || (!mode && !engineUrl)) return local;
  if (mode === "engine" && engineUrl) return new EngineHttpStorage(engineUrl);
  if ((mode === "composite" || !mode) && engineUrl) {
    return new CompositeStorage([local, new EngineHttpStorage(engineUrl)]);
  }
  return local;
}

function defaultLocalDir(): string {
  return join(homedir(), ".atomyx", "test-cases");
}

function slugify(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "untitled";
}
