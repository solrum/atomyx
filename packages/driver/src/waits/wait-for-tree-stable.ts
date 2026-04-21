import type { Clock } from "@atomyx/core/infra";
import type { Driver } from "../driver/driver.port.js";
import type { TreeNode } from "../tree/tree-node.js";
import { waitUntil } from "./wait-until.js";

/**
 * Wait until the UI tree snapshot stops changing for `quietMs`.
 * Alternative to `driver.waitForIdle()` for drivers without a
 * native idle primitive (canWaitForIdle === false).
 *
 * Algorithm:
 *
 *   - Fetch a snapshot, hash it, remember the time.
 *   - On each poll, fetch again. Hash changed? reset the timer.
 *     Hash unchanged for `quietMs`? return the latest tree.
 *
 * The "hash" is a structural string of attributes + bounds; it is
 * NOT a cryptographic hash, just a deep identity signature. Slow
 * animations (progress spinners, shimmer placeholders) keep the
 * tree mutating and will cause this primitive to TIME OUT rather
 * than succeed prematurely — callers who want "any tree will do"
 * can pass a short `quietMs` (e.g. 100ms) to tolerate those.
 */
export interface WaitForTreeStableOptions {
  readonly driver: Driver;
  readonly quietMs: number;
  readonly clock: Clock;
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
}

export async function waitForTreeStable(
  opts: WaitForTreeStableOptions,
): Promise<TreeNode> {
  const timeoutMs = opts.timeoutMs ?? 3000;
  const intervalMs = opts.intervalMs ?? 100;
  let lastHash = "";
  let stableSince = -1;
  const result = await waitUntil<TreeNode>({
    fetch: () => opts.driver.hierarchy(),
    predicate: (tree) => {
      const hash = hashTree(tree);
      const now = opts.clock.now();
      if (hash !== lastHash) {
        lastHash = hash;
        stableSince = now;
        return false;
      }
      return now - stableSince >= opts.quietMs;
    },
    timeoutMs,
    intervalMs,
    clock: opts.clock,
    kind: `waitForTreeStable(quietMs=${opts.quietMs})`,
  });
  return result;
}

function hashTree(node: TreeNode): string {
  const parts: string[] = [];
  visit(node, parts);
  return parts.join("|");
}

function visit(node: TreeNode, out: string[]): void {
  // Attributes sorted so insertion order can't produce different
  // hashes for structurally identical trees.
  const keys = Object.keys(node.attributes).sort();
  out.push("[");
  for (const k of keys) {
    out.push(k, "=", node.attributes[k] ?? "", ";");
  }
  if (node.focused === true) out.push("F;");
  if (node.clickable === true) out.push("C;");
  if (node.enabled === false) out.push("D;");
  for (const child of node.children) {
    visit(child, out);
  }
  out.push("]");
}
