import type { UiTreeNode } from "../../../domain/features/runtime/index.js";

export type { UiTreeNode };

/**
 * Stable path from the tree root to a node. Each segment is the
 * child index, dot-separated. Root = `""`. First child of root =
 * `"0"`. Third grandchild under that = `"0.2"`.
 *
 * Stable across refresh as long as tree shape is unchanged —
 * reliable enough for "remember which node was selected" without
 * paying for a global node-id allocation.
 */
export type UiNodePath = string;

export interface UiInspectorSnapshot {
  readonly tree: UiTreeNode | null;
  readonly selectedPath: UiNodePath | null;
  readonly loading: boolean;
  readonly error: string | null;
  /**
   * Device id the current tree was captured for. When the picker
   * switches to another device, callers know whether to trigger a
   * refresh or keep the stale snapshot.
   */
  readonly capturedForDeviceId: string | null;
  /**
   * Wall-clock time of the last successful capture, used by
   * consumers (e.g. the mirror overlay) to fade out the highlight
   * once the snapshot gets old relative to live video.
   */
  readonly capturedAt: number | null;
  /**
   * When true, the tree label appends the raw `class` (the
   * platform's underlying type — e.g. iOS `staticText`,
   * Android `android.widget.TextView`) after the canonical
   * `role`, so a developer auditing why a node is classified a
   * given way can see both layers at once.
   */
  readonly showRaw: boolean;
  /**
   * Auto-refresh polls `refresh(capturedForDeviceId)` while
   * enabled, until the user disables it or `clear()` drops the
   * captured device. Persisted across sessions via
   * `StudioSettings.inspectorAutoRefresh`.
   */
  readonly autoRefreshEnabled: boolean;
  /**
   * Polling period in ms. The store clamps writes to ≥2000 so a
   * misconfigured value does not flood the bridge.
   */
  readonly autoRefreshIntervalMs: number;
  /**
   * True while the inspector is suppressing a tick because the
   * user just touched the mirror — gives the device UI a moment
   * to settle before the next dump.
   */
  readonly autoRefreshPaused: boolean;
}

export interface UiInspectorApi {
  getSnapshot(): UiInspectorSnapshot;
  subscribe(listener: () => void): () => void;
  refresh(deviceId: string): Promise<void>;
  select(path: UiNodePath | null): void;
  clear(): void;
  setShowRaw(value: boolean): void;
  setAutoRefreshEnabled(enabled: boolean): void;
  setAutoRefreshInterval(intervalMs: number): void;
  /**
   * Called by mirror handlers right after a user touch / swipe /
   * long-press so auto-refresh skips ticks for a short window —
   * the captured tree should reflect the post-interaction state,
   * not a frame mid-transition.
   */
  notifyInteraction(): void;
  /**
   * Releases the auto-refresh timer. Wired by the composition
   * root on app shutdown; tests use it to keep node:test from
   * holding the event loop open.
   */
  dispose(): void;
}
