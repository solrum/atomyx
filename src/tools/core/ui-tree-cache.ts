import { createHash } from "node:crypto";
import type { CompactElement, DeviceController } from "../../adapters/device-controller.port.js";

/**
 * Short-lived cache for the compact UI tree. Two purposes:
 *
 *   1. Dedupe `get_ui_tree` / `find_element` calls within a few seconds
 *      so agents can query the tree multiple times without hitting the
 *      device every time.
 *   2. Force a consistent view of "the current screen" across multiple
 *      strategy calls in a single tool execute() — the tool sees the
 *      same dump even if the tree changes under it.
 *
 * Cache is invalidated by mutating tools via `ctx.invalidateUiCache()`
 * (wired in `src/server.ts` dispatcher). A manual `invalidate()` method
 * is also exposed for explicit invalidation from tool handlers.
 */
export interface CachedDump {
  elements: CompactElement[];
  fingerprint: string;
  at: number;
  ageMs: number;
  cached: boolean;
}

export class UiTreeCache {
  private lastDump: { fingerprint: string; at: number; elements: CompactElement[] } | null = null;

  constructor(private readonly freshMs = 2000) {}

  /**
   * Return a fresh dump — either from the cache if it's still within
   * `freshMs`, or by calling the controller. Mark the result with
   * `cached: true` if it came from cache so callers can tell.
   */
  async ensureDump(
    controller: Pick<DeviceController, "getUiSummary">,
  ): Promise<CachedDump> {
    const now = Date.now();
    if (this.lastDump && now - this.lastDump.at < this.freshMs) {
      return {
        elements: this.lastDump.elements,
        fingerprint: this.lastDump.fingerprint,
        at: this.lastDump.at,
        ageMs: now - this.lastDump.at,
        cached: true,
      };
    }
    const elements = await controller.getUiSummary();
    const fingerprint = this.fingerprintOf(elements);
    this.lastDump = { fingerprint, at: now, elements };
    return { elements, fingerprint, at: now, ageMs: 0, cached: false };
  }

  /**
   * Peek at the last dump without fetching. Used to detect "no action has
   * happened since last dump" for the get_ui_tree dedupe block.
   */
  peek(): { fingerprint: string; at: number; elements: CompactElement[] } | null {
    return this.lastDump;
  }

  /**
   * Invalidate the cache. Called by the server dispatcher after any
   * mutating tool runs.
   */
  invalidate(): void {
    this.lastDump = null;
  }

  /**
   * Compute a stable sha1 fingerprint from the compact elements — used
   * to detect "is this the same screen as last time". Two dumps with
   * the same fingerprint mean the tree didn't change.
   */
  fingerprintOf(elements: CompactElement[]): string {
    const h = createHash("sha1");
    const sorted = [...elements].sort((a, b) => {
      const at = a.bounds?.top ?? 0;
      const bt = b.bounds?.top ?? 0;
      if (at !== bt) return at - bt;
      return (a.bounds?.left ?? 0) - (b.bounds?.left ?? 0);
    });
    for (const el of sorted) {
      h.update(el.selector?.resourceId ?? "");
      h.update("|");
      h.update(el.selector?.contentDesc ?? "");
      h.update("|");
      h.update(el.selector?.text ?? "");
      h.update("|");
      h.update(el.role ?? "");
      h.update("|");
      h.update(el.label ?? "");
      h.update("|");
      h.update(String(el.bounds?.left ?? 0));
      h.update(",");
      h.update(String(el.bounds?.top ?? 0));
      h.update(",");
      h.update(String(el.bounds?.right ?? 0));
      h.update(",");
      h.update(String(el.bounds?.bottom ?? 0));
      h.update(";");
    }
    return h.digest("hex").slice(0, 12);
  }
}
