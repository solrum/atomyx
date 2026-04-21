import { promises as fs } from "node:fs";
import path from "node:path";
import os from "node:os";
import type { Storage } from "./storage.port.js";

/**
 * Default filesystem Storage implementation. Writes each record
 * as a pretty-printed JSON file under a configurable root.
 *
 * Key → path mapping:
 *
 *   "bugs/123"         → <root>/bugs/123.json
 *   "runs/abc"         → <root>/runs/abc.json
 *   "case-studies/x"   → <root>/case-studies/x.md   (string values → .md)
 *
 * Rationale: case-study records are markdown text, not JSON —
 * persisting them as `.md` files lets humans read them directly
 * with `cat` / `less`, and agents read them back verbatim. The
 * heuristic is: if the value passed to `save` is a plain string,
 * write as `.md`; otherwise JSON. `load` tries both extensions.
 *
 * Root defaults to `~/.atomyx` (home-scoped). Override via the
 * `root` constructor option or the `ATOMYX_STORAGE_DIR` env var.
 */
export class FileStorage implements Storage {
  private readonly root: string;

  constructor(opts: { root?: string } = {}) {
    this.root =
      opts.root ??
      process.env.ATOMYX_STORAGE_DIR ??
      path.join(os.homedir(), ".atomyx");
  }

  async save(key: string, data: unknown): Promise<void> {
    const isString = typeof data === "string";
    const file = this.pathFor(key, isString ? "md" : "json");
    await fs.mkdir(path.dirname(file), { recursive: true });
    const body = isString ? (data as string) : JSON.stringify(data, null, 2);
    await fs.writeFile(file, body, "utf8");
  }

  async load<T = unknown>(key: string): Promise<T | null> {
    const jsonPath = this.pathFor(key, "json");
    const mdPath = this.pathFor(key, "md");
    for (const p of [jsonPath, mdPath]) {
      try {
        const body = await fs.readFile(p, "utf8");
        if (p.endsWith(".md")) return body as unknown as T;
        return JSON.parse(body) as T;
      } catch (err) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
      }
    }
    return null;
  }

  async list(prefix?: string): Promise<string[]> {
    const base = prefix ? path.join(this.root, prefix) : this.root;
    try {
      const entries = await this.walk(base);
      return entries
        .map((f) => path.relative(this.root, f))
        .map((f) => f.replace(/\.(json|md)$/, ""))
        .sort();
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") return [];
      throw err;
    }
  }

  async delete(key: string): Promise<void> {
    for (const ext of ["json", "md"] as const) {
      try {
        await fs.unlink(this.pathFor(key, ext));
      } catch (err) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
      }
    }
  }

  private pathFor(key: string, ext: "json" | "md"): string {
    const safe = key.replace(/\.\./g, "_"); // crude path-traversal guard
    return path.join(this.root, `${safe}.${ext}`);
  }

  private async walk(dir: string): Promise<string[]> {
    const result: string[] = [];
    const entries = await fs.readdir(dir, { withFileTypes: true });
    for (const e of entries) {
      const full = path.join(dir, e.name);
      if (e.isDirectory()) {
        result.push(...(await this.walk(full)));
      } else if (e.isFile() && /\.(json|md)$/.test(e.name)) {
        result.push(full);
      }
    }
    return result;
  }
}

/**
 * In-memory Storage — for tests and ephemeral sessions.
 */
export class InMemoryStorage implements Storage {
  private readonly store = new Map<string, unknown>();

  async save(key: string, data: unknown): Promise<void> {
    this.store.set(key, data);
  }

  async load<T = unknown>(key: string): Promise<T | null> {
    return (this.store.get(key) as T) ?? null;
  }

  async list(prefix?: string): Promise<string[]> {
    const keys = Array.from(this.store.keys());
    return (prefix ? keys.filter((k) => k.startsWith(prefix)) : keys).sort();
  }

  async delete(key: string): Promise<void> {
    this.store.delete(key);
  }
}
