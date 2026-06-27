import type { TreeNode } from "@atomyx/driver";
import type { Session } from "../../infra/session/session.js";

export interface InspectionServiceDeps {
  readonly session: Session;
  /**
   * UI tree cache TTL. Defaults to 2s (matches the public contract
   * in docs/tools.md). Zero disables caching — useful for tests.
   */
  readonly cacheTtlMs?: number;
}

/**
 * Read-only operations that introspect the device surface.
 *
 * UI tree captures are cached for a short TTL to avoid thrashing
 * the device on the Inspector's tight polling. Mutating actions
 * (handled by other services: AppService.launch, ScriptRunner
 * stepping) should call `invalidate()` to drop stale snapshots.
 */
export class InspectionService {
  private readonly session: Session;
  private readonly cacheTtlMs: number;
  private cached: { readonly tree: TreeNode; readonly capturedAt: number } | null = null;

  constructor(deps: InspectionServiceDeps) {
    this.session = deps.session;
    this.cacheTtlMs = deps.cacheTtlMs ?? 2000;
  }

  async getUiTree(opts: { readonly fresh?: boolean } = {}): Promise<TreeNode> {
    const now = Date.now();
    if (
      !opts.fresh &&
      this.cached &&
      now - this.cached.capturedAt < this.cacheTtlMs
    ) {
      return this.cached.tree;
    }
    const driver = this.session.requireDevice().driver;
    const tree = await driver.hierarchy();
    this.cached = { tree, capturedAt: now };
    return tree;
  }

  async screenshot(): Promise<Uint8Array> {
    const driver = this.session.requireDevice().driver;
    return driver.screenshot();
  }

  invalidate(): void {
    this.cached = null;
  }
}
